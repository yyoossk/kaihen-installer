package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeGalFTP is a minimal FTP server for the gallery client: an
// in-memory file tree, MLSD (optionally refused, to exercise the LIST
// fallback), LIST, SIZE, REST and RETR.
type fakeGalFTP struct {
	ln     net.Listener
	mu     sync.Mutex
	files  map[string][]byte
	dirs   map[string]bool
	noMLSD bool
}

func newFakeGalFTP(t *testing.T, files map[string][]byte, noMLSD bool) *fakeGalFTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeGalFTP{ln: ln, files: files, dirs: map[string]bool{}, noMLSD: noMLSD}
	for p := range files {
		d := p
		for {
			i := strings.LastIndex(d, "/")
			if i <= 0 {
				break
			}
			d = d[:i]
			f.dirs[d] = true
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.handle(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeGalFTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

// children returns the direct entries of a directory path.
func (f *fakeGalFTP) children(dir string) (dirs, files []string) {
	seen := map[string]bool{}
	for p := range f.files {
		if !strings.HasPrefix(p, dir+"/") {
			continue
		}
		rest := p[len(dir)+1:]
		if i := strings.Index(rest, "/"); i >= 0 {
			if d := rest[:i]; !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		} else {
			files = append(files, rest)
		}
	}
	return dirs, files
}

func (f *fakeGalFTP) handle(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	send := func(s string) { fmt.Fprintf(w, "%s\r\n", s); w.Flush() }
	send("220 fake")
	var pasvLn net.Listener
	var restAt int64
	data := func(payload []byte) {
		send("150 opening")
		dc, err := pasvLn.Accept()
		if err != nil {
			send("425 no data")
			return
		}
		dc.Write(payload)
		dc.Close()
		pasvLn.Close()
		pasvLn = nil
		send("226 done")
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg := line, ""
		if i := strings.Index(line, " "); i >= 0 {
			verb, arg = line[:i], line[i+1:]
		}
		switch verb {
		case "USER":
			send("230 ok")
		case "PASS":
			send("230 ok")
		case "TYPE":
			send("200 ok")
		case "CWD":
			if f.dirs[arg] {
				send("250 ok")
			} else {
				send("550 no such folder")
			}
		case "PASV":
			pasvLn, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				send("425 cannot listen")
				continue
			}
			p := pasvLn.Addr().(*net.TCPAddr).Port
			send(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)", p/256, p%256))
		case "MLSD":
			if f.noMLSD {
				send("502 not implemented")
				continue
			}
			dirs, files := f.children(arg)
			var b strings.Builder
			for _, d := range dirs {
				fmt.Fprintf(&b, "type=dir;modify=20261118000000; %s\r\n", d)
			}
			for _, n := range files {
				fmt.Fprintf(&b, "type=file;size=%d;modify=20261118000000; %s\r\n", len(f.files[arg+"/"+n]), n)
			}
			data([]byte(b.String()))
		case "LIST":
			dirs, files := f.children(arg)
			var b strings.Builder
			for _, d := range dirs {
				fmt.Fprintf(&b, "drwxr-xr-x 2 user group 0 Nov 18 2026 %s\r\n", d)
			}
			for _, n := range files {
				fmt.Fprintf(&b, "-rw-r--r-- 1 user group %d Nov 18 2026 %s\r\n", len(f.files[arg+"/"+n]), n)
			}
			data([]byte(b.String()))
		case "SIZE":
			if b, ok := f.files[arg]; ok {
				send(fmt.Sprintf("213 %d", len(b)))
			} else {
				send("550 no file")
			}
		case "REST":
			restAt, _ = strconv.ParseInt(arg, 10, 64)
			send("350 restart at " + arg)
		case "RETR":
			b, ok := f.files[arg]
			if !ok {
				send("550 no file")
				continue
			}
			if restAt > 0 {
				if restAt > int64(len(b)) {
					restAt = int64(len(b))
				}
				b = b[restAt:]
				restAt = 0
			}
			data(b)
		case "DELE":
			if _, ok := f.files[arg]; ok {
				delete(f.files, arg)
				send("250 deleted")
			} else {
				send("550 no file")
			}
		case "QUIT":
			send("221 bye")
			return
		default:
			send("502 not implemented")
		}
	}
}

func galTestFiles() map[string][]byte {
	jpg := makeJPEG(64, 48)
	return map[string][]byte{
		"/Nintendo/Album/2026/11/18/2026111812000000-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.jpg": jpg,
		"/Nintendo/Album/2026/11/18/2026111813000000-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB.mp4": []byte("fake mp4 data 0123456789"),
		"/Nintendo/Album/2026/11/17/2026111710300000-CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC.jpg": jpg,
		"/Nintendo/Album/2026/11/17/notes.txt": []byte("not media"),
	}
}

func makeJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 5), 128, 255})
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func TestGalWalkMLSD(t *testing.T) {
	f := newFakeGalFTP(t, galTestFiles(), false)
	s, err := galConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()
	if s.prefix != "" {
		t.Fatalf("prefix = %q, want plain dialect", s.prefix)
	}
	items, err := s.walk()
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (txt excluded)", len(items))
	}
	// Newest first: the 13:00 video, then 12:00 photo, then the 17th.
	if items[0].Type != "video" || items[0].Taken != "20261118130000" {
		t.Errorf("items[0] = %+v", items[0])
	}
	if items[1].Type != "photo" || items[1].Path != "2026/11/18/2026111812000000-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.jpg" {
		t.Errorf("items[1] = %+v", items[1])
	}
	if items[2].Taken != "20261117103000" {
		t.Errorf("items[2] = %+v", items[2])
	}
	if items[0].Size != int64(len("fake mp4 data 0123456789")) {
		t.Errorf("video size = %d", items[0].Size)
	}
}

func TestGalWalkLISTFallback(t *testing.T) {
	f := newFakeGalFTP(t, galTestFiles(), true)
	s, err := galConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()
	items, err := s.walk()
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 via LIST fallback", len(items))
	}
}

func TestGalRetrRangeAndThumb(t *testing.T) {
	f := newFakeGalFTP(t, galTestFiles(), false)
	s, err := galConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()
	abs := "/Nintendo/Album/2026/11/18/2026111813000000-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB.mp4"
	total, ok := s.size(abs)
	if !ok || total != int64(len("fake mp4 data 0123456789")) {
		t.Fatalf("size = %d, %v", total, ok)
	}
	a, b, ok := parseRange("bytes=5-9", total)
	if !ok || a != 5 || b != 9 {
		t.Fatalf("parseRange = %d-%d %v", a, b, ok)
	}
	var buf bytes.Buffer
	if err := s.retr(abs, a, &limitWriter{w: &buf, n: b - a + 1}); err != nil {
		t.Fatalf("retr: %v", err)
	}
	if buf.String() != "mp4 d" {
		t.Errorf("range bytes = %q, want %q", buf.String(), "mp4 d")
	}
	// Thumbnail of the test JPEG comes back as a decodable JPEG.
	jpgAbs := "/Nintendo/Album/2026/11/18/2026111812000000-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA.jpg"
	th, err := galThumb(s, jpgAbs, "test|jpg")
	if err != nil {
		t.Fatalf("thumb: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(th))
	if err != nil {
		t.Fatalf("thumb decode: %v", err)
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 48 {
		t.Errorf("thumb size = %v, want 64x48 (already small)", img.Bounds())
	}
}

func TestGalDelete(t *testing.T) {
	files := galTestFiles()
	f := newFakeGalFTP(t, files, false)
	s, err := galConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()
	victim := "/Nintendo/Album/2026/11/17/2026111710300000-CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC.jpg"
	if err := s.del(victim); err != nil {
		t.Fatalf("del: %v", err)
	}
	if _, ok := files[victim]; ok {
		t.Error("file still present after DELE")
	}
	if err := s.del(victim); err == nil {
		t.Error("del of a missing file succeeded, want error")
	}
	items, err := s.walk()
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("items after delete = %d, want 2", len(items))
	}
}

func TestGalHelpers(t *testing.T) {
	if got := takenFromName("2026111812000000-AAAA.jpg"); got != "20261118120000" {
		t.Errorf("taken = %q", got)
	}
	if got := takenFromName("2026131812000000-AAAA.jpg"); got != "" {
		t.Errorf("taken(month 13) = %q, want empty", got)
	}
	if got := takenFromName("photo.jpg"); got != "" {
		t.Errorf("taken(short) = %q, want empty", got)
	}
	if _, ok := galCleanRel("../secret.jpg"); ok {
		t.Error("galCleanRel accepted ..")
	}
	if c, ok := galCleanRel("2026/11/18/x.jpg"); !ok || c != "2026/11/18/x.jpg" {
		t.Errorf("galCleanRel = %q %v", c, ok)
	}
	if e, ok := parseMLSDLine("type=dir;modify=20261118000000; 2026"); !ok || !e.Dir || e.Name != "2026" {
		t.Errorf("mlsd dir = %+v %v", e, ok)
	}
	if e, ok := parseLISTLine("-rw-r--r-- 1 user group 123 Nov 18 2026 a b.jpg"); !ok || e.Dir || e.Size != 123 || e.Name != "a b.jpg" {
		t.Errorf("list file = %+v %v", e, ok)
	}
	big := image.NewRGBA(image.Rect(0, 0, 960, 540))
	small := scaleToFit(big, 480)
	if small.Bounds().Dx() != 480 || small.Bounds().Dy() != 270 {
		t.Errorf("scaleToFit = %v, want 480x270", small.Bounds())
	}
	if _, _, ok := parseRange("bytes=0-99,200-299", 1000); ok {
		t.Error("parseRange accepted a multi-range")
	}
	if a, b, ok := parseRange("bytes=-100", 1000); !ok || a != 900 || b != 999 {
		t.Errorf("suffix range = %d-%d %v", a, b, ok)
	}
}
