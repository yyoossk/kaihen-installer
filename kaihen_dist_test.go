package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Live checks of the pinned kaiHEN dist (20261117.1) and its two
// variants. Stock: ensureKaihen must download the real release zip,
// verify it against the pin, and unpack the root layout the bundled
// kit engine expects (hen/main, game/main, hbmenu.nro,
// programs/ounceshell.nro). Patched: the same dist with
// switch2-modbridge's prebuilt ro:dmnt binaries (ro/main, hen/main)
// overlaid and verified against their own pins, tagged
// "20261117.1+patched". Switching variants re-prepares the dist.

const (
	stockHenSHA = "b4f161e36482d50de006b3caee909eb8413a42b6593a602324cface3d2a5ead9"
	stockRoSHA  = "2e577a4c680a3ba580bcda2f8d41ada443cb33cad79e06230ce4bcf6700661b7"
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
