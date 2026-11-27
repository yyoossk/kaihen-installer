package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeNodeZip(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("node-v24.22.3-win-x64/node.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEnsureNode(t *testing.T) {
	oldDir := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = oldDir }()
	oldOS, oldDL, oldExeSize := nodeRuntimeOS, nodeDownload, nodeExeSize
	defer func() { nodeRuntimeOS, nodeDownload, nodeExeSize = oldOS, oldDL, oldExeSize }()
	nodeRuntimeOS = "windows"
	t.Setenv("KAIHEN_NODE", "")
	t.Setenv("CIVINSTALLER_NODE", "")
	content := []byte("fake-node")
	nodeExeSize = int64(len(content))
	dst := filepath.Join(exeDir, "runtime", "node.exe")
	log := func(string) {}

	// An environment-configured node wins: no download, nothing written.
	t.Setenv("KAIHEN_NODE", "/somewhere/node")
	if err := ensureNode(log, "en"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("node.exe written despite KAIHEN_NODE")
	}
	t.Setenv("KAIHEN_NODE", "")

	// Missing: downloads once and extracts node.exe from the zip.
	calls := 0
	nodeDownload = func() ([]byte, error) { calls++; return fakeNodeZip(t, content), nil }
	if err := ensureNode(log, "en"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("extracted node.exe mismatch: %v", err)
	}
	// Present with the pinned size: no second download.
	if err := ensureNode(log, "en"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("downloaded %d times, want 1", calls)
	}

	// A failed download surfaces the localized error and writes nothing.
	os.Remove(dst)
	nodeDownload = func() ([]byte, error) { return nil, errors.New("boom") }
	if err := ensureNode(log, "ja"); err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("want nodeDlFail error, got %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("node.exe written despite failed download")
	}
}
