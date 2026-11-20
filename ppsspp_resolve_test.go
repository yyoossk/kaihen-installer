package main

import "testing"

// Live resolution check for the PPSSPP NX2 catalog entry: the newest
// release must resolve and the asset picker must choose the bare
// PPSSPP-NX2.nro (about 67MB — under the catalog download cap; the
// 64MiB cap in kaihen.go applies only to the manual FTP upload
// endpoint, not to catalog installs).
func TestPPSSPPResolves(t *testing.T) {
	a := &appEntry{ID: "XITRIX/ppsspp-nx2", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.1.0" {
		t.Fatalf("tag = %q, want %q", tag, "v0.1.0")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "PPSSPP-NX2.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "PPSSPP-NX2.nro")
	}
}
