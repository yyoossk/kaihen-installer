package main

import "testing"

// Live resolution check for the Vita3K catalog entry: the newest
// release must resolve and the asset picker must choose the bare
// Vita3K-switch2.nro.
func TestVita3KResolves(t *testing.T) {
	a := &appEntry{ID: "davidkalil10/Vita3K-nx2", Kind: "nro", Source: "release"}
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
	if a.AssetName != "Vita3K-switch2.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "Vita3K-switch2.nro")
	}
}
