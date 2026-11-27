package main

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// Downloaded payloads pile up next to the app: the prepared kaiHEN
// distributions, the catalog's app caches (one extra file alone is
// 411 MB), the SWFence bundle cache and the autoloader downloads.
// The home page shows their total size and can clear them; anything
// cleared is simply downloaded again when it is next needed.

func cacheDirs() []string {
	return []string{
		pathOf("kaihen"),
		appsCacheRoot(),
		pathOf("swfence-cache"),
		pathOf("autoloader"),
		pathOf("autoloader-sd"),
	}
}

func dirSize(dir string) int64 {
	var total int64
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, ierr := d.Info(); ierr == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func cacheTotal() int64 {
	var total int64
	for _, d := range cacheDirs() {
		total += dirSize(d)
	}
	return total
}

func cacheSizeHandler(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"bytes": cacheTotal()})
}

func cacheClearHandler(w http.ResponseWriter, r *http.Request) {
	before := cacheTotal()
	// Entries must forget their downloaded state, or an install
	// would trust a tag whose files are gone and fail on the
	// missing cache file. Installed state lives on the SD card and
	// is kept.
	appsMu.Lock()
	for _, a := range appsList {
		a.DownloadedTag = ""
	}
	saveAppsLocked()
	appsMu.Unlock()
	for _, d := range cacheDirs() {
		os.RemoveAll(d)
	}
	jsonOK(w, map[string]any{"bytes": before})
}
