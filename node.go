package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The launcher engine (CivSwitch Kit) runs on Node.js. The Windows
// build used to bundle node.exe (94 MB — most of the package); it is
// now downloaded once, on the first kaiHEN start, pinned by size and
// SHA-256 like the other downloads, and kept in runtime/ next to the
// exe. Self-updates never touch runtime/, so it is fetched only once.
// The pin is the official Node.js v24.22.3 win-x64 zip; its node.exe
// is byte-identical to the one earlier builds bundled.
const nodeZipURL = "https://nodejs.org/dist/v24.22.3/node-v24.22.3-win-x64.zip"

var (
	nodeZipSize         = 37762649
	nodeZipSHA          = "e612af99f626ab415ea225af6bce5c971feb448cb5cae250bb1c3fa50c5f7d9f"
	nodeExeSize   int64 = 93944648
	nodeRuntimeOS       = runtime.GOOS
	nodeDownload        = func() ([]byte, error) {
		return httpGetPinned(nodeZipURL, nodeZipSize, nodeZipSHA)
	}
)

// ensureNode makes sure node.exe is available for the engine on
// Windows. It is a no-op when a node path is configured through the
// environment (Android passes its bundled libnode.so that way), on
// other platforms (nodeExe falls back to the system node), and when
// the pinned node.exe is already in runtime/ — including the copy
// bundled by older builds, which is the same file.
func ensureNode(log logFn, lang string) error {
	if os.Getenv("KAIHEN_NODE") != "" || os.Getenv("CIVINSTALLER_NODE") != "" {
		return nil
	}
	if nodeRuntimeOS != "windows" {
		return nil
	}
	dst := pathOf("runtime", "node.exe")
	if st, err := os.Stat(dst); err == nil && st.Size() == nodeExeSize {
		return nil
	}
	log(tr(lang, "nodeDl"))
	data, err := nodeDownload()
	if err != nil {
		return errors.New(tr(lang, "nodeDlFail"))
	}
	exe, err := unzipNodeExe(data)
	if err != nil || int64(len(exe)) != nodeExeSize {
		return errors.New(tr(lang, "nodeDlFail"))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, exe, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// unzipNodeExe extracts the node.exe entry from a Node.js win-x64 zip.
func unzipNodeExe(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.Name == "node.exe" || strings.HasSuffix(f.Name, "/node.exe") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			var buf bytes.Buffer
			_, rerr := buf.ReadFrom(rc)
			rc.Close()
			if rerr != nil {
				return nil, rerr
			}
			return buf.Bytes(), nil
		}
	}
	return nil, errors.New("node.exe not in zip")
}
