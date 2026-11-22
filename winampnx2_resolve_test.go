package main

import "testing"

// Live resolution check for the WinampNX2 catalog entry: the newest
// release must resolve and the asset picker must choose the SD zip
// (the release ships switch/WinampNX2/WinampNX2.nro inside a zip).
func TestWinampNX2Resolves(t *testing.T) {
	a := &appEntry{ID: "amariscalc/WinampNX2", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.4.0" {
		t.Fatalf("tag = %q, want %q", tag, "v0.4.0")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.Source != "zip" {
		t.Fatalf("picked source = %q, want %q", a.Source, "zip")
	}
}
