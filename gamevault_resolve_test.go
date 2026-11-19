package main

import "testing"

// Live resolution check for the Game Vault catalog entry: the newest
// release must resolve and the asset picker must choose the bare
// .nro (the release also ships runtime/source zips, which the
// catalog must not pick: the runtime zip carries a different HEN
// bundle and PC-side launcher files).
func TestGameVaultResolves(t *testing.T) {
	a := &appEntry{ID: "flex36ty/switch2-gamevault", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if tag != "v0.4.5" {
		t.Fatalf("tag = %q, want %q", tag, "v0.4.5")
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "game-vault-v045.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "game-vault-v045.nro")
	}
}
