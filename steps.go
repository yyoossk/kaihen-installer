package main

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// buildStage compresses the loader payload into the hex stage the save
// trigger carries: decoder + eval(inflate(base64(raw-deflate(wrapped)))).
func buildStage(core string) (string, error) {
	wrapped := "(async function() {\ntry{\n" + core +
		"\n} catch (e) {\nawait log('EXCEPTION: ' + e.message);\nawait log(e.stack);\n}\n})();"
	var buf bytes.Buffer
	fw, err := flate.NewWriter(&buf, flate.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write([]byte(wrapped)); err != nil {
		return "", err
	}
	if err := fw.Close(); err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	stage := decoderText + "\neval(str(inflate(B64(\"" + b64 + "\"))));"
	return hex.EncodeToString([]byte(stage)), nil
}

// stepA installs the persistent trigger save (XSTRIG.Civ7Save) through the
// running loader: build the stage, upload the save template in chunks,
// then run the apply payload, which splices the stage into the template
// and writes the trigger on the console.
func stepA(ip, lang string, log logFn) error {
	raw, err := os.ReadFile(loaderFile())
	if err != nil {
		return errors.New(tr(lang, "loaderReadErr"))
	}
	core := strings.ReplaceAll(string(raw), "\r\n", "\n")
	stageHex, err := buildStage(core)
	if err != nil {
		return errors.New(tr(lang, "stageErr"))
	}
	log(tr(lang, "stageOk", len(stageHex)))

	tpl, err := os.ReadFile(pathOf("assets", "save_template.bin"))
	if err != nil {
		return errors.New(tr(lang, "tplReadErr"))
	}
	if len(tpl) < 4096 || string(tpl[:4]) != "CIV7" {
		return errors.New(tr(lang, "tplBad"))
	}
	tplB64 := base64.StdEncoding.EncodeToString(tpl)

	stagerJSON, _ := json.Marshal(stagerText)
	stageJSON, _ := json.Marshal(stageHex)
	applyJS := strings.ReplaceAll(blockTemplate, "__STAGER__", string(stagerJSON))
	applyJS = strings.ReplaceAll(applyJS, "__STAGE_HEX__", string(stageJSON))
	applyJS = strings.ReplaceAll(applyJS, "__TPL_B64_LEN__", fmt.Sprint(len(tplB64)))

	if _, err := sendCode(ip, "log('STARTING INSTALLER');window.__tpl=null;'reset'", 30*time.Second); err != nil {
		return errors.New(tr(lang, "connErr"))
	}
	n := (len(tplB64) + tplChunk - 1) / tplChunk
	t0 := time.Now()
	for i := 0; i < n; i++ {
		part := tplB64[i*tplChunk : min((i+1)*tplChunk, len(tplB64))]
		out, err := sendCode(ip, fmt.Sprintf("(window.__tpl=window.__tpl||[]).push('%s');'chunk %d/%d'", part, i+1, n), 300*time.Second)
		if err != nil || !strings.Contains(out, "Executed") {
			return errors.New(tr(lang, "chunkSendErr", i+1, n))
		}
		log(fmt.Sprintf("chunk %d/%d", i+1, n))
	}
	log(tr(lang, "tplSent", n, time.Since(t0).Seconds()))

	out, err := sendCode(ip, applyJS, 300*time.Second)
	if err != nil {
		return errors.New(tr(lang, "applyErr"))
	}
	log(strings.TrimSpace(out))
	if !strings.Contains(out, "INSTALL DONE") {
		return errors.New(tr(lang, "noDone"))
	}
	return nil
}

type lineWriter struct{ log logFn }

func (w *lineWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.ReplaceAll(string(p), "\r", ""), "\n") {
		if line != "" {
			w.log(line)
		}
	}
	return len(p), nil
}
