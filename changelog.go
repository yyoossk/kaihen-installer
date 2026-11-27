package main

import (
	"encoding/json"
	"net/http"
)

// changelogEntry is one release's notes in the in-app update
// history: the build tag plus its changelog bullets, split by
// language. The data itself is generated from CHANGELOG.md into
// changelog_data.go (see gen-changelog.py).
type changelogEntry struct {
	Tag string
	EN  []string
	JA  []string
}

// changelogHandler serves the update history for the history page:
// every entry's bullets in the requested language (Japanese for
// lang=ja, English otherwise), falling back to whichever list is
// non-empty.
func changelogHandler(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	type entry struct {
		Tag   string   `json:"tag"`
		Lines []string `json:"lines"`
	}
	out := struct {
		Entries []entry `json:"entries"`
	}{Entries: []entry{}}
	for _, e := range changelogData {
		lines := e.EN
		if lang == "ja" && len(e.JA) > 0 {
			lines = e.JA
		}
		if lines == nil {
			lines = []string{}
		}
		out.Entries = append(out.Entries, entry{Tag: e.Tag, Lines: lines})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
