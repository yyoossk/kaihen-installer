package main

import "testing"

// Live: sh00kapic/PKForge-Switch2 (offline Pokémon save editor) must
// resolve to its latest release and pick the AIO edition .nro among
// the four editions it ships.
func TestPkforgeResolves(t *testing.T) {
	a := &appEntry{ID: "sh00kapic/PKForge-Switch2", Kind: "nro", Source: "release", AssetHint: "PKForge-AIO.nro", InstallPath: "PKForge/PKForge.nro"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.7.4" {
		t.Fatalf("tag = %q, want %q", tag, "v0.7.4")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "PKForge-AIO.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "PKForge-AIO.nro")
	}
}

// Offline: with all four editions present, the hint must select the
// AIO edition exactly (the full-featured one without the 215 MB
// animation payload), never a sibling edition or the source archive.
func TestPickPkforgeEdition(t *testing.T) {
	a := &appEntry{ID: "sh00kapic/PKForge-Switch2", Kind: "nro", Source: "release", AssetHint: "PKForge-AIO.nro"}
	assets := []relAsset{
		{Name: "PKForge-2D.nro", URL: "https://example.invalid/2d"},
		{Name: "PKForge-AIO-Animated.nro", URL: "https://example.invalid/anim"},
		{Name: "PKForge-AIO.nro", URL: "https://example.invalid/aio"},
		{Name: "PKForge-Portraits.nro", URL: "https://example.invalid/port"},
		{Name: "PKForge-source-v0.7.4.tar.gz", URL: "https://example.invalid/src"},
		{Name: "SHA256SUMS", URL: "https://example.invalid/sums"},
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no asset picked")
	}
	if a.AssetName != "PKForge-AIO.nro" || a.Source != "release" {
		t.Fatalf("picked %q (source %q), want PKForge-AIO.nro (release)", a.AssetName, a.Source)
	}
}

// Offline: a bare .nro with an InstallPath goes to that fixed SD
// path; without one it lands in switch/ under its asset name.
func TestAppInstallRel(t *testing.T) {
	pk := &appEntry{AssetName: "PKForge-AIO.nro", InstallPath: "PKForge/PKForge.nro"}
	if got := appInstallRel(pk); got != "PKForge/PKForge.nro" {
		t.Fatalf("appInstallRel(pkforge) = %q, want %q", got, "PKForge/PKForge.nro")
	}
	plain := &appEntry{AssetName: "some-app.nro"}
	if got := appInstallRel(plain); got != "some-app.nro" {
		t.Fatalf("appInstallRel(plain) = %q, want %q", got, "some-app.nro")
	}
}
