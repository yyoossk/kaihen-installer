package main

import "testing"

// Live: cainan-c/reset-launch-version (resets a title's launch
// version to 1.0.0 so the lowest cartridge version can run again)
// publishes a single release tagged "latest" whose dist.zip carries
// switch/reset-launch-version.nro.
func TestResetLaunchVersionResolves(t *testing.T) {
	a := &appEntry{ID: "cainan-c/reset-launch-version", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "latest" {
		t.Fatalf("tag = %q, want %q", tag, "latest")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.Source != "zip" {
		t.Fatalf("source = %q, want %q", a.Source, "zip")
	}
}
