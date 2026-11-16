//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jchv/go-webview2"
)

// openEmbeddedUI shows the installer UI in an embedded WebView2 window
// instead of the default browser. Returns false when WebView2 is not
// available, so the caller falls back to the default browser.
func openEmbeddedUI(url string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  pathOf(".webview2"),
		WindowOptions: webview2.WindowOptions{
			Title:  "kaiHEN Installer (build " + buildStamp + ")",
			Width:  1020,
			Height: 840,
			Center: true,
		},
	})
	if w == nil {
		return false
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
	return true
}

// clearWebViewCache deletes the WebView2 page-cache folders inside the
// .webview2 profile next to the exe, so a stale cached page can never be
// shown again. Only cache directories are removed: Local Storage (the
// saved console IPs, language choice, Pi password) is left untouched.
func clearWebViewCache() {
	root := pathOf(".webview2")
	cacheNames := map[string]bool{
		"Cache": true, "Code Cache": true, "GPUCache": true,
		"CacheStorage": true, "DawnCache": true,
		"GrShaderCache": true, "ShaderCache": true,
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && cacheNames[info.Name()] {
			_ = os.RemoveAll(path)
			return filepath.SkipDir
		}
		if info.IsDir() && strings.HasSuffix(info.Name(), "Cache") && info.Name() != "Local Storage" {
			_ = os.RemoveAll(path)
			return filepath.SkipDir
		}
		return nil
	})
}
