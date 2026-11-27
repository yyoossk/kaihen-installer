package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Release history for the catalog software and apps, served to the
// update-history page. The history comes from the publisher's own
// release list: the GitHub API for GitHub repos, the GitLab API for
// the one GitLab catalog entry. Fetched on demand, one app at a
// time, only when the user opens that app's history.

type appRelease struct {
	Tag  string `json:"tag"`
	Name string `json:"name"`
	Date string `json:"date"`
	Body string `json:"body"`
	Pre  bool   `json:"pre"`
}

// appHistoryGet fetches a URL body; a var so tests can stub it.
var appHistoryGet = httpGet

func parseGitHubReleases(data []byte) ([]appRelease, error) {
	var raw []struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		PublishedAt string `json:"published_at"`
		Body        string `json:"body"`
		Prerelease  bool   `json:"prerelease"`
		Draft       bool   `json:"draft"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := []appRelease{}
	for _, r := range raw {
		if r.Draft {
			continue
		}
		out = append(out, appRelease{
			Tag: r.TagName, Name: r.Name, Date: r.PublishedAt,
			Body: trimHistoryBody(r.Body), Pre: r.Prerelease,
		})
	}
	return out, nil
}

func parseGitLabReleases(data []byte) ([]appRelease, error) {
	var raw []struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		ReleasedAt  string `json:"released_at"`
		CreatedAt   string `json:"created_at"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := []appRelease{}
	for _, r := range raw {
		date := r.ReleasedAt
		if date == "" {
			date = r.CreatedAt
		}
		out = append(out, appRelease{
			Tag: r.TagName, Name: r.Name, Date: date,
			Body: trimHistoryBody(r.Description),
		})
	}
	return out, nil
}

// trimHistoryBody caps a release body so one huge notes field
// cannot swamp the page.
func trimHistoryBody(s string) string {
	s = strings.TrimSpace(s)
	const max = 3000
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func fetchAppHistory(id, host string) ([]appRelease, error) {
	if host == "gitlab" {
		u := "https://gitlab.com/api/v4/projects/" + url.PathEscape(id) + "/releases?per_page=20"
		data, err := appHistoryGet(u)
		if err != nil {
			return nil, err
		}
		return parseGitLabReleases(data)
	}
	data, err := appHistoryGet("https://api.github.com/repos/" + id + "/releases?per_page=20")
	if err != nil {
		return nil, err
	}
	return parseGitHubReleases(data)
}

// appsHistoryHandler serves one app's release history:
// /api/apps/history?id=<owner/repo>. The special id "kaihen" is
// kaiHEN itself. Unknown ids are rejected.
func appsHistoryHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	name, host := "", ""
	if id == "kaihen" {
		id, name = "ouncebrew/kaihen", "kaiHEN"
	} else {
		appsInit()
		appsMu.Lock()
		a := findAppLocked(id)
		if a != nil {
			name, host = a.Name, a.Host
			if name == "" {
				parts := strings.Split(a.ID, "/")
				name = parts[len(parts)-1]
			}
		}
		appsMu.Unlock()
		if a == nil {
			http.Error(w, "unknown app", http.StatusNotFound)
			return
		}
	}
	rels, err := fetchAppHistory(id, host)
	if err != nil {
		http.Error(w, fmt.Sprintf("history fetch failed: %v", err), http.StatusBadGateway)
		return
	}
	jsonOK(w, map[string]any{"id": id, "name": name, "releases": rels})
}
