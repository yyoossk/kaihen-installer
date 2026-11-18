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
}

func galConnect(ip string, port int) (*galSession, error) {
	ctrl, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), 8*time.Second)
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
	return s.rd.readReply()
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
	return net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(n[4]*256+n[5])), 10*time.Second)
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
	s.rd.readReply() // 226/250; content not needed
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

// walk collects every media file under the album root (the layout is
// YYYY/MM/DD, but the walk tolerates any nesting up to depth 4).
func (s *galSession) walk() ([]galItem, error) {
	var items []galItem
	var rec func(abs, rel string, depth int) error
	rec = func(abs, rel string, depth int) error {
		ents, err := s.list(abs)
		if err != nil {
			return err
		}
		for _, e := range ents {
			childRel := e.Name
			if rel != "" {
				childRel = rel + "/" + e.Name
			}
			if e.Dir {
				if depth < 4 {
					if err := rec(abs+"/"+e.Name, childRel, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			if mt := galMediaType(e.Name); mt != "" {
				items = append(items, galItem{
					Path:  childRel,
					Name:  e.Name,
					Type:  mt,
					Size:  e.Size,
					Taken: takenFromName(e.Name),
				})
			}
		}
		return nil
	}
	if err := rec(s.prefix+galAlbumRoot, "", 0); err != nil {
		return nil, err
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

func galleryListHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	if !validIP(ip) {
		galWriteErr(w, lang, http.StatusBadRequest)
		return
	}
	s, used, err := galResolve(ip, src)
	if err != nil {
		galWriteErr(w, lang, http.StatusBadGateway)
		return
	}
	defer s.close()
	items, err := s.walk()
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

// galSessionFor resolves one request's session and absolute path.
func galSessionFor(w http.ResponseWriter, r *http.Request) (*galSession, string, bool) {
	q := r.URL.Query()
	ip, src, lang := q.Get("ip"), q.Get("src"), q.Get("lang")
	rel, ok := galCleanRel(q.Get("path"))
	if !validIP(ip) || !ok || galMediaType(rel) == "" {
		galWriteErr(w, lang, http.StatusBadRequest)
		return nil, "", false
	}
	s, _, err := galResolve(ip, src)
	if err != nil {
		galWriteErr(w, lang, http.StatusBadGateway)
		return nil, "", false
	}
	return s, s.prefix + galAlbumRoot + "/" + rel, true
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
	s, abs, ok := galSessionFor(w, r)
	if !ok {
		return
	}
	defer s.close()
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
	s, abs, ok := galSessionFor(w, r)
	if !ok {
		return
	}
	defer s.close()
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
