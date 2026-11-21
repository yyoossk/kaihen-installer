package main

import "testing"

// Live resolution check for the KaiDump catalog entry: the newest
// release must resolve and the asset picker must choose the bare
// KaiDump.nro.
func TestKaiDumpResolves(t *testing.T) {
	a := &appEntry{ID: "BlafKing/KaiDump", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "1.0.5" {
		t.Fatalf("tag = %q, want %q", tag, "1.0.5")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "KaiDump.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "KaiDump.nro")
	}
}
