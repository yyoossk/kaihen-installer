package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The RetroArch release zip keeps its data in a top-level retroarch/
// folder (assets, cores, info) next to switch/. The tree classifier
// must keep that folder whole: without it the menu has no fonts
// (Japanese garbles) and no cores load. Regression test for the
// 2026-11-18 report.
func TestDownloadNroZipKeepsRetroarchTree(t *testing.T) {
	exeDir = t.TempDir()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"switch/retroarch/retroarch_switch.nro":        "frontend",
		"retroarch/assets/ozone/regular.ttf":           "font-regular",
		"retroarch/assets/ozone/bold.ttf":              "font-bold",
		"retroarch/cores/picodrive_libretro_libnx.nro": "core",
		"retroarch/info/picodrive_libretro.info":       "info",
		"kaihen/plugins/ftpd.nso":                      "plugin",
		"README.txt":                                   "junk",
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	a := &appEntry{ID: "test/ra", Kind: "nro", LatestTag: "v1"}
	if err := downloadNroZipLocked(a, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(appCacheDir(a.ID), "root")
	for rel, want := range map[string]string{
		"switch/retroarch/retroarch_switch.nro":        "frontend",
		"retroarch/assets/ozone/regular.ttf":           "font-regular",
		"retroarch/assets/ozone/bold.ttf":              "font-bold",
		"retroarch/cores/picodrive_libretro_libnx.nro": "core",
		"retroarch/info/picodrive_libretro.info":       "info",
		"kaihen/plugins/ftpd.nso":                      "plugin",
	} {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("missing tree file %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s: content %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "README.txt")); !os.IsNotExist(err) {
		t.Errorf("README.txt should have been dropped, stat err = %v", err)
	}
}
