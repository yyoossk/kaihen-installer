package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheSizeAndClear(t *testing.T) {
	oldDir := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = oldDir }()
	appsList = []*appEntry{{ID: "x/y", Name: "X", DownloadedTag: "v1", InstalledTag: "v1"}}
	t.Cleanup(func() { appsList = nil })

	put := func(rel string, n int) {
		p := filepath.Join(exeDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("kaihen/dist.bin", 1000)
	put("apps-cache/x__y/app.nro", 2000)
	put("swfence-cache/router.json", 500)

	rec := httptest.NewRecorder()
	cacheSizeHandler(rec, httptest.NewRequest("GET", "/api/cache", nil))
	var size struct {
		Bytes int64 `json:"bytes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&size); err != nil {
		t.Fatal(err)
	}
	if size.Bytes != 3500 {
		t.Fatalf("cache size = %d, want 3500", size.Bytes)
	}

	rec = httptest.NewRecorder()
	cacheClearHandler(rec, httptest.NewRequest("POST", "/api/cache/clear", nil))
	var cleared struct {
		Bytes int64 `json:"bytes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.Bytes != 3500 {
		t.Fatalf("cleared = %d, want 3500", cleared.Bytes)
	}
	for _, d := range cacheDirs() {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("cache dir %s still exists after clear", d)
		}
	}
	// The entry forgets its download but keeps its installed state.
	if appsList[0].DownloadedTag != "" {
		t.Fatal("DownloadedTag not reset by cache clear")
	}
	if appsList[0].InstalledTag != "v1" {
		t.Fatal("InstalledTag must survive a cache clear")
	}
	if got := cacheTotal(); got != 0 {
		t.Fatalf("cache total after clear = %d, want 0", got)
	}
}
