package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func pageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, strings.ReplaceAll(pageHTML, "__BUILDSTAMP__", buildStamp))
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func pingHost(ip string) bool {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("ping", "-n", "1", "-w", "2000", ip)
	} else {
		cmd = exec.Command("ping", "-c", "1", "-W", "2", ip)
	}
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "ttl=")
}

func checkHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IP   string `json:"ip"`
		Lang string `json:"lang"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if !validIP(body.IP) {
		jsonOK(w, map[string]any{"ok": false, "conn": false, "ping": false, "ftp": false, "detail": tr(body.Lang, "ipInvalid")})
		return
	}
	// The three probes are independent, so they run in parallel: one
	// after another, their timeouts stacked up and made the check
	// take seconds to report a console that is simply off.
	// Homebrew installs talk to OunceShell's FTP server (port 1337),
	// falling back to the FTPD plugin (port 5000); the homebrew page
	// reports whether either is listening.
	var ftp, ok, ping bool
	var why string
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for _, port := range []string{"1337", "5000"} {
			if c, err := lanDialTimeout("tcp", net.JoinHostPort(body.IP, port), 1500*time.Millisecond); err == nil {
				c.Close()
				ftp = true
				break
			}
		}
	}()
	go func() {
		defer wg.Done()
		ok, why = probeLoader(body.IP)
	}()
	go func() {
		defer wg.Done()
		ping = pingHost(body.IP)
	}()
	wg.Wait()
	detail := ""
	if !ok {
		switch {
		case strings.HasPrefix(why, "err:"):
			detail = tr(body.Lang, "chkDetailErr", strings.TrimPrefix(why, "err:"))
		case why == "noreply":
			detail = tr(body.Lang, "chkDetailNone")
		case strings.HasPrefix(why, "mismatch:"):
			detail = tr(body.Lang, "chkDetailDiff", strings.TrimPrefix(why, "mismatch:"))
		}
	}
	jsonOK(w, map[string]any{"ok": ok, "conn": ok, "ping": ping, "ftp": ftp, "detail": detail})
}

func quitHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"ok": true})
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
}

func openFolderHandler(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	var dir string
	switch body.Name {
	case "pico":
		dir = pathOf("pico")
	case "pico-jp":
		dir = pathOf("pico", "jp")
	case "pico-en":
		dir = pathOf("pico", "en")
	case "autoloader-sd":
		dir = pathOf("autoloader-sd")
	default:
		http.Error(w, "bad folder", http.StatusBadRequest)
		return
	}
	os.MkdirAll(dir, 0o755)
	mirrorFolderExternal(dir)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	cmd.Start()
	jsonOK(w, map[string]any{"ok": true})
}

// --- ipconfig ---------------------------------------------------------------

func ipconfigText() (string, error) {
	if f := os.Getenv("CIVINSTALLER_IPCONFIG_FILE"); f != "" {
		b, err := os.ReadFile(f)
		return string(b), err
	}
	if runtime.GOOS != "windows" {
		return "", errors.New("windows only")
	}
	out, err := exec.Command("cmd", "/c", "chcp 65001 >nul & ipconfig /all").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseIPConfig(text string) (gw, cidr, ip, mask string) {
	var curIP, curMask, curGW string
	flush := func() (string, string, string, string, bool) {
		if curIP != "" && curGW != "" && curMask != "" {
			return curGW, cidrOf(curIP, curMask), curIP, curMask, true
		}
		return "", "", "", "", false
	}
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		low := strings.ToLower(t)
		isField := strings.Contains(t, ":")
		if !isField {
			if g, c, i2, m, ok := flush(); ok {
				return g, c, i2, m
			}
			curIP, curMask, curGW = "", "", ""
			continue
		}
		val := strings.TrimSpace(t[strings.Index(t, ":")+1:])
		val = strings.TrimSpace(strings.TrimSuffix(val, "(優先)"))
		val = strings.TrimSpace(strings.TrimSuffix(val, "(Preferred)"))
		switch {
		case strings.Contains(t, "IPv4") || strings.Contains(low, "ipv4 address"):
			if ip2 := net.ParseIP(val); ip2 != nil && curIP == "" {
				curIP = val
			}
		case strings.Contains(t, "マスク") || strings.Contains(t, "Subnet Mask") || strings.Contains(low, "subnet mask"):
			if net.ParseIP(val) != nil {
				curMask = val
			}
		case strings.Contains(t, "ゲートウェイ") || strings.Contains(low, "default gateway"):
			if net.ParseIP(val) != nil && curGW == "" {
				curGW = val
			}
		}
	}
	if g, c, i2, m, ok := flush(); ok {
		return g, c, i2, m
	}
	return "", "", "", ""
}

func cidrOf(ip, mask string) string {
	ip4 := net.ParseIP(ip).To4()
	m4 := net.ParseIP(mask).To4()
	if ip4 == nil || m4 == nil {
		return ""
	}
	ones, _ := net.IPMask(m4).Size()
	netIP := ip4.Mask(net.IPMask(m4))
	return fmt.Sprintf("%s/%d", netIP.String(), ones)
}

func ipconfigHandler(w http.ResponseWriter, r *http.Request) {
	text, err := ipconfigText()
	if err != nil {
		jsonOK(w, map[string]any{"error": "ipconfig failed"})
		return
	}
	gw, cidr, ip, mask := parseIPConfig(text)
	jsonOK(w, map[string]any{"gateway": gw, "cidr": cidr, "ip": ip, "mask": mask})
}

// --- updates ----------------------------------------------------------------

// dnsServers lists the DNS servers named by KAIHEN_DNS. The Android
// shell sets it from the phone's active network: Android has no
// /etc/resolv.conf, so Go's built-in resolver falls back to
// 127.0.0.1:53 there and every download fails with a name error.
// Desktop builds never set it and keep the system resolver.
var dnsServers = func() []string {
	s := os.Getenv("KAIHEN_DNS")
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}()

var dnsRotate uint32

// webTransport is the shared transport for downloads: proxy-aware like
// the default transport, plus an explicit DNS resolver when KAIHEN_DNS
// names the servers to ask.
var webTransport = func() *http.Transport {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	if len(dnsServers) > 0 {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				i := atomic.AddUint32(&dnsRotate, 1)
				d := net.Dialer{Timeout: 6 * time.Second}
				return d.DialContext(ctx, "udp", net.JoinHostPort(dnsServers[int(i)%len(dnsServers)], "53"))
			},
		}
		tr.DialContext = (&net.Dialer{Timeout: 30 * time.Second, Resolver: resolver}).DialContext
	} else {
		tr.DialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	return tr
}()

func webClient() *http.Client {
	return &http.Client{Timeout: 120 * time.Second, Transport: webTransport}
}

func httpGet(url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(transferCtx(), "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	cl := webClient()
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// ---------- transfer cancellation ----------

// One catalog install (download + upload) runs at a time. It
// registers a cancellable context here; the page's cancel button
// reaches it through /api/cancel. Downloads thread the context into
// their requests, and the upload loops poll transferCancelled().
var opState struct {
	sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

var errTransferCancelled = errors.New("transfer cancelled")

func beginTransfer() context.Context {
	opState.Lock()
	defer opState.Unlock()
	if opState.cancel != nil {
		opState.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	opState.ctx, opState.cancel = ctx, cancel
	return ctx
}

func endTransfer() {
	opState.Lock()
	defer opState.Unlock()
	if opState.cancel != nil {
		opState.cancel()
	}
	opState.ctx, opState.cancel = nil, nil
}

func cancelTransfer() {
	opState.Lock()
	defer opState.Unlock()
	if opState.cancel != nil {
		opState.cancel()
	}
}

func transferCtx() context.Context {
	opState.Lock()
	defer opState.Unlock()
	if opState.ctx != nil {
		return opState.ctx
	}
	return context.Background()
}

func transferCancelled() bool {
	return transferCtx().Err() != nil
}

// ---------- resumable file downloads ----------

// A file download writes dst+".part" first and renames it into
// place only when complete, remembering the URL and total in
// dst+".part.meta". An interrupted download therefore leaves a
// valid prefix behind, and the next attempt resumes from it with a
// Range request instead of starting over.
type partMeta struct {
	URL   string `json:"url"`
	Total int64  `json:"total"`
}

func writePartMeta(part, url string, total int64) {
	b, _ := json.Marshal(partMeta{URL: url, Total: total})
	os.WriteFile(part+".meta", b, 0o644)
}

func readPartMeta(part string) (partMeta, bool) {
	b, err := os.ReadFile(part + ".meta")
	if err != nil {
		return partMeta{}, false
	}
	var m partMeta
	if json.Unmarshal(b, &m) != nil || m.URL == "" {
		return partMeta{}, false
	}
	return m, true
}

func dropPart(part string) {
	os.Remove(part)
	os.Remove(part + ".meta")
}

func finishPart(part, dst string) error {
	if err := os.Rename(part, dst); err != nil {
		return err
	}
	os.Remove(part + ".meta")
	return nil
}

// tryResume continues an interrupted download of url into dst from
// its leftover .part file. engaged is false when there is nothing
// usable to resume (the caller then downloads from the start); a
// stale .part (different URL, or the server's total changed) is
// discarded in that case.
func tryResume(url, dst string, progress func(done, total int64)) (bool, error) {
	part := dst + ".part"
	info, err := os.Stat(part)
	if err != nil || info.Size() == 0 {
		return false, nil
	}
	meta, ok := readPartMeta(part)
	if !ok || meta.URL != url {
		dropPart(part)
		return false, nil
	}
	have := info.Size()
	req, err := http.NewRequestWithContext(transferCtx(), "GET", url, nil)
	if err != nil {
		return true, err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	resp, err := (&http.Client{Transport: webTransport}).Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	var total int64
	switch resp.StatusCode {
	case 206:
		total = contentRangeTotal(resp.Header.Get("Content-Range"))
		if meta.Total > 0 && total > 0 && meta.Total != total {
			dropPart(part)
			return false, nil
		}
		if total <= 0 {
			total = meta.Total
		}
	case 416: // the .part cannot be continued (e.g. already whole)
		dropPart(part)
		return false, nil
	case 200: // the server ignores ranges: start over
		dropPart(part)
		return false, nil
	default:
		return true, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(part, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return true, err
	}
	done := have
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return true, werr
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			if transferCancelled() {
				return true, errTransferCancelled
			}
			return true, rerr
		}
	}
	if err := f.Close(); err != nil {
		return true, err
	}
	if total > 0 && done != total {
		return true, io.ErrUnexpectedEOF
	}
	if progress != nil {
		progress(done, done)
	}
	return true, finishPart(part, dst)
}

// downloadSequential streams url to dst in one request, through the
// .part file, keeping a partial .part when the transfer breaks so
// the next attempt can resume it.
func downloadSequential(url, dst string, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(transferCtx(), "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	cl := &http.Client{Transport: webTransport}
	resp, err := cl.Do(req)
	if err != nil {
		if transferCancelled() {
			return errTransferCancelled
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	writePartMeta(part, url, resp.ContentLength)
	buf := make([]byte, 1<<20)
	var done int64
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			if transferCancelled() {
				return errTransferCancelled
			}
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return finishPart(part, dst)
}

// httpGetToFile streams a download straight to a file, for payloads
// too large to hold in memory (hundreds of MB). It reports progress
// as (done, total) after each chunk; total is -1 when the server
// gives no length. Unlike webClient it has no overall timeout: a
// big file on a slow link may legitimately take many minutes. An
// interrupted download resumes from its leftover partial file.
func httpGetToFile(url, dst string, progress func(done, total int64)) error {
	if engaged, err := tryResume(url, dst, progress); engaged {
		return err
	}
	return downloadSequential(url, dst, progress)
}

// ---------- parallel range downloads ----------

// Big catalog payloads (a 90 MB emulator, the 411 MB browser image)
// arrive over a single connection today. When the server honors
// range requests and the body is large, fetching it in a few
// parallel parts is noticeably faster on a fast connection. Every
// failure of the parallel path falls back to the plain download.
var (
	bigDlThreshold int64 = 16 << 20
	bigDlParts           = 4
)

// contentRangeTotal parses the total out of a Content-Range header
// ("bytes 0-0/12345"); -1 when it is not there.
func contentRangeTotal(h string) int64 {
	i := strings.LastIndex(h, "/")
	if i < 0 {
		return -1
	}
	n, err := strconv.ParseInt(strings.TrimSpace(h[i+1:]), 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// probeRange sends a one-byte range GET. On a 206 it returns the
// total size with the response already closed; on a 200 the open
// response IS the whole body (the server ignores ranges) and the
// caller reads it; any other status is an error.
func probeRange(url string) (total int64, body io.ReadCloser, err error) {
	req, err := http.NewRequestWithContext(transferCtx(), "GET", url, nil)
	if err != nil {
		return -1, nil, err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	req.Header.Set("Range", "bytes=0-0")
	resp, err := (&http.Client{Transport: webTransport}).Do(req)
	if err != nil {
		return -1, nil, err
	}
	switch resp.StatusCode {
	case 206:
		t := contentRangeTotal(resp.Header.Get("Content-Range"))
		resp.Body.Close()
		return t, nil, nil
	case 200:
		return -1, resp.Body, nil
	default:
		resp.Body.Close()
		return -1, nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

// fetchRanges downloads [0, total) of url in bigDlParts parallel
// range requests. write is called once per part with the part's
// offset and exact length; it must be safe for concurrent use.
func fetchRanges(url string, total int64, write func(off int64, part io.Reader, n int64) error) error {
	parts := bigDlParts
	chunk := (total + int64(parts) - 1) / int64(parts)
	var wg sync.WaitGroup
	errCh := make(chan error, parts)
	for p := 0; p < parts; p++ {
		off := int64(p) * chunk
		if off >= total {
			break
		}
		end := off + chunk - 1
		if end >= total {
			end = total - 1
		}
		wg.Add(1)
		go func(off, end int64) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(transferCtx(), "GET", url, nil)
			if err != nil {
				errCh <- err
				return
			}
			req.Header.Set("User-Agent", "CivInstaller")
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, end))
			resp, err := (&http.Client{Transport: webTransport}).Do(req)
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 206 {
				errCh <- fmt.Errorf("HTTP %d", resp.StatusCode)
				return
			}
			if err := write(off, resp.Body, end-off+1); err != nil {
				errCh <- err
			}
		}(off, end)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// httpGetBig is httpGet for large payloads, using parallel ranges
// when the server supports them.
func httpGetBig(url string) ([]byte, error) {
	total, body, err := probeRange(url)
	if err != nil {
		return nil, err
	}
	if body != nil { // 200: the probe response is the download
		defer body.Close()
		return io.ReadAll(io.LimitReader(body, 512<<20))
	}
	if total < bigDlThreshold || total > 512<<20 {
		return httpGet(url)
	}
	buf := make([]byte, total)
	err = fetchRanges(url, total, func(off int64, part io.Reader, n int64) error {
		_, err := io.ReadFull(part, buf[off:off+n])
		return err
	})
	if err != nil {
		return httpGet(url) // the plain path is the fallback
	}
	return buf, nil
}

// httpGetToFileFast is httpGetToFile with parallel range parts for
// large, range-capable downloads. A leftover partial file resumes
// first; when the parallel path fails, the plain sequential
// download (which keeps a resumable partial file) is the fallback.
func httpGetToFileFast(url, dst string, progress func(done, total int64)) error {
	if engaged, err := tryResume(url, dst, progress); engaged {
		return err
	}
	total, body, err := probeRange(url)
	if err != nil {
		return err
	}
	if body != nil || total < bigDlThreshold {
		if body != nil {
			body.Close()
		}
		return downloadSequential(url, dst, progress)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if err := f.Truncate(total); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	var done int64
	var progMu sync.Mutex
	err = fetchRanges(url, total, func(off int64, part io.Reader, n int64) error {
		buf := make([]byte, 1<<20)
		w := int64(0)
		for w < n {
			nr, rerr := part.Read(buf)
			if nr > 0 {
				if _, werr := f.WriteAt(buf[:nr], off+w); werr != nil {
					return werr
				}
				w += int64(nr)
				if progress != nil {
					d := atomic.AddInt64(&done, int64(nr))
					progMu.Lock()
					progress(d, total)
					progMu.Unlock()
				}
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return rerr
			}
		}
		if w != n {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		if transferCancelled() {
			return errTransferCancelled
		}
		return downloadSequential(url, dst, progress)
	}
	if progress != nil {
		progress(total, total)
	}
	return nil
}

// ---------- console IP discovery ----------

// scanSubnets returns the /24 networks this machine sits on: one per
// IPv4 interface address, plus the Android shell's reported LAN
// address (KAIHEN_LAN_IP) when set.
func scanSubnets() []string {
	seen := map[string]bool{}
	var nets []string
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		ip4 := ip.To4()
		if ip4 == nil || ip4.IsLoopback() {
			return
		}
		base := fmt.Sprintf("%d.%d.%d.0", ip4[0], ip4[1], ip4[2])
		if !seen[base] {
			seen[base] = true
			nets = append(nets, base)
		}
	}
	if v := os.Getenv("KAIHEN_LAN_IP"); v != "" {
		add(net.ParseIP(v))
	}
	if ifs, err := net.Interfaces(); err == nil {
		for _, ifc := range ifs {
			if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, err := ifc.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok {
					add(ipn.IP)
				}
			}
		}
	}
	return nets
}

// probeConsolePorts reports how strongly host looks like the
// console: 2 = an FTP server on 1337/5000 answered with an FTP
// greeting (a bare connect is not proof — some networks accept
// every connection), 1 = the loader port (9026) accepted a
// connection, 0 = neither.
func probeConsolePorts(host string) int {
	for _, port := range []int{ozFtpPort, ozPluginPort} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 300*time.Millisecond)
		if err != nil {
			continue
		}
		c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		buf := make([]byte, 4)
		n, rerr := c.Read(buf)
		c.Close()
		if rerr == nil && n >= 3 && string(buf[:3]) == "220" {
			return 2
		}
	}
	if c, err := net.DialTimeout("tcp", net.JoinHostPort(host, "9026"), 300*time.Millisecond); err == nil {
		c.Close()
		return 1
	}
	return 0
}

// findIpHandler scans the local /24 networks for candidate console
// addresses, so the user can pick theirs instead of typing it.
// FTP-verified addresses come first.
func findIpHandler(w http.ResponseWriter, r *http.Request) {
	var hosts []string
	for _, base := range scanSubnets() {
		parts := strings.Split(base, ".")
		for i := 1; i < 255; i++ {
			hosts = append(hosts, parts[0]+"."+parts[1]+"."+parts[2]+"."+strconv.Itoa(i))
		}
	}
	type hit struct {
		ip  string
		lvl int
	}
	var hits []hit
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 48)
	for _, h := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if lvl := probeConsolePorts(h); lvl > 0 {
				mu.Lock()
				hits = append(hits, hit{h, lvl})
				mu.Unlock()
			}
		}(h)
	}
	wg.Wait()
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].lvl != hits[j].lvl {
			return hits[i].lvl > hits[j].lvl
		}
		return hits[i].ip < hits[j].ip
	})
	found := []string{}
	for _, h := range hits {
		found = append(found, h.ip)
	}
	jsonOK(w, map[string]any{"ok": true, "ips": found})
}

// httpGetFinal is httpGet plus the final URL after redirects (GitHub
// release pages encode the tag in the redirect target).
func httpGetFinal(u string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(transferCtx(), "GET", u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "CivInstaller")
	cl := webClient()
	resp, err := cl.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, "", err
	}
	return b, resp.Request.URL.String(), nil
}

func getJSON(url string, v any) error {
	b, err := httpGet(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// ftpUpload uploads one file to the CivHomebrew FTP server on ip:5000.

type ftpReader struct {
	conn net.Conn
	buf  []byte
}

func newFTPReader(c net.Conn) *ftpReader { return &ftpReader{conn: c} }

func (f *ftpReader) readLine() (string, error) {
	for {
		if i := bytes.IndexByte(f.buf, '\n'); i >= 0 {
			line := string(f.buf[:i+1])
			f.buf = f.buf[i+1:]
			return strings.TrimRight(line, "\r\n"), nil
		}
		tmp := make([]byte, 4096)
		n, err := f.conn.Read(tmp)
		if n > 0 {
			f.buf = append(f.buf, tmp[:n]...)
		}
		if err != nil {
			return "", err
		}
	}
}

func (f *ftpReader) readReply() (int, string) {
	line, err := f.readLine()
	if err != nil {
		return 0, ""
	}
	code := 0
	fmt.Sscanf(line, "%d", &code)
	if len(line) > 3 && line[3] == '-' {
		// multiline: read until "NNN " line
		for {
			l2, err := f.readLine()
			if err != nil {
				break
			}
			if len(l2) > 3 {
				c2 := 0
				fmt.Sscanf(l2, "%d", &c2)
				if c2 == code && l2[3] == ' ' {
					break
				}
			}
		}
	}
	return code, line
}

// ---------- startup consent gate ----------

func consentGiven() bool {
	_, err := os.Stat(pathOf("consent-agreed.txt"))
	return err == nil
}

func consentStateHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"given": consentGiven()})
}

func consentAgreeHandler(w http.ResponseWriter, r *http.Request) {
	_ = os.WriteFile(pathOf("consent-agreed.txt"), []byte(time.Now().Format(time.RFC3339)), 0o644)
	jsonOK(w, map[string]any{"ok": true})
}
