package main

import "testing"

// Live resolution check for the Chromium NX catalog entry: the release
// ships both a Switch 1 (chromium-nx.nro) and a Switch 2
// (chromium-nx2.nro) build plus a 411 MB ext4 image; the entry's
// AssetHint must select the Switch 2 .nro exactly.
func TestChromiumNxResolves(t *testing.T) {
	a := &appEntry{ID: "shodowlo/chromium-nx", Kind: "nro", Source: "release", AssetHint: "chromium-nx2.nro",
		Extras: []extraAsset{{Name: "chromium-full.ext4", Dir: "switch/chromium-nx"}}}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.2.0" {
		t.Fatalf("tag = %q, want %q", tag, "v0.2.0")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "chromium-nx2.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "chromium-nx2.nro")
	}
	if err := resolveExtras(a, assets); err != nil {
		t.Fatalf("resolveExtras: %v", err)
	}
	if a.Extras[0].URL == "" {
		t.Fatal("the ext4 image extra did not resolve from the latest release")
	}
}

// Regression (found on hardware, build 20261123-1818): an entry
// carried over from an older build already has LatestTag and
// AssetURL set. fetchRelease's unchanged-release fast path used to
// return no asset list for it, so the extras never resolved and the
// install failed with "no file matching the latest release" right
// after downloading the main file. A refresh must resolve the
// extras for such an entry too.
func TestChromiumNxResolvesCarriedOver(t *testing.T) {
	b := &appEntry{ID: "shodowlo/chromium-nx", Kind: "nro", Source: "release", AssetHint: "chromium-nx2.nro",
		LatestTag:     "v0.2.0",
		AssetURL:      "https://github.com/shodowlo/chromium-nx/releases/download/v0.2.0/chromium-nx2.nro",
		DownloadedTag: "v0.2.0",
		Extras:        []extraAsset{{Name: "chromium-full.ext4", Dir: "switch/chromium-nx"}}}
	if !extrasUnresolved(b) {
		t.Fatal("the carried-over entry should start with unresolved extras")
	}
	if err := refreshAppLocked(b, false, nil); err != nil {
		t.Fatalf("refreshAppLocked: %v", err)
	}
	if b.Extras[0].URL == "" {
		t.Fatal("carried-over entry: the ext4 extra URL is still unresolved after refresh")
	}
}

// Offline picker check: with a hint set, the Switch 1 .nro listed
// first must NOT be picked, and a missing hinted asset fails closed.
func TestPickNroAssetHint(t *testing.T) {
	assets := []relAsset{
		{Name: "chromium-full.ext4", URL: "https://example.invalid/ext4"},
		{Name: "chromium-nx.nro", URL: "https://example.invalid/nx"},
		{Name: "chromium-nx2.nro", URL: "https://example.invalid/nx2"},
	}
	a := &appEntry{ID: "shodowlo/chromium-nx", Kind: "nro", Source: "release", AssetHint: "chromium-nx2.nro"}
	if !pickNroAsset(a, assets) {
		t.Fatalf("picker failed with hint")
	}
	if a.AssetName != "chromium-nx2.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "chromium-nx2.nro")
	}
	b := &appEntry{ID: "shodowlo/chromium-nx", Kind: "nro", Source: "release", AssetHint: "chromium-nx2.nro"}
	if pickNroAsset(b, assets[:2]) {
		t.Fatalf("picker accepted a list without the hinted asset")
	}
}
