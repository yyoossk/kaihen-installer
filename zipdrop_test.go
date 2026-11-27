package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A dropped release-style zip: the switch/ tree lands at the SD root
// and loose files land under switch/ with their paths kept.
func TestInstallZipDrop(t *testing.T) {
	f := newFakeOzFTP(t, "/switch", "/kaihen/plugins")
	raw := makeZip(t, map[string]string{
		"pkg/switch/foo/foo.nro":  "NRODATA",
		"kaihen/plugins/ftpd.nso": "NSODATA",
		"bar.nro":                 "LOOSE",
		"sub/readme.txt":          "README",
		"../evil.nro":             "EVIL",
	})
	if err := installZipAddrs([]string{f.ln.Addr().String()}, raw, "en", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[string]string{
		"/switch/foo/foo.nro":      "NRODATA",
		"/kaihen/plugins/ftpd.nso": "NSODATA",
		"/switch/bar.nro":          "LOOSE",
		"/switch/sub/readme.txt":   "README",
	}
	for path, content := range want {
		if got := string(f.files[path]); got != content {
			t.Errorf("%s = %q, want %q", path, got, content)
		}
	}
	for path := range f.files {
		if strings.Contains(path, "evil") {
			t.Errorf("path traversal entry was stored: %s", path)
		}
	}
}

// An empty or asset-less zip is refused before any connection work
// beyond parsing.
func TestInstallZipDropEmpty(t *testing.T) {
	raw := makeZip(t, map[string]string{})
	if err := installZipAddrs([]string{"127.0.0.1:1"}, raw, "en", nil); err == nil {
		t.Fatal("empty zip installed without error")
	}
}

func TestScanSubnetsShape(t *testing.T) {
	for _, n := range scanSubnets() {
		if !strings.HasSuffix(n, ".0") {
			t.Errorf("subnet %q does not end in .0", n)
		}
	}
}
