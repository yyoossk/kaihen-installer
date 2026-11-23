package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeOzFTP is a minimal FTP server speaking just enough of the
// protocol for ozConnect. A directory set decides CWD answers, and
// MKD adds any path it is asked for (even a bogus one), so a test
// can prove the client never probes with MKD. Every command is
// recorded.
type fakeOzFTP struct {
	ln         net.Listener
	mu         sync.Mutex
	dirs       map[string]bool
	cmds       []string
	files      map[string][]byte
	pasvLn     net.Listener
	killOnStor bool // drop the control connection mid-STOR, never confirming
	lastData   net.Conn
}

func newFakeOzFTP(t *testing.T, dirs ...string) *fakeOzFTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOzFTP{ln: ln, dirs: map[string]bool{}, files: map[string][]byte{}}
	for _, d := range dirs {
		f.dirs[d] = true
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

func (f *fakeOzFTP) handle(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	send := func(s string) { fmt.Fprintf(w, "%s\r\n", s); w.Flush() }
	send("220 fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		f.mu.Lock()
		f.cmds = append(f.cmds, line)
		f.mu.Unlock()
		verb, arg := line, ""
		if i := strings.Index(line, " "); i >= 0 {
			verb, arg = line[:i], line[i+1:]
		}
		switch verb {
		case "USER":
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
		case "MKD":
			f.mu.Lock()
			f.dirs[arg] = true
			f.mu.Unlock()
			send("257 created")
		case "PASV":
			if f.pasvLn != nil {
				f.pasvLn.Close()
			}
			pl, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				send("425 no data")
				continue
			}
			f.pasvLn = pl
			port := pl.Addr().(*net.TCPAddr).Port
			send(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)", port>>8, port&255))
		case "STOR":
			send("150 ok")
			dc, err := f.pasvLn.Accept()
			if err == nil && f.killOnStor {
				// Keep the data connection open but abandon the
				// control connection: the transfer is never
				// confirmed, like a server dropping mid-upload.
				f.lastData = dc
				return
			}
			if err != nil {
				send("425 no data")
				continue
			}
			data, _ := io.ReadAll(dc)
			dc.Close()
			f.pasvLn.Close()
			f.pasvLn = nil
			f.mu.Lock()
			f.files[arg] = data
			f.mu.Unlock()
			send("226 ok")
		case "SIZE":
			f.mu.Lock()
			data, ok := f.files[arg]
			f.mu.Unlock()
			if ok {
				send(fmt.Sprintf("213 %d", len(data)))
			} else {
				send("550 no such file")
			}
		case "QUIT":
			send("221 bye")
			return
		default:
			send("502 not implemented")
		}
	}
}

func (f *fakeOzFTP) saw(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// ftpd layout (kaiHEN 20261114d and later): the SD card is the FTP
// root and /switch exists. Even though this server would accept an
// MKD of the bogus sdmc: path, the client must resolve the plain
// dialect and never send that MKD.
func TestOzDialectFtpd(t *testing.T) {
	f := newFakeOzFTP(t, "/switch")
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if s.prefix != "" {
		t.Fatalf("prefix = %q, want \"\" (ftpd)", s.prefix)
	}
	if f.saw("MKD /sdmc:") {
		t.Fatal("client probed the sdmc: path with MKD")
	}
}

// ftpvita layout (kaiHEN 20261114c): the root lists devices, the SD
// folder is /sdmc:/switch, and a device-less /switch does not exist.
func TestOzDialectFtpvita(t *testing.T) {
	f := newFakeOzFTP(t, "/sdmc:/switch")
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if s.prefix != "/sdmc:" {
		t.Fatalf("prefix = %q, want /sdmc: (ftpvita)", s.prefix)
	}
}

// Fresh card on ftpd: /switch does not exist yet; the client creates
// the plain folder and uses the plain dialect.
func TestOzDialectFreshCard(t *testing.T) {
	f := newFakeOzFTP(t)
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if s.prefix != "" {
		t.Fatalf("prefix = %q, want \"\" (ftpd, fresh card)", s.prefix)
	}
	if !f.saw("MKD /switch") {
		t.Fatal("client did not create the missing /switch")
	}
}

// Install fallback: when the first address (OunceShell, 1337) does
// not answer, the session falls through to the second address (the
// FTPD plugin, 5000), with the plain ftpd dialect.
func TestOzConnectFallbackToPlugin(t *testing.T) {
	plugin := newFakeOzFTP(t, "/switch")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := ln.Addr().String()
	ln.Close()
	s, err := ozConnectAddrs([]string{dead, plugin.ln.Addr().String()}, "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if s.prefix != "" {
		t.Fatalf("prefix = %q, want \"\" (plugin ftpd)", s.prefix)
	}
	if !plugin.saw("USER anonymous") {
		t.Fatal("plugin server was not contacted")
	}
}

// storFile streams a local file to the server byte for byte, and a
// second upload of the same file is skipped by size (the large-extra
// path used for Chromium NX's image).
func TestOzStorFileAndSkip(t *testing.T) {
	f := newFakeOzFTP(t, "/switch")
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	local := filepath.Join(t.TempDir(), "chromium-full.ext4")
	content := make([]byte, 3<<20+123)
	for i := range content {
		content[i] = byte(i * 7)
	}
	if err := os.WriteFile(local, content, 0o644); err != nil {
		t.Fatal(err)
	}
	rel := "switch/chromium-nx/chromium-full.ext4"
	if err := uploadFileSession(s, rel, local, nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := f.files["/switch/chromium-nx/chromium-full.ext4"]
	f.mu.Unlock()
	if len(got) != len(content) {
		t.Fatalf("stored %d bytes, want %d", len(got), len(content))
	}
	for i := range content {
		if got[i] != content[i] {
			t.Fatalf("stored byte %d differs", i)
		}
	}
	if rs, ok := s.remoteSize(rel); !ok || rs != int64(len(content)) {
		t.Fatalf("remoteSize = %d, %v; want %d, true", rs, ok, len(content))
	}
	countStor := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		n := 0
		for _, c := range f.cmds {
			if strings.HasPrefix(c, "STOR ") {
				n++
			}
		}
		return n
	}
	if n := countStor(); n != 1 {
		t.Fatalf("STOR count = %d, want 1", n)
	}
	if err := uploadFileSession(s, rel, local, nil); err != nil {
		t.Fatal(err)
	}
	if n := countStor(); n != 1 {
		t.Fatalf("second upload re-sent the file: STOR count = %d, want 1", n)
	}
}

// When an upload dies on the first server (here: the control
// connection drops mid-STOR and the transfer is never confirmed),
// the file upload retries on the second server and lands there.
func TestOzUploadRetriesOnSecondServer(t *testing.T) {
	bad := newFakeOzFTP(t, "/switch")
	bad.killOnStor = true
	good := newFakeOzFTP(t, "/switch")
	local := filepath.Join(t.TempDir(), "chromium-full.ext4")
	content := []byte(strings.Repeat("x", 65536))
	if err := os.WriteFile(local, content, 0o644); err != nil {
		t.Fatal(err)
	}
	var logs []string
	log := func(s string) { logs = append(logs, s) }
	err := uploadOzFileAddrs([]string{bad.ln.Addr().String(), good.ln.Addr().String()},
		"switch/chromium-nx/chromium-full.ext4", local, "en", log)
	if err != nil {
		t.Fatal(err)
	}
	good.mu.Lock()
	got := good.files["/switch/chromium-nx/chromium-full.ext4"]
	good.mu.Unlock()
	if len(got) != len(content) {
		t.Fatalf("second server stored %d bytes, want %d", len(got), len(content))
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "Retrying on another connection") {
		t.Fatalf("the retry was not logged: %v", logs)
	}
	if !strings.Contains(joined, "transfer not confirmed") {
		t.Fatalf("the raw failure cause was not logged: %v", logs)
	}
}

// When OunceShell answers, the plugin address is never tried.
func TestOzConnectPrefersOunceShell(t *testing.T) {
	oz := newFakeOzFTP(t, "/switch")
	plugin := newFakeOzFTP(t, "/switch")
	s, err := ozConnectAddrs([]string{oz.ln.Addr().String(), plugin.ln.Addr().String()}, "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	plugin.mu.Lock()
	contacted := len(plugin.cmds) > 0
	plugin.mu.Unlock()
	if contacted {
		t.Fatal("plugin was contacted even though OunceShell answered")
	}
}
