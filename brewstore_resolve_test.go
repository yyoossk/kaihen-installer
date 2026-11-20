package main

import "testing"

// Live resolution check for the BrewStore catalog entry: the app
// must resolve the newest release and its single brewstore.nro
// asset.
func TestBrewStoreResolves(t *testing.T) {
	a := &appEntry{ID: "HerXayah/brewstore", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "1.2.4" {
		t.Fatalf("tag = %q, want 1.2.4", tag)
	}
	found := false
	for _, as := range assets {
		if as.Name == "brewstore.nro" {
			found = true
		}
	}
	if !found {
		t.Fatalf("brewstore.nro not among assets: %v", assets)
	}
}
