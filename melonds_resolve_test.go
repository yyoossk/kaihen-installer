package main

import "testing"

// Live: ac1dburnz/melonDS-switch2 (experimental melonDS port for
// Switch 2) is prerelease-only; it must resolve through the
// prerelease fallback and pick the release zip (which carries the
// switch/melonds/ tree, including the required romlist.bin).
func TestMelonDSResolves(t *testing.T) {
	a := &appEntry{ID: "ac1dburnz/melonDS-switch2", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.8.3-s2-test.1" {
		t.Fatalf("tag = %q, want %q", tag, "v0.8.3-s2-test.1")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.Source != "zip" {
		t.Fatalf("source = %q, want %q", a.Source, "zip")
	}
}
