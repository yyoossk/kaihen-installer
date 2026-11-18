package main

// Offline WAN Emulator (TheNxtBigThing8) — a Raspberry Pi 4B that sits
// on the router's WAN port and answers the console's connection test
// locally, so the test passes with no real internet uplink.
//
// This file installs the pinned release's scripts onto the Pi over
// SSH: setup_pi_wan.sh stages the configuration (run while the Pi is
// still on a LAN port), verify_pi_wan.sh checks the result after the
// cable has been moved to the WAN port (the Pi is then 10.42.0.1).

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const oweRepo = "TheNxtBigThing8/Offline-WAN-Emulator"
const oweTag = "v0.1.0-beta"

// oweScripts returns the two release scripts, downloading the pinned
// tag's source archive from GitHub on first use and caching the
// scripts in the wanemu folder next to the executable.
func oweScripts(lang string, log logFn) (setup, verify []byte, err error) {
	dir := pathOf("wanemu")
	sp := filepath.Join(dir, "setup_pi_wan.sh")
	vp := filepath.Join(dir, "verify_pi_wan.sh")
	if b1, e1 := os.ReadFile(sp); e1 == nil && len(b1) > 0 {
		if b2, e2 := os.ReadFile(vp); e2 == nil && len(b2) > 0 {
			return b1, b2, nil
		}
	}
	log(tr(lang, "oweDl"))
	raw, err := httpGet("https://codeload.github.com/" + oweRepo + "/zip/refs/tags/" + oweTag)
	if err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, nil, err
	}
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if base != "setup_pi_wan.sh" && base != "verify_pi_wan.sh" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			return nil, nil, err
		}
		rc.Close()
		if base == "setup_pi_wan.sh" {
			setup = buf.Bytes()
		} else {
			verify = buf.Bytes()
		}
	}
	if len(setup) == 0 || len(verify) == 0 {
		return nil, nil, errors.New("scripts not found in the " + oweTag + " archive")
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(sp, setup, 0o644)
	os.WriteFile(vp, verify, 0o644)
	return setup, verify, nil
}

func oweDial(ip, user, pass string) (*ssh.Client, error) {
	host := strings.TrimSpace(ip)
	if !strings.Contains(host, ":") {
		host += ":22"
	}
	conf := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	return sshDialLAN(host, conf)
}

func oweInstallHandler(w http.ResponseWriter, r *http.Request) { oweServe(w, r, false) }
func oweVerifyHandler(w http.ResponseWriter, r *http.Request)  { oweServe(w, r, true) }

func oweServe(w http.ResponseWriter, r *http.Request, verifyOnly bool) {
	q := r.URL.Query()
	ip := strings.TrimSpace(q.Get("ip"))
	user := strings.TrimSpace(q.Get("user"))
	pass := q.Get("pass")
	lang := q.Get("lang")

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
	if ip == "" || user == "" {
		fail(errors.New(tr(lang, "oweBadInput")))
		return
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

	setup, verify, err := oweScripts(lang, send)
	if err != nil {
		fail(err)
		return
	}
	send(tr(lang, "oweConn"))
	cl, err := oweDial(ip, user, pass)
	if err != nil {
		fail(errors.New(tr(lang, "oweSSH") + err.Error()))
		return
	}
	defer cl.Close()

	send(tr(lang, "oweUp"))
	if verifyOnly {
		if _, err := fenceRunStdin(cl, "cat > /tmp/verify_pi_wan.sh && chmod +x /tmp/verify_pi_wan.sh", string(verify), send); err != nil {
			fail(err)
			return
		}
		send(tr(lang, "oweVerifyRun"))
		out, _ := fenceRunStdin(cl, "sudo -S -p '' bash /tmp/verify_pi_wan.sh 2>&1", pass+"\n", send)
		if !strings.Contains(out, "== Pi network ==") {
			fail(errors.New(tr(lang, "oweNoStage")))
			return
		}
		send("RESULT|ok")
		send(tr(lang, "done"))
		return
	}
	if _, err := fenceRunStdin(cl, "cat > /tmp/setup_pi_wan.sh && chmod +x /tmp/setup_pi_wan.sh", string(setup), send); err != nil {
		fail(err)
		return
	}
	if _, err := fenceRunStdin(cl, "cat > /tmp/verify_pi_wan.sh && chmod +x /tmp/verify_pi_wan.sh", string(verify), send); err != nil {
		fail(err)
		return
	}
	send(tr(lang, "oweRun"))
	out, _ := fenceRunStdin(cl, "sudo -S -p '' bash /tmp/setup_pi_wan.sh 2>&1", pass+"\n", send)
	if !strings.Contains(out, "STAGED SUCCESSFULLY") {
		fail(errors.New(tr(lang, "oweNoStage")))
		return
	}
	send(tr(lang, "oweStaged"))
	send("RESULT|ok")
	send(tr(lang, "done"))
}
