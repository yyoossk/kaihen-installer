package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPruneStaleAppCache(t *testing.T) {
	oldDir := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = oldDir }()
	a := &appEntry{ID: "x/y", AssetName: "new.nro"}
	dir := appCacheDir(a.ID)
	if err := os.MkdirAll(filepath.Join(dir, "root"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"new.nro", "old-name.nro", "leftover.zip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pruneStaleAppCacheLocked(a)
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	// The current asset and the root/ tree survive; stale files go.
	if len(names) != 2 {
		t.Fatalf("cache after prune = %v, want [new.nro root]", names)
	}
}
