package main

import "testing"

// Live resolution check for the KaiRadio catalog entry: the repo
// publishes prereleases only, so the releases-page fallback must
// find v1.0.0-rc1 and its single KaiRadio.nro asset.
func TestKaiRadioResolves(t *testing.T) {
	a := &appEntry{ID: "mizuiroame/kairadio", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.0.1" {
		t.Fatalf("tag = %q, want v1.0.1", tag)
	}
	found := false
	for _, as := range assets {
		if as.Name == "KaiRadio.nro" {
			found = true
		}
	}
	if !found {
		t.Fatalf("KaiRadio.nro not among assets: %v", assets)
	}
}
