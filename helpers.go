package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
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
)

type logFn func(string)

func setupExeDir() {
	if h := os.Getenv("CIVINSTALLER_HOME"); h != "" {
		exeDir = h
		return
	}
	exe, err := os.Executable()
	if err != nil {
		exeDir = "."
		return
	}
	exeDir = filepath.Dir(exe)
}


func nodeExe() string {
	if p := os.Getenv("CIVINSTALLER_NODE"); p != "" {
		return p
	}
	p := pathOf("runtime", "node.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return "node"
}

func loaderFile() string { return pathOf("loader", "core.js") }

func validIP(s string) bool {
	return net.ParseIP(strings.TrimSpace(s)) != nil
}

// sendCode delivers one JS payload to the loader on ip:9026, half-closes the
// write side and reads the loader's reply until EOF or the timeout.
func sendCode(ip, code string, timeout time.Duration) (string, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(loaderPort)), 8*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(code)); err != nil {
		return "", err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	conn.SetReadDeadline(time.Now().Add(timeout))
	var sb strings.Builder
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				break
			}
			if sb.Len() > 0 {
				break
			}
			return sb.String(), err
		}
	}
	return sb.String(), nil
}

// probeLoader checks whether the loader on the loader port is alive, using
// the same probe as the bundled install.py: a loader only sends back text
// the payload itself prints with log(), so the probe asks it to log a unique
// token and requires that token in the reply. The second return value says
// why the probe failed: "err:<go error>" (could not even exchange data),
// "noreply" (connected and closed, but zero bytes came back), or
// "mismatch:<reply head>" (something answered, but not the loader's echo).
func probeLoader(ip string) (bool, string) {
	var rb [16]byte
	tok := "IZK_PROBE_"
	if _, err := rand.Read(rb[:]); err == nil {
		tok += hex.EncodeToString(rb[:])
	} else {
		tok += fmt.Sprint(time.Now().UnixNano())
	}
	out, err := sendCode(ip, "log('"+tok+"')", 15*time.Second)
	if strings.Contains(out, tok) {
		return true, ""
	}
	if err != nil {
		return false, "err:" + err.Error()
	}
	if len(out) == 0 {
		return false, "noreply"
	}
	head := strings.TrimSpace(out)
	if len(head) > 80 {
		head = head[:80]
	}
	return false, "mismatch:" + head
}

func sseHandler(fn func(ip, lang string, log logFn) error) http.HandlerFunc {
	return sseHandlerMode(fn, "run")
}

// sseHandlerMode streams fn's log lines as server-sent events and appends a
// final RESULT|ok / RESULT|ng marker (mode "run") before the "=== " line.
func sseHandlerMode(fn func(ip, lang string, log logFn) error, mode string) http.HandlerFunc {
	return sseHandlerModeIP(fn, mode, true)
}

// sseHandlerNoIP is sseHandler for jobs that never talk to the console
// (e.g. preparing files on the PC), so no console IP is required.
func sseHandlerNoIP(fn func(ip, lang string, log logFn) error) http.HandlerFunc {
	return sseHandlerModeIP(fn, "run", false)
}

func sseHandlerModeIP(fn func(ip, lang string, log logFn) error, mode string, needIP bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IP   string `json:"ip"`
			Lang string `json:"lang"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		ip := body.IP
		if ip == "" {
			ip = r.URL.Query().Get("ip")
		}
		lang := body.Lang
		if lang == "" {
			lang = r.URL.Query().Get("lang")
		}
		if needIP && !validIP(ip) {
			http.Error(w, "invalid ip", http.StatusBadRequest)
			return
		}
		runningMu.Lock()
		if running {
			runningMu.Unlock()
			http.Error(w, "busy", http.StatusConflict)
			return
		}
		running = true
		runningMu.Unlock()
		defer func() {
			runningMu.Lock()
			running = false
			runningMu.Unlock()
		}()
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
		err := fn(ip, lang, send)
		if mode == "run" {
			if err == nil {
				send("RESULT|ok")
				send(tr(lang, "done"))
			} else {
				send("RESULT|ng")
				send(tr(lang, "failed") + err.Error())
			}
		} else {
			if err == nil {
				send(tr(lang, "done"))
			} else {
				send(tr(lang, "failed") + err.Error())
			}
		}
	}
}

// scanLines streams a reader's lines to log until EOF.
func scanLines(r io.Reader, log logFn) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		log(sc.Text())
	}
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
