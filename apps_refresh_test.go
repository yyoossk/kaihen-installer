package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedAppsJSON writes the given entries as the apps.json of a fresh
// exeDir and resets the in-memory list so appsInit loads them, the
// way an app restart loads a returning user's saved state.
func seedAppsJSON(t *testing.T, entries []*appEntry) {
	t.Helper()
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(appsFile(), raw, 0o644); err != nil {
		t.Fatalf("write seed apps.json: %v", err)
	}
	appsList = nil
	appsInit()
	t.Cleanup(func() { appsList = nil })
}

func findSeeded(t *testing.T, id string) *appEntry {
	t.Helper()
	appsMu.Lock()
	defer appsMu.Unlock()
	a := findAppLocked(id)
	if a == nil {
		t.Fatalf("seeded entry %s not loaded", id)
	}
	return a
}

// Offline regression: a returning user's WinampNX2 entry was already
// resolved to Source "zip" (its release ships an SD zip). The
// startup definition refresh must not overwrite that resolved Source
// with the builtin seed ("release") — when it did, the zip was
// treated as a bare .nro and the install failed with jsBadFile.
// The same refresh must still propagate genuinely new definition
// fields (here: PKForge's InstallPath) to entries that predate them.
func TestAppsInitKeepsResolvedSource(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()

	seedAppsJSON(t, []*appEntry{
		{ID: "amariscalc/WinampNX2", Kind: "nro", Builtin: true, Name: "WinampNX2",
			Source: "zip", AssetName: "",
			AssetURL:  "https://github.com/amariscalc/WinampNX2/releases/download/v0.6.0/WinampNX2-v0.6.0-SD.zip",
			LatestTag: "v0.6.0"},
		{ID: "sh00kapic/PKForge-Switch2", Kind: "nro", Builtin: true, Name: "PKForge",
			Source: "release", AssetHint: "PKForge-AIO.nro", InstallPath: ""},
		{ID: "Rodpad/SW2PS2", Kind: "nro", Builtin: true, Name: "SW2PS2",
			Source: "release", PreRelease: false},
	})

	w := findSeeded(t, "amariscalc/WinampNX2")
	if w.Source != "zip" {
		t.Fatalf("resolved Source was overwritten: got %q, want %q", w.Source, "zip")
	}
	pk := findSeeded(t, "sh00kapic/PKForge-Switch2")
	if pk.InstallPath != "PKForge/PKForge.nro" {
		t.Fatalf("definition refresh lost: InstallPath = %q, want %q", pk.InstallPath, "PKForge/PKForge.nro")
	}
	sw := findSeeded(t, "Rodpad/SW2PS2")
	if !sw.PreRelease {
		t.Fatal("definition refresh lost: SW2PS2 PreRelease is still false")
	}
	// An entry with no resolved asset name yet has no cached payload
	// file; the cache dir itself must not count as one.
	if p, ok := appCacheFile("amariscalc/WinampNX2"); ok {
		t.Fatalf("appCacheFile returned %q for an entry with no cached file", p)
	}
}

// Live regression, the user's exact path: a carried-over WinampNX2
// entry (resolved, never downloaded) is installed after an app
// restart. The refresh must download the SD zip and extract its
// switch/ tree into the cache root, ready for the FTP tree upload.
func TestWinampNx2InstallPrepCarriedOver(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()

	seedAppsJSON(t, []*appEntry{
		{ID: "amariscalc/WinampNX2", Kind: "nro", Builtin: true, Name: "WinampNX2",
			Source: "zip", AssetName: "",
			AssetURL:  "https://github.com/amariscalc/WinampNX2/releases/download/v0.6.0/WinampNX2-v0.6.0-SD.zip",
			LatestTag: "v0.6.0"},
	})
	a := findSeeded(t, "amariscalc/WinampNX2")

	appsMu.Lock()
	err := refreshAppLocked(a, true, nil)
	appsMu.Unlock()
	if err != nil {
		t.Fatalf("refreshAppLocked: %v", err)
	}
	if a.Source != "zip" {
		t.Fatalf("source = %q, want %q", a.Source, "zip")
	}
	if a.DownloadedTag != "v0.6.0" {
		t.Fatalf("downloadedTag = %q, want %q", a.DownloadedTag, "v0.6.0")
	}
	nro := filepath.Join(appCacheDir(a.ID), "root", "switch", "WinampNX2", "WinampNX2.nro")
	if _, err := os.Stat(nro); err != nil {
		t.Fatalf("extracted tree missing %s: %v", nro, err)
	}
}
