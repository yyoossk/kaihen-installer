//go:build !windows

package main

// openEmbeddedUI is Windows-only (WebView2); elsewhere the caller falls
// back to the default browser.
func openEmbeddedUI(url string) bool { return false }

// clearWebViewCache is a no-op outside Windows (no embedded WebView2).
func clearWebViewCache() {}
