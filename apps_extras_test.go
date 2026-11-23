package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Offline: extras resolve by exact asset name from the same release,
// and an extra the release does not carry is an error.
func TestResolveExtras(t *testing.T) {
	assets := []relAsset{
		{Name: "chromium-nx2.nro", URL: "https://example.invalid/nx2"},
		{Name: "chromium-full.ext4", URL: "https://example.invalid/ext4"},
	}
	a := &appEntry{ID: "shodowlo/chromium-nx", Extras: []extraAsset{{Name: "chromium-full.ext4", Dir: "switch/chromium-nx"}}}
	if err := resolveExtras(a, assets); err != nil {
		t.Fatal(err)
	}
	if a.Extras[0].URL != "https://example.invalid/ext4" {
		t.Fatalf("extra URL = %q", a.Extras[0].URL)
	}
	b := &appEntry{ID: "x/y", Extras: []extraAsset{{Name: "missing.bin", Dir: "switch/x"}}}
	if err := resolveExtras(b, assets); err == nil {
		t.Fatal("an extra missing from the release was accepted")
	}
}

func TestExtrasEqual(t *testing.T) {
	base := []extraAsset{{Name: "a.bin", Dir: "switch/a", URL: "https://example.invalid/1"}}
	sameDef := []extraAsset{{Name: "a.bin", Dir: "switch/a"}}
	if !extrasEqual(base, sameDef) {
		t.Fatal("same name/dir with different resolved URL must compare equal")
	}
	if extrasEqual(base, []extraAsset{{Name: "b.bin", Dir: "switch/a"}}) {
		t.Fatal("different name compared equal")
	}
	if extrasEqual(base, nil) {
		t.Fatal("different length compared equal")
	}
}

// The whole download path with extras, against a local HTTP server
// standing in for the release URLs: the main file and the extra land
// in the cache, and the downloaded tag is only recorded once both
// are there.
func TestDownloadAppWithExtras(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/main.nro", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("NRODATA"))
	})
	mux.HandleFunc("/big.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("B", 100000)))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	a := &appEntry{ID: "test/app", Kind: "nro", Source: "release", AssetName: "main.nro",
		AssetURL: srv.URL + "/main.nro", LatestTag: "v1",
		Extras: []extraAsset{{Name: "big.bin", Dir: "switch/app", URL: srv.URL + "/big.bin"}}}
	if !extrasCachedMissing(a) {
		t.Fatal("extras should be missing before the download")
	}
	if err := downloadAppLocked(a, nil); err != nil {
		t.Fatal(err)
	}
	if a.DownloadedTag != "v1" {
		t.Fatalf("DownloadedTag = %q, want v1", a.DownloadedTag)
	}
	main, err := os.ReadFile(filepath.Join(appCacheDir(a.ID), "main.nro"))
	if err != nil || string(main) != "NRODATA" {
		t.Fatalf("cached main file = %q, %v", main, err)
	}
	extra, err := os.ReadFile(extraCachePath(a, "big.bin"))
	if err != nil || len(extra) != 100000 {
		t.Fatalf("cached extra = %d bytes, %v", len(extra), err)
	}
	if extrasCachedMissing(a) {
		t.Fatal("extras should be cached after the download")
	}
}

// When the extra cannot be fetched, the download fails and the
// entry's tag stays stale, so the next install retries instead of
// installing the app without the file it needs.
func TestDownloadAppExtraFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/main.nro", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("NRODATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()
	a := &appEntry{ID: "test/app2", Kind: "nro", Source: "release", AssetName: "main.nro",
		AssetURL: srv.URL + "/main.nro", LatestTag: "v1",
		Extras: []extraAsset{{Name: "big.bin", Dir: "switch/app", URL: srv.URL + "/missing.bin"}}}
	if err := downloadAppLocked(a, nil); err == nil {
		t.Fatal("download with an unfetchable extra succeeded")
	}
	if a.DownloadedTag == "v1" {
		t.Fatal("DownloadedTag was recorded although the extra failed")
	}
}
