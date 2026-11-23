package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// --- kaiHEN ------------------------------------------------------------------
//
// kaiHEN (ouncebrew/kaihen) is a homebrew enabler for Switch 2. It is started
// from the PC by the CivSwitch Kit engine (mza1010/civswitch-kit on Gitee,
// v1.1.0; GPL-2.0 — a CivSwitch + ns2jb port whose port/ tree is bundled
// verbatim under assets/kaihen). This handler mirrors the FS/OunceCake
// flows: the kaiHEN release zip is lazy-downloaded once with a pinned
// SHA-256 and extracted beside the exe, then
// `node hen.cjs --dir <dist> <pc-ip>` runs as a long-lived SSE job.
//
// The kit's engine only listens on <pc-ip>:39380; pushing the bridge to the
// loader is a separate step in the kit (its save-tools/send_js.py), so this
// handler does it itself, mirroring the loader protocol: once the engine
// prints it is waiting, it fills the PC address into civ_bridge.js, sends
// it to the loader on the console at :9026, half-closes, and streams the
// loader's log lines back into the job log. The bridge connects out to the
// engine, which acquires the FS session, installs kaiHEN's RO image, and
// starts HEN ("HEN PID ..." is the success marker). The engine keeps
// holding the session until Civilization VII closes; HEN itself then stays
// resident until the console restarts. Acquisition retries live inside the
// kit engine (up to 3 attempts with same-boot canary re-rolls), so there is
// deliberately no external fail-fast watchdog: a run ends when the engine
// exits, when HEN startup reports failure, or when the page goes away.

const (
	// Pinned to 20261117.1. Its hbmenu carries the official fix for
	// the textInit() 2345-0011 failure (since 20261116, hbmenu no
	// longer looks up the system language at startup), so the dist
	// is used exactly as published, with no local modification. The
	// engine validates only that the dist files exist (no version
	// or hash check); the layout is unchanged from earlier releases.
	kaihenTag  = "20261117.1"
	kaihenURL  = "https://github.com/ouncebrew/kaihen/releases/download/20261117.1/kaiHEN-20261117.1.zip"
	kaihenSize = 12347885
	kaihenSHA  = "3f7ded0b360b2f0bf97f8e958c3bfe93a8d76dd8d43ce2e8f7d060e647ad0978"
)

var (
	khMu      sync.Mutex
	khRunning bool
)

func kaihenDistDir() string { return pathOf("kaihen") }

func kaihenReady() bool {
	root := kaihenDistDir()
	tag, err := os.ReadFile(filepath.Join(root, ".kaihen-tag"))
	if err != nil || strings.TrimSpace(string(tag)) != kaihenTag {
		return false
	}
	for _, rel := range []string{"bootstrap.bin", "hbmenu.nro",
		filepath.Join("ro", "main"), filepath.Join("hen", "main"),
		filepath.Join("game", "main"), filepath.Join("programs", "ounceshell.nro")} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			return false
		}
	}
	return true
}

func ensureKaihen(log logFn, lang string) error {
	if kaihenReady() {
		return nil
	}
	log(tr(lang, "khDl"))
	raw, err := httpGet(kaihenURL)
	if err != nil {
		return errors.New(tr(lang, "khDlFail"))
	}
	if len(raw) != kaihenSize || sha256Hex(raw) != kaihenSHA {
		return errors.New(tr(lang, "badContent"))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return errors.New(tr(lang, "zipErr"))
	}
	root := kaihenDistDir()
	// Drop any previous distribution so stale files cannot survive an update.
	for _, name := range []string{"bootstrap.bin", "hbmenu.nro", "ro", "hen",
		"game", "programs", "licenses", ".kaihen-tag"} {
		os.RemoveAll(filepath.Join(root, name))
	}
	for _, f := range zr.File {
		rel := filepath.Clean(f.Name)
		if rel == "." || strings.HasPrefix(rel, "..") {
			continue
		}
		dst := filepath.Join(root, rel)
		if f.FileInfo().IsDir() {
			os.MkdirAll(dst, 0o755)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return errors.New(tr(lang, "zipErr"))
		}
		var buf bytes.Buffer
		_, copyErr := buf.ReadFrom(rc)
		rc.Close()
		if copyErr != nil {
			return errors.New(tr(lang, "zipErr"))
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".kaihen-tag"),
		[]byte(kaihenTag+"\n"), 0o644); err != nil {
		return err
	}
	if !kaihenReady() {
		return errors.New(tr(lang, "missingReq"))
	}
	log(tr(lang, "khDlDone"))
	return nil
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
		return
	}
	if killProcGroup(cmd.Process.Pid) {
		return
	}
	cmd.Process.Kill()
}

func kaihenStartHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IP   string `json:"ip"`
		Lang string `json:"lang"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	ip, lang := body.IP, body.Lang
	if ip == "" {
		ip = r.URL.Query().Get("ip")
	}
	if lang == "" {
		lang = r.URL.Query().Get("lang")
	}
	if !validIP(ip) {
		http.Error(w, "invalid ip", http.StatusBadRequest)
		return
	}
	khMu.Lock()
	if khRunning {
		khMu.Unlock()
		http.Error(w, "busy", http.StatusConflict)
		return
	}
	khRunning = true
	khMu.Unlock()
	defer func() {
		khMu.Lock()
		khRunning = false
		khMu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fl, _ := w.(http.Flusher)
	var sendMu sync.Mutex
	rawSend := func(s string) {
		sendMu.Lock()
		defer sendMu.Unlock()
		for _, line := range strings.Split(s, "\n") {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		if fl != nil {
			fl.Flush()
		}
	}
	// send is what producer goroutines (the engine output scan and the
	// bridge log stream) write through. Once the handler reaches its
	// final result it flips sendDead, and late lines are dropped instead
	// of being written into a completed HTTP response — a write like
	// that races the server's connection reuse and crashes the process
	// (a failed run followed by one more loader log line was enough).
	var sendDead atomic.Bool
	send := func(s string) {
		if sendDead.Load() {
			return
		}
		rawSend(s)
	}
	fail := func(err error) {
		rawSend("RESULT|ng")
		rawSend(tr(lang, "failed") + err.Error())
	}

	if err := ensureKaihen(send, lang); err != nil {
		fail(err)
		return
	}
	script := pathOf("assets", "kaihen", "hen.cjs")
	if _, err := os.Stat(script); err != nil {
		fail(errors.New(tr(lang, "missingReq")))
		return
	}
	// The kit engine listens on this PC and the console connects out to
	// it, so it needs the PC address the console can reach: the source
	// address the kernel picks for the route to the console.
	pcIP := localAddressFor(ip)
	if pcIP == "" {
		fail(errors.New(tr(lang, "connErr")))
		return
	}
	cmd := exec.Command(nodeExe(), script, "--dir", kaihenDistDir(), pcIP)
	setProcGroup(cmd)
	cmd.Dir = pathOf("assets", "kaihen")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fail(err)
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		fail(err)
		return
	}
	runStart := time.Now()
	// Closing the page (or the app) ends the launcher: hen.cjs releases
	// the console side when its child gets the signal, and killing the
	// tree is the Windows equivalent.
	go func() {
		<-r.Context().Done()
		killProcessTree(cmd)
	}()
	done := make(chan struct{})
	pushDone := make(chan struct{})
	var readySent atomic.Bool
	var failSeen atomic.Bool
	var pushStarted atomic.Bool
	var pushFailed atomic.Bool
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanLines(stdout, func(line string) {
			if strings.Contains(line, "Waiting for ") && strings.Contains(line, ":39380") &&
				pushStarted.CompareAndSwap(false, true) {
				// The engine is listening; push the bridge to the loader.
				go func() {
					defer close(pushDone)
					if err := pushBridge(send, ip, pcIP, done); err != nil {
						select {
						case <-done:
							// The run ended first; its own result stands.
						default:
							pushFailed.Store(true)
							killProcessTree(cmd)
						}
					}
				}()
			}
			if (strings.Contains(line, "HEN PID ") || strings.Contains(line, "HEN launch completed!")) &&
				readySent.CompareAndSwap(false, true) {
				send("HENREADY|ok")
			}
			if strings.Contains(line, "HEN startup failed") && failSeen.CompareAndSwap(false, true) {
				// hen.cjs now waits with the held RPC live; give its
				// diagnostics a moment, then end the run as failed.
				go func() {
					time.Sleep(10 * time.Second)
					killProcessTree(cmd)
				}()
			}
			send(line)
		})
	}()
	// hen.cjs spawns the engine with its stderr inherited, so a surviving
	// grandchild can hold the output pipe open after hen.cjs itself has
	// exited. Wait for whichever finishes first: the output stream
	// closing (the whole tree is done) or the process exiting.
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waitCh:
		select {
		case <-scanDone:
		case <-time.After(2 * time.Second):
		}
	case <-scanDone:
		waitErr = <-waitCh
	}
	// The run is over, but the bridge connection to the loader can
	// outlive it: the loader keeps the log stream open, and its
	// goroutine writes into this response. Close the run out, wait for
	// that goroutine to finish, and only then write the result — a
	// late write into a completed response crashes the process.
	close(done)
	if pushStarted.Load() {
		select {
		case <-pushDone:
		case <-time.After(3 * time.Second):
		}
	}
	sendDead.Store(true)
	if pushFailed.Load() {
		// The bridge never reached the loader on the console.
		rawSend("FAILREASON|noloader")
		fail(errors.New(tr(lang, "connErr")))
		return
	}
	if waitErr != nil && readySent.Load() {
		// HEN already started; a later cleanup error in the engine must
		// not turn the run into a failure (the kit's patch 33 makes the
		// same call inside start-hen). Keep it visible in the log only.
		rawSend(waitErr.Error())
		waitErr = nil
	}
	if waitErr == nil {
		rawSend("RESULT|ok")
		rawSend(tr(lang, "done"))
	} else {
		reason := "other"
		if e := latestRunError(runStart); e != "" {
			reason = classifyRunError(e)
		}
		rawSend("FAILREASON|" + reason)
		rawSend("RESULT|ng")
		rawSend(tr(lang, "failed") + waitErr.Error())
	}
}

// classifyRunError maps the engine's result.json error text to a short
// reason code the page turns into a localized explanation. The kit
// README (§2-5) sorts failures into the same buckets: a dropped session,
// and the timing lottery (canary miss, a batch that never terminated,
// heap drift) — all retried the same way, but worth telling apart so the
// user can see a plain lottery miss for what it is. "disconnect" is
// checked first because the disconnect error's text also mentions the
// acquisition log path.
func classifyRunError(errText string) string {
	e := strings.ToLower(errText)
	switch {
	case strings.Contains(e, "disconnect"):
		return "disconn"
	case strings.Contains(e, "canary"),
		strings.Contains(e, "timed out before memory was ready"),
		strings.Contains(e, "did not terminate"),
		strings.Contains(e, "allocation changed"),
		strings.Contains(e, "acquisition"):
		return "race"
	default:
		return "other"
	}
}

// latestRunError returns the error text from the newest engine run
// folder written since the given time, or "" when there is none. Each
// run leaves engine/runs/switch2-*/result.json behind.
func latestRunError(since time.Time) string {
	return latestRunErrorIn(pathOf("assets", "kaihen", "engine", "runs"), since)
}

func latestRunErrorIn(runsDir string, since time.Time) string {
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, en := range entries {
		if !en.IsDir() || !strings.HasPrefix(en.Name(), "switch2-") {
			continue
		}
		rj := filepath.Join(runsDir, en.Name(), "result.json")
		fi, err := os.Stat(rj)
		if err != nil || fi.ModTime().Before(since.Add(-2*time.Minute)) || fi.ModTime().Before(bestTime) {
			continue
		}
		best, bestTime = rj, fi.ModTime()
	}
	if best == "" {
		return ""
	}
	data, err := os.ReadFile(best)
	if err != nil {
		return ""
	}
	var rec struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &rec) != nil {
		return ""
	}
	return rec.Error
}

// localAddressFor returns the local IPv4 address used to reach the given
// console address: a connected UDP socket reveals the source address the
// kernel picks for the route, without sending anything. Falls back to the
// only non-internal IPv4 interface address, or "" when there is no usable
// (or no unambiguous) address.
func localAddressFor(consoleIP string) string {
	if conn, err := net.Dial("udp", net.JoinHostPort(consoleIP, "9026")); err == nil {
		addr, ok := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if ok && addr.IP.To4() != nil {
			return addr.IP.String()
		}
	}
	var found string
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.To4() == nil || ip.IsLinkLocalUnicast() {
					continue
				}
				if found != "" {
					return ""
				}
				found = ip.String()
			}
		}
	}
	return found
}

var bridgePCIPPattern = regexp.MustCompile(`PC_IP:\s*'[^']*'`)

// pushBridge sends the kit's civ_bridge.js to the CivSwitch loader on the
// console (TCP :9026) with the PC address filled in, then streams the
// loader's log output into the job log until the loader closes the
// connection. It mirrors the kit's save-tools/send_js.py step (send the
// payload as-is, half-close, read the log stream). The dial is retried
// for up to 90 seconds because the loader may still be starting; done is
// closed when the run ends and aborts the retries. The loader keeps the
// log connection open for its whole lifetime, which outlasts the run, so
// done also closes the connection: the read must not still be running
// (writing into the response) after the handler has finished.
func pushBridge(send logFn, consoleIP, pcIP string, done <-chan struct{}) error {
	raw, err := os.ReadFile(pathOf("assets", "kaihen", "civ_bridge.js"))
	if err != nil {
		return err
	}
	if !bridgePCIPPattern.Match(raw) {
		return errors.New("civ_bridge.js has no PC_IP entry to fill in")
	}
	code := bridgePCIPPattern.ReplaceAll(raw, []byte("PC_IP: '"+pcIP+"'"))
	deadline := time.Now().Add(90 * time.Second)
	var conn net.Conn
	for {
		conn, err = lanDialTimeout("tcp", net.JoinHostPort(consoleIP, "9026"), 5*time.Second)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-done:
			return errors.New("run ended before the bridge could be sent")
		case <-time.After(2 * time.Second):
		}
	}
	defer conn.Close()
	if _, err := conn.Write(code); err != nil {
		return err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-done:
			conn.Close()
		case <-stop:
		}
	}()
	scanLines(conn, func(line string) { send(line) })
	return nil
}

// --- kaiHEN homebrew install over FTP ----------------------------------------
//
// OunceShell (staged into the SD card's /switch folder by the launcher)
// carries an FTP server that the user starts from its file browser with
// SELECT (the - button). It listens on port 1337 and accepts any login.
// The path dialect depends on the kaiHEN release: 20261114c ships ftpvita,
// whose root lists devices, so the SD folder is /sdmc:/switch; 20261114d
// ships ftpd, which serves the SD card at its root, so the folder is
// /switch. Each session resolves which dialect answers by CWD at
// connect time (see ozConnect). .nro files stored there are listed by
// hbmenu, which scans sdmc:/switch.
//
// Installs can also go through the FTPD plugin (port 5000, an ftpd with
// the SD card at its root), the same pair the gallery offers: ozConnect
// tries OunceShell first, then the plugin.

const ozFtpPort = 1337
const ozPluginPort = 5000

// ozSession is one control connection to OunceShell's FTP server with the
// SD-card path dialect resolved: ftpvita (kaiHEN 20261114c) lists devices at
// its root, so SD paths start with "/sdmc:"; ftpd (20261114d) serves the SD
// card at its root, so paths have no prefix.
type ozSession struct {
	ctrl   net.Conn
	rd     *ftpReader
	prefix string // "/sdmc:" or ""
	lang   string
	addr   string // the address this session connected to
}

// ozConnect opens an install session to the console: OunceShell's FTP
// (port 1337) first, then the FTPD plugin (port 5000), mirroring the
// gallery's auto source. When both fail, the OunceShell error is the
// one reported.
func ozConnect(ip, lang string) (*ozSession, error) {
	return ozConnectAddrs([]string{
		net.JoinHostPort(ip, strconv.Itoa(ozFtpPort)),
		net.JoinHostPort(ip, strconv.Itoa(ozPluginPort)),
	}, lang)
}

func ozConnectAddrs(addrs []string, lang string) (*ozSession, error) {
	var firstErr error
	for i, addr := range addrs {
		s, err := ozConnectAddr(addr, lang)
		if err == nil {
			return s, nil
		}
		if i == 0 {
			firstErr = err
		}
	}
	return nil, firstErr
}

func ozConnectAddr(addr, lang string) (*ozSession, error) {
	ctrl, err := lanDialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, errors.New(tr(lang, "khFtpConn"))
	}
	s := &ozSession{ctrl: ctrl, lang: lang, addr: addr}
	ctrl.SetDeadline(time.Now().Add(600 * time.Second))
	s.rd = newFTPReader(ctrl)
	if code, _ := s.rd.readReply(); code != 220 {
		ctrl.Close()
		return nil, errors.New(tr(lang, "khFtpConn"))
	}
	code, _ := s.cmd("USER anonymous")
	if code == 331 {
		code, _ = s.cmd("PASS guest@")
	}
	if code != 230 && code != 202 {
		ctrl.Close()
		return nil, errors.New(tr(lang, "khFtpConn"))
	}
	s.cmd("TYPE I")
	// Resolve the dialect by CWD alone, plain /switch first: it is
	// the current (ftpd) layout, and on ftpvita a device-less path
	// simply fails, so the sdmc: probe still gets its turn. Never
	// probe the sdmc: path with MKD: on ftpd that could create a
	// real folder literally named "sdmc:" on the card and make the
	// probe pick the wrong dialect, stranding uploads where hbmenu
	// never looks. MKD is used only to create a missing /switch on
	// a fresh card.
	if code, _ := s.cmd("CWD /switch"); code == 250 {
		s.prefix = ""
		return s, nil
	}
	if code, _ := s.cmd("CWD /sdmc:/switch"); code == 250 {
		s.prefix = "/sdmc:"
		return s, nil
	}
	if code, _ := s.cmd("MKD /switch"); code/100 == 2 {
		if code, _ := s.cmd("CWD /switch"); code == 250 {
			s.prefix = ""
			return s, nil
		}
	}
	ctrl.Close()
	return nil, errors.New(tr(lang, "ftpErr"))
}

func (s *ozSession) cmd(c string) (int, string) {
	fmt.Fprintf(s.ctrl, "%s\r\n", c)
	return s.rd.readReply()
}

func (s *ozSession) close() {
	s.cmd("QUIT")
	s.ctrl.Close()
}

// ensureDir makes sure prefix/rel exists, creating one path segment at a
// time (neither FTP dialect creates nested paths in one MKD).
func (s *ozSession) ensureDir(rel string) error {
	if rel == "" || rel == "." {
		return nil
	}
	cur := s.prefix
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." {
			continue
		}
		cur += "/" + seg
		if code, _ := s.cmd("CWD " + cur); code == 250 {
			continue
		}
		if code, _ := s.cmd("MKD " + cur); code/100 != 2 {
			return errors.New(tr(s.lang, "ftpErr"))
		}
		if code, _ := s.cmd("CWD " + cur); code != 250 {
			return errors.New(tr(s.lang, "ftpErr"))
		}
	}
	return nil
}

// openStore prepares an upload to prefix/rel: it creates the parent
// folders, negotiates a passive data connection and issues STOR. The
// caller writes the content to the returned connection and finishes
// with finishStore.
func (s *ozSession) openStore(rel string) (net.Conn, error) {
	// A large upload can take far longer than the connect-time
	// control deadline (600s); give the transfer an hour.
	s.ctrl.SetDeadline(time.Now().Add(3600 * time.Second))
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if dir := pathDir(rel); dir != "" {
		if err := s.ensureDir(dir); err != nil {
			return nil, err
		}
	}
	code, line := s.cmd("PASV")
	if code != 227 {
		return nil, fmt.Errorf("PASV rejected: %d %s", code, line)
	}
	start := strings.Index(line, "(")
	end := strings.Index(line, ")")
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("bad PASV reply: %s", line)
	}
	parts := strings.Split(line[start+1:end], ",")
	if len(parts) != 6 {
		return nil, fmt.Errorf("bad PASV reply: %s", line)
	}
	nums := make([]int, 6)
	for i, p := range parts {
		fmt.Sscanf(strings.TrimSpace(p), "%d", &nums[i])
	}
	host, _, _ := net.SplitHostPort(s.ctrl.RemoteAddr().String())
	dc, err := lanDialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(nums[4]<<8|nums[5])), 10*time.Second)
	if err != nil {
		return nil, err
	}
	if code, line := s.cmd("STOR " + s.prefix + "/" + rel); code != 150 && code != 125 {
		dc.Close()
		return nil, fmt.Errorf("STOR rejected: %d %s", code, line)
	}
	return dc, nil
}

func (s *ozSession) finishStore(dc net.Conn) error {
	dc.Close()
	if code, line := s.rd.readReply(); code != 226 && code != 250 {
		return fmt.Errorf("transfer not confirmed: %d %s", code, line)
	}
	return nil
}

// stor uploads data to prefix/rel, creating parent folders as needed.
func (s *ozSession) stor(rel string, data []byte) error {
	dc, err := s.openStore(rel)
	if err != nil {
		return errors.New(tr(s.lang, "ftpErr"))
	}
	if _, err := dc.Write(data); err != nil {
		dc.Close()
		return errors.New(tr(s.lang, "ftpErr"))
	}
	if err := s.finishStore(dc); err != nil {
		return errors.New(tr(s.lang, "ftpErr"))
	}
	return nil
}

// storFile uploads a local file to prefix/rel, streaming it in chunks
// (release extras can be hundreds of MB), reporting progress as
// (sent, total) after each chunk.
func (s *ozSession) storFile(rel, local string, progress func(sent, total int64)) error {
	f, err := os.Open(local)
	if err != nil {
		return errors.New(tr(s.lang, "ftpErr"))
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return errors.New(tr(s.lang, "ftpErr"))
	}
	dc, err := s.openStore(rel)
	if err != nil {
		return err
	}
	buf := make([]byte, 1<<20)
	var sent int64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := dc.Write(buf[:n]); werr != nil {
				dc.Close()
				return fmt.Errorf("data write at %d/%d bytes: %w", sent, info.Size(), werr)
			}
			sent += int64(n)
			if progress != nil {
				progress(sent, info.Size())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			dc.Close()
			return fmt.Errorf("local read: %w", rerr)
		}
	}
	return s.finishStore(dc)
}

// remoteSize reports the size of prefix/rel via SIZE, when the server
// knows the file.
func (s *ozSession) remoteSize(rel string) (int64, bool) {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	code, line := s.cmd("SIZE " + s.prefix + "/" + rel)
	if code != 213 {
		return 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// ftpUploadOzFile uploads one local file to an SD-relative path on
// the console. It tries OunceShell (1337) first and the FTPD plugin
// (5000) second — not just at connect time: if an upload fails
// partway on one server, it retries on the other, because a large
// file is exactly where one server may drop the transfer. The raw
// cause of each failure goes to the log; the returned error stays
// the plain user-facing one.
func ftpUploadOzFile(ip, rel, local, lang string, log logFn) error {
	return uploadOzFileAddrs([]string{
		net.JoinHostPort(ip, strconv.Itoa(ozFtpPort)),
		net.JoinHostPort(ip, strconv.Itoa(ozPluginPort)),
	}, rel, local, lang, log)
}

func uploadOzFileAddrs(addrs []string, rel, local, lang string, log logFn) error {
	var lastErr error
	for i, addr := range addrs {
		s, err := ozConnectAddr(addr, lang)
		if err != nil {
			lastErr = err
			continue
		}
		if log != nil {
			log("FTP: " + addr)
		}
		err = uploadFileSession(s, rel, local, log)
		s.close()
		if err == nil {
			return nil
		}
		lastErr = err
		if log != nil {
			log("FTP: " + err.Error())
			if i+1 < len(addrs) {
				log(tr(lang, "extraRetry"))
			}
		}
	}
	if lastErr != nil {
		return errors.New(tr(lang, "ftpErr"))
	}
	return errors.New(tr(lang, "ftpErr"))
}

// uploadFileSession stores local at rel over an open session. A
// remote file that already has the same size is left as is, so
// reinstalling an entry with a large extra file stays fast.
func uploadFileSession(s *ozSession, rel, local string, log logFn) error {
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	if rs, ok := s.remoteSize(rel); ok && rs == info.Size() {
		if log != nil {
			log(tr(s.lang, "extraSkip", rel))
		}
		return nil
	}
	lastStep := -1
	return s.storFile(rel, local, func(sent, total int64) {
		if log == nil || total <= 0 {
			return
		}
		if step := int(sent * 10 / total); step != lastStep {
			lastStep = step
			log(fmt.Sprintf("%s: %d%%", rel, sent*100/total))
		}
	})
}

func pathDir(rel string) string {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return ""
	}
	return rel[:i]
}

// ftpUploadOz uploads one .nro into the console's switch folder through
// OunceShell's FTP server and returns the remote folder used.
func ftpUploadOz(ip, name string, data []byte, lang string) (string, error) {
	s, err := ozConnect(ip, lang)
	if err != nil {
		return "", err
	}
	defer s.close()
	if err := s.stor("switch/"+name, data); err != nil {
		return "", err
	}
	return s.prefix + "/switch", nil
}

// ftpUploadOzTree uploads every file under rootDir to the SD card,
// preserving the relative paths (a release zip extracted to the SD root:
// switch/..., kaihen/plugins/..., and so on).
func ftpUploadOzTree(ip, rootDir string, lang string, log logFn) error {
	s, err := ozConnect(ip, lang)
	if err != nil {
		return err
	}
	defer s.close()
	return filepath.Walk(rootDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if log != nil {
			log(rel)
		}
		return s.stor(rel, data)
	})
}

// ftpDeleteOz deletes one file from the console: the switch folder first,
// then the kaiHEN plugins folder. It reports whether anything was deleted.
func ftpDeleteOz(ip, name string, lang string) (bool, error) {
	s, err := ozConnect(ip, lang)
	if err != nil {
		return false, err
	}
	defer s.close()
	for _, rel := range []string{"switch/" + name, "kaihen/plugins/" + name} {
		if code, _ := s.cmd("DELE " + s.prefix + "/" + rel); code == 250 {
			return true, nil
		}
	}
	return false, nil
}

// ftpUploadOzPath uploads one file to a path relative to the console's
// switch folder, creating folders as needed, and returns the remote
// folder used.
func ftpUploadOzPath(ip, rel string, data []byte, lang string) (string, error) {
	s, err := ozConnect(ip, lang)
	if err != nil {
		return "", err
	}
	defer s.close()
	if err := s.stor("switch/"+rel, data); err != nil {
		return "", err
	}
	return s.prefix + "/switch", nil
}

// nroFtpHandler receives one file as the raw request body and stores it
// under the console's switch folder through OunceShell's FTP server.
// The name parameter is the file's relative path: a plain name for a
// loose file, or folder/name when a whole folder was picked or dropped
// (homebrew often needs its sibling files next to the .nro).
func nroFtpHandler(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	lang := r.URL.Query().Get("lang")
	if !validIP(ip) {
		jsonOK(w, map[string]any{"ok": false, "error": tr(lang, "ipInvalid")})
		return
	}
	rel := strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(r.URL.Query().Get("name"))), "/")
	if rel == "" || rel == "." {
		jsonOK(w, map[string]any{"ok": false, "error": tr(lang, "khBadNro")})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (64<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 64<<20 {
		jsonOK(w, map[string]any{"ok": false, "error": tr(lang, "khBadNro")})
		return
	}
	dir, err := ftpUploadOzPath(ip, rel, data, lang)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true, "dir": dir})
}
