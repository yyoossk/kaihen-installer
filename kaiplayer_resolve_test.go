package main

import "testing"

// Live resolution check for the KaiPlayer catalog entry: it must
// resolve the newest release and its single KaiPlayer.nro asset.
func TestKaiPlayerResolves(t *testing.T) {
	a := &appEntry{ID: "mizuiroame/kaiplayer", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.7.0" {
		t.Fatalf("tag = %q, want v1.7.0", tag)
	}
	found := false
	for _, as := range assets {
		if as.Name == "KaiPlayer.nro" {
			found = true
		}
	}
	if !found {
		t.Fatalf("KaiPlayer.nro not among assets: %v", assets)
	}
}
