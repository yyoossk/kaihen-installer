// kaiHEN Installer — standalone GUI for installing kaiHEN and
// homebrew on Switch 2 through the CivSwitch loader chain.
//
// No Python / Node installation is needed on the user's machine; the
// bundled Node.js (runtime/node.exe) runs the kaiHEN launcher engine.
//
// Folder layout (everything relative to this executable):
//
//	loader/core.js            the loader payload — replace this file to
//	                          install a future loader version
//	assets/save_template.bin  save template for the loader save (XSTRIG)
//	assets/kaihen/            kaiHEN launcher engine (civ7-fs-kaihen)
//	runtime/                  bundled Node.js
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed decoder.txt
var decoderText string // JS inflate decoder, verbatim from compress_stage.js

//go:embed block.txt
var blockTemplate string // apply-payload JS from installer-creator/install.py

//go:embed stager.txt
var stagerText string // STAGER string from installer-creator/install.py

const loaderPort = 9026
const tplChunk = 400000 // b64 chars per loader connection (install.py CHUNK)

var (
	exeDir    string
	busyMu    sync.Mutex
	running   bool
	runningMu sync.Mutex
)

func pathOf(parts ...string) string {
	return filepath.Join(append([]string{exeDir}, parts...)...)
}

// sseLang streams a language-only operation (no console IP involved) with
// the standard RESULT framing.
func sseLang(fn func(lang string, log logFn) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Lang string `json:"lang"`
		}
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}
		lang := body.Lang
		if lang == "" {
			lang = r.URL.Query().Get("lang")
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
		if err := fn(lang, send); err != nil {
			send("RESULT|ng")
			send(tr(lang, "failed") + err.Error())
		} else {
			send("RESULT|ok")
			send(tr(lang, "done"))
		}
	}
}

func main() {
	setupExeDir()
	appsInit()
	mux := http.NewServeMux()
	mux.HandleFunc("/", pageHandler)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.HandleFunc("/api/check", checkHandler)
	mux.HandleFunc("/api/quit", quitHandler)
	mux.HandleFunc("/api/consentState", consentStateHandler)
	mux.HandleFunc("/api/consentAgree", consentAgreeHandler)
	mux.HandleFunc("/api/openfolder", openFolderHandler)
	mux.HandleFunc("/api/ipconfig", ipconfigHandler)
	mux.HandleFunc("/api/stepA", sseHandler(stepA))
	mux.HandleFunc("/api/autoloader", sseHandler(autoloaderInstall))
	mux.HandleFunc("/api/autoloaderSd", sseHandlerNoIP(autoloaderSdPrepare))
	mux.HandleFunc("/api/kaihenStart", kaihenStartHandler)
	mux.HandleFunc("/api/nroFtp", nroFtpHandler)
	mux.HandleFunc("/api/apps", appsListHandler)
	mux.HandleFunc("/api/apps/add", appsAddHandler)
	mux.HandleFunc("/api/apps/delete", appsDeleteHandler)
	mux.HandleFunc("/api/apps/install", appsInstallHandler)
	mux.HandleFunc("/api/hbDeleteFile", hbDeleteFileHandler)
	mux.HandleFunc("/api/fencechk", fenceChkHandler)
	mux.HandleFunc("/api/fenceinstall", fenceInstallHandler)
	mux.HandleFunc("/api/fencestatus", fenceStatusHandler)
	mux.HandleFunc("/api/fenceoff", fenceOffHandler)
	mux.HandleFunc("/api/oweinstall", oweInstallHandler)
	mux.HandleFunc("/api/oweverify", oweVerifyHandler)

	clearWebViewCache()
	var ln net.Listener
	var err error
	for _, p := range []int{47653, 47654, 47655, 47656, 47657} {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			break
		}
	}
	if ln == nil {
		fmt.Fprintln(os.Stderr, "no free port in 47653-47657")
		os.Exit(1)
	}
	addr := ln.Addr().String()
	url := fmt.Sprintf("http://%s/?v=%d", addr, time.Now().UnixNano())
	fmt.Println("CivInstaller GUI:", url)
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	if !openEmbeddedUI(url) {
		openBrowser(url)
		select {}
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
