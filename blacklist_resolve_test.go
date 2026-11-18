package main

import "testing"

// Live resolution check for the Blacklist Tool catalog entry. The
// newest release (v1.1.0) was published without any attached binary,
// so resolution must fall back to v1.0.0, the newest release that
// actually ships blacklist-tool-switch2.nro. When the author
// attaches the v1.1.0 binary, update the expected tag to v1.1.0.
func TestBlacklistToolResolves(t *testing.T) {
	a := &appEntry{ID: "s8n/blacklist-tool", Kind: "nro", Source: "release"}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v1.0.0" {
		t.Fatalf("tag = %q, want v1.0.0 (v1.1.0 ships no binary)", tag)
	}
	found := false
	for _, as := range assets {
		if as.Name == "blacklist-tool-switch2.nro" {
			found = true
		}
	}
	if !found {
		t.Fatalf("blacklist-tool-switch2.nro not among assets: %v", assets)
	}
}
