package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeFmFTP is a small in-memory FTP server with write commands
// (STOR, MKD, RMD, DELE, RNFR/RNTO) for the file-manager tests.
type fakeFmFTP struct {
	ln    net.Listener
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
}

func newFakeFmFTP(t *testing.T, files map[string][]byte) *fakeFmFTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeFmFTP{ln: ln, files: files, dirs: map[string]bool{"/": true, "/switch": true}}
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

func (f *fakeFmFTP) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeFmFTP) children(dir string) (dirs, files []string) {
	seen := map[string]bool{}
	for p := range f.files {
		if !strings.HasPrefix(p, dir+"/") && dir != "/" {
			continue
		}
		rest := strings.TrimPrefix(p, dir+"/")
		if dir == "/" {
			rest = strings.TrimPrefix(p, "/")
		}
		if i := strings.Index(rest, "/"); i >= 0 {
			if d := rest[:i]; !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		} else {
			files = append(files, rest)
		}
	}
	for d := range f.dirs {
		if d == dir || d == "/" {
			continue
		}
		parent := d[:strings.LastIndex(d, "/")]
		if parent == "" {
			parent = "/"
		}
		if parent == dir {
			name := d[strings.LastIndex(d, "/")+1:]
			if !seen[name] {
				seen[name] = true
				dirs = append(dirs, name)
			}
		}
	}
	return dirs, files
}

func (f *fakeFmFTP) handle(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	send := func(s string) { fmt.Fprintf(w, "%s\r\n", s); w.Flush() }
	send("220 fake")
	var pasvLn net.Listener
	var rnfr string
	openData := func() (net.Conn, bool) {
		send("150 opening")
		dc, err := pasvLn.Accept()
		if err != nil {
			send("425 no data")
			return nil, false
		}
		pasvLn.Close()
		pasvLn = nil
		return dc, true
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
		case "USER", "PASS":
			send("230 ok")
		case "TYPE":
			send("200 ok")
		case "CWD":
			f.mu.Lock()
			ok := f.dirs[arg]
			f.mu.Unlock()
			if ok {
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
			f.mu.Lock()
			dirs, files := f.children(arg)
			var b strings.Builder
			for _, d := range dirs {
				fmt.Fprintf(&b, "type=dir;modify=20261118000000; %s\r\n", d)
			}
			for _, n := range files {
				fp := arg + "/" + n
				if arg == "/" {
					fp = "/" + n
				}
				fmt.Fprintf(&b, "type=file;size=%d;modify=20261118000000; %s\r\n", len(f.files[fp]), n)
			}
			f.mu.Unlock()
			if dc, ok := openData(); ok {
				dc.Write([]byte(b.String()))
				dc.Close()
				send("226 done")
			}
		case "SIZE":
			f.mu.Lock()
			b, ok := f.files[arg]
			f.mu.Unlock()
			if ok {
				send(fmt.Sprintf("213 %d", len(b)))
			} else {
				send("550 no file")
			}
		case "RETR":
			f.mu.Lock()
			b, ok := f.files[arg]
			f.mu.Unlock()
			if !ok {
				send("550 no file")
				continue
			}
			if dc, okd := openData(); okd {
				dc.Write(b)
				dc.Close()
				send("226 done")
			}
		case "STOR":
			if dc, ok := openData(); ok {
				data, _ := io.ReadAll(dc)
				dc.Close()
				f.mu.Lock()
				f.files[arg] = data
				f.mu.Unlock()
				send("226 done")
			}
		case "DELE":
			f.mu.Lock()
			_, ok := f.files[arg]
			if ok {
				delete(f.files, arg)
			}
			f.mu.Unlock()
			if ok {
				send("250 deleted")
			} else {
				send("550 no file")
			}
		case "MKD":
			f.mu.Lock()
			if f.dirs[arg] {
				f.mu.Unlock()
				send("550 exists")
				continue
			}
			f.dirs[arg] = true
			f.mu.Unlock()
			send("257 created")
		case "RMD":
			f.mu.Lock()
			_, ok := f.dirs[arg]
			if ok {
				delete(f.dirs, arg)
			}
			f.mu.Unlock()
			if ok {
				send("250 removed")
			} else {
				send("550 no dir")
			}
		case "RNFR":
			f.mu.Lock()
			_, isFile := f.files[arg]
			isDir := f.dirs[arg]
			f.mu.Unlock()
			if isFile || isDir {
				rnfr = arg
				send("350 ready")
			} else {
				send("550 no such")
			}
		case "RNTO":
			f.mu.Lock()
			if b, ok := f.files[rnfr]; ok {
				f.files[arg] = b
				delete(f.files, rnfr)
			} else if f.dirs[rnfr] {
				f.dirs[arg] = true
				delete(f.dirs, rnfr)
				for p, b := range f.files {
					if strings.HasPrefix(p, rnfr+"/") {
						f.files[arg+p[len(rnfr):]] = b
						delete(f.files, p)
					}
				}
			}
			f.mu.Unlock()
			rnfr = ""
			send("250 renamed")
		case "QUIT":
			send("221 bye")
			return
		default:
			send("502 not implemented")
		}
	}
}

func fmTestFiles() map[string][]byte {
	return map[string][]byte{
		"/switch/a.nro":      []byte("AAA"),
		"/switch/sub/b.txt":  []byte("BBBB"),
		"/switch/sub/c.txt":  []byte("CC"),
		"/readme.txt":        []byte("R"),
		"/Nintendo/info.txt": []byte("N"),
	}
}

func TestFmList(t *testing.T) {
	f := newFakeFmFTP(t, fmTestFiles())
	s, err := fmConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()
	if s.prefix != "" {
		t.Errorf("prefix = %q, want plain", s.prefix)
	}
	items, err := fmList(s, "/")
	if err != nil {
		t.Fatalf("list root: %v", err)
	}
	// Folders first (Nintendo, switch), then files (readme.txt).
	if len(items) != 3 || !items[0].Dir || !items[1].Dir || items[2].Dir {
		t.Fatalf("root items = %+v", items)
	}
	if items[2].Name != "readme.txt" || items[2].Size != 1 {
		t.Errorf("file item = %+v", items[2])
	}
	sub, err := fmList(s, "/switch")
	if err != nil {
		t.Fatalf("list /switch: %v", err)
	}
	if len(sub) != 2 || sub[0].Name != "sub" || !sub[0].Dir || sub[1].Name != "a.nro" {
		t.Errorf("/switch items = %+v", sub)
	}
}

func TestFmOps(t *testing.T) {
	files := fmTestFiles()
	f := newFakeFmFTP(t, files)
	s, err := fmConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.close()

	// New folder (incl. nested via MkdirAll).
	if code, _ := s.cmd("MKD /newdir"); code != 257 {
		t.Fatalf("MKD code = %d", code)
	}
	fmMkdirAll(s, "/x/y/z")
	f.mu.Lock()
	if !f.dirs["/x/y/z"] {
		t.Error("MkdirAll did not create /x/y/z")
	}
	f.mu.Unlock()

	// Rename within a folder.
	if code, _ := s.cmd("RNFR /switch/a.nro"); code != 350 {
		t.Fatalf("RNFR code = %d", code)
	}
	if code, _ := s.cmd("RNTO /switch/renamed.nro"); code != 250 {
		t.Fatalf("RNTO code = %d", code)
	}
	if _, ok := files["/switch/renamed.nro"]; !ok {
		t.Error("rename did not move the file")
	}

	// Recursive delete of a folder tree.
	if err := fmDelete(s, "/switch/sub", true, 0); err != nil {
		t.Fatalf("delete dir: %v", err)
	}
	if _, ok := files["/switch/sub/b.txt"]; ok {
		t.Error("b.txt survived recursive delete")
	}
	f.mu.Lock()
	if f.dirs["/switch/sub"] {
		t.Error("/switch/sub survived recursive delete")
	}
	f.mu.Unlock()

	// Delete a single file.
	if err := fmDelete(s, "/readme.txt", false, 0); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if _, ok := files["/readme.txt"]; ok {
		t.Error("readme.txt survived delete")
	}
}

func TestFmCopy(t *testing.T) {
	files := fmTestFiles()
	f := newFakeFmFTP(t, files)
	srcS, err := fmConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect src: %v", err)
	}
	defer srcS.close()
	dstS, err := fmConnect("127.0.0.1", f.port())
	if err != nil {
		t.Fatalf("connect dst: %v", err)
	}
	defer dstS.close()

	// Copy one file into /Nintendo.
	if err := fmCopy(srcS, dstS, "/switch/a.nro", "/Nintendo", false, 0); err != nil {
		t.Fatalf("copy file: %v", err)
	}
	if got := string(files["/Nintendo/a.nro"]); got != "AAA" {
		t.Errorf("copied content = %q", got)
	}
	// Copy a whole folder into /newdir.
	dstS.cmd("MKD /newdir")
	if err := fmCopy(srcS, dstS, "/switch/sub", "/newdir", true, 0); err != nil {
		t.Fatalf("copy dir: %v", err)
	}
	if got := string(files["/newdir/sub/b.txt"]); got != "BBBB" {
		t.Errorf("copied tree content = %q", got)
	}
}

func TestFmPaths(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"/", "/", true},
		{"/switch/../etc", "", false},
		{"switch/a.nro", "/switch/a.nro", true},
		{"/switch//a.nro", "/switch/a.nro", true},
		{"..", "", false},
		{"/a/b/../../c", "", false},
	} {
		got, ok := fmCleanPath(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("fmCleanPath(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	if fmParent("/switch/a.nro") != "/switch" || fmParent("/a.nro") != "/" || fmParent("/") != "/" {
		t.Error("fmParent wrong")
	}
	if fmJoin("/", "x") != "/x" || fmJoin("/a", "x") != "/a/x" {
		t.Error("fmJoin wrong")
	}
	if !fmValidName("a b.nro") || fmValidName("a/b") || fmValidName("..") || fmValidName("") {
		t.Error("fmValidName wrong")
	}
}
