package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Live resolution check for the KaiVesper catalog entry: it is a
// prerelease-only repo, so the entry must resolve the newest release
// overall (v0.2.1-beta line) and pick the bare kaivesper.nro.
func TestKaiVesperResolves(t *testing.T) {
	a := &appEntry{ID: "Sank-kab/KaiVesper", Kind: "nro", Source: "release", PreRelease: true, Patched: true}
	tag, assets, err := fetchRelease(a)
	if err != nil {
		t.Fatalf("fetchRelease: %v", err)
	}
	if !strings.HasPrefix(tag, "v0.2.1-beta") && !strings.HasPrefix(tag, "v0.2") {
		t.Fatalf("tag = %q, want a v0.2.x tag", tag)
	}
	if !pickNroAsset(a, assets) {
		t.Fatalf("no downloadable asset among: %v", assets)
	}
	if a.AssetName != "kaivesper.nro" {
		t.Fatalf("picked asset = %q, want %q", a.AssetName, "kaivesper.nro")
	}
}

// The catalog split: the stock listing must not contain patched-only
// entries, and the patched listing (cat=patched) must contain only
// patched-only entries.
func TestAppsListCatalogSplit(t *testing.T) {
	oldDir := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = oldDir }()
	oldList := appsList
	defer func() { appsList = oldList }()

	now := time.Now().Unix()
	appsList = []*appEntry{
		{ID: "owner/stockapp", Kind: "nro", Builtin: true, Name: "StockApp", Source: "release", LastCheck: now},
		{ID: "Sank-kab/KaiVesper", Kind: "nro", Builtin: true, Name: "KaiVesper", Source: "release", PreRelease: true, Patched: true, LastCheck: now},
	}

	list := func(cat string) []map[string]any {
		req := httptest.NewRequest("GET", "/api/apps?kind=nro&cat="+cat, nil)
		rec := httptest.NewRecorder()
		appsListHandler(rec, req)
		var out struct {
			Apps []map[string]any `json:"apps"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("cat=%s: bad JSON: %v", cat, err)
		}
		return out.Apps
	}

	stock := list("stock")
	if len(stock) != 1 || stock[0]["id"] != "owner/stockapp" {
		t.Fatalf("stock listing = %v, want only owner/stockapp", stock)
	}
	patched := list("patched")
	if len(patched) != 1 || patched[0]["id"] != "Sank-kab/KaiVesper" {
		t.Fatalf("patched listing = %v, want only Sank-kab/KaiVesper", patched)
	}
	// No cat parameter defaults to the stock catalog.
	def := list("")
	if len(def) != 1 || def[0]["id"] != "owner/stockapp" {
		t.Fatalf("default listing = %v, want only owner/stockapp", def)
	}
}
