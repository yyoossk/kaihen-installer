package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type fenceBody struct {
	Router  string `json:"router"`
	Host    string `json:"host"`
	Pass    string `json:"pass"`
	Lang    string `json:"lang"`
	MacWiFi string `json:"macwifi"`
	MacEth  string `json:"maceth"`
	Subnet  string `json:"subnet"`
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// addr returns the router address. The page sends it as "host"
// (and as "ip" for the install query); "router" is also accepted.
// The two sides once disagreed on the key name, which left the
// address empty and made every SWFence operation fail at dial.
func (b fenceBody) addr() string {
	if b.Router != "" {
		return b.Router
	}
	return b.Host
}

func fenceDial(router, pass string) (*ssh.Client, error) {
	host := strings.TrimSpace(router)
	if !strings.Contains(host, ":") {
		host += ":22"
	}
	conf := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	return sshDialLAN(host, conf)
}

// sshDialLAN dials over lanDialTimeout so SSH to a router on a
// no-internet Wi-Fi still leaves via Wi-Fi on Android.
func sshDialLAN(host string, conf *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := lanDialTimeout("tcp", host, conf.Timeout)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, host, conf)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func fenceExec(cl *ssh.Client, cmd string) (string, error) {
	sess, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	return string(out), err
}

func fenceRunStdin(cl *ssh.Client, cmd, input string, log logFn) (string, error) {
	sess, err := cl.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	stdin, err := sess.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := sess.Start(cmd); err != nil {
		return "", err
	}
	go func() {
		io.WriteString(stdin, input)
		stdin.Close()
	}()
	var out strings.Builder
	done := make(chan struct{})
	go func() { scanLines(stderr, log); close(done) }()
	scanLines(stdout, func(line string) {
		out.WriteString(line + "\n")
		if log != nil {
			log(line)
		}
	})
	<-done
	err = sess.Wait()
	return out.String(), err
}

func fenceRelease(cl *ssh.Client) string {
	out, _ := fenceExec(cl, `grep DISTRIB_RELEASE /etc/openwrt_release | cut -d= -f2 | tr -d "'\""`)
	v := strings.TrimSpace(out)
	if k := strings.Index(v, "="); k >= 0 {
		v = strings.TrimSpace(v[k+1:])
	}
	return strings.Trim(v, "'\"")
}

func fenceSupported(release string) bool {
	return strings.HasPrefix(release, "24.10.") || strings.HasPrefix(release, "25.12.")
}

func fenceHasFW4(cl *ssh.Client) bool {
	out, _ := fenceExec(cl, "command -v fw4")
	return strings.TrimSpace(out) != ""
}

func fenceGuardsUp(cl *ssh.Client) bool {
	o1, e1 := fenceExec(cl, "nft list chain inet fw4 swfence_policy 2>/dev/null")
	o2, e2 := fenceExec(cl, "nft list chain inet fw4 swfence_early 2>/dev/null")
	return e1 == nil && e2 == nil && strings.TrimSpace(o1) != "" && strings.TrimSpace(o2) != ""
}

func fenceLeases(cl *ssh.Client) []map[string]string {
	out, _ := fenceExec(cl, "cat /tmp/dhcp.leases 2>/dev/null")
	leases := []map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 {
			leases = append(leases, map[string]string{"mac": f[1], "ip": f[2], "name": f[3]})
		}
	}
	return leases
}

// fenceLANPrefix asks the router for its global IPv4 addresses and
// returns the LAN network (e.g. "192.168.1.0/24") of the address the
// user connected to. The check response carries it as "cidr" and the
// page fills the allowed-subnet field with it.
func fenceLANPrefix(cl *ssh.Client, routerIP string) string {
	out, _ := fenceExec(cl, "ip -4 -o addr show scope global")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for i, tok := range f {
			if tok == "inet" && i+1 < len(f) {
				ip, ipnet, err := net.ParseCIDR(f[i+1])
				if err == nil && ip.String() == routerIP {
					return ipnet.String()
				}
			}
		}
	}
	return ""
}

func fenceChkHandler(w http.ResponseWriter, r *http.Request) {
	var b fenceBody
	json.NewDecoder(r.Body).Decode(&b)
	cl, err := fenceDial(b.addr(), b.Pass)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": tr(b.Lang, "fSSH") + ": " + err.Error()})
		return
	}
	defer cl.Close()
	rel := fenceRelease(cl)
	if !fenceSupported(rel) || !fenceHasFW4(cl) {
		jsonOK(w, map[string]any{"ok": false, "supported": false, "release": rel, "error": tr(b.Lang, "fUnsup")})
		return
	}
	installed := false
	if out, _ := fenceExec(cl, "command -v swfence"); strings.TrimSpace(out) != "" {
		installed = true
	}
	enabled := false
	if out, _ := fenceExec(cl, "uci get swfence.main.enabled"); strings.TrimSpace(out) == "1" {
		enabled = true
	}
	host := b.addr()
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	jsonOK(w, map[string]any{
		"ok": true, "supported": true, "release": rel,
		"installed": installed, "enabled": enabled, "guards": fenceGuardsUp(cl),
		"leases": fenceLeases(cl),
		"cidr":   fenceLANPrefix(cl, host),
	})
}

func fenceLatestTag() (string, error) {
	cl := &http.Client{
		Timeout:   30 * time.Second,
		Transport: webTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("GET", "https://github.com/byizmysh/SWFence/releases/latest", nil)
	req.Header.Set("User-Agent", "CivInstaller")
	resp, err := cl.Do(req)
	if err == nil && resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		if i := strings.LastIndex(loc, "/"); i >= 0 {
			return loc[i+1:], nil
		}
	}
	if resp != nil {
		resp.Body.Close()
	}
	var rel ghRelease
	if err := getJSON("https://api.github.com/repos/byizmysh/SWFence/releases/latest", &rel); err != nil {
		return "", err
	}
	return rel.TagName, nil
}

func fenceInstallHandler(w http.ResponseWriter, r *http.Request) {
	var b fenceBody
	if r.URL.RawQuery != "" {
		q := r.URL.Query()
		b = fenceBody{Router: firstNonEmpty(q.Get("router"), q.Get("ip")), Pass: q.Get("pass"), Lang: q.Get("lang"),
			MacWiFi: firstNonEmpty(q.Get("macwifi"), q.Get("macw")), MacEth: firstNonEmpty(q.Get("maceth"), q.Get("mace")), Subnet: q.Get("subnet")}
	} else {
		json.NewDecoder(r.Body).Decode(&b)
	}
	lang := b.Lang
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fl, _ := w.(http.Flusher)
	send := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		if fl != nil {
			fl.Flush()
		}
	}
	fail := func(err error) {
		send("RESULT|ng")
		send(tr(lang, "failed") + err.Error())
	}
	runningMu.Lock()
	if running {
		runningMu.Unlock()
		fail(errors.New(tr(lang, "jsBusy")))
		return
	}
	running = true
	runningMu.Unlock()
	defer func() {
		runningMu.Lock()
		running = false
		runningMu.Unlock()
	}()

	cl, err := fenceDial(b.addr(), b.Pass)
	if err != nil {
		fail(fmt.Errorf("%s: %w", tr(lang, "fSSH"), err))
		return
	}
	defer cl.Close()
	rel := fenceRelease(cl)
	if !fenceSupported(rel) || !fenceHasFW4(cl) {
		fail(errors.New(tr(lang, "fUnsup")))
		return
	}
	// The install uses only the bundle downloaded beforehand (the
	// "Download SWFence" button): while the phone or PC is talking to
	// the router it may have no path to GitHub at all.
	tag := fenceCachedTag()
	if tag == "" {
		fail(errors.New(tr(lang, "fNeedDl")))
		return
	}
	send(tr(lang, "fTag") + tag)

	// Back up the router configuration first. sysupgrade is called by
	// its absolute path (an SSH exec session's PATH is not guaranteed
	// to include /sbin), and the archive is fetched with cat: the SSH
	// channel is 8-bit clean, and a base64 round-trip only adds a
	// failure mode. Every failure carries the underlying error so the
	// log says what actually went wrong on the router.
	bkOut, err := fenceExec(cl, "/sbin/sysupgrade -b /tmp/swfence-backup.tar.gz")
	if err != nil {
		if s := strings.TrimSpace(bkOut); s != "" {
			send(s)
		}
		fail(fmt.Errorf("%s: %w", tr(lang, "fBkFail"), err))
		return
	}
	bkData, err := fenceExec(cl, "cat /tmp/swfence-backup.tar.gz")
	if err != nil {
		fail(fmt.Errorf("%s: %w", tr(lang, "fBkFail"), err))
		return
	}
	raw := []byte(bkData)
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		fail(fmt.Errorf("%s (%d bytes)", tr(lang, "fBkFail"), len(raw)))
		return
	}
	os.MkdirAll(pathOf("swfence-backups"), 0o755)
	bkName := "swfence-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	if err := os.WriteFile(filepath.Join(pathOf("swfence-backups"), bkName), raw, 0o644); err != nil {
		fail(fmt.Errorf("%s: %w", tr(lang, "fBkFail"), err))
		return
	}
	send(tr(lang, "fBackup") + bkName)

	// flow offloading must be off for the nft rules to see every packet
	fenceExec(cl, "uci set network.@device[0].flow_offloading='0'; uci set network.@device[0].flow_offloading_hw='0'; uci commit network")
	send(tr(lang, "fOff"))

	send(tr(lang, "fInst"))
	if err := fenceInstallBundle(cl, tag, rel, send); err != nil {
		fail(fmt.Errorf("%s: %w", tr(lang, "fNoSw"), err))
		return
	}
	if out, _ := fenceExec(cl, "command -v swfence"); strings.TrimSpace(out) == "" {
		fail(errors.New(tr(lang, "fNoSw")))
		return
	}

	macs := []string{}
	for _, m := range []string{b.MacWiFi, b.MacEth} {
		if m = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(m), "-", ":")); m != "" {
			macs = append(macs, m)
		}
	}
	if len(macs) == 0 {
		fail(errors.New(tr(lang, "fNoMac")))
		return
	}
	routerIP := strings.TrimSpace(b.Router)
	if i := strings.LastIndex(routerIP, ":"); i > 0 && !strings.Contains(routerIP[i:], ".") {
		routerIP = routerIP[:i]
	}
	cfg := map[string]string{
		"enabled":    "1",
		"macs":       strings.Join(macs, " "),
		"router_ip":  routerIP,
		"lan_subnet": strings.TrimSpace(b.Subnet),
		"local_host": "",
		"local_port": "",
	}
	cfgJSON, _ := json.Marshal(cfg)
	send(tr(lang, "fCfg"))
	out, err := fenceRunStdin(cl, "swfence configure", string(cfgJSON), send)
	if err != nil || !strings.Contains(out, "\"ok\": true") && !strings.Contains(out, "\"ok\":true") {
		fail(errors.New(tr(lang, "fCfgFail")))
		return
	}
	send(tr(lang, "fVer"))
	if !fenceGuardsUp(cl) {
		fail(errors.New(tr(lang, "fVerFail")))
		return
	}
	send("RESULT|ok")
	send(tr(lang, "done"))
}

func fenceStatusHandler(w http.ResponseWriter, r *http.Request) {
	var b fenceBody
	json.NewDecoder(r.Body).Decode(&b)
	cl, err := fenceDial(b.addr(), b.Pass)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": tr(b.Lang, "fSSH") + ": " + err.Error()})
		return
	}
	defer cl.Close()
	out, _ := fenceExec(cl, "swfence status 2>&1; swfence diagnostics 2>&1 | head -40")
	jsonOK(w, map[string]any{"ok": true, "output": out})
}

func fenceOffHandler(w http.ResponseWriter, r *http.Request) {
	var b fenceBody
	json.NewDecoder(r.Body).Decode(&b)
	cl, err := fenceDial(b.addr(), b.Pass)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": tr(b.Lang, "fSSH") + ": " + err.Error()})
		return
	}
	defer cl.Close()
	if _, err := fenceExec(cl, "uci set swfence.main.enabled='0'; uci commit swfence; swfence apply"); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// --- SWFence bundle: download first, install offline -----------------
//
// The release tarball (swfence-<version>.tar.gz) is upstream's own
// offline bundle: install.sh, all four packages and SHA256SUMS. The
// download step fetches it while the phone or PC still has internet;
// the install step only uploads it over SSH and follows INSTALL.md's
// local procedure, so neither side downloads anything at install time.

func fenceCacheDir() string { return pathOf("swfence-cache") }

func fenceBundleName(tag string) string {
	return "swfence-" + strings.TrimPrefix(tag, "v") + ".tar.gz"
}

func fenceCachedTag() string {
	b, err := os.ReadFile(filepath.Join(fenceCacheDir(), "tag.txt"))
	if err != nil {
		return ""
	}
	tag := strings.TrimSpace(string(b))
	if !fenceValidTag(tag) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(fenceCacheDir(), fenceBundleName(tag))); err != nil {
		return ""
	}
	return tag
}

// fenceValidTag gates a tag before it is used in a file name or a
// command on the router.
func fenceValidTag(tag string) bool {
	if tag == "" || len(tag) > 64 {
		return false
	}
	for i, c := range tag {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '.' || c == '_' || c == '-'
		if !ok || i == 0 && !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func fenceDlHandler(w http.ResponseWriter, r *http.Request) {
	var b fenceBody
	json.NewDecoder(r.Body).Decode(&b)
	lang := b.Lang
	failMsg := func(err error) {
		msg := tr(lang, "fDlFail")
		if err != nil {
			msg += ": " + err.Error()
		}
		jsonOK(w, map[string]any{"ok": false, "error": msg})
	}
	tag, err := fenceLatestTag()
	if err != nil || !fenceValidTag(tag) {
		failMsg(err)
		return
	}
	name := fenceBundleName(tag)
	data, err := httpGet("https://github.com/byizmysh/SWFence/releases/download/" + tag + "/" + name)
	if err != nil {
		failMsg(err)
		return
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		failMsg(errors.New("the downloaded file is not a gzip archive"))
		return
	}
	dir := fenceCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		failMsg(err)
		return
	}
	if ents, _ := os.ReadDir(dir); ents != nil {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "swfence-") && strings.HasSuffix(e.Name(), ".tar.gz") && e.Name() != name {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		failMsg(err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "tag.txt"), []byte(tag), 0o644); err != nil {
		failMsg(err)
		return
	}
	jsonOK(w, map[string]any{"ok": true, "tag": tag})
}

func fenceCacheHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"tag": fenceCachedTag()})
}

const (
	fenceUploadCmd  = "cat > /tmp/swfence-bundle.tar.gz"
	fenceExtractCmd = "rm -rf /tmp/swfence-bundle && mkdir -p /tmp/swfence-bundle && tar -xzf /tmp/swfence-bundle.tar.gz -C /tmp/swfence-bundle"
	fenceFindCmd    = "find /tmp/swfence-bundle -name SHA256SUMS | head -n 1"
	fenceRepoCmd    = "mkdir -p /etc/swfence && printf '%s\n' 'byizmysh/SWFence' > /etc/swfence/repository && swfence setup"
)

// fenceInstallBundle uploads the cached bundle and installs the
// package pair for the router's release family, verifying SHA256SUMS
// on the router exactly like upstream's offline procedure. No network
// is used on either side.
func fenceInstallBundle(cl *ssh.Client, tag, release string, send logFn) error {
	data, err := os.ReadFile(filepath.Join(fenceCacheDir(), fenceBundleName(tag)))
	if err != nil {
		return err
	}
	var inst string
	switch {
	case strings.HasPrefix(release, "24.10."):
		inst = "opkg install ./swfence-24.10.ipk ./luci-app-swfence-24.10.ipk"
	case strings.HasPrefix(release, "25.12."):
		inst = "apk add --allow-untrusted ./swfence-25.12.apk ./luci-app-swfence-25.12.apk"
	default:
		return errors.New("unsupported release " + release)
	}
	if _, err := fenceRunStdin(cl, fenceUploadCmd, string(data), send); err != nil {
		return err
	}
	if _, err := fenceRunStdin(cl, fenceExtractCmd, "", send); err != nil {
		return err
	}
	out, err := fenceExec(cl, fenceFindCmd)
	if err != nil {
		return err
	}
	sums := strings.TrimSpace(out)
	dir := strings.TrimSuffix(sums, "/SHA256SUMS")
	if sums == "" || dir == sums || !strings.HasPrefix(dir, "/tmp/swfence-bundle") ||
		strings.ContainsAny(dir, "\"' \t") {
		return errors.New("SHA256SUMS not found in the uploaded bundle")
	}
	if _, err := fenceRunStdin(cl, "cd "+dir+" && sha256sum -c SHA256SUMS", "", send); err != nil {
		return err
	}
	if _, err := fenceRunStdin(cl, "cd "+dir+" && "+inst, "", send); err != nil {
		return err
	}
	if _, err := fenceRunStdin(cl, fenceRepoCmd, "", send); err != nil {
		return err
	}
	return nil
}
