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
	"time"
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
				send("550 no such file")
			}
		case "RMD":
			f.mu.Lock()
			delete(f.dirs, arg)
			f.mu.Unlock()
			send("250 removed")
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

// The kaihen/lib sync uploads every library file of the prepared
// dist to /kaihen/lib on the card, skips files already there with
// the same size, and is a no-op for a dist without a library tree.
func TestSyncKaihenLib(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()

	f := newFakeOzFTP(t, "/switch")
	addr := f.ln.Addr().String()

	// No dist at all: no-op, and the server is never contacted.
	if err := syncKaihenLibAddrs([]string{addr}, "en", func(string) {}); err != nil {
		t.Fatalf("sync without dist: %v", err)
	}
	f.mu.Lock()
	contacted := len(f.cmds) > 0
	f.mu.Unlock()
	if contacted {
		t.Fatal("server contacted even though the dist has no lib tree")
	}

	libDir := filepath.Join(kaihenDistDir(), "sdcard", "kaihen", "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("fake libounce bytes")
	if err := os.WriteFile(filepath.Join(libDir, "libounce.so.1"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncKaihenLibAddrs([]string{addr}, "en", func(string) {}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	f.mu.Lock()
	got := f.files["/kaihen/lib/libounce.so.1"]
	stors := 0
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "STOR ") {
			stors++
		}
	}
	f.mu.Unlock()
	if string(got) != string(content) {
		t.Fatalf("synced file = %q, want %q", got, content)
	}
	if stors != 1 {
		t.Fatalf("STOR count = %d, want 1", stors)
	}
	// Second sync: same size on the card, nothing is re-sent.
	if err := syncKaihenLibAddrs([]string{addr}, "en", func(string) {}); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	f.mu.Lock()
	stors = 0
	for _, c := range f.cmds {
		if strings.HasPrefix(c, "STOR ") {
			stors++
		}
	}
	f.mu.Unlock()
	if stors != 1 {
		t.Fatalf("second sync re-sent the file: STOR count = %d, want 1", stors)
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
	// Since the parallel-probe change (build 20261128-0235) both
	// servers may be logged into briefly; the contract that matters
	// is that the preferred OunceShell session is the one returned.
	if s.addr != oz.ln.Addr().String() {
		t.Fatalf("picked %s, want OunceShell %s", s.addr, oz.ln.Addr().String())
	}
}

func TestOzConnectAddrsParallel(t *testing.T) {
	live := newFakeOzFTP(t, "/switch")
	liveAddr := live.ln.Addr().String()
	// The preferred address never answers (an unroutable IP: the
	// dial hangs until its timeout). The fallback must be picked
	// after the grace period, not after the full dial timeout.
	start := time.Now()
	s, err := ozConnectAddrs([]string{"10.255.255.1:1337", liveAddr}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if s.addr != liveAddr {
		t.Fatalf("picked %s, want the live fallback %s", s.addr, liveAddr)
	}
	s.close()
	if el := time.Since(start); el > 8*time.Second {
		t.Fatalf("fallback took %v; the parallel probe did not help", el)
	}
	// When both answer, the preferred address wins.
	live2 := newFakeOzFTP(t, "/switch")
	s2, err := ozConnectAddrs([]string{liveAddr, live2.ln.Addr().String()}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if s2.addr != liveAddr {
		t.Fatalf("picked %s, want the preferred %s", s2.addr, liveAddr)
	}
	s2.close()
}

func TestPruneEngineRuns(t *testing.T) {
	oldDir := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = oldDir }()
	runs := filepath.Join(exeDir, "assets", "kaihen", "engine", "runs")
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 8; i++ {
		d := filepath.Join(runs, "run"+string(rune('a'+i)))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mt := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(d, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	pruneEngineRuns()
	ents, err := os.ReadDir(runs)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != engineRunsKeep {
		t.Fatalf("%d run folders left, want %d", len(ents), engineRunsKeep)
	}
	// The survivors are the newest five (d..h of a..h).
	for _, e := range ents {
		if e.Name() < "rund" {
			t.Fatalf("old run folder %s survived", e.Name())
		}
	}
}

// Reinstalling a file that is already on the SD card with the same
// size must not upload it again; a different size must.
func TestStorSkipSameSize(t *testing.T) {
	f := newFakeOzFTP(t, "/switch")
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	data := []byte("hello nro")
	if err := storSkip(s, "switch/test.nro", data, nil); err != nil {
		t.Fatal(err)
	}
	if !f.saw("STOR") {
		t.Fatal("first upload did not STOR")
	}
	f.mu.Lock()
	f.cmds = nil
	f.mu.Unlock()
	if err := storSkip(s, "switch/test.nro", data, nil); err != nil {
		t.Fatal(err)
	}
	if f.saw("STOR") {
		t.Fatal("same-size reinstall uploaded again")
	}
	if err := storSkip(s, "switch/test.nro", []byte("different size!"), nil); err != nil {
		t.Fatal(err)
	}
	if !f.saw("STOR") {
		t.Fatal("changed file was not uploaded")
	}
}

// Deleting a zip-tree entry from the console removes every file of
// the tree (and only those).
func TestDeleteAppFilesTree(t *testing.T) {
	f := newFakeOzFTP(t, "/switch", "/switch/app", "/kaihen", "/kaihen/plugins")
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "switch", "app"), 0o755)
	os.WriteFile(filepath.Join(root, "switch", "app", "app.nro"), []byte("nro"), 0o644)
	os.WriteFile(filepath.Join(root, "switch", "app", "data.bin"), []byte("data"), 0o644)
	f.mu.Lock()
	f.files["/switch/app/app.nro"] = []byte("nro")
	f.files["/switch/app/data.bin"] = []byte("data")
	f.files["/switch/other.nro"] = []byte("keep")
	f.mu.Unlock()
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	a := &appEntry{ID: "x/y", Source: "zip"}
	deleted, err := deleteAppFilesSession(s, a, root)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("nothing reported deleted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files["/switch/app/app.nro"]; ok {
		t.Fatal("tree file still present")
	}
	if _, ok := f.files["/switch/other.nro"]; !ok {
		t.Fatal("unrelated file was deleted")
	}
}

// Deleting an entry with a fixed install path removes that path.
func TestDeleteAppFilesInstallPath(t *testing.T) {
	f := newFakeOzFTP(t, "/switch", "/switch/PKForge")
	f.mu.Lock()
	f.files["/switch/PKForge/PKForge.nro"] = []byte("nro")
	f.mu.Unlock()
	s, err := ozConnectAddr(f.ln.Addr().String(), "en")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	a := &appEntry{ID: "x/y", AssetName: "PKForge-AIO.nro", InstallPath: "PKForge/PKForge.nro"}
	deleted, err := deleteAppFilesSession(s, a, "")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("nothing reported deleted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files["/switch/PKForge/PKForge.nro"]; ok {
		t.Fatal("install-path file still present")
	}
}
