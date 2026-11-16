package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Autoloader save (SeanHeuc/civ-autoloader) -------------------------------
//
// The autoloader save carries the CivSwitchFS + kaiHEN autoloader payload:
// once it is installed as XSTRIG.Civ7Save, every "Load Game" runs the FS
// acquisition and the kaiHEN boot on the console itself, with no PC and no
// network. The save is installed through the already-running loader on
// :9026, using the exact protocol of the fork's tools/persist-install.mjs:
//
//   1. send the persist bootstrap (build artifact of SeanHeuc/CivSwitchFS
//      @59a1ea6; it is not published anywhere downloadable, so it is
//      embedded here and pinned by SHA-256),
//   2. send the pristine save template (downloaded from the same fork,
//      pinned by size + SHA-256) as base64 chunks of 400,000 chars,
//   3. call installFromTemplate() and wait for the completion marker.
//
// The ESP32 firmware of civ-autoloader ships byte-identical artifacts
// (its make_persist_blobs.py reads the same dist/persist outputs), so this
// network install produces the same save the board would.
//
// The SD card files come from SeanHeuc/civ-autoloader @8709c54
// (packages/sdcard/vendor), each pinned by SHA-256. hbmenu.nro is patched
// in place after its hash verifies: the textInit() failure (2345-0011,
// ounce-hbmenu's setMakeLanguage returning BadInput when the console
// language is not in the available list) is turned into success by
// replacing the `ldr w0, [sp, #0x1c]` at file offset 0xa5608 with
// `mov w0, #0`, exactly the patch verified against the bundled binary.

//go:embed autoloader_bootstrap.js
var autoloaderBootstrap []byte

const (
	autoloaderBootstrapSHA = "8179c983aa2853ca4f56fa3f4bd9315eb1f304e85ec5fd6562145b9332082b9b"

	autoloaderTplURL  = "https://raw.githubusercontent.com/SeanHeuc/CivSwitchFS/59a1ea6667944583b2305412472ec319deeee009/persist/save_template.bin"
	autoloaderTplSize = 1159730
	autoloaderTplSHA  = "32fa8a0d6dd0a480d8e541e63ea44c4619bc6f7be4282cfc3cb6a4f6d305dfae"

	autoloaderVendorBase = "https://raw.githubusercontent.com/SeanHeuc/civ-autoloader/8709c5459b3ca92be684b0f8644014c9d7557340/packages/sdcard/vendor/"

	hbmenuSrcSHA     = "274df7a0e95334c4746f98552e57d3d9c897d9ae98b7d247aa8e1841e63cb797"
	hbmenuPatchedSHA = "420cd59e0ba1644e52d7cc63b568390ec31e03bbb10f1ddc09baf94f3bf3063e"
	hbmenuPatchOff   = 0xa5608
)

var hbmenuOrigIns = []byte{0xe0, 0x1f, 0x40, 0xb9}  // ldr w0, [sp, #0x1c]
var hbmenuPatchIns = []byte{0x00, 0x00, 0x80, 0x52} // mov w0, #0

// autoloaderSDFile is one SD card file: repo path under vendor/, destination
// path inside the autoloader-sd folder, and the SHA-256 of the file as it
// must finally sit on disk (for hbmenu.nro: after the textInit patch).
type autoloaderSDFile struct {
	src   string
	dst   string
	sha   string
	srcDl string // hash of the downloaded bytes, when different from sha
}

var autoloaderSDFiles = []autoloaderSDFile{
	{src: "hbmenu.nro", dst: "hbmenu.nro", sha: hbmenuPatchedSHA, srcDl: hbmenuSrcSHA},
	{src: "bootstrap.bin", dst: filepath.Join("kaihen", "bootstrap.bin"), sha: "dee76b820d4504e39ba360e238226a0c96665a443bfd0cb334e34ba38b4c068d"},
	{src: "hen/main", dst: filepath.Join("kaihen", "bin", "hen", "main"), sha: "7220bbc935cb364aeb1f7381d442ea4e22c6bf767e18e4eb5dd8a6bb44a9265a"},
	{src: "ro/main", dst: filepath.Join("kaihen", "bin", "ro", "main"), sha: "a9a60df28f942b5a26ce55772d2185203182d4fa9feba5a2b574b7e39eb74f42"},
	{src: "game/main", dst: filepath.Join("kaihen", "bin", "game", "main"), sha: "f72ac5f94b8263a83e2ae698ca5ef91b192127d01778105293419d896247563c"},
	{src: "hen-options.txt", dst: filepath.Join("kaihen", "hen-options.txt"), sha: "1c368a92808971f885ad87982289c843708aec57103019f4b0dcef1b63ca4baf"},
	{src: "plugins/ftpd.nso", dst: filepath.Join("kaihen", "plugins", "ftpd.nso"), sha: "17e50a96491f4ec49dfc5be3ea05141a106aee6e401c7c6d468da932a3490cee"},
}

func autoloaderSDDir() string { return pathOf("autoloader-sd") }

func fileSHA(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256Hex(raw)
}

// ensureAutoloaderTemplate downloads (once, hash-pinned) the save template
// used by the install protocol: autoloader/save_template.bin beside the exe.
func ensureAutoloaderTemplate(log logFn, lang string) error {
	if sha256Hex(autoloaderBootstrap) != autoloaderBootstrapSHA {
		return errors.New(tr(lang, "alBadHb"))
	}
	tplPath := pathOf("autoloader", "save_template.bin")
	if fileSHA(tplPath) == autoloaderTplSHA {
		return nil
	}
	log(tr(lang, "alDl"))
	raw, err := httpGet(autoloaderTplURL)
	if err != nil {
		return errors.New(tr(lang, "dlErr"))
	}
	if len(raw) != autoloaderTplSize || sha256Hex(raw) != autoloaderTplSHA {
		return errors.New(tr(lang, "badContent"))
	}
	if err := os.MkdirAll(filepath.Dir(tplPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tplPath, raw, 0o644); err != nil {
		return err
	}
	return nil
}

// ensureAutoloaderSD downloads (once, hash-pinned) the SD card files,
// patches hbmenu.nro, and stages everything beside the exe in
// autoloader-sd/<SD layout>.
func ensureAutoloaderSD(log logFn, lang string) error {
	need := false
	for _, f := range autoloaderSDFiles {
		if fileSHA(filepath.Join(autoloaderSDDir(), f.dst)) != f.sha {
			need = true
		}
	}
	if need {
		log(tr(lang, "alDl"))
	}
	for _, f := range autoloaderSDFiles {
		dst := filepath.Join(autoloaderSDDir(), f.dst)
		if fileSHA(dst) == f.sha {
			continue
		}
		raw, err := httpGet(autoloaderVendorBase + f.src)
		if err != nil {
			return errors.New(tr(lang, "dlErr"))
		}
		want := f.sha
		if f.srcDl != "" {
			want = f.srcDl
		}
		if sha256Hex(raw) != want {
			return errors.New(tr(lang, "badContent"))
		}
		if f.srcDl != "" {
			// hbmenu.nro: apply the textInit patch, but only to the exact
			// verified binary and only if the instruction matches.
			if len(raw) < hbmenuPatchOff+4 ||
				string(raw[hbmenuPatchOff:hbmenuPatchOff+4]) != string(hbmenuOrigIns) {
				return errors.New(tr(lang, "alBadHb"))
			}
			copy(raw[hbmenuPatchOff:hbmenuPatchOff+4], hbmenuPatchIns)
			if sha256Hex(raw) != hbmenuPatchedSHA {
				return errors.New(tr(lang, "alBadHb"))
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
	}
	log(tr(lang, "alSdReady", autoloaderSDDir()))
	return nil
}

// autoloaderSdPrepare is the /api/autoloaderSd job: prepare the SD card
// files only. No console is involved; the page opens the folder on success.
func autoloaderSdPrepare(ip, lang string, log logFn) error {
	return ensureAutoloaderSD(log, lang)
}

func installFailedLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Installation failed: ") {
			return strings.TrimPrefix(line, "Installation failed: ")
		}
	}
	return ""
}

// autoloaderInstall is the /api/autoloader job: prepare the save template,
// then run the persist-install protocol against the loader on the console.
func autoloaderInstall(ip, lang string, log logFn) error {
	if err := ensureAutoloaderTemplate(log, lang); err != nil {
		return err
	}
	tpl, err := os.ReadFile(pathOf("autoloader", "save_template.bin"))
	if err != nil || sha256Hex(tpl) != autoloaderTplSHA {
		return errors.New(tr(lang, "badContent"))
	}

	out, err := sendCode(ip, string(autoloaderBootstrap)+"\nlog(\"Installer ready.\");", 120*time.Second)
	if err != nil {
		return errors.New(tr(lang, "connErr"))
	}
	if msg := installFailedLine(out); msg != "" {
		return errors.New(msg)
	}
	if !strings.Contains(out, "Installer ready.") {
		return errors.New(tr(lang, "alNoReady"))
	}

	var rb [16]byte
	if _, err := rand.Read(rb[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(rb[:])
	tokenJSON, _ := json.Marshal(token)

	tplB64 := base64.StdEncoding.EncodeToString(tpl)
	const chunkChars = 400000
	n := (len(tplB64) + chunkChars - 1) / chunkChars
	for i := 0; i < n; i++ {
		part := tplB64[i*chunkChars : min((i+1)*chunkChars, len(tplB64))]
		partJSON, _ := json.Marshal(part)
		marker := fmt.Sprintf("Template %d/%d received.", i+1, n)
		code := fmt.Sprintf("window.__cfsPersistInstaller.stageChunk(%s,%d,%d,%s);log(%q);",
			tokenJSON, i+1, i*chunkChars, partJSON, marker)
		ok := false
		for attempt := 0; attempt < 3 && !ok; attempt++ {
			out, err := sendCode(ip, code, 180*time.Second)
			if err == nil && strings.Contains(out, marker) {
				ok = true
			}
		}
		if !ok {
			return errors.New(tr(lang, "chunkSendErr", i+1, n))
		}
		log(fmt.Sprintf("chunk %d/%d", i+1, n))
	}

	const doneMarker = "Installed. Restart the game, then Load a game."
	final := "(async function(){try{await window.__cfsPersistInstaller.installFromTemplate();" +
		"await log(\"" + doneMarker + "\");}" +
		"catch(error){await log('Installation failed: '+error.message);}" +
		"finally{window.__cfsPersistInstaller.dispose();delete window.__cfsPersistInstaller;}})()"
	out, err = sendCode(ip, final, 300*time.Second)
	if err != nil {
		return errors.New(tr(lang, "applyErr"))
	}
	log(strings.TrimSpace(out))
	if msg := installFailedLine(out); msg != "" {
		return errors.New(msg)
	}
	if !strings.Contains(out, doneMarker) {
		return errors.New(tr(lang, "alNoDone"))
	}
	return nil
}
