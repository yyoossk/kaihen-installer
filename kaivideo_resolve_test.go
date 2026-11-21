package main

import "testing"

// Live resolution check for the KaiVideo catalog entry: the app
// must resolve the newest release and its single KaiVideo.nro
// asset.
func TestKaiVideoResolves(t *testing.T) {
	a := &appEntry{ID: "mizuiroame/kaivideo", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.10.0" {
		t.Fatalf("tag = %q, want v1.10.0", tag)
	}
	found := false
	for _, as := range assets {
		if as.Name == "KaiVideo.nro" {
			found = true
		}
	}
	if !found {
		t.Fatalf("KaiVideo.nro not among assets: %v", assets)
	}
}
