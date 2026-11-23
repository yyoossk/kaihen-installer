package main

import "testing"

// Live: TheAJLive/Cemu-NX2 (Cemu, the Wii U emulator, ported to
// Switch 2 for kaiHEN) must resolve to its latest release and pick
// the bare Switch 2 .nro.
func TestCemuNx2Resolves(t *testing.T) {
	a := &appEntry{ID: "TheAJLive/Cemu-NX2", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v1.4.1" {
		t.Fatalf("tag = %q, want %q", tag, "v1.4.1")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "cemu-switch2.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "cemu-switch2.nro")
	}
}
