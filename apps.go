package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type appEntry struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // "nro"
	Host          string `json:"host,omitempty"` // "" = GitHub, "gitlab" = GitLab
	Builtin       bool   `json:"builtin"`
	Name          string `json:"name,omitempty"`
	Source        string `json:"source,omitempty"` // "release" | "zip"
	AssetName     string `json:"assetName"`
	AssetURL      string `json:"assetUrl,omitempty"`
	ZipInner      string `json:"zipInner,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	Size          int64  `json:"size,omitempty"`
	LatestTag     string `json:"latestTag"`
	DownloadedTag string `json:"downloadedTag"`
	InstalledTag  string `json:"installedTag"`
	LastCheck     int64  `json:"lastCheck"`
}

var (
	appsMu   sync.Mutex
	appsList []*appEntry
)

func appsFile() string    { return pathOf("apps.json") }
func deletedFile() string { return pathOf("apps-deleted.json") }
func appsCacheRoot() string {
	return pathOf("apps-cache")
}
func appCacheDir(id string) string {
	return filepath.Join(appsCacheRoot(), strings.ReplaceAll(id, "/", "__"))
}
func appCacheFile(id string) (string, bool) {
	appsMu.Lock()
	defer appsMu.Unlock()
	for _, a := range appsList {
		if a.ID == id {
			p := filepath.Join(appCacheDir(id), a.AssetName)
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
			return "", false
		}
	}
	return "", false
}

func builtinApps() []*appEntry {
	return []*appEntry{
		// kaiHEN homebrew catalog (the official hblist, ouncebrew/hblist).
		// Nothing is bundled: only the repo reference is seeded here. The
		// .nro (or release zip) is downloaded from GitHub when the user
		// installs or updates, and installed through OunceShell's FTP.
		{ID: "souldbminerr/90DNS-Tester", Kind: "nro", Builtin: true, Name: "90DNS Tester", Source: "release"},
		{ID: "souldbminerr/switch_internal_speakers_nro", Kind: "nro", Builtin: true, Name: "Force Internal Speaker", Source: "release"},
		{ID: "NerdsMcGee/kaihen-ftpd-plugin", Kind: "nro", Builtin: true, Name: "FTPD Plugin", Source: "release"},
		{ID: "wowilookgood/Checkpoint", Kind: "nro", Builtin: true, Name: "Checkpoint", Source: "release"},
		{ID: "wowilookgood/PKSM", Kind: "nro", Builtin: true, Name: "PKSM", Source: "release"},
		{ID: "wowilookgood/NXGallery", Kind: "nro", Builtin: true, Name: "NXGallery", Source: "release"},
		{ID: "chiaki-ng/akira-ounce", Kind: "nro", Builtin: true, Name: "Chiaki-ng/Akira", Source: "release"},
		{ID: "NerdsMcGee/retroarch-s2", Kind: "nro", Builtin: true, Name: "RetroArch", Source: "release"},
		{ID: "wowilookgood/kaizhar", Kind: "nro", Builtin: true, Name: "Kaizhar", Source: "release"},
		{ID: "wowilookgood/mgba", Kind: "nro", Builtin: true, Name: "mGBA", Source: "release"},
		{ID: "MegaCadeDev/dolphin-nx2", Kind: "nro", Builtin: true, Name: "Dolphin NX2", Source: "release"},
		{ID: "Azkali/moonlight-nx2", Host: "gitlab", Kind: "nro", Builtin: true, Name: "Moonlight", Source: "release"},
		{ID: "XITRIX/Moonlight-Switch", Kind: "nro", Builtin: true, Name: "Moonlight-Switch", Source: "release"},
	}
}

func loadDeleted() map[string]bool {
	out := map[string]bool{}
	if raw, err := os.ReadFile(deletedFile()); err == nil {
		var ids []string
		if json.Unmarshal(raw, &ids) == nil {
			for _, id := range ids {
				out[id] = true
			}
		}
	}
	return out
}

func saveDeleted(set map[string]bool) {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	raw, _ := json.Marshal(ids)
	os.WriteFile(deletedFile(), raw, 0o644)
}

func appsInit() {
	appsMu.Lock()
	defer appsMu.Unlock()
	if appsList != nil {
		return
	}
	appsList = []*appEntry{}
	if raw, err := os.ReadFile(appsFile()); err == nil {
		json.Unmarshal(raw, &appsList)
	}
	deleted := loadDeleted()
	have := map[string]bool{}
	for _, a := range appsList {
		have[a.ID] = true
	}
	changed := false
	for _, b := range builtinApps() {
		if !have[b.ID] && !deleted[b.ID] {
			appsList = append(appsList, b)
			changed = true
		}
	}
	if changed {
		saveAppsLocked()
	}
}

func saveAppsLocked() {
	raw, _ := json.MarshalIndent(appsList, "", "  ")
	os.WriteFile(appsFile(), raw, 0o644)
}

func findAppLocked(id string) *appEntry {
	for _, a := range appsList {
		if a.ID == id {
			return a
		}
	}
	return nil
}

type relAsset struct {
	Name string
	URL  string
}

var errNoRelease = errors.New("no release")

// fetchRelease resolves the latest release of an entry from the host's
// release pages: for GitHub, the /releases/latest redirect target gives
// the tag and the release's expanded_assets fragment lists the files
// (no API quota involved); for GitLab, the releases permalink returns
// the release's asset links. When the tag is unchanged and an asset is
// already picked, assets comes back nil and the caller keeps its pick.
func fetchRelease(a *appEntry) (string, []relAsset, error) {
	if a.Host == "gitlab" {
		var rel struct {
			TagName string `json:"tag_name"`
			Assets  struct {
				Links []struct {
					Name string `json:"name"`
					URL  string `json:"url"`
				} `json:"links"`
			} `json:"assets"`
		}
		u := "https://gitlab.com/api/v4/projects/" + url.QueryEscape(a.ID) + "/releases/permalink/latest"
		if err := getJSON(u, &rel); err != nil {
			if strings.Contains(err.Error(), "404") {
				return "", nil, errNoRelease
			}
			return "", nil, err
		}
		if rel.TagName == "" {
			return "", nil, errNoRelease
		}
		if rel.TagName == a.LatestTag && a.AssetURL != "" {
			return rel.TagName, nil, nil
		}
		out := []relAsset{}
		for _, l := range rel.Assets.Links {
			out = append(out, relAsset{Name: l.Name, URL: l.URL})
		}
		return rel.TagName, out, nil
	}
	_, final, err := httpGetFinal("https://github.com/" + a.ID + "/releases/latest")
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return "", nil, errNoRelease
		}
		return "", nil, err
	}
	i := strings.Index(final, "/releases/tag/")
	if i < 0 {
		return "", nil, errNoRelease
	}
	tag, err := url.PathUnescape(strings.TrimSuffix(final[i+len("/releases/tag/"):], "/"))
	if err != nil || tag == "" {
		return "", nil, errNoRelease
	}
	if tag == a.LatestTag && a.AssetURL != "" {
		return tag, nil, nil
	}
	body, err := httpGet("https://github.com/" + a.ID + "/releases/expanded_assets/" + url.PathEscape(tag))
	if err != nil {
		return "", nil, err
	}
	re := regexp.MustCompile(`href="(/[^"]+/releases/download/[^"]+)"`)
	out := []relAsset{}
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		href := m[1]
		if seen[href] {
			continue
		}
		seen[href] = true
		name, _ := url.PathUnescape(href[strings.LastIndex(href, "/")+1:])
		out = append(out, relAsset{Name: name, URL: "https://github.com" + href})
	}
	return tag, out, nil
}

// pickNroAsset selects the downloadable asset of a kaiHEN homebrew
// release: a direct .nro when one is published, otherwise the release
// .zip (whose contents are classified when downloaded). Returns false
// when neither exists.
func pickNroAsset(a *appEntry, assets []relAsset) bool {
	for _, as := range assets {
		if strings.HasSuffix(strings.ToLower(as.Name), ".nro") {
			a.Source = "release"
			a.AssetName = as.Name
			a.AssetURL = as.URL
			a.Size = 0
			a.ZipInner = ""
			return true
		}
	}
	for _, as := range assets {
		if strings.HasSuffix(strings.ToLower(as.Name), ".zip") {
			a.Source = "zip"
			a.AssetName = ""
			a.AssetURL = as.URL
			a.Size = 0
			a.ZipInner = ""
			return true
		}
	}
	return false
}

// refreshAppLocked updates LatestTag from the entry's release page;
// with allowDownload it also fetches the file when it is missing or
// stale.
func refreshAppLocked(a *appEntry, allowDownload bool, log logFn) error {
	a.LastCheck = time.Now().Unix()
	tag, assets, err := fetchRelease(a)
	if err == errNoRelease {
		return errors.New(tr("", "appNoAsset"))
	}
	if err != nil {
		return errors.New(tr("", "apiErr"))
	}
	a.LatestTag = tag
	if assets != nil {
		if !pickNroAsset(a, assets) {
			return errors.New(tr("", "appNoAsset"))
		}
	}
	if allowDownload && a.DownloadedTag != a.LatestTag {
		return downloadAppLocked(a, log)
	}
	return nil
}

func downloadAppLocked(a *appEntry, log logFn) error {
	if a.AssetURL == "" {
		return errors.New(tr("", "appNoAsset"))
	}
	if log != nil {
		log(tr("", "fetching", a.AssetURL))
	}
	raw, err := httpGet(a.AssetURL)
	if err != nil {
		return errors.New(tr("", "dlErr"))
	}
	if a.Source == "zip" {
		return downloadNroZipLocked(a, raw)
	}
	data := raw
	if a.SHA256 != "" && sha256Hex(data) != a.SHA256 {
		return errors.New(tr("", "badSum"))
	}
	if a.Size > 0 && int64(len(data)) != a.Size {
		return errors.New(tr("", "badContent"))
	}
	dir := appCacheDir(a.ID)
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, a.AssetName), data, 0o644); err != nil {
		return errors.New(tr("", "writeErr"))
	}
	a.DownloadedTag = a.LatestTag
	return nil
}

// downloadNroZipLocked handles a kaiHEN homebrew release zip whose contents
// were not known in advance. Three shapes occur in the hblist catalog:
// a single .nro at the zip root (cached as one file, installed into
// /switch), several .nro files (installed into /switch), and a tree meant
// to be extracted at the SD root (paths under switch/ or kaihen/, e.g.
// RetroArch or the FTPD plugin) which is extracted verbatim into the
// cache's root/ folder and uploaded path by path.
func downloadNroZipLocked(a *appEntry, raw []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return errors.New(tr("", "zipErr"))
	}
	type zent struct {
		f   *zip.File
		rel string // SD-relative path when a switch/ or kaihen/ segment exists
	}
	var tree []zent
	var rootNros []*zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if name == "" || strings.HasPrefix(name, "../") || name == ".." {
			continue
		}
		parts := strings.Split(name, "/")
		segAt := -1
		for i, p := range parts {
			if p == "switch" || p == "kaihen" {
				segAt = i
				break
			}
		}
		if segAt >= 0 {
			tree = append(tree, zent{f, strings.Join(parts[segAt:], "/")})
		} else if len(parts) == 1 && strings.HasSuffix(strings.ToLower(name), ".nro") {
			rootNros = append(rootNros, f)
		}
	}
	dir := appCacheDir(a.ID)
	if len(tree) == 0 && len(rootNros) == 0 {
		return errors.New(tr("", "appNoAsset"))
	}
	if len(tree) == 0 && len(rootNros) == 1 {
		f := rootNros[0]
		rc, err := f.Open()
		if err != nil {
			return errors.New(tr("", "zipErr"))
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return errors.New(tr("", "zipErr"))
		}
		os.RemoveAll(dir)
		os.MkdirAll(dir, 0o755)
		a.AssetName = filepath.Base(f.Name)
		if err := os.WriteFile(filepath.Join(dir, a.AssetName), data, 0o644); err != nil {
			return errors.New(tr("", "writeErr"))
		}
		a.DownloadedTag = a.LatestTag
		return nil
	}
	// Tree mode: extract under root/, with loose root .nro files placed in
	// switch/ where hbmenu looks for them.
	rootDir := filepath.Join(dir, "root")
	os.RemoveAll(dir)
	os.MkdirAll(rootDir, 0o755)
	mainName := ""
	write := func(f *zip.File, rel string) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return err
		}
		dst := filepath.Join(rootDir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		return os.WriteFile(dst, data, 0o644)
	}
	for _, z := range tree {
		if err := write(z.f, z.rel); err != nil {
			return errors.New(tr("", "zipErr"))
		}
		base := filepath.Base(z.rel)
		if mainName == "" && (strings.HasSuffix(strings.ToLower(base), ".nro") || strings.HasSuffix(strings.ToLower(base), ".nso")) {
			mainName = base
		}
	}
	for _, f := range rootNros {
		if err := write(f, "switch/"+filepath.Base(f.Name)); err != nil {
			return errors.New(tr("", "zipErr"))
		}
		if mainName == "" {
			mainName = filepath.Base(f.Name)
		}
	}
	a.AssetName = mainName
	a.DownloadedTag = a.LatestTag
	return nil
}

func appView(a *appEntry) map[string]any {
	size := a.Size
	downloaded := false
	if fi, err := os.Stat(filepath.Join(appCacheDir(a.ID), a.AssetName)); err == nil {
		downloaded = true
		size = fi.Size()
	}
	if !downloaded && a.Kind == "nro" {
		if _, err := os.Stat(filepath.Join(appCacheDir(a.ID), "root")); err == nil {
			downloaded = true
		}
	}
	hasUpdate := a.LatestTag != "" && a.DownloadedTag != "" && a.LatestTag != a.DownloadedTag
	if a.Kind == "nro" {
		hasUpdate = a.LatestTag != "" &&
			((a.DownloadedTag != "" && a.LatestTag != a.DownloadedTag) ||
				(a.InstalledTag != "" && a.LatestTag != a.InstalledTag))
	}
	name := a.Name
	if name == "" {
		parts := strings.Split(a.ID, "/")
		name = parts[len(parts)-1]
	}
	return map[string]any{
		"id": a.ID, "kind": a.Kind, "builtin": a.Builtin, "name": name,
		"latestTag": a.LatestTag, "downloadedTag": a.DownloadedTag, "installedTag": a.InstalledTag,
		"size": size, "downloaded": downloaded,
		"updateAvailable": hasUpdate, "hasUpdate": hasUpdate,
	}
}

func appsListHandler(w http.ResponseWriter, r *http.Request) {
	appsInit()
	kind := r.URL.Query().Get("kind")
	force := r.URL.Query().Get("force") == "1"
	appsMu.Lock()
	defer appsMu.Unlock()
	out := []map[string]any{}
	for _, a := range appsList {
		if kind != "" && a.Kind != kind {
			continue
		}
		_, statErr := os.Stat(filepath.Join(appCacheDir(a.ID), a.AssetName))
		if a.Kind == "nro" {
			// kaiHEN homebrew is never downloaded just by listing it:
			// refresh only checks the latest release tag (at startup,
			// on demand, or when the last check is over 10 minutes old).
			if force || a.LastCheck == 0 || time.Since(time.Unix(a.LastCheck, 0)) > 10*time.Minute {
				refreshAppLocked(a, false, nil)
			}
			out = append(out, appView(a))
			continue
		}
		if statErr != nil {
			// first open of this install page: download lazily
			refreshAppLocked(a, true, nil)
		} else if force {
			refreshAppLocked(a, false, nil)
		}
		out = append(out, appView(a))
	}
	saveAppsLocked()
	jsonOK(w, map[string]any{"apps": out})
}

func parseRepoURL(rawurl string) (string, string, bool) {
	s := strings.TrimSpace(rawurl)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	host := ""
	var rest string
	switch {
	case strings.HasPrefix(s, "github.com/"):
		rest = s[len("github.com/"):]
	case strings.HasPrefix(s, "gitlab.com/"):
		host = "gitlab"
		rest = s[len("gitlab.com/"):]
	default:
		return "", "", false
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if host == "" {
		return parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), host, true
	}
	keep := []string{}
	for _, p := range parts {
		if p == "-" {
			break
		}
		keep = append(keep, p)
	}
	if len(keep) < 2 {
		return "", "", false
	}
	return strings.Join(keep, "/"), host, true
}

func appsAddHandler(w http.ResponseWriter, r *http.Request) {
	appsInit()
	var body struct {
		URL  string `json:"url"`
		Kind string `json:"kind"`
		Lang string `json:"lang"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	id, host, ok := parseRepoURL(body.URL)
	if body.Kind != "nro" {
		ok = false
	}
	if !ok {
		jsonOK(w, map[string]any{"ok": false, "error": tr(body.Lang, "appBadURL")})
		return
	}
	appsMu.Lock()
	defer appsMu.Unlock()
	if findAppLocked(id) != nil {
		jsonOK(w, map[string]any{"ok": true})
		return
	}
	a := &appEntry{ID: id, Host: host, Kind: "nro", Source: "release"}
	if err := refreshAppLocked(a, true, nil); err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	appsList = append(appsList, a)
	saveAppsLocked()
	jsonOK(w, map[string]any{"ok": true})
}

func appsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	appsInit()
	var body struct {
		ID string `json:"id"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	appsMu.Lock()
	defer appsMu.Unlock()
	a := findAppLocked(body.ID)
	if a == nil {
		jsonOK(w, map[string]any{"ok": false, "error": tr("", "appNotFound")})
		return
	}
	if a.Builtin {
		set := loadDeleted()
		set[a.ID] = true
		saveDeleted(set)
	}
	kept := appsList[:0]
	for _, x := range appsList {
		if x.ID != body.ID {
			kept = append(kept, x)
		}
	}
	appsList = kept
	os.RemoveAll(appCacheDir(body.ID))
	saveAppsLocked()
	jsonOK(w, map[string]any{"ok": true})
}

func appsInstallHandler(w http.ResponseWriter, r *http.Request) {
	appsInit()
	ip := r.URL.Query().Get("ip")
	id := r.URL.Query().Get("id")
	lang := r.URL.Query().Get("lang")
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
		send("APPRESULT|ng")
		send(tr(lang, "failed") + err.Error())
	}
	if !validIP(ip) {
		fail(errors.New(tr(lang, "ipInvalid")))
		return
	}
	appsMu.Lock()
	a := findAppLocked(id)
	appsMu.Unlock()
	if a == nil {
		fail(errors.New(tr(lang, "appNotFound")))
		return
	}
	if a.Kind == "nro" {
		// kaiHEN homebrew: make sure the latest release is downloaded,
		// then push it to the console through OunceShell's FTP server.
		appsMu.Lock()
		refreshAppLocked(a, true, send)
		appsMu.Unlock()
		rootDir := filepath.Join(appCacheDir(id), "root")
		if _, err := os.Stat(rootDir); err == nil {
			if err := ftpUploadOzTree(ip, rootDir, lang, send); err != nil {
				fail(err)
				return
			}
		} else if p, ok := appCacheFile(id); ok {
			data, err := os.ReadFile(p)
			if err != nil {
				fail(errors.New(tr(lang, "jsBadFile")))
				return
			}
			if _, err := ftpUploadOz(ip, a.AssetName, data, lang); err != nil {
				fail(err)
				return
			}
		} else {
			fail(errors.New(tr(lang, "appNoAsset")))
			return
		}
		appsMu.Lock()
		if cur := findAppLocked(id); cur != nil {
			cur.InstalledTag = cur.DownloadedTag
			saveAppsLocked()
		}
		appsMu.Unlock()
		send("APPRESULT|ok")
		send(tr(lang, "done"))
		return
	}
}

// hbDeleteFileHandler removes an installed kaiHEN homebrew file from the
// console's SD card through OunceShell's FTP server (the switch folder, or
// the kaiHEN plugins folder for plugin files) and clears its installed tag.
func hbDeleteFileHandler(w http.ResponseWriter, r *http.Request) {
	appsInit()
	var body struct {
		ID   string `json:"id"`
		IP   string `json:"ip"`
		Lang string `json:"lang"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if !validIP(body.IP) {
		jsonOK(w, map[string]any{"ok": false, "error": tr(body.Lang, "ipInvalid")})
		return
	}
	appsMu.Lock()
	a := findAppLocked(body.ID)
	var name string
	if a != nil {
		name = a.AssetName
	}
	appsMu.Unlock()
	if a == nil || name == "" {
		jsonOK(w, map[string]any{"ok": false, "error": tr(body.Lang, "appNotFound")})
		return
	}
	deleted, err := ftpDeleteOz(body.IP, name, body.Lang)
	if err != nil {
		jsonOK(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	appsMu.Lock()
	if cur := findAppLocked(body.ID); cur != nil {
		cur.InstalledTag = ""
		saveAppsLocked()
	}
	appsMu.Unlock()
	jsonOK(w, map[string]any{"ok": true, "deleted": deleted})
}
