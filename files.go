package main

// File manager: browse and operate on the console's SD card over
// the same two FTP servers as the gallery (OunceShell on 1337,
// the FTPD plugin on 5000), but for the whole card instead of
// only the album. Supports listing, new folder, rename, move,
// server-side copy, recursive delete, upload (streamed) and
// download (streamed). Paths from the client are root-relative
// ("/switch/foo.nro"); the FTP dialect prefix ("" or "/sdmc:")
// is resolved by CWD only, never by MKD probes.

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fmConnect logs in and resolves the root dialect: plain paths
// first (ftpd), then the ftpvita /sdmc: prefix. CWD only.
func fmConnect(ip string, port int) (*galSession, error) {
	ctrl, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		return nil, err
	}
	s := &galSession{ctrl: ctrl}
	ctrl.SetDeadline(time.Now().Add(600 * time.Second))
	s.rd = newFTPReader(ctrl)
	if code, _ := s.rd.readReply(); code != 220 {
		ctrl.Close()
		return nil, errors.New("no greeting")
	}
	code, _ := s.cmd("USER anonymous")
	if code == 331 {
		code, _ = s.cmd("PASS guest@")
	}
	if code != 230 && code != 202 {
		ctrl.Close()
		return nil, errors.New("login refused")
	}
	s.cmd("TYPE I")
	if code, _ := s.cmd("CWD /switch"); code == 250 {
		s.prefix = ""
		return s, nil
	}
	if code, _ := s.cmd("CWD /sdmc:/switch"); code == 250 {
		s.prefix = "/sdmc:"
		return s, nil
	}
	s.prefix = ""
	return s, nil
}

// fmResolve connects through the requested source ("oz",
// "plugin" or "auto" = OunceShell first, then the plugin).
func fmResolve(ip, src string) (*galSession, string, error) {
	switch src {
	case "oz":
		s, err := fmConnect(ip, galOzPort)
		return s, "oz", err
	case "plugin":
		s, err := fmConnect(ip, galPluginPort)
		return s, "plugin", err
	default:
		if s, err := fmConnect(ip, galOzPort); err == nil {
			return s, "oz", nil
		}
		s, err := fmConnect(ip, galPluginPort)
		return s, "plugin", err
	}
}

// fmCleanPath validates a client-supplied root-relative path and
// returns its cleaned form ("/" for the root). Traversal outside
// the root is rejected.
func fmCleanPath(p string) (string, bool) {
	p = strings.ReplaceAll(p, "\\", "/")
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	c := path.Clean("/" + p)
	return c, true
}

// fmValidName validates one path segment (folder/file name).
func fmValidName(n string) bool {
	if n == "" || n == "." || n == ".." || len(n) > 255 {
		return false
	}
	return !strings.ContainsAny(n, "/\\\x00")
}

// fmAbs maps a cleaned root-relative path to an absolute FTP path.
func fmAbs(s *galSession, rel string) string {
	if rel == "/" {
		if s.prefix == "" {
			return "/"
		}
		return s.prefix + "/"
	}
	return s.prefix + rel
}

// fmParent returns the parent of a cleaned path ("/" at top).
func fmParent(p string) string {
	d := path.Dir(p)
	if d == "." {
		return "/"
	}
	return d
}

// fmJoin joins a cleaned directory path and a name.
func fmJoin(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

type fmItem struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// fmList lists one directory, folders first, then by name.
func fmList(s *galSession, rel string) ([]fmItem, error) {
	ents, err := s.list(fmAbs(s, rel))
	if err != nil {
		return nil, err
	}
	items := []fmItem{}
	for _, e := range ents {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		items = append(items, fmItem{Name: e.Name, Dir: e.Dir, Size: e.Size})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Dir != items[j].Dir {
			return items[i].Dir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

// fmDelete removes a file, or a folder with everything inside it
// (depth-capped). Returns the first error hit.
func fmDelete(s *galSession, rel string, isDir bool, depth int) error {
	if !isDir {
		code, _ := s.cmd("DELE " + fmAbs(s, rel))
		if code != 250 {
			return errors.New("delete failed")
		}
		return nil
	}
	if depth > 12 {
		return errors.New("too deep")
	}
	ents, err := s.list(fmAbs(s, rel))
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		if err := fmDelete(s, fmJoin(rel, e.Name), e.Dir, depth+1); err != nil {
			return err
		}
	}
	code, _ := s.cmd("RMD " + fmAbs(s, rel))
	if code != 250 {
		return errors.New("rmdir failed")
	}
	return nil
}

// fmMkdirAll creates every missing segment of a directory path.
func fmMkdirAll(s *galSession, rel string) {
	if rel == "/" || rel == "" {
		return
	}
	cur := ""
	for _, seg := range strings.Split(strings.TrimPrefix(rel, "/"), "/") {
		cur += "/" + seg
		s.cmd("MKD " + fmAbs(s, cur)) // 550 when it already exists: fine
	}
}

// fmCopy copies a file (streamed through this server, using a
// second session for the write side) or a whole folder tree.
func fmCopy(srcS, dstS *galSession, from, toDir string, isDir bool, depth int) error {
	dst := fmJoin(toDir, path.Base(from))
	if !isDir {
		rc, err := srcS.transfer("RETR " + fmAbs(srcS, from))
		if err != nil {
			return err
		}
		wc, err := dstS.transfer("STOR " + fmAbs(dstS, dst))
		if err != nil {
			rc.Close()
			srcS.finish()
			return err
		}
		_, copyErr := io.Copy(wc, rc)
		rc.Close()
		srcS.finish()
		wc.Close()
		dstS.finish()
		return copyErr
	}
	if depth > 12 {
		return errors.New("too deep")
	}
	dstS.cmd("MKD " + fmAbs(dstS, dst))
	ents, err := srcS.list(fmAbs(srcS, from))
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		if err := fmCopy(srcS, dstS, fmJoin(from, e.Name), dst, e.Dir, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// ---------- HTTP handlers ----------

func fmWriteErr(w http.ResponseWriter, lang string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": tr(lang, "fmErr")})
}

func fmWriteOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// filesCheckHandler probes both FTP servers the way the file
// manager connects (login + dialect), and reports which answer.
func filesCheckHandler(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	res := map[string]bool{"oz": false, "plugin": false}
	if validIP(ip) {
		if s, err := fmConnect(ip, galOzPort); err == nil {
			res["oz"] = true
			s.close()
		}
		if s, err := fmConnect(ip, galPluginPort); err == nil {
			res["plugin"] = true
			s.close()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func filesListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	rel, ok := fmCleanPath(q.Get("path"))
	if !validIP(ip) || !ok {
		fmWriteErr(w, lang)
		return
	}
	s, used, err := fmResolve(ip, src)
	if err != nil {
		fmWriteErr(w, lang)
		return
	}
	defer s.close()
	items, err := fmList(s, rel)
	if err != nil {
		fmWriteErr(w, lang)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "path": rel, "items": items, "src": used,
	})
}

type fmBody struct {
	IP    string `json:"ip"`
	Src   string `json:"src"`
	Lang  string `json:"lang"`
	Path  string `json:"path"`
	Dir   string `json:"dir"`
	Name  string `json:"name"`
	From  string `json:"from"`
	ToDir string `json:"toDir"`
	IsDir bool   `json:"isDir"`
}

func fmDecode(r *http.Request) (fmBody, bool) {
	var b fmBody
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		return b, false
	}
	return b, validIP(b.IP)
}

// filesMkdirHandler creates one folder inside Dir.
func filesMkdirHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := fmDecode(r)
	dir, okDir := fmCleanPath(b.Dir)
	if !ok || !okDir || !fmValidName(b.Name) {
		fmWriteErr(w, b.Lang)
		return
	}
	s, _, err := fmResolve(b.IP, b.Src)
	if err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	defer s.close()
	code, _ := s.cmd("MKD " + fmAbs(s, fmJoin(dir, b.Name)))
	if code != 257 && code != 250 {
		fmWriteErr(w, b.Lang)
		return
	}
	fmWriteOK(w)
}

// filesDeleteHandler deletes a file or a whole folder tree.
func filesDeleteHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := fmDecode(r)
	rel, okPath := fmCleanPath(b.Path)
	if !ok || !okPath || rel == "/" {
		fmWriteErr(w, b.Lang)
		return
	}
	s, _, err := fmResolve(b.IP, b.Src)
	if err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	defer s.close()
	if err := fmDelete(s, rel, b.IsDir, 0); err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	fmWriteOK(w)
}

// filesRenameHandler renames (RNFR/RNTO) within the same folder;
// with ToDir set it moves to another folder instead.
func filesRenameHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := fmDecode(r)
	from, okFrom := fmCleanPath(b.From)
	if !ok || !okFrom || from == "/" {
		fmWriteErr(w, b.Lang)
		return
	}
	var to string
	if b.ToDir != "" {
		dir, okDir := fmCleanPath(b.ToDir)
		if !okDir {
			fmWriteErr(w, b.Lang)
			return
		}
		to = fmJoin(dir, path.Base(from))
	} else {
		if !fmValidName(b.Name) {
			fmWriteErr(w, b.Lang)
			return
		}
		to = fmJoin(fmParent(from), b.Name)
	}
	if to == from {
		fmWriteErr(w, b.Lang)
		return
	}
	s, _, err := fmResolve(b.IP, b.Src)
	if err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	defer s.close()
	code, _ := s.cmd("RNFR " + fmAbs(s, from))
	if code != 350 {
		fmWriteErr(w, b.Lang)
		return
	}
	code, _ = s.cmd("RNTO " + fmAbs(s, to))
	if code != 250 {
		fmWriteErr(w, b.Lang)
		return
	}
	fmWriteOK(w)
}

// filesCopyHandler copies a file or folder tree into ToDir.
func filesCopyHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := fmDecode(r)
	from, okFrom := fmCleanPath(b.From)
	toDir, okDir := fmCleanPath(b.ToDir)
	if !ok || !okFrom || !okDir || from == "/" || from == toDir {
		fmWriteErr(w, b.Lang)
		return
	}
	srcS, _, err := fmResolve(b.IP, b.Src)
	if err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	defer srcS.close()
	dstS, _, err := fmResolve(b.IP, b.Src)
	if err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	defer dstS.close()
	if err := fmCopy(srcS, dstS, from, toDir, b.IsDir, 0); err != nil {
		fmWriteErr(w, b.Lang)
		return
	}
	fmWriteOK(w)
}

// filesUploadHandler streams the request body to Path on the
// console, creating parent folders as needed.
func filesUploadHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	rel, ok := fmCleanPath(q.Get("path"))
	if !validIP(ip) || !ok || rel == "/" {
		fmWriteErr(w, lang)
		return
	}
	s, _, err := fmResolve(ip, src)
	if err != nil {
		fmWriteErr(w, lang)
		return
	}
	defer s.close()
	fmMkdirAll(s, fmParent(rel))
	dc, err := s.transfer("STOR " + fmAbs(s, rel))
	if err != nil {
		fmWriteErr(w, lang)
		return
	}
	_, copyErr := io.Copy(dc, r.Body)
	dc.Close()
	s.finish()
	if copyErr != nil {
		fmWriteErr(w, lang)
		return
	}
	fmWriteOK(w)
}

// filesDownloadHandler streams one file to the browser (used by
// the download button, drag-out via DownloadURL, and Android's
// DownloadManager).
func filesDownloadHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	rel, ok := fmCleanPath(q.Get("path"))
	if !validIP(ip) || !ok || rel == "/" {
		fmWriteErr(w, lang)
		return
	}
	s, _, err := fmResolve(ip, src)
	if err != nil {
		fmWriteErr(w, lang)
		return
	}
	defer s.close()
	name := path.Base(rel)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(name, "\"", "")+"\"")
	if size, ok := s.size(fmAbs(s, rel)); ok {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	dc, err := s.transfer("RETR " + fmAbs(s, rel))
	if err != nil {
		return
	}
	io.Copy(w, dc)
	dc.Close()
	s.finish()
}
