package main

import "testing"

// Live resolution check for the OunceMTP catalog entry: the newest
// release must resolve and the asset picker must choose the bare
// ouncemtp.nro.
func TestOunceMTPResolves(t *testing.T) {
	a := &appEntry{ID: "venustrikes/ouncemtp", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v1.0.0" {
		t.Fatalf("tag = %q, want %q", tag, "v1.0.0")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "ouncemtp.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "ouncemtp.nro")
	}
}
