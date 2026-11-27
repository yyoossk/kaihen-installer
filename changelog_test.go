package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The generated changelog data must cover the published history:
// every entry carries a tag and at least one bullet in at least one
// language, and the handler serves the requested language's lines.
func TestChangelogData(t *testing.T) {
	if len(changelogData) < 20 {
		t.Fatalf("changelogData has %d entries, want the full published history", len(changelogData))
	}
	for _, e := range changelogData {
		if e.Tag == "" {
			t.Fatal("changelog entry with an empty tag")
		}
		if len(e.EN) == 0 && len(e.JA) == 0 {
			t.Fatalf("changelog entry %s has no bullets", e.Tag)
		}
	}
}

func TestChangelogHandler(t *testing.T) {
	for _, tc := range []struct {
		lang string
		want []string
	}{
		{"ja", changelogData[0].JA},
		{"en", changelogData[0].EN},
		{"fr", changelogData[0].EN},
	} {
		if len(tc.want) == 0 {
			continue
		}
		rec := httptest.NewRecorder()
		changelogHandler(rec, httptest.NewRequest("GET", "/api/changelog?lang="+tc.lang, nil))
		var out struct {
			Entries []struct {
				Tag   string   `json:"tag"`
				Lines []string `json:"lines"`
			} `json:"entries"`
		}
		if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&out); err != nil {
			t.Fatalf("lang %s: decode: %v", tc.lang, err)
		}
		if len(out.Entries) != len(changelogData) {
			t.Fatalf("lang %s: %d entries, want %d", tc.lang, len(out.Entries), len(changelogData))
		}
		if out.Entries[0].Tag != changelogData[0].Tag {
			t.Fatalf("lang %s: first tag = %s, want %s", tc.lang, out.Entries[0].Tag, changelogData[0].Tag)
		}
		if len(out.Entries[0].Lines) == 0 || out.Entries[0].Lines[0] != tc.want[0] {
			t.Fatalf("lang %s: first entry lines do not match the %s bullets", tc.lang, tc.lang)
		}
	}
}
