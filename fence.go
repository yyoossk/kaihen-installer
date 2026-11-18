package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	return ssh.Dial("tcp", host, conf)
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
	jsonOK(w, map[string]any{
		"ok": true, "supported": true, "release": rel,
		"installed": installed, "enabled": enabled, "guards": fenceGuardsUp(cl),
		"leases": fenceLeases(cl),
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
	tag, err := fenceLatestTag()
	if err != nil || tag == "" {
		fail(errors.New(tr(lang, "fTagFail")))
		return
	}
	send(tr(lang, "fTag") + tag)

	// back up the router configuration to the PC first
	if _, err := fenceExec(cl, "sysupgrade -b /tmp/swfence-backup.tar.gz"); err != nil {
		fail(errors.New(tr(lang, "fBkFail")))
		return
	}
	b64out, err := fenceExec(cl, "base64 /tmp/swfence-backup.tar.gz")
	if err != nil {
		fail(errors.New(tr(lang, "fBkFail")))
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(b64out), "\n", ""))
	if err != nil || len(raw) == 0 {
		fail(errors.New(tr(lang, "fBkFail")))
		return
	}
	os.MkdirAll(pathOf("swfence-backups"), 0o755)
	bkName := "swfence-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	if err := os.WriteFile(filepath.Join(pathOf("swfence-backups"), bkName), raw, 0o644); err != nil {
		fail(errors.New(tr(lang, "fBkFail")))
		return
	}
	send(tr(lang, "fBackup") + bkName)

	// flow offloading must be off for the nft rules to see every packet
	fenceExec(cl, "uci set network.@device[0].flow_offloading='0'; uci set network.@device[0].flow_offloading_hw='0'; uci commit network")
	send(tr(lang, "fOff"))

	send(tr(lang, "fInst"))
	dl := fmt.Sprintf("wget -O /tmp/swfence-install.sh https://github.com/byizmysh/SWFence/releases/download/%s/install.sh && sh /tmp/swfence-install.sh", tag)
	if _, err := fenceRunStdin(cl, dl, "", send); err != nil {
		fail(errors.New(tr(lang, "fNoSw")))
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
