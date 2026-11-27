package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Self-update: the installer repo's GitHub releases carry the Windows
// zip and the Android APK of each build, named by the build stamp.
// At startup the page asks /api/update/check whether a newer release
// exists; /api/update/apply performs the platform's update:
//   - Windows: download the zip, swap the exe and the bundled resource
//     folders (assets/, loader/, pico/) and restart. Everything else
//     beside the exe (settings, downloaded homebrew, the kaiHEN
//     folder, staged SD files, the consent record) is user data and is
//     never touched. The replaced exe is kept as kaiHEN Installer.exe.old
//     until the next ordinary launch, so a broken update can be undone
//     by renaming it back.
//   - Android: download the APK into the app's external files folder;
//     the page then asks the shell (KaihenBridge) to hand it to the
//     system installer. App data survives an APK overwrite install.
// Other platforms only report the check result.

const updateRepo = "yyoossk/kaihen-installer"

var stampRe = regexp.MustCompile(`^\d{8}-\d{4}$`)

// latestRelease resolves the newest release of the installer repo from
// its release pages (no API quota): the tag from the /releases/latest
// redirect, and the asset download URLs from the expanded_assets
// fragment, keyed by file extension (".zip" / ".apk").
func latestRelease() (string, map[string]string, error) {
	_, final, err := httpGetFinal("https://github.com/" + updateRepo + "/releases/latest")
	if err != nil {
		return "", nil, err
	}
	i := strings.Index(final, "/releases/tag/")
	if i < 0 {
		return "", nil, errors.New("no release tag")
	}
	tag, err := url.PathUnescape(strings.TrimSuffix(final[i+len("/releases/tag/"):], "/"))
	if err != nil || !stampRe.MatchString(tag) {
		return "", nil, errors.New("no release tag")
	}
	body, err := httpGet("https://github.com/" + updateRepo + "/releases/expanded_assets/" + url.PathEscape(tag))
	if err != nil {
		return "", nil, err
	}
	re := regexp.MustCompile(`href="(/[^"]+/releases/download/[^"]+)"`)
	assets := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		href := m[1]
		ext := strings.ToLower(filepath.Ext(href))
		if (ext == ".zip" || ext == ".apk") && assets[ext] == "" {
			assets[ext] = "https://github.com" + href
		}
	}
	return tag, assets, nil
}

// releaseNotes fetches a release's notes text from the GitHub API
// (best effort: "" when the API is unreachable or rate-limited).
// The in-app history cannot serve this: the running build's bundled
// notes end at its own version, so the new release's notes have to
// come from the release itself.
func releaseNotes(tag string) string {
	raw, err := httpGet("https://api.github.com/repos/" + updateRepo + "/releases/tags/" + url.PathEscape(tag))
	if err != nil {
		return ""
	}
	var rec struct {
		Body string `json:"body"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return ""
	}
	body := strings.TrimSpace(rec.Body)
	if len(body) > 3000 {
		body = body[:3000]
	}
	return body
}

func updateCheckHandler(w http.ResponseWriter, r *http.Request) {
	tag, _, err := latestRelease()
	if err != nil {
		jsonOK(w, map[string]any{"available": false, "current": buildStamp})
		return
	}
	out := map[string]any{"available": tag > buildStamp, "current": buildStamp, "latest": tag}
	if tag > buildStamp {
		out["notes"] = releaseNotes(tag)
	}
	jsonOK(w, out)
}

func updateApply(ip, lang string, log logFn) error {
	tag, assets, err := latestRelease()
	if err != nil {
		return errors.New(tr(lang, "apiErr"))
	}
	if tag <= buildStamp {
		return errors.New(tr(lang, "updNone"))
	}
	switch runtime.GOOS {
	case "windows":
		return applyUpdateWindows(tag, assets[".zip"], log, lang)
	case "android":
		return applyUpdateAndroid(tag, assets[".apk"], log, lang)
	}
	return errors.New(tr(lang, "updUnsupported"))
}

// stageZip downloads a release zip and extracts it under dir,
// rejecting absolute or parent-escaping entry names.
func stageZip(zipURL, dir string, log logFn, lang string) error {
	log(tr(lang, "fetching", zipURL))
	raw, err := httpGetBig(zipURL)
	if err != nil {
		return errors.New(tr(lang, "dlErr"))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return errors.New(tr(lang, "zipErr"))
	}
	os.RemoveAll(dir)
	for _, f := range zr.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		dst := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(dst, 0o755)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return errors.New(tr(lang, "zipErr"))
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return errors.New(tr(lang, "zipErr"))
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return errors.New(tr(lang, "writeErr"))
		}
	}
	return nil
}

func applyUpdateWindows(tag, zipURL string, log logFn, lang string) error {
	if zipURL == "" {
		return errors.New(tr(lang, "appNoAsset"))
	}
	stage := pathOf(".update")
	if err := stageZip(zipURL, stage, log, lang); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	newExe := filepath.Join(stage, "kaiHEN Installer.exe")
	if _, err := os.Stat(newExe); err != nil {
		return errors.New(tr(lang, "missingReq"))
	}
	exePath, err := os.Executable()
	if err != nil {
		return errors.New(tr(lang, "writeErr"))
	}
	oldPath := exePath + ".old"
	os.Remove(oldPath)
	if err := os.Rename(exePath, oldPath); err != nil {
		return errors.New(tr(lang, "writeErr"))
	}
	if err := copyFile(newExe, exePath); err != nil {
		os.Rename(oldPath, exePath)
		return errors.New(tr(lang, "writeErr"))
	}
	// Bundled resource folders are replaced wholesale; they hold no
	// user data. Everything else beside the exe is left as it is.
	for _, d := range []string{"assets", "loader", "pico"} {
		src := filepath.Join(stage, d)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := pathOf(d)
		os.RemoveAll(dst)
		if err := copyTree(src, dst); err != nil {
			return errors.New(tr(lang, "writeErr"))
		}
	}
	log(tr(lang, "updRestart"))
	cmd := exec.Command(exePath)
	cmd.Env = append(os.Environ(), "KAIHEN_UPDATED=1")
	if err := cmd.Start(); err != nil {
		return errors.New(tr(lang, "writeErr"))
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func applyUpdateAndroid(tag, apkURL string, log logFn, lang string) error {
	if apkURL == "" {
		return errors.New(tr(lang, "appNoAsset"))
	}
	dir := os.Getenv("KAIHEN_EXTERNAL_DIR")
	if dir == "" {
		return errors.New(tr(lang, "updUnsupported"))
	}
	log(tr(lang, "fetching", apkURL))
	raw, err := httpGetBig(apkURL)
	if err != nil {
		return errors.New(tr(lang, "dlErr"))
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return errors.New(tr(lang, "badContent"))
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "AndroidManifest.xml" {
			found = true
			break
		}
	}
	if !found {
		return errors.New(tr(lang, "badContent"))
	}
	os.MkdirAll(dir, 0o755)
	dst := filepath.Join(dir, "kaihen-update.apk")
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return errors.New(tr(lang, "writeErr"))
	}
	// The page watches for this line and hands the file to the shell,
	// which opens the system installer for it.
	log("UPDATEAPK|kaihen-update.apk")
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// cleanupUpdateLeftovers removes the previous build's exe backup and
// staging folder. It runs on ordinary launches only: the first launch
// of a freshly updated build (KAIHEN_UPDATED set) keeps the backup so
// the update can still be undone by hand.
func cleanupUpdateLeftovers() {
	if os.Getenv("KAIHEN_UPDATED") != "" {
		return
	}
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
	os.RemoveAll(pathOf(".update"))
}
