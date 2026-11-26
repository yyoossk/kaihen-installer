package main

import "testing"

// Live resolution check for the Local Update Tool catalog entry: the
// newest release must resolve and the asset picker must choose the
// bare local-update-tool.nro (installed under its own folder via
// InstallPath).
func TestLocalUpdateToolResolves(t *testing.T) {
	a := &appEntry{ID: "AlexTech01/local-update-tool", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "1.0.0" {
		t.Fatalf("tag = %q, want %q", tag, "1.0.0")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "local-update-tool.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "local-update-tool.nro")
	}
	if a.Source != "release" {
		t.Fatalf("source = %q, want %q", a.Source, "release")
	}
}
