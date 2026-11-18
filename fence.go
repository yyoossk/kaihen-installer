package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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
	resp := map[string]any{
		"ok": true, "supported": true, "release": rel,
		"installed": installed, "enabled": enabled, "guards": fenceGuardsUp(cl),
		"leases": fenceLeases(cl),
		"cidr":   fenceLANPrefix(cl, host),
	}
	// When the SWFence bundle is already downloaded, also learn
	// what this router will need at install time (feeds, installed
	// packages, missing dependencies) so the download step can
	// fetch the missing .ipk files while the phone has internet.
	if strings.HasPrefix(rel, "24.10.") {
		if tag := fenceCachedTag(); tag != "" {
			if _, instSet, _, missing, err := fenceDepState(cl, tag); err == nil {
				feeds := []fenceFeed{}
				if out, err := fenceExec(cl, "cat /etc/opkg/distfeeds.conf"); err == nil {
					feeds = fenceParseFeeds(out)
				}
				names := make([]string, 0, len(instSet))
				for n := range instSet {
					names = append(names, n)
				}
				sort.Strings(names)
				fenceSaveProfile(&fenceProfile{Release: rel, Feeds: feeds, Installed: names, Missing: missing})
				resp["missing"] = missing
			}
		}
	}
	jsonOK(w, resp)
}

// fenceLatestTagFn is a seam for tests.
var fenceLatestTagFn = fenceLatestTag

// fenceNetErr reports whether err looks like "this device cannot
// reach the internet" (DNS, dial, timeout), as opposed to a
// protocol or data problem.
func fenceNetErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, p := range []string{"dial tcp", "dial udp", "lookup ", "i/o timeout", "no such host", "network is unreachable", "connection refused", "connection reset", "TLS handshake", "Client.Timeout", "server misbehaving"} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
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

// fenceASCII folds full-width ASCII variants (U+FF01-FF5E) and
// the ideographic space to plain ASCII and drops zero-width
// characters. SWFence accepts printable ASCII only, and a
// Japanese IME easily produces full-width digits and colons in
// the MAC and subnet fields.
func fenceASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '！' && r <= '～':
			r -= 0xFEE0
		case r == '　':
			r = ' '
		case r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff':
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

var fenceMACRe = regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`)

// fenceNormMAC normalizes a user-entered MAC address (full-width
// tolerant, hyphens become colons, lower case) and reports
// whether the result is a well-formed MAC address.
func fenceNormMAC(s string) (string, bool) {
	m := strings.ToLower(strings.ReplaceAll(fenceASCII(s), "-", ":"))
	return m, fenceMACRe.MatchString(m)
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
		var me *fenceMissingError
		if errors.As(err, &me) {
			fail(errors.New(tr(lang, "fMissingDeps") + strings.Join(me.Names, ", ") + " — " + tr(lang, "fMissingHint")))
			return
		}
		fail(fmt.Errorf("%s: %w", tr(lang, "fNoSw"), err))
		return
	}
	if out, _ := fenceExec(cl, "command -v swfence"); strings.TrimSpace(out) == "" {
		fail(errors.New(tr(lang, "fNoSw")))
		return
	}

	macs := []string{}
	for _, m := range []string{b.MacWiFi, b.MacEth} {
		if fenceASCII(m) == "" {
			continue
		}
		nm, ok := fenceNormMAC(m)
		if !ok {
			fail(errors.New(tr(lang, "fBadMac")))
			return
		}
		macs = append(macs, nm)
	}
	if len(macs) == 0 {
		fail(errors.New(tr(lang, "fNoMac")))
		return
	}
	routerIP := fenceASCII(b.Router)
	if i := strings.LastIndex(routerIP, ":"); i > 0 && !strings.Contains(routerIP[i:], ".") {
		routerIP = routerIP[:i]
	}
	subnet := fenceASCII(b.Subnet)
	if _, ipnet, err := net.ParseCIDR(subnet); err == nil {
		subnet = ipnet.String()
	} else {
		fail(errors.New(tr(lang, "fBadSubnet")))
		return
	}
	cfg := map[string]string{
		"enabled":    "1",
		"macs":       strings.Join(macs, " "),
		"router_ip":  routerIP,
		"lan_subnet": subnet,
		"local_host": "",
		"local_port": "",
	}
	cfgJSON, _ := json.Marshal(cfg)
	send(tr(lang, "fCfg"))
	out, err := fenceRunStdin(cl, "swfence configure", string(cfgJSON), send)
	if err != nil || !strings.Contains(out, "\"ok\": true") && !strings.Contains(out, "\"ok\":true") {
		msg := tr(lang, "fCfgFail")
		var rep struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &rep) == nil && rep.Message != "" {
			msg += ": " + rep.Message
		} else if err != nil {
			msg += ": " + err.Error()
		}
		fail(errors.New(msg))
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

// fenceDownloadBundle fetches a SWFence release bundle into the
// cache, replacing any older bundle.
func fenceDownloadBundle(tag string) error {
	name := fenceBundleName(tag)
	data, err := httpGet("https://github.com/byizmysh/SWFence/releases/download/" + tag + "/" + name)
	if err != nil {
		return err
	}
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return errors.New("the downloaded file is not a gzip archive")
	}
	dir := fenceCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if ents, _ := os.ReadDir(dir); ents != nil {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "swfence-") && strings.HasSuffix(e.Name(), ".tar.gz") && e.Name() != name {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "tag.txt"), []byte(tag), 0o644)
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
	// Prefer the cached bundle: while this device is on the
	// router's network it may have no internet at all, and a
	// failed "latest" lookup must not discard a good download.
	tag := fenceCachedTag()
	lt, lerr := fenceLatestTagFn()
	switch {
	case lerr == nil && fenceValidTag(lt):
		if lt != tag {
			if err := fenceDownloadBundle(lt); err != nil {
				if tag == "" {
					if fenceNetErr(err) {
						failMsg(errors.New(tr(lang, "fOffline")))
						return
					}
					failMsg(err)
					return
				}
				// A cached bundle exists: keep it and carry on.
			} else {
				tag = lt
			}
		}
	case tag == "":
		if lerr != nil && fenceNetErr(lerr) {
			failMsg(errors.New(tr(lang, "fOffline")))
			return
		}
		failMsg(lerr)
		return
	}
	deps := []string{}
	if prof := fenceLoadProfile(); prof != nil && len(prof.Missing) > 0 {
		fetched, unresolved, err := fenceFetchDeps(prof)
		if err != nil {
			if fenceNetErr(err) {
				failMsg(errors.New(tr(lang, "fOffline")))
				return
			}
			failMsg(err)
			return
		}
		if len(unresolved) > 0 {
			prof.Missing = unresolved
			fenceSaveProfile(prof)
			failMsg(errors.New(tr(lang, "fMissingDeps") + strings.Join(unresolved, ", ")))
			return
		}
		deps = fetched
		prof.Missing = nil
		fenceSaveProfile(prof)
	}
	jsonOK(w, map[string]any{"ok": true, "tag": tag, "deps": deps})
}

func fenceCacheHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"tag": fenceCachedTag()})
}

const (
	fenceUploadCmd  = "cat > /tmp/swfence-bundle.tar.gz"
	fenceExtractCmd = "rm -rf /tmp/swfence-bundle && mkdir -p /tmp/swfence-bundle && tar -xzf /tmp/swfence-bundle.tar.gz -C /tmp/swfence-bundle"
	fenceFindCmd    = "find /tmp/swfence-bundle -name SHA256SUMS | head -n 1"
	// fenceRepoCmd records the upstream repository (read by
	// "swfence status" for the project URL). Upstream's install.sh
	// follows it with "swfence setup", but that is an interactive
	// wizard (it reads MACs/subnet from the terminal and dies on
	// EOF); this app collects the same values in its own steps and
	// sends them to "swfence configure" as JSON instead.
	fenceRepoCmd       = "mkdir -p /etc/swfence && printf '%s\n' 'byizmysh/SWFence' > /etc/swfence/repository"
	fenceOpkgUpdateCmd = "opkg update"
	fenceApkUpdateCmd  = "apk update"

	// fenceWgetWrapCmd installs a wget shim that forces IPv4.
	// opkg/apk fetch through "wget" (uclient-fetch), which fails
	// with "Failed to send request: Operation not permitted" when
	// DNS offers IPv6 addresses but the router has no working IPv6
	// route: uclient-fetch does not fall back to IPv4. Prefixing
	// PATH with the shim directory makes their downloads use -4.
	fenceWgetWrapCmd = "mkdir -p /tmp/swfence-bin && printf '%s\n' '#!/bin/sh' 'exec uclient-fetch -4 \"$@\"' > /tmp/swfence-bin/wget && chmod +x /tmp/swfence-bin/wget"
	fencePathPrefix  = "PATH=/tmp/swfence-bin:/usr/sbin:/usr/bin:/sbin:/bin "
	// fenceListFetchCmd is the fallback when opkg update still
	// fails: fetch each feed's Packages.gz straight into
	// /var/opkg-lists under its feed name, again forcing IPv4.
	fenceListFetchCmd = "mkdir -p /var/opkg-lists && while read -r t n u x; do case \"$t\" in src/gz) uclient-fetch -4 -q -O /var/opkg-lists/$n \"$u/Packages.gz\" || exit 1;; esac; done < /etc/opkg/distfeeds.conf"
)

// fenceInstallBundle uploads the cached bundle and installs the
// package pair for the router's release family, verifying SHA256SUMS
// on the router exactly like upstream's offline procedure. No network
// is used on either side.
// fenceNetDiag runs a short router network self-check (routing
// table, addresses, gateway and public-IP reachability, DNS, one
// IPv4 fetch of the first feed), streams it into the install log,
// and returns a one-line summary that callers embed in the failure
// message itself, so the reason survives even a truncated log copy.
func fenceNetDiag(cl *ssh.Client, send logFn) string {
	send("--- router network diagnostics ---")
	run := func(c string) string {
		send("$ " + c)
		out, _ := fenceExec(cl, c)
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if line != "" {
				send(line)
			}
		}
		return out
	}
	routes := run("ip -4 route")
	gw := ""
	for _, line := range strings.Split(routes, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "default" && net.ParseIP(f[2]) != nil {
			gw = f[2]
			break
		}
	}
	addrsOut := run("ip -4 -o addr show scope global")
	var addrs []string
	for _, line := range strings.Split(addrsOut, "\n") {
		f := strings.Fields(line)
		for i, tok := range f {
			if tok == "inet" && i+1 < len(f) && len(f) >= 2 {
				addrs = append(addrs, f[1]+":"+f[i+1])
			}
		}
	}
	pingOK := func(out string) string {
		if strings.Contains(out, "1 received") || strings.Contains(out, "0% packet loss") {
			return "ok"
		}
		return "ng"
	}
	gwPing := "ng"
	if gw != "" {
		gwPing = pingOK(run("ping -c 1 -W 2 " + gw))
	} else {
		send("no default gateway")
	}
	netPing := pingOK(run("ping -c 1 -W 2 1.1.1.1"))
	dnsOut := run("nslookup downloads.openwrt.org 127.0.0.1 | head -8")
	dns := "ng"
	if strings.Count(dnsOut, "Address") >= 2 && !strings.Contains(dnsOut, "can't") {
		dns = "ok"
	}
	fetchOut := run("u=$(awk '$1==\"src/gz\"{print $3; exit}' /etc/opkg/distfeeds.conf); uclient-fetch -4 -O /dev/null \"$u/Packages.gz\"; echo EXIT=$?")
	v4 := "ng"
	if strings.Contains(fetchOut, "EXIT=0") {
		v4 = "ok"
	}
	return fmt.Sprintf("diag[gw=%s gwping=%s netping=%s dns=%s v4fetch=%s addrs=%s]",
		gw, gwPing, netPing, dns, v4, strings.Join(addrs, ","))
}

// ---- offline dependency handling ----
//
// Verified against the real opkg (OpenWrt 24.10.0 rootfs, sandbox
// experiments E1-E5): installing local .ipk files needs NO package
// lists and NO router internet, as long as every dependency is
// either already installed on the router or given as a file in the
// same command. When one is missing, opkg refuses with the
// misleading "Unknown package 'swfence'". So the app computes the
// missing set itself: it reads the Depends fields out of the
// downloaded bundle, compares them with the router's installed
// packages, and the phone downloads any missing .ipk from the
// router's own feeds (learned at connection-check time) to hand
// over at install time.

type ipkInfo struct {
	Package string
	Depends []string
}

// depNamesOf flattens a Depends field ("kernel (= 1.2), a | b, c")
// into plain package names ("kernel", "a", "c").
func depNamesOf(s string) []string {
	var out []string
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if i := strings.Index(tok, "("); i >= 0 {
			tok = strings.TrimSpace(tok[:i])
		}
		if strings.Contains(tok, "|") {
			tok = strings.TrimSpace(strings.SplitN(tok, "|", 2)[0])
		}
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// parseControlFields parses a debian-style control stanza,
// honouring continuation lines.
func parseControlFields(text string) map[string]string {
	fields := map[string]string{}
	key := ""
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if key != "" {
				fields[key] += "\n" + line
			}
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			continue
		}
		key = line[:i]
		fields[key] = strings.TrimSpace(line[i+1:])
	}
	return fields
}

func tarGzMember(data []byte, base string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == base {
			return io.ReadAll(tr)
		}
	}
	return nil, errors.New(base + " not found in archive")
}

// parseIPKBytes reads the control fields of an .ipk (a tar.gz
// wrapping control.tar.gz wrapping ./control).
func parseIPKBytes(data []byte) (ipkInfo, error) {
	ctrlGz, err := tarGzMember(data, "control.tar.gz")
	if err != nil {
		return ipkInfo{}, err
	}
	ctl, err := tarGzMember(ctrlGz, "control")
	if err != nil {
		return ipkInfo{}, err
	}
	f := parseControlFields(string(ctl))
	return ipkInfo{Package: f["Package"], Depends: depNamesOf(f["Depends"])}, nil
}

// fenceBundleIPKs parses every .ipk inside the cached bundle.
func fenceBundleIPKs(tag string) ([]ipkInfo, error) {
	data, err := os.ReadFile(filepath.Join(fenceCacheDir(), fenceBundleName(tag)))
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	var out []ipkInfo
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(h.Name, ".ipk") {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		info, err := parseIPKBytes(b)
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	if len(out) == 0 {
		return nil, errors.New("no .ipk packages in the bundle")
	}
	return out, nil
}

// fenceInstalledSet returns the router's installed package names.
func fenceInstalledSet(cl *ssh.Client) (map[string]bool, error) {
	out, err := fenceExec(cl, "opkg list-installed")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.Index(line, " - "); i > 0 {
			line = line[:i]
		}
		set[line] = true
	}
	return set, nil
}

var fenceDepFileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.ipk$`)

// fenceCachedDeps maps package name -> local file for every
// dependency .ipk the phone downloaded into the cache.
func fenceCachedDeps() (map[string]string, error) {
	out := map[string]string{}
	ents, err := os.ReadDir(filepath.Join(fenceCacheDir(), "deps"))
	if err != nil {
		return out, nil
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ipk") {
			continue
		}
		p := filepath.Join(fenceCacheDir(), "deps", e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		info, err := parseIPKBytes(b)
		if err != nil || info.Package == "" {
			continue
		}
		out[info.Package] = p
	}
	return out, nil
}

// fenceMissingError reports dependency packages that are neither
// installed on the router nor in the local cache.
type fenceMissingError struct{ Names []string }

func (e *fenceMissingError) Error() string {
	return "missing dependency packages: " + strings.Join(e.Names, ", ")
}

// fenceDepState gathers the offline-install facts for the cached
// bundle: its packages, the router's installed set, the cached
// dependency files, and what is still missing.
func fenceDepState(cl *ssh.Client, tag string) (infos []ipkInfo, installed map[string]bool, cached map[string]string, missing []string, err error) {
	infos, err = fenceBundleIPKs(tag)
	if err != nil {
		return
	}
	installed, err = fenceInstalledSet(cl)
	if err != nil {
		return
	}
	cached, err = fenceCachedDeps()
	if err != nil {
		return
	}
	have := map[string]bool{}
	for _, in := range infos {
		have[in.Package] = true
	}
	for name := range cached {
		have[name] = true
	}
	seen := map[string]bool{}
	for _, in := range infos {
		for _, d := range in.Depends {
			if !installed[d] && !have[d] && !seen[d] {
				seen[d] = true
				missing = append(missing, d)
			}
		}
	}
	sort.Strings(missing)
	return
}

type fenceFeed struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Gz   bool   `json:"gz"`
}

// fenceProfile is what the connection check learns about a router
// (its package feeds, installed set and missing dependencies), so
// the download step can fetch exactly the .ipk files the install
// will need while the phone still has internet.
type fenceProfile struct {
	Release   string      `json:"release"`
	Feeds     []fenceFeed `json:"feeds"`
	Installed []string    `json:"installed"`
	Missing   []string    `json:"missing"`
}

func fenceProfilePath() string { return filepath.Join(fenceCacheDir(), "router.json") }

func fenceSaveProfile(p *fenceProfile) {
	if err := os.MkdirAll(fenceCacheDir(), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	os.WriteFile(fenceProfilePath(), b, 0o644)
}

func fenceLoadProfile() *fenceProfile {
	b, err := os.ReadFile(fenceProfilePath())
	if err != nil {
		return nil
	}
	var p fenceProfile
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return &p
}

func fenceParseFeeds(conf string) []fenceFeed {
	var out []fenceFeed
	for _, line := range strings.Split(conf, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && (f[0] == "src/gz" || f[0] == "src") {
			out = append(out, fenceFeed{Name: f[1], URL: f[2], Gz: f[0] == "src/gz"})
		}
	}
	return out
}

func gunzipBytes(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(gz)
}

type pkgStanza struct {
	name, file string
	deps       []string
}

func parsePackagesIndex(text string) []pkgStanza {
	var out []pkgStanza
	for _, block := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		f := parseControlFields(block)
		if f["Package"] == "" || f["Filename"] == "" {
			continue
		}
		out = append(out, pkgStanza{f["Package"], f["Filename"], depNamesOf(f["Depends"])})
	}
	return out
}

// fenceFetchDeps downloads the profile's missing packages (and,
// recursively, their own missing dependencies) from the router's
// feeds into the local cache. It runs on the phone/PC side, which
// has internet at download time.
func fenceFetchDeps(prof *fenceProfile) (fetched []string, unresolved []string, err error) {
	type stanza struct {
		file string
		deps []string
		feed fenceFeed
	}
	index := map[string]stanza{}
	for _, f := range prof.Feeds {
		u := f.URL + "/Packages"
		if f.Gz {
			u += ".gz"
		}
		raw, err := httpGet(u)
		if err != nil {
			return nil, nil, err
		}
		if f.Gz {
			if raw, err = gunzipBytes(raw); err != nil {
				return nil, nil, err
			}
		}
		for _, st := range parsePackagesIndex(string(raw)) {
			if _, dup := index[st.name]; !dup {
				index[st.name] = stanza{st.file, st.deps, f}
			}
		}
	}
	installed := map[string]bool{}
	for _, n := range prof.Installed {
		installed[n] = true
	}
	selected := map[string]stanza{}
	queue := append([]string{}, prof.Missing...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if installed[name] || name == "kernel" {
			continue
		}
		if _, ok := selected[name]; ok {
			continue
		}
		st, ok := index[name]
		if !ok {
			unresolved = append(unresolved, name)
			continue
		}
		selected[name] = st
		queue = append(queue, st.deps...)
	}
	dir := filepath.Join(fenceCacheDir(), "deps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	for name, st := range selected {
		base := path.Base(st.file)
		if !fenceDepFileRe.MatchString(base) {
			unresolved = append(unresolved, name)
			continue
		}
		data, err := httpGet(st.feed.URL + "/" + st.file)
		if err != nil {
			return nil, nil, err
		}
		if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
			return nil, nil, errors.New("downloaded dependency is not a gzip archive: " + base)
		}
		if err := os.WriteFile(filepath.Join(dir, base), data, 0o644); err != nil {
			return nil, nil, err
		}
		fetched = append(fetched, name)
	}
	sort.Strings(fetched)
	sort.Strings(unresolved)
	return fetched, unresolved, nil
}

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
	isApk := strings.HasPrefix(release, "25.12.")
	if isApk {
		// The apk flavour keeps the upstream flow: refresh the
		// package lists on the router, then add the local files.
		if _, err := fenceRunStdin(cl, fenceWgetWrapCmd, "", send); err != nil {
			return err
		}
		if _, err := fenceRunStdin(cl, fencePathPrefix+fenceApkUpdateCmd, "", send); err != nil {
			return fmt.Errorf("%s: %w | %s", fenceApkUpdateCmd, err, fenceNetDiag(cl, send))
		}
		if _, err := fenceRunStdin(cl, "cd "+dir+" && "+fencePathPrefix+inst, "", send); err != nil {
			return err
		}
	} else {
		// Offline opkg install (see the dependency notes above):
		// no package lists, no router internet. Dependencies must
		// already be installed or sit in the local cache as files
		// the phone downloaded earlier; otherwise stop and name
		// them instead of letting opkg fail with "Unknown package".
		_, installed, cached, missing, err := fenceDepState(cl, tag)
		_ = installed
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return &fenceMissingError{Names: missing}
		}
		var depFiles []string
		for _, f := range cached {
			depFiles = append(depFiles, f)
		}
		sort.Strings(depFiles)
		if len(depFiles) > 0 {
			if _, err := fenceRunStdin(cl, "mkdir -p /tmp/swfence-deps", "", send); err != nil {
				return err
			}
			for _, f := range depFiles {
				base := filepath.Base(f)
				if !fenceDepFileRe.MatchString(base) {
					return errors.New("bad dependency file name: " + base)
				}
				b, err := os.ReadFile(f)
				if err != nil {
					return err
				}
				if _, err := fenceRunStdin(cl, "cat > /tmp/swfence-deps/"+base, string(b), send); err != nil {
					return err
				}
				inst += " /tmp/swfence-deps/" + base
			}
		}
		if _, err := fenceRunStdin(cl, "cd "+dir+" && "+inst, "", send); err != nil {
			// opkg's exit code is unreliable in edge cases (it can
			// report errors after configuring everything); the
			// ground truth is whether swfence is on the router now.
			if out, _ := fenceExec(cl, "command -v swfence"); strings.TrimSpace(out) == "" {
				return err
			}
			send("opkg reported an error, but swfence is installed; continuing")
		}
	}
	if _, err := fenceRunStdin(cl, fenceRepoCmd, "", send); err != nil {
		return err
	}
	return nil
}
