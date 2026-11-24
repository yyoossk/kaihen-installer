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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// extraAsset is an additional file from the same release that an app
// needs next to its .nro (e.g. Chromium NX's 411 MB browser image): it
// is resolved from the latest release alongside the main asset,
// downloaded to the app's cache, and uploaded to Dir on the SD card at
// install time.
type extraAsset struct {
	Name string `json:"name"` // exact release asset name
	Dir  string `json:"dir"`  // SD-relative folder, e.g. "switch/chromium-nx"
	URL  string `json:"url,omitempty"`
}

type appEntry struct {
	ID            string       `json:"id"`
	Kind          string       `json:"kind"`           // "nro"
	Host          string       `json:"host,omitempty"` // "" = GitHub, "gitlab" = GitLab
	Builtin       bool         `json:"builtin"`
	Name          string       `json:"name,omitempty"`
	Source        string       `json:"source,omitempty"`      // "release" | "zip"
	AssetHint     string       `json:"assetHint,omitempty"`   // exact release asset name to prefer (e.g. a Switch 2 build among several .nro assets)
	InstallPath   string       `json:"installPath,omitempty"` // SD path relative to switch/ when a bare .nro must be installed under a fixed folder/name instead of switch/<asset name>
	Extras        []extraAsset `json:"extras,omitempty"`      // extra files from the same release the app needs on the SD card
	AssetName     string       `json:"assetName"`
	AssetURL      string       `json:"assetUrl,omitempty"`
	ZipInner      string       `json:"zipInner,omitempty"`
	SHA256        string       `json:"sha256,omitempty"`
	Size          int64        `json:"size,omitempty"`
	LatestTag     string       `json:"latestTag"`
	DownloadedTag string       `json:"downloadedTag"`
	InstalledTag  string       `json:"installedTag"`
	LastCheck     int64        `json:"lastCheck"`
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
		{ID: "Olmectron/dekopon", Kind: "nro", Builtin: true, Name: "Dekopon for SW2", Source: "release"},
		{ID: "mizuiroame/kaiplayer", Kind: "nro", Builtin: true, Name: "KaiPlayer", Source: "release"},
		{ID: "mizuiroame/kairadio", Kind: "nro", Builtin: true, Name: "KaiRadio", Source: "release"},
		{ID: "mizuiroame/kaivideo", Kind: "nro", Builtin: true, Name: "KaiVideo", Source: "release"},
		{ID: "HerXayah/brewstore", Kind: "nro", Builtin: true, Name: "BrewStore", Source: "release"},
		{ID: "s8n/blacklist-tool", Kind: "nro", Builtin: true, Name: "Blacklist Tool", Source: "release"},
		{ID: "Rodpad/SW2NES", Kind: "nro", Builtin: true, Name: "SW2NES", Source: "release"},
		{ID: "Rodpad/SW2SNES", Kind: "nro", Builtin: true, Name: "SW2SNES", Source: "release"},
		{ID: "Rodpad/SW2Transfer", Kind: "nro", Builtin: true, Name: "SW2Transfer", Source: "release"},
		{ID: "flex36ty/switch2-gamevault", Kind: "nro", Builtin: true, Name: "Game Vault", Source: "release"},
		{ID: "XITRIX/ppsspp-nx2", Kind: "nro", Builtin: true, Name: "PPSSPP NX2", Source: "release"},
		{ID: "Rodpad/SW2PS2", Kind: "nro", Builtin: true, Name: "SW2PS2", Source: "release"},
		{ID: "BlafKing/KaiDump", Kind: "nro", Builtin: true, Name: "KaiDump", Source: "release"},
		{ID: "venustrikes/ouncemtp", Kind: "nro", Builtin: true, Name: "OunceMTP", Source: "release"},
		{ID: "amariscalc/WinampNX2", Kind: "nro", Builtin: true, Name: "WinampNX2", Source: "release"},
		{ID: "Olmectron/eden-switch2-poc", Kind: "nro", Builtin: true, Name: "Eden for SW2 (POC)", Source: "release"},
		{ID: "shodowlo/chromium-nx", Kind: "nro", Builtin: true, Name: "Chromium NX", Source: "release", AssetHint: "chromium-nx2.nro", Extras: []extraAsset{{Name: "chromium-full.ext4", Dir: "switch/chromium-nx"}}},
		{ID: "davidkalil10/Vita3K-nx2", Kind: "nro", Builtin: true, Name: "Vita3K", Source: "release"},
		{ID: "TheAJLive/Cemu-NX2", Kind: "nro", Builtin: true, Name: "Cemu NX2", Source: "release"},
		{ID: "ac1dburnz/melonDS-switch2", Kind: "nro", Builtin: true, Name: "melonDS-switch2", Source: "release"},
		{ID: "sh00kapic/PKForge-Switch2", Kind: "nro", Builtin: true, Name: "PKForge", Source: "release", AssetHint: "PKForge-AIO.nro", InstallPath: "PKForge/PKForge.nro"},
		{ID: "cainan-c/reset-launch-version", Kind: "nro", Builtin: true, Name: "Reset Launch Version", Source: "release"},
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
	// Refresh the definition fields of builtin entries from the code,
	// so a catalog change to an existing entry (a new asset hint, an
	// extra file the app needs) reaches installs whose apps.json
	// predates it. Download/install state stays the user's own.
	defs := map[string]*appEntry{}
	for _, b := range builtinApps() {
		defs[b.ID] = b
	}
	for _, a := range appsList {
		b, ok := defs[a.ID]
		if !ok {
			continue
		}
		if a.Kind != b.Kind || a.Host != b.Host || a.Source != b.Source || a.AssetHint != b.AssetHint || a.InstallPath != b.InstallPath || !extrasEqual(a.Extras, b.Extras) {
			a.Kind, a.Host, a.Source, a.AssetHint, a.InstallPath = b.Kind, b.Host, b.Source, b.AssetHint, b.InstallPath
			a.Extras = append([]extraAsset(nil), b.Extras...)
			changed = true
		}
	}
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

// extrasEqual compares two extra-file definitions by name and target
// folder (the resolved URL is runtime state, not part of the
// definition).
func extrasEqual(a, b []extraAsset) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Dir != b[i].Dir {
			return false
		}
	}
	return true
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
		if rel.TagName == a.LatestTag && a.AssetURL != "" && !extrasUnresolved(a) {
			return rel.TagName, nil, nil
		}
		out := []relAsset{}
		for _, l := range rel.Assets.Links {
			out = append(out, relAsset{Name: l.Name, URL: l.URL})
		}
		return rel.TagName, out, nil
	}
	tag := ""
	_, final, err := httpGetFinal("https://github.com/" + a.ID + "/releases/latest")
	if err == nil {
		if i := strings.Index(final, "/releases/tag/"); i >= 0 {
			if t, uerr := url.PathUnescape(strings.TrimSuffix(final[i+len("/releases/tag/"):], "/")); uerr == nil {
				tag = t
			}
		}
	}
	if tag == "" {
		// Prerelease-only repositories have no /releases/latest target
		// (it 404s); take the newest entry from the releases list page
		// instead, which includes prereleases.
		body, lerr := httpGet("https://github.com/" + a.ID + "/releases")
		if lerr != nil {
			if strings.Contains(lerr.Error(), "404") {
				return "", nil, errNoRelease
			}
			return "", nil, lerr
		}
		m := regexp.MustCompile(`/releases/tag/([^"/]+)`).FindSubmatch(body)
		if m == nil {
			return "", nil, errNoRelease
		}
		t, uerr := url.PathUnescape(string(m[1]))
		if uerr != nil || t == "" {
			return "", nil, errNoRelease
		}
		tag = t
	}
	if tag == a.LatestTag && a.AssetURL != "" && !extrasUnresolved(a) {
		return tag, nil, nil
	}
	out, err := fetchReleaseAssets(a.ID, tag)
	if err != nil {
		return "", nil, err
	}
	if len(out) > 0 {
		return tag, out, nil
	}
	// The newest release carries no downloadable files at all (the
	// author published the tag without attaching binaries). Walk the
	// releases list, newest first, and use the first older release
	// that actually has files; when the author attaches the binaries
	// to the newest release later, it wins again automatically.
	listBody, lerr := httpGet("https://github.com/" + a.ID + "/releases")
	if lerr != nil {
		return tag, out, nil
	}
	seenTags := map[string]bool{tag: true}
	for _, m := range regexp.MustCompile(`/releases/tag/([^"/]+)`).FindAllSubmatch(listBody, -1) {
		t, uerr := url.PathUnescape(string(m[1]))
		if uerr != nil || t == "" || seenTags[t] {
			continue
		}
		seenTags[t] = true
		assets, ferr := fetchReleaseAssets(a.ID, t)
		if ferr != nil {
			continue
		}
		if len(assets) > 0 {
			return t, assets, nil
		}
	}
	return tag, out, nil
}

// fetchReleaseAssets returns the downloadable files attached to one
// GitHub release, parsed from its expanded_assets fragment.
func fetchReleaseAssets(id, tag string) ([]relAsset, error) {
	body, err := httpGet("https://github.com/" + id + "/releases/expanded_assets/" + url.PathEscape(tag))
	if err != nil {
		return nil, err
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
	return out, nil
}

// pickNroAsset selects the downloadable asset of a kaiHEN homebrew
// release: a direct .nro when one is published, otherwise the release
// .zip (whose contents are classified when downloaded). Returns false
// when neither exists.
func pickNroAsset(a *appEntry, assets []relAsset) bool {
	if a.AssetHint != "" {
		// The entry names its asset exactly (e.g. a release that ships
		// both a Switch 1 and a Switch 2 .nro). Fail closed when the
		// named asset is absent rather than picking the wrong build.
		for _, as := range assets {
			if as.Name == a.AssetHint && strings.HasSuffix(strings.ToLower(as.Name), ".nro") {
				a.Source = "release"
				a.AssetName = as.Name
				a.AssetURL = as.URL
				a.Size = 0
				a.ZipInner = ""
				return true
			}
		}
		return false
	}
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
		if err := resolveExtras(a, assets); err != nil {
			return err
		}
	}
	if allowDownload && (a.DownloadedTag != a.LatestTag || extrasCachedMissing(a)) {
		return downloadAppLocked(a, log)
	}
	return nil
}

// resolveExtras fills each declared extra file's download URL from
// the same release's asset list, matching by exact name (the extras
// always come from the latest release, like the main asset). An
// extra the release does not carry fails the refresh: the app would
// not run without it.
// extrasUnresolved reports whether any extra still lacks its
// download URL. The URL is not part of the builtin definition, so an
// entry carried over from an older build has the definition (from
// the appsInit merge) but no URL until a refresh resolves it.
func extrasUnresolved(a *appEntry) bool {
	for _, ex := range a.Extras {
		if ex.URL == "" {
			return true
		}
	}
	return false
}

func resolveExtras(a *appEntry, assets []relAsset) error {
	for i := range a.Extras {
		a.Extras[i].URL = ""
		for _, as := range assets {
			if as.Name == a.Extras[i].Name {
				a.Extras[i].URL = as.URL
				break
			}
		}
		if a.Extras[i].URL == "" {
			return errors.New(tr("", "appNoAsset"))
		}
	}
	return nil
}

func extrasDir(id string) string { return filepath.Join(appCacheDir(id), "extras") }

func extraCachePath(a *appEntry, name string) string {
	return filepath.Join(extrasDir(a.ID), name)
}

// extrasCachedMissing reports whether an entry declares extra files
// that are not in the local cache (e.g. the entry gained an extra in
// a newer build after the app was already downloaded).
func extrasCachedMissing(a *appEntry) bool {
	for _, ex := range a.Extras {
		if _, err := os.Stat(extraCachePath(a, ex.Name)); err != nil {
			return true
		}
	}
	return false
}

func downloadAppLocked(a *appEntry, log logFn) error {
	if a.AssetURL == "" {
		return errors.New(tr("", "appNoAsset"))
	}
	// When only the extras are missing (the entry is already at the
	// latest tag and its main file is cached), skip re-downloading
	// the main file and fetch just the extras.
	skipMain := a.Source != "zip" && a.DownloadedTag == a.LatestTag && a.AssetName != ""
	if skipMain {
		if _, err := os.Stat(filepath.Join(appCacheDir(a.ID), a.AssetName)); err != nil {
			skipMain = false
		}
	}
	if !skipMain {
		if log != nil {
			log(tr("", "fetching", a.AssetURL))
		}
		raw, err := httpGet(a.AssetURL)
		if err != nil {
			return errors.New(tr("", "dlErr"))
		}
		if a.Source == "zip" {
			if err := downloadNroZipLocked(a, raw); err != nil {
				return err
			}
		} else {
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
		}
	}
	if err := downloadExtrasLocked(a, log); err != nil {
		return err
	}
	a.DownloadedTag = a.LatestTag
	return nil
}

// downloadExtrasLocked fetches the entry's extra files (resolved
// from the latest release by refreshAppLocked) into the cache. The
// extras folder is rebuilt from scratch so a tag change never leaves
// a stale extra behind. Files are streamed to disk: an extra can be
// hundreds of MB.
func downloadExtrasLocked(a *appEntry, log logFn) error {
	if len(a.Extras) == 0 {
		return nil
	}
	dir := extrasDir(a.ID)
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	for _, ex := range a.Extras {
		if ex.URL == "" {
			return errors.New(tr("", "appNoAsset"))
		}
		if log != nil {
			log(tr("", "extraDl", ex.Name))
		}
		lastStep := -1
		progress := func(done, total int64) {
			if log == nil || total <= 0 {
				return
			}
			if step := int(done * 10 / total); step != lastStep {
				lastStep = step
				log(fmt.Sprintf("%s: %d%%", ex.Name, done*100/total))
			}
		}
		if err := httpGetToFile(ex.URL, filepath.Join(dir, ex.Name), progress); err != nil {
			return errors.New(tr("", "dlErr"))
		}
	}
	return nil
}

// downloadNroZipLocked handles a kaiHEN homebrew release zip whose contents
// were not known in advance. Three shapes occur in the hblist catalog:
// a single .nro at the zip root (cached as one file, installed into
// /switch), several .nro files (installed into /switch), and a tree meant
// to be extracted at the SD root (paths under switch/ or kaihen/, or a
// top-level retroarch/ data folder, e.g. RetroArch or the FTPD plugin)
// which is extracted verbatim into the cache's root/ folder and uploaded
// path by path.
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
		if parts[0] == "retroarch" {
			// RetroArch keeps its data (assets, cores, info) in a
			// folder at the SD root next to switch/. Without it the
			// menu has no fonts (non-Latin languages garble) and no
			// cores load, so keep the whole tree.
			segAt = 0
		}
		for i, p := range parts {
			if segAt >= 0 {
				break
			}
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
	// The catalog is shown in name order.
	sort.SliceStable(out, func(i, j int) bool {
		ni, _ := out[i]["name"].(string)
		nj, _ := out[j]["name"].(string)
		li, lj := strings.ToLower(ni), strings.ToLower(nj)
		if li != lj {
			return li < lj
		}
		ii, _ := out[i]["id"].(string)
		ij, _ := out[j]["id"].(string)
		return ii < ij
	})
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

// appInstallRel returns the SD path (relative to switch/) a bare
// .nro is installed to: the entry's fixed InstallPath when one is
// set (the app expects its own folder and file name), otherwise the
// asset name directly in switch/.
func appInstallRel(a *appEntry) string {
	if a.InstallPath != "" {
		return a.InstallPath
	}
	return a.AssetName
}

// installExtras uploads the entry's cached extra files to their
// folders on the SD card, after the main file is in place.
func installExtras(a *appEntry, ip, lang string, log logFn) error {
	for _, ex := range a.Extras {
		local := extraCachePath(a, ex.Name)
		if _, err := os.Stat(local); err != nil {
			return errors.New(tr(lang, "appNoAsset"))
		}
		rel := strings.TrimSuffix(ex.Dir, "/") + "/" + ex.Name
		if err := ftpUploadOzFile(ip, rel, local, lang, log); err != nil {
			return err
		}
	}
	return nil
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
		rerr := refreshAppLocked(a, true, send)
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
			var upErr error
			if a.InstallPath != "" {
				_, upErr = ftpUploadOzPath(ip, appInstallRel(a), data, lang)
			} else {
				_, upErr = ftpUploadOz(ip, a.AssetName, data, lang)
			}
			if upErr != nil {
				fail(upErr)
				return
			}
		} else {
			// Nothing downloaded and nothing cached: report the
			// refresh error itself (e.g. GitHub unreachable) rather
			// than claiming the release has no matching file.
			if rerr != nil {
				fail(rerr)
			} else {
				fail(errors.New(tr(lang, "appNoAsset")))
			}
			return
		}
		if err := installExtras(a, ip, lang, send); err != nil {
			fail(err)
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
