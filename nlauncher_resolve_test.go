package main

import (
	"strings"
	"testing"
)

// Live resolution check for the N Launcher catalog entry: the
// release carries both a bare n-launcher.nro and an SD zip whose
// only runtime file is the same nro (byte-identical, verified
// 2026-11-27); the picker must take the bare nro, and the entry
// installs it into its own folder via InstallPath.
func TestNLauncherResolves(t *testing.T) {
	a := &appEntry{ID: "davidkalil10/n-launcher", Kind: "nro", Source: "release", InstallPath: "n-launcher/n-launcher.nro"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if !strings.HasPrefix(tag, "v0.0.") {
		t.Fatalf("tag = %q, want a v0.0.x tag", tag)
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "n-launcher.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "n-launcher.nro")
	}
	if a.Source == "zip" {
		t.Fatalf("picked the SD zip; want the bare nro")
	}
	if got := appInstallRel(a); got != "n-launcher/n-launcher.nro" {
		t.Fatalf("install rel = %q, want %q", got, "n-launcher/n-launcher.nro")
	}
}
