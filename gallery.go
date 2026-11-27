package main

// Gallery: an NXGallery-style view of the console's album, served over
// FTP instead of NXGallery's own HTTP server. The page shows the SD
// card album (Nintendo/Album) as a thumbnail grid; tapping an item
// opens it full-size (videos play in the viewer) with its details and
// a download button — the same operations as NXGallery's web page.
//
// Two FTP servers can provide the files: OunceShell (port 1337,
// started with SELECT; kaiHEN 20261114c speaks the ftpvita dialect
// with /sdmc: paths, 20261114d+ speaks ftpd with the SD card at the
// root) and the FTPD plugin (port 5000, passive only, SD at the root).
// src=auto tries OunceShell first, then the plugin.
//
// Captures in the console's internal storage are NOT reachable this
// way: they are encrypted and only the system album service can read
// them (NXGallery on the console uses that service). Only the SD card
// album is listed here, and the page says so.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	galOzPort     = 1337
	galPluginPort = 5000
	galAlbumRoot  = "/Nintendo/Album"
	galThumbMax   = 480
	galFileMax    = 64 << 20 // sanity cap for thumbnail source reads
)

// ---------- FTP browse session ----------

type galSession struct {
	ctrl   net.Conn
	rd     *ftpReader
	prefix string // "/sdmc:" or ""
	// broken is set when the control connection's state can no
	// longer be trusted (a transport failure, a cut transfer).
	// Reply-code failures (a refused MLSD, a missing file) do NOT
	// set it: the session stays in sync and reusable.
	broken bool
}

func galConnect(ip string, port int) (*galSession, error) {
	ctrl, err := lanDialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 8*time.Second)
	if err != nil {
		return nil, err
	}
	s := &galSession{ctrl: ctrl}
	ctrl.SetDeadline(time.Now().Add(300 * time.Second))
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
	// Resolve the dialect by CWD alone (never MKD a probe path: on
	// ftpd a successful MKD of the sdmc: path would create a real
	// folder named "sdmc:" — the same rule as ozConnect).
	if code, _ := s.cmd("CWD " + galAlbumRoot); code == 250 {
		s.prefix = ""
		return s, nil
	}
	if code, _ := s.cmd("CWD /sdmc:" + galAlbumRoot); code == 250 {
		s.prefix = "/sdmc:"
		return s, nil
	}
	ctrl.Close()
	return nil, errors.New("no album folder")
}

func (s *galSession) cmd(c string) (int, string) {
	fmt.Fprintf(s.ctrl, "%s\r\n", c)
	code, line := s.rd.readReply()
	if code == 0 { // readReply reports transport failures as 0
		s.broken = true
	}
	return code, line
}

func (s *galSession) close() {
	s.cmd("QUIT")
	s.ctrl.Close()
}

// pasv opens a passive data connection.
func (s *galSession) pasv() (net.Conn, error) {
	code, line := s.cmd("PASV")
	if code != 227 {
		return nil, fmt.Errorf("PASV: %d", code)
	}
	i := strings.Index(line, "(")
	j := strings.Index(line, ")")
	if i < 0 || j <= i {
		return nil, errors.New("PASV: bad reply")
	}
	parts := strings.Split(line[i+1:j], ",")
	if len(parts) != 6 {
		return nil, errors.New("PASV: bad reply")
	}
	n := make([]int, 6)
	for k, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, errors.New("PASV: bad reply")
		}
		n[k] = v
	}
	host := fmt.Sprintf("%d.%d.%d.%d", n[0], n[1], n[2], n[3])
	dc, err := lanDialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(n[4]*256+n[5])), 10*time.Second)
	if err != nil {
		s.broken = true
	}
	return dc, err
}

// transfer runs a data command and returns the data connection; the
// caller reads it to EOF and then calls finish for the final reply.
func (s *galSession) transfer(cmd string) (net.Conn, error) {
	dc, err := s.pasv()
	if err != nil {
		return nil, err
	}
	code, _ := s.cmd(cmd)
	if code != 150 && code != 125 {
		dc.Close()
		return nil, fmt.Errorf("%s: %d", cmd, code)
	}
	return dc, nil
}

func (s *galSession) finish() {
	if code, _ := s.rd.readReply(); code == 0 { // 226/250 normally
		s.broken = true
	}
}

type galEntry struct {
	Name string
	Dir  bool
	Size int64
}

// list returns the entries of one absolute FTP directory path.
func (s *galSession) list(abs string) ([]galEntry, error) {
	if ents, err := s.listCmd("MLSD " + abs); err == nil {
		return ents, nil
	}
	return s.listCmd("LIST " + abs)
}

func (s *galSession) listCmd(cmd string) ([]galEntry, error) {
	dc, err := s.transfer(cmd)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(dc, 8<<20))
	dc.Close()
	s.finish()
	if err != nil {
		s.broken = true
		return nil, err
	}
	var out []galEntry
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		if strings.HasPrefix(cmd, "MLSD") {
			if e, ok := parseMLSDLine(line); ok {
				out = append(out, e)
			}
		} else {
			if e, ok := parseLISTLine(line); ok {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// parseMLSDLine parses "type=dir;modify=...; name" fact lines.
func parseMLSDLine(line string) (galEntry, bool) {
	i := strings.Index(line, " ")
	if i < 0 {
		return galEntry{}, false
	}
	facts, name := line[:i], strings.TrimSpace(line[i+1:])
	if name == "." || name == ".." || name == "" {
		return galEntry{}, false
	}
	e := galEntry{Name: name}
	for _, f := range strings.Split(facts, ";") {
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.ToLower(kv[0]) {
		case "type":
			e.Dir = strings.EqualFold(kv[1], "dir")
		case "size":
			e.Size, _ = strconv.ParseInt(kv[1], 10, 64)
		}
	}
	return e, true
}

// parseLISTLine parses unix-style LIST output:
// "-rw-r--r-- 1 user group 12345 Nov 18  2026 name" (files and dirs).
func parseLISTLine(line string) (galEntry, bool) {
	if len(line) < 2 || (line[0] != '-' && line[0] != 'd') {
		return galEntry{}, false
	}
	f := strings.Fields(line)
	if len(f) < 9 {
		return galEntry{}, false
	}
	size, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return galEntry{}, false
	}
	name := strings.Join(f[8:], " ")
	if name == "." || name == ".." {
		return galEntry{}, false
	}
	return galEntry{Name: name, Dir: line[0] == 'd', Size: size}, true
}

// retr streams a file (absolute FTP path) starting at offset.
func (s *galSession) retr(abs string, offset int64, w io.Writer) error {
	dc, err := s.pasv()
	if err != nil {
		return err
	}
	if offset > 0 {
		if code, _ := s.cmd("REST " + strconv.FormatInt(offset, 10)); code != 350 {
			dc.Close()
			return fmt.Errorf("REST: %d", code)
		}
	}
	code, _ := s.cmd("RETR " + abs)
	if code != 150 && code != 125 {
		dc.Close()
		return fmt.Errorf("RETR: %d", code)
	}
	_, err = io.Copy(w, dc)
	dc.Close()
	s.finish()
	if err != nil {
		// The data stream was cut short; whether the control side
		// is still in sync is uncertain, so do not reuse it.
		s.broken = true
	}
	return err
}

func (s *galSession) size(abs string) (int64, bool) {
	code, line := s.cmd("SIZE " + abs)
	if code != 213 {
		return 0, false
	}
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0, false
	}
	n, err := strconv.ParseInt(f[len(f)-1], 10, 64)
	return n, err == nil
}

// ---------- album model ----------

type galItem struct {
	Path  string `json:"path"`  // relative to the album root, slash-joined
	Name  string `json:"name"`  // file name
	Type  string `json:"type"`  // photo | video
	Size  int64  `json:"size"`  // bytes (0 when the server did not say)
	Taken string `json:"taken"` // "20251114123456" from the file name, or ""
}

func galMediaType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png":
		return "photo"
	case ".mp4":
		return "video"
	}
	return ""
}

// takenFromName extracts the capture timestamp Switch album file
// names start with (yyyyMMddHHmmss), or "" when it is not there.
func takenFromName(name string) string {
	if len(name) < 14 {
		return ""
	}
	t := name[:14]
	for _, c := range t {
		if c < '0' || c > '9' {
			return ""
		}
	}
	mo, _ := strconv.Atoi(t[4:6])
	dy, _ := strconv.Atoi(t[6:8])
	hh, _ := strconv.Atoi(t[8:10])
	if mo < 1 || mo > 12 || dy < 1 || dy > 31 || hh > 23 {
		return ""
	}
	return t
}

// galWalk collects every media file under the album root (the
// layout is YYYY/MM/DD, but the walk tolerates any nesting up to
// depth 4). Directory listings run concurrently — bounded, so the
// console's FTP servers (the plugin accepts at most 4 clients) are
// never flooded: listing the date folders one by one over FTP
// round trips was the slowest part of opening the gallery. Each
// listing runs on its own pooled session.
func galWalk(ip, src string) ([]galItem, string, error) {
	// The first session also establishes the dialect prefix the
	// album root lives under.
	s0, used, release0, err := galAcquire(ip, src)
	if err != nil {
		return nil, "", err
	}
	root := s0.prefix + galAlbumRoot
	release0()
	listDir := func(abs string) ([]galEntry, error) {
		s, _, release, err := galAcquire(ip, src)
		if err != nil {
			return nil, err
		}
		defer release()
		return s.list(abs)
	}
	items, err := galWalkTree(listDir, root)
	return items, used, err
}

func galWalkTree(listDir func(abs string) ([]galEntry, error), root string) ([]galItem, error) {
	var mu sync.Mutex
	var items []galItem
	var firstErr error
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var rec func(abs, rel string, depth int)
	rec = func(abs, rel string, depth int) {
		defer wg.Done()
		mu.Lock()
		failed := firstErr != nil
		mu.Unlock()
		if failed {
			return
		}
		sem <- struct{}{}
		ents, err := listDir(abs)
		<-sem
		if err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
			return
		}
		for _, e := range ents {
			childRel := e.Name
			if rel != "" {
				childRel = rel + "/" + e.Name
			}
			if e.Dir {
				if depth < 4 {
					wg.Add(1)
					go rec(abs+"/"+e.Name, childRel, depth+1)
				}
				continue
			}
			if mt := galMediaType(e.Name); mt != "" {
				mu.Lock()
				items = append(items, galItem{
					Path:  childRel,
					Name:  e.Name,
					Type:  mt,
					Size:  e.Size,
					Taken: takenFromName(e.Name),
				})
				mu.Unlock()
			}
		}
	}
	wg.Add(1)
	rec(root, "", 0)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Taken != items[j].Taken {
			return items[i].Taken > items[j].Taken // newest first
		}
		return items[i].Name > items[j].Name
	})
	return items, nil
}

// galResolve connects through the requested source ("oz", "plugin" or
// "auto" = OunceShell first, then the plugin) and returns the session
// plus the source that answered.
func galResolve(ip, src string) (*galSession, string, error) {
	switch src {
	case "oz":
		s, err := galConnect(ip, galOzPort)
		return s, "oz", err
	case "plugin":
		s, err := galConnect(ip, galPluginPort)
		return s, "plugin", err
	default:
		if s, err := galConnect(ip, galOzPort); err == nil {
			return s, "oz", nil
		}
		s, err := galConnect(ip, galPluginPort)
		return s, "plugin", err
	}
}

var galResolveFn = galResolve

// ---------- gallery session pool ----------

// Connecting and logging in for every gallery request — every
// thumbnail in the grid — was the gallery's main cost, so sessions
// are pooled per (console, source) and reused. An FTP control
// connection is strictly sequential, so a pooled session serves one
// request at a time; a request that finds the pool busy opens a
// short-lived session of its own, and parallel thumbnail loads
// still proceed (the console's servers accept a few clients).
type galPoolKey struct{ ip, src string }

type galPoolSlot struct {
	mu   sync.Mutex // held while the pooled session is checked out
	sess *galSession
	used string
	idle time.Time
}

var (
	galPoolMu    sync.Mutex
	galPoolSlots = map[galPoolKey]*galPoolSlot{}
)

func galSlot(ip, src string) *galPoolSlot {
	galPoolMu.Lock()
	defer galPoolMu.Unlock()
	k := galPoolKey{ip, src}
	slot, ok := galPoolSlots[k]
	if !ok {
		slot = &galPoolSlot{}
		galPoolSlots[k] = slot
	}
	return slot
}

// galPoolAlive checks a pooled session that has been idle a while:
// servers drop idle connections, so prove it with NOOP first.
func galPoolAlive(s *galSession, idle time.Time) bool {
	if time.Since(idle) < 15*time.Second {
		return true
	}
	code, _ := s.cmd("NOOP")
	return code >= 200 && code < 300 && !s.broken
}

// galAcquire returns a session for one gallery request and the
// source it resolved to. The release function must be called once:
// it returns a healthy session to the pool for the next request and
// closes anything else.
func galAcquire(ip, src string) (*galSession, string, func(), error) {
	slot := galSlot(ip, src)
	if slot.mu.TryLock() {
		sess, used := slot.sess, slot.used
		slot.sess = nil
		if sess != nil {
			// The connect-time deadline still applies; renew it.
			sess.ctrl.SetDeadline(time.Now().Add(300 * time.Second))
			if sess.broken || !galPoolAlive(sess, slot.idle) {
				sess.close()
				sess = nil
			}
		}
		if sess == nil {
			var err error
			sess, used, err = galResolveFn(ip, src)
			if err != nil {
				slot.mu.Unlock()
				return nil, "", nil, err
			}
		}
		return sess, used, func() {
			if sess.broken {
				sess.close()
			} else {
				slot.sess = sess
				slot.used = used
				slot.idle = time.Now()
			}
			slot.mu.Unlock()
		}, nil
	}
	// Pool busy: a private session, closed on release.
	sess, used, err := galResolveFn(ip, src)
	if err != nil {
		return nil, "", nil, err
	}
	return sess, used, func() { sess.close() }, nil
}

var galPrefetching sync.Map // "ip|src" while a prefetch is running

// galPrefetch warms the thumbnail cache for the first page of a
// freshly listed album (photos only; one page is 30 items, the
// page's own page size), so opening the gallery shows its first
// screen without waiting for each thumbnail. Later pages load
// their thumbnails when shown. It goes through the session pool
// like any other request: a thumbnail the user asks for meanwhile
// takes its own session and never waits for the prefetch.
func galPrefetch(ip, src string, items []galItem) {
	key := ip + "|" + src
	if _, busy := galPrefetching.LoadOrStore(key, true); busy {
		return
	}
	defer galPrefetching.Delete(key)
	n := 0
	for _, it := range items {
		if it.Type != "photo" {
			continue
		}
		if n >= 30 {
			break
		}
		n++
		thumbKey := ip + "|" + src + "|" + it.Path
		galThumbMu.Lock()
		_, cached := galThumbCache[thumbKey]
		galThumbMu.Unlock()
		if cached {
			continue
		}
		s, _, release, err := galAcquire(ip, src)
		if err != nil {
			return
		}
		_, err = galThumb(s, s.prefix+galAlbumRoot+"/"+it.Path, thumbKey)
		release()
		if err != nil {
			return // the console went away; stop warming
		}
	}
}

// galCleanRel validates a client-supplied album-relative path: no
// traversal segments, and the result stays under the album root.
func galCleanRel(rel string) (string, bool) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", false
		}
	}
	c := path.Clean("/" + rel)
	if c == "/" {
		return "", false
	}
	return strings.TrimPrefix(c, "/"), true
}

// ---------- HTTP handlers ----------

func galWriteErr(w http.ResponseWriter, lang string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": tr(lang, "galErr")})
}

// galleryCheckHandler probes both gallery FTP servers the way the
// gallery itself connects (login + album folder), so the page's
// connection check can say which of them answers.
func galleryCheckHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, lang := q.Get("ip"), q.Get("lang")
	w.Header().Set("Content-Type", "application/json")
	if !validIP(ip) {
		galWriteErr(w, lang, http.StatusBadRequest)
		return
	}
	probe := func(port int) bool {
		s, err := galConnect(ip, port)
		if err != nil {
			return false
		}
		s.close()
		return true
	}
	json.NewEncoder(w).Encode(map[string]bool{
		"oz":     probe(galOzPort),
		"plugin": probe(galPluginPort),
	})
}

// (No delete endpoint: deleting an album file over FTP leaves a
// console album entry that cannot be removed even on the console
// itself — its screenshot service errors with 2206-0023 when the
// file is missing — so the gallery never deletes. Verified on
// real hardware, 2026-11-18.)

func galleryListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	if !validIP(ip) {
		galWriteErr(w, lang, http.StatusBadRequest)
		return
	}
	items, used, err := galWalk(ip, src)
	if err == nil {
		// Warm the thumbnail cache in the background, so scrolling
		// the grid does not generate each thumbnail on first sight.
		go galPrefetch(ip, src, items)
	}
	if err != nil {
		galWriteErr(w, lang, http.StatusBadGateway)
		return
	}
	photos, videos := 0, 0
	for _, it := range items {
		if it.Type == "video" {
			videos++
		} else {
			photos++
		}
	}
	if items == nil {
		items = []galItem{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"items": items, "photos": photos, "videos": videos, "src": used,
	})
}

// galSessionFor acquires one request's session and absolute path.
func galSessionFor(w http.ResponseWriter, r *http.Request) (*galSession, string, func(), bool) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	rel, ok := galCleanRel(q.Get("path"))
	if !validIP(ip) || !ok || galMediaType(rel) == "" {
		galWriteErr(w, lang, http.StatusBadRequest)
		return nil, "", nil, false
	}
	s, _, release, err := galAcquire(ip, src)
	if err != nil {
		galWriteErr(w, lang, http.StatusBadGateway)
		return nil, "", nil, false
	}
	return s, s.prefix + galAlbumRoot + "/" + rel, release, true
}

func galContentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".mp4":
		return "video/mp4"
	}
	return "application/octet-stream"
}

func galleryFileHandler(w http.ResponseWriter, r *http.Request) {
	s, abs, release, ok := galSessionFor(w, r)
	if !ok {
		return
	}
	defer release()
	name := path.Base(abs)
	w.Header().Set("Content-Type", galContentType(name))
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	}
	// Range support (video playback seeks): SIZE + REST.
	var start, end int64 = 0, -1
	total, hasSize := s.size(abs)
	if rng := r.Header.Get("Range"); rng != "" && hasSize {
		if a, b, ok := parseRange(rng, total); ok {
			start, end = a, b
		}
	}
	if end >= 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else if hasSize {
		w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
	}
	// Stream through a limiting writer when a range end is set.
	var dst io.Writer = w
	if end >= 0 {
		dst = &limitWriter{w: w, n: end - start + 1}
	}
	_ = s.retr(abs, start, dst) // headers are already sent; a mid-stream failure just ends the body
}

// galWriteZip streams the given album-relative files into a zip
// archive on w. Entries keep their album-relative paths (folders
// preserved) and are stored uncompressed: photos and videos are
// already compressed formats.
func galWriteZip(w io.Writer, fetch func(rel string) ([]byte, error), rels []string) error {
	// Fetch a few files at a time in parallel while the archive is
	// written in selection order: a multi-select download no longer
	// waits for one file to finish before the next starts moving.
	type fetched struct {
		idx  int
		data []byte
		err  error
	}
	results := make(chan fetched, len(rels))
	slots := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, rel := range rels {
		wg.Add(1)
		go func(idx int, rel string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			data, err := fetch(rel)
			results <- fetched{idx, data, err}
		}(i, rel)
	}
	go func() { wg.Wait(); close(results) }()
	zw := zip.NewWriter(w)
	pending := map[int]fetched{}
	for next := 0; next < len(rels); {
		f, ok := pending[next]
		if !ok {
			r, open := <-results
			if !open {
				return errors.New("gallery zip: fetchers stopped early")
			}
			pending[r.idx] = r
			continue
		}
		delete(pending, next)
		if f.err != nil {
			return f.err
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: rels[next], Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.data); err != nil {
			return err
		}
		next++
	}
	return zw.Close()
}

// galleryZipHandler serves the page's selected files as one zip
// download (the "download selected" action). Paths arrive as
// repeated p parameters and are validated like single-file paths.
func galleryZipHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	if !validIP(ip) {
		galWriteErr(w, lang, http.StatusBadRequest)
		return
	}
	var rels []string
	seen := map[string]bool{}
	for _, p := range q["p"] {
		rel, ok := galCleanRel(p)
		if !ok || galMediaType(rel) == "" || seen[rel] {
			galWriteErr(w, lang, http.StatusBadRequest)
			return
		}
		seen[rel] = true
		rels = append(rels, rel)
	}
	if len(rels) == 0 || len(rels) > 200 {
		galWriteErr(w, lang, http.StatusBadRequest)
		return
	}
	_, _, release, err := galAcquire(ip, src)
	if err != nil {
		galWriteErr(w, lang, http.StatusBadGateway)
		return
	}
	release()
	fetch := func(rel string) ([]byte, error) {
		fs, _, frelease, err := galAcquire(ip, src)
		if err != nil {
			return nil, err
		}
		defer frelease()
		var buf bytes.Buffer
		if err := fs.retr(fs.prefix+galAlbumRoot+"/"+rel, 0, &buf); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"album.zip\"")
	_ = galWriteZip(w, fetch, rels) // headers are sent; a failure just truncates the archive
}

// parseRange parses a single "bytes=a-b" / "bytes=a-" / "bytes=-b"
// range against a total size.
func parseRange(h string, total int64) (int64, int64, bool) {
	if !strings.HasPrefix(h, "bytes=") || total <= 0 {
		return 0, 0, false
	}
	spec := strings.TrimSpace(h[len("bytes="):])
	if strings.Contains(spec, ",") {
		return 0, 0, false // single ranges only
	}
	i := strings.Index(spec, "-")
	if i < 0 {
		return 0, 0, false
	}
	aStr, bStr := spec[:i], spec[i+1:]
	if aStr == "" { // suffix range
		n, err := strconv.ParseInt(bStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, true
	}
	a, err := strconv.ParseInt(aStr, 10, 64)
	if err != nil || a < 0 || a >= total {
		return 0, 0, false
	}
	b := total - 1
	if bStr != "" {
		b, err = strconv.ParseInt(bStr, 10, 64)
		if err != nil || b < a {
			return 0, 0, false
		}
		if b >= total {
			b = total - 1
		}
	}
	return a, b, true
}

type limitWriter struct {
	w io.Writer
	n int64
}

// Write forwards at most the remaining limit and consumes the rest
// silently, so io.Copy never sees a short write.
func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if int64(len(q)) > l.n {
		q = q[:l.n]
	}
	n, err := l.w.Write(q)
	l.n -= int64(n)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

// ---------- thumbnails ----------

var (
	galThumbMu    sync.Mutex
	galThumbCache = map[string][]byte{}
	galThumbOrder []string
)

// thumb returns a small JPEG rendering of a photo, cached in memory.
func galThumb(s *galSession, abs, key string) ([]byte, error) {
	galThumbMu.Lock()
	if b, ok := galThumbCache[key]; ok {
		galThumbMu.Unlock()
		return b, nil
	}
	galThumbMu.Unlock()

	var buf bytes.Buffer
	if err := s.retr(abs, 0, &limitWriter{w: &buf, n: galFileMax}); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(&buf)
	if err != nil {
		return nil, err
	}
	small := scaleToFit(img, galThumbMax)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	b := out.Bytes()
	galThumbMu.Lock()
	galThumbCache[key] = b
	galThumbOrder = append(galThumbOrder, key)
	for len(galThumbOrder) > 120 { // keep at most ~120 thumbnails
		delete(galThumbCache, galThumbOrder[0])
		galThumbOrder = galThumbOrder[1:]
	}
	galThumbMu.Unlock()
	return b, nil
}

// scaleToFit shrinks img so both dimensions are <= max (bilinear);
// images already small enough pass through unchanged.
func scaleToFit(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return img
	}
	nw, nh := w, h
	if w >= h {
		nw, nh = max, h*max/w
	} else {
		nw, nh = w*max/h, max
	}
	if nh < 1 {
		nh = 1
	}
	if nw < 1 {
		nw = 1
	}
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		fy := (float64(y)+0.5)*float64(h)/float64(nh) - 0.5
		y0 := int(fy)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := y0 + 1
		if y1 >= h {
			y1 = h - 1
		}
		ty := fy - float64(int(fy))
		if y0 == y1 {
			ty = 0
		}
		for x := 0; x < nw; x++ {
			fx := (float64(x)+0.5)*float64(w)/float64(nw) - 0.5
			x0 := int(fx)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := x0 + 1
			if x1 >= w {
				x1 = w - 1
			}
			tx := fx - float64(int(fx))
			if x0 == x1 {
				tx = 0
			}
			dst.Set(x, y, bilerp(src, x0, y0, x1, y1, tx, ty))
		}
	}
	return dst
}

func bilerp(src *image.RGBA, x0, y0, x1, y1 int, tx, ty float64) color.RGBA {
	c00 := src.RGBAAt(x0, y0)
	c10 := src.RGBAAt(x1, y0)
	c01 := src.RGBAAt(x0, y1)
	c11 := src.RGBAAt(x1, y1)
	mix := func(a, b, c, d uint8) uint8 {
		top := float64(a)*(1-tx) + float64(b)*tx
		bot := float64(c)*(1-tx) + float64(d)*tx
		return uint8(top*(1-ty) + bot*ty + 0.5)
	}
	return color.RGBA{
		R: mix(c00.R, c10.R, c01.R, c11.R),
		G: mix(c00.G, c10.G, c01.G, c11.G),
		B: mix(c00.B, c10.B, c01.B, c11.B),
		A: 255,
	}
}

func galleryThumbHandler(w http.ResponseWriter, r *http.Request) {
	s, abs, release, ok := galSessionFor(w, r)
	if !ok {
		return
	}
	defer release()
	if galMediaType(abs) != "photo" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	key := q.Get("ip") + "|" + q.Get("src") + "|" + q.Get("path")
	b, err := galThumb(s, abs, key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b)
}
