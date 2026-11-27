package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Live checks of the pinned kaiHEN dists and their two variants.
// Stock: ensureKaihen must download the real 20261127.1 release
// zip, verify it against the pin, and unpack the sdcard/ layout
// into the root layout the bundled kit engine expects (hen/main,
// game/main, hbmenu.nro, programs/ounceshell.nro), keeping the
// kaihen/lib shared-library tree for the SD sync. Patched: the
// separate 20261117.1 base dist with switch2-modbridge's prebuilt
// ro:dmnt binaries (ro/main, hen/main) overlaid and verified
// against their own pins, tagged "20261117.1+patched". Switching
// variants re-prepares the dist.

const (
	stockHenSHA = "10bd5c8e702d88c821f6a3af1e0d37668ce9f0649f0005d745a45c05196dffce"
	stockRoSHA  = "22f7954c015ec144bb3bc5d57fea592685156940b0b4d4f3b9d53cb1588644ea"
)

func TestEnsureKaihenDist(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()

	if err := ensureKaihen(func(string) {}, "en", "stock"); err != nil {
		t.Fatalf("ensureKaihen stock: %v", err)
	}
	if !kaihenReady("stock") {
		t.Fatal("kaihenReady(stock) is false after ensureKaihen")
	}
	if kaihenReady("patched") {
		t.Fatal("kaihenReady(patched) is true for a stock dist")
	}
	root := kaihenDistDir()
	if got := fileSHA(filepath.Join(root, "hen", "main")); got != stockHenSHA {
		t.Fatalf("stock hen/main sha = %s, want %s", got, stockHenSHA)
	}
	if got := fileSHA(filepath.Join(root, "ro", "main")); got != stockRoSHA {
		t.Fatalf("stock ro/main sha = %s, want %s", got, stockRoSHA)
	}
	// The shared-library tree must survive preparation: catalog
	// installs sync it to the SD card for dynamically linked apps.
	if _, err := os.Stat(filepath.Join(root, "sdcard", "kaihen", "lib", "libounce.so.1")); err != nil {
		t.Fatalf("stock dist lost its kaihen/lib tree: %v", err)
	}
}

func TestEnsureKaihenDistPatched(t *testing.T) {
	old := exeDir
	exeDir = t.TempDir()
	defer func() { exeDir = old }()

	if err := ensureKaihen(func(string) {}, "en", "patched"); err != nil {
		t.Fatalf("ensureKaihen patched: %v", err)
	}
	if !kaihenReady("patched") {
		t.Fatal("kaihenReady(patched) is false after ensureKaihen")
	}
	if kaihenReady("stock") {
		t.Fatal("kaihenReady(stock) is true for a patched dist")
	}
	root := kaihenDistDir()
	if got := fileSHA(filepath.Join(root, "hen", "main")); got != modbridgeHenSHA {
		t.Fatalf("patched hen/main sha = %s, want %s", got, modbridgeHenSHA)
	}
	if got := fileSHA(filepath.Join(root, "ro", "main")); got != modbridgeRoSHA {
		t.Fatalf("patched ro/main sha = %s, want %s", got, modbridgeRoSHA)
	}
	// Switching back to stock re-prepares the dist with the stock
	// binaries.
	if err := ensureKaihen(func(string) {}, "en", "stock"); err != nil {
		t.Fatalf("ensureKaihen back to stock: %v", err)
	}
	if !kaihenReady("stock") {
		t.Fatal("kaihenReady(stock) is false after switching back")
	}
	if got := fileSHA(filepath.Join(root, "hen", "main")); got != stockHenSHA {
		t.Fatalf("switched-back hen/main sha = %s, want %s", got, stockHenSHA)
	}
	if _, err := os.Stat(filepath.Join(root, "hbmenu.nro")); err != nil {
		t.Fatalf("hbmenu.nro missing after switch back: %v", err)
	}
}
