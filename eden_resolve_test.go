package main

import "testing"

// Live resolution check for the Eden (Switch 2 POC) catalog entry: the
// repo publishes prereleases only, so resolution goes through the
// prerelease fallback; the picker must choose the bare eden.nro.
func TestEdenPocResolves(t *testing.T) {
	a := &appEntry{ID: "Olmectron/eden-switch2-poc", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.7.0" {
		t.Fatalf("tag = %q, want %q", tag, "v0.7.0")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "eden.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "eden.nro")
	}
}
