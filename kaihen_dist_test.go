package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Live check of the pinned kaiHEN dist (20261123): ensureKaihen must
// download the real release zip, verify it against the pin, unpack
// the new sdcard/ layout, and place the byte-identical legacy-layout
// copies the bundled kit engine looks for (hen/main, game/main,
// hbmenu.nro, programs/ounceshell.nro). The shared libraries for
// dynamically linked homebrew must be present under
// sdcard/kaihen/lib, ready for syncKaihenLib.
func TestEnsureKaihenDist(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()

	if err := ensureKaihen(func(string) {}, "en"); err != nil {
		t.Fatalf("ensureKaihen: %v", err)
	}
	if !kaihenReady() {
		t.Fatal("kaihenReady is false after ensureKaihen")
	}
	root := kaihenDistDir()
	for _, cp := range [][2]string{
		{filepath.Join("sdcard", "kaihen", "bin", "hen", "main"), filepath.Join("hen", "main")},
		{filepath.Join("sdcard", "kaihen", "bin", "game", "main"), filepath.Join("game", "main")},
		{filepath.Join("sdcard", "hbmenu.nro"), "hbmenu.nro"},
		{filepath.Join("sdcard", "switch", "ounceshell.nro"), filepath.Join("programs", "ounceshell.nro")},
	} {
		src, err := os.ReadFile(filepath.Join(root, cp[0]))
		if err != nil {
			t.Fatalf("read %s: %v", cp[0], err)
		}
		dst, err := os.ReadFile(filepath.Join(root, cp[1]))
		if err != nil {
			t.Fatalf("read %s: %v", cp[1], err)
		}
		if !bytes.Equal(src, dst) {
			t.Fatalf("%s is not a byte-identical copy of %s", cp[1], cp[0])
		}
	}
	libs, err := os.ReadDir(filepath.Join(root, "sdcard", "kaihen", "lib"))
	if err != nil {
		t.Fatalf("lib dir: %v", err)
	}
	if len(libs) != 8 {
		t.Fatalf("lib dir holds %d files, want 8", len(libs))
	}
}
