package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestMsgsAllLangs(t *testing.T) {
	langs := []string{"ja", "en", "fr", "de", "it", "es", "ru", "pt", "pl", "ko", "zh-Hans", "zh-Hant"}
	if len(msgs) != 12 {
		t.Fatalf("msgs has %d langs", len(msgs))
	}
	for _, lang := range langs {
		if normalizeLang(lang) != lang {
			t.Errorf("normalizeLang(%s) = %s", lang, normalizeLang(lang))
		}
		for key := range msgs["en"] {
			v := tr(lang, key)
			if v == "" {
				t.Errorf("%s/%s empty", lang, key)
			}
			if lang != "en" && lang != "ja" && v == msgs["en"][key] {
				t.Errorf("%s/%s falls back to en: %q", lang, key, v)
			}
		}
		for _, k := range []string{"done", "failed"} {
			if !strings.HasPrefix(tr(lang, k), "=== ") {
				t.Errorf("%s/%s missing === prefix", lang, k)
			}
		}
		// verb-bearing keys must Sprintf cleanly
		checks := map[string][]any{
			"stageOk": {42}, "chunkSendErr": {1, 4}, "tplSent": {4, 1.5},
			"fetching": {"x"},
		}
		for k, args := range checks {
			out := fmt.Sprintf(tr(lang, k), args...)
			_ = out
			got := tr(lang, k, args...)
			if strings.Contains(got, "%!") {
				t.Errorf("%s/%s sprintf broken: %q", lang, k, got)
			}
		}
	}
	if normalizeLang("xx") != "en" || normalizeLang("") != "en" {
		t.Error("normalizeLang fallback broken")
	}
}
