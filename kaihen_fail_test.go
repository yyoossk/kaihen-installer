package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyRunError(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want string
	}{
		// The user's real failure records (2026-11-15 runs).
		{"disconnect", "Error: Error: Console disconnected. Inspect D:\\Downloads\\kaiHEN_Installer (2)\\assets\\kaihen\\engine\\runs\\switch2-8mZrUG\\switch2\\acquisition-1794735694409-1234-1.log", "disconn"},
		{"raceTimeout", "Error: FS acquisition exited or timed out before memory was ready. Inspect D:\\Downloads\\kaiHEN_Installer (2)\\assets\\kaihen\\engine\\runs\\switch2-lsc4yd\\switch2\\acquisition.log", "race"},
		// Kit README §2-5 categories.
		{"canary", "Positive canary verification failed: canary 0/30", "race"},
		{"nativeBatch", "Native batch did not terminate", "race"},
		{"drift", "General-heap allocation changed during acquisition", "race"},
		{"unknown", "something entirely unexpected", "other"},
		{"empty", "", "other"},
	}
	for _, c := range cases {
		if got := classifyRunError(c.err); got != c.want {
			t.Errorf("%s: classifyRunError = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLatestRunErrorIn(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, errText string, age time.Duration) {
		sub := filepath.Join(dir, name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(sub, "result.json")
		q, _ := json.Marshal(errText)
		body := `{"verified":false,"error":` + string(q) + `}`
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	mk("switch2-old", "Console disconnected", 30*time.Minute)
	mk("switch2-new", "FS acquisition exited or timed out before memory was ready", time.Minute)
	mk("other-dir", "canary 0/30", time.Minute)

	since := time.Now().Add(-10 * time.Minute)
	if got := latestRunErrorIn(dir, since); got != "FS acquisition exited or timed out before memory was ready" {
		t.Errorf("latestRunErrorIn = %q, want the newest switch2-* error", got)
	}
	if got := latestRunErrorIn(dir, time.Now().Add(time.Hour)); got != "" {
		t.Errorf("future window: got %q, want empty", got)
	}
	if got := latestRunErrorIn(filepath.Join(dir, "nope"), since); got != "" {
		t.Errorf("missing dir: got %q, want empty", got)
	}
}
