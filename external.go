package main

import (
	"io"
	"os"
	"path/filepath"
)

// mirrorFolderExternal copies dir into the folder named by the
// KAIHEN_EXTERNAL_DIR environment variable, when one is set. The Android
// wrapper sets it to the app-specific external storage directory so the
// files the GUI stages (the autoloader SD files, the Pico UF2s) are
// reachable from a file manager or over USB. On desktop the variable is
// never set and this does nothing. Errors are ignored: mirroring is a
// convenience, never a reason to fail the operation itself.
func mirrorFolderExternal(dir string) {
	ext := os.Getenv("KAIHEN_EXTERNAL_DIR")
	if ext == "" {
		return
	}
	dst := filepath.Join(ext, filepath.Base(dir))
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			os.MkdirAll(target, 0o755)
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil
		}
		out, err := os.Create(target)
		if err != nil {
			return nil
		}
		defer out.Close()
		io.Copy(out, in)
		return nil
	})
}
