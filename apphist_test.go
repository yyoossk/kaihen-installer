package main

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// The parsers must map the real GitHub/GitLab release-list shapes
// onto appRelease, skipping drafts and capping bodies.
func TestParseGitHubReleases(t *testing.T) {
	data := []byte(`[
		{"tag_name":"v2.0","name":"Big update","published_at":"2026-01-02T03:04:05Z","body":"notes","prerelease":false,"draft":false},
		{"tag_name":"v2.1-beta","name":"","published_at":"2026-01-03T00:00:00Z","body":"","prerelease":true,"draft":false},
		{"tag_name":"v9.9","name":"secret","published_at":"2026-01-04T00:00:00Z","body":"","prerelease":false,"draft":true}
	]`)
	rels, err := parseGitHubReleases(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 {
		t.Fatalf("got %d releases, want 2 (draft skipped)", len(rels))
	}
	if rels[0].Tag != "v2.0" || rels[0].Name != "Big update" || rels[0].Date != "2026-01-02T03:04:05Z" || rels[0].Body != "notes" || rels[0].Pre {
		t.Fatalf("first release mapped wrong: %+v", rels[0])
	}
	if !rels[1].Pre {
		t.Fatalf("second release should be marked prerelease: %+v", rels[1])
	}
	if _, err := parseGitHubReleases([]byte(`{"message":"nope"}`)); err == nil {
		t.Fatal("an error object must not parse as a release list")
	}
}

func TestParseGitLabReleases(t *testing.T) {
	data := []byte(`[
		{"tag_name":"v1.0","name":"First","released_at":"2026-02-01T00:00:00Z","description":"desc"},
		{"tag_name":"v0.9","name":"","created_at":"2026-01-01T00:00:00Z","description":""}
	]`)
	rels, err := parseGitLabReleases(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 2 || rels[0].Tag != "v1.0" || rels[0].Body != "desc" {
		t.Fatalf("gitlab releases mapped wrong: %+v", rels)
	}
	if rels[1].Date != "2026-01-01T00:00:00Z" {
		t.Fatalf("created_at fallback missing: %+v", rels[1])
	}
	long := strings.Repeat("x", 5000)
	if got := trimHistoryBody(long); len(got) != 3000 {
		t.Fatalf("body cap = %d, want 3000", len(got))
	}
}

// The handler resolves catalog ids and the kaihen special case,
// and rejects unknown ids.
func TestAppsHistoryHandler(t *testing.T) {
	old := appHistoryGet
	defer func() { appHistoryGet = old }()
	appHistoryGet = func(u string) ([]byte, error) {
		if strings.Contains(u, "api.github.com/repos/ouncebrew/kaihen/") {
			return []byte(`[{"tag_name":"20261127.1","name":"","published_at":"2026-11-27T00:00:00Z","body":"b","prerelease":false,"draft":false}]`), nil
		}
		if strings.Contains(u, "api.github.com/repos/Rodpad/SW2NES/") {
			return []byte(`[{"tag_name":"v2.14.0","name":"","published_at":"2026-11-27T00:00:00Z","body":"","prerelease":false,"draft":false}]`), nil
		}
		if strings.Contains(u, "gitlab.com/api/v4/projects/Azkali%2Fmoonlight-nx2/") {
			return []byte(`[{"tag_name":"v1","name":"","released_at":"2026-01-01T00:00:00Z","description":"d"}]`), nil
		}
		return nil, errors.New("unexpected url " + u)
	}
	for _, tc := range []struct{ id, wantTag string }{
		{"kaihen", "20261127.1"},
		{"Rodpad/SW2NES", "v2.14.0"},
		{"Azkali/moonlight-nx2", "v1"},
	} {
		rec := httptest.NewRecorder()
		appsHistoryHandler(rec, httptest.NewRequest("GET", "/api/apps/history?id="+tc.id, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"tag":"`+tc.wantTag+`"`) {
			t.Fatalf("id %s: code %d body %s", tc.id, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	appsHistoryHandler(rec, httptest.NewRequest("GET", "/api/apps/history?id=nobody/nothing", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown id: code %d, want 404", rec.Code)
	}
}
