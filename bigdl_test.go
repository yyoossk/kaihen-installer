package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// bigDlFixture serves a deterministic 4 MB blob: range-capable when
// ranges is set (via http.ServeContent), a plain 200 otherwise.
func bigDlFixture(t *testing.T, ranges bool) (*httptest.Server, []byte, *int64) {
	t.Helper()
	blob := make([]byte, 4<<20)
	for i := range blob {
		blob[i] = byte(i*31 + 7)
	}
	var rangeReqs int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			atomic.AddInt64(&rangeReqs, 1)
		}
		if ranges {
			http.ServeContent(w, r, "blob.bin", time.Unix(1700000000, 0), bytes.NewReader(blob))
			return
		}
		w.Header().Set("Content-Length", "4194304")
		w.Write(blob)
	}))
	t.Cleanup(srv.Close)
	return srv, blob, &rangeReqs
}

func withLowThreshold(t *testing.T) {
	t.Helper()
	old := bigDlThreshold
	bigDlThreshold = 1 << 20
	t.Cleanup(func() { bigDlThreshold = old })
}

func TestHttpGetBigParallel(t *testing.T) {
	withLowThreshold(t)
	srv, blob, rangeReqs := bigDlFixture(t, true)
	got, err := httpGetBig(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("parallel download content mismatch")
	}
	if n := atomic.LoadInt64(rangeReqs); n < 2 {
		t.Fatalf("only %d range requests; the parallel path did not run", n)
	}
}

func TestHttpGetBigNoRanges(t *testing.T) {
	withLowThreshold(t)
	srv, blob, _ := bigDlFixture(t, false)
	got, err := httpGetBig(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("no-range fallback content mismatch")
	}
}

func TestHttpGetToFileFastParallel(t *testing.T) {
	withLowThreshold(t)
	srv, blob, rangeReqs := bigDlFixture(t, true)
	dst := filepath.Join(t.TempDir(), "blob.bin")
	var lastDone, lastTotal int64
	err := httpGetToFileFast(srv.URL, dst, func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("parallel file download content mismatch")
	}
	if n := atomic.LoadInt64(rangeReqs); n < 2 {
		t.Fatalf("only %d range requests; the parallel path did not run", n)
	}
	if lastDone != int64(len(blob)) || lastTotal != int64(len(blob)) {
		t.Fatalf("final progress = %d/%d, want %d/%d", lastDone, lastTotal, len(blob), len(blob))
	}
}

func TestHttpGetToFileFastNoRanges(t *testing.T) {
	withLowThreshold(t)
	srv, blob, _ := bigDlFixture(t, false)
	dst := filepath.Join(t.TempDir(), "blob.bin")
	if err := httpGetToFileFast(srv.URL, dst, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("no-range file fallback content mismatch")
	}
}

func TestHttpGetPinnedBig(t *testing.T) {
	withLowThreshold(t)
	srv, blob, rangeReqs := bigDlFixture(t, true)
	sha := sha256Hex(blob)
	got, err := httpGetPinned(srv.URL, len(blob), sha)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatal("pinned download content mismatch")
	}
	if n := atomic.LoadInt64(rangeReqs); n < 2 {
		t.Fatalf("only %d range requests; the parallel path did not run", n)
	}
	// A wrong pin is still rejected after the parallel download.
	if _, err := httpGetPinned(srv.URL, len(blob), sha256Hex([]byte("other"))); err != errPinnedContent {
		t.Fatalf("wrong pin: err = %v, want errPinnedContent", err)
	}
}
