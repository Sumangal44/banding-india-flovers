package updater

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// stubCompare stands in for GitHub's compare API. It counts hits (to prove the
// cache) and returns commits oldest-first, the order GitHub uses, so the
// reversal to newest-first is exercised. Each request must carry the headers
// GitHub requires, or the real endpoint 403s.
func stubCompare(t *testing.T, total int, commits [][2]string) (*httptest.Server, *int32) {
	t.Helper()
	arr := make([]map[string]any, 0, len(commits))
	for _, c := range commits {
		arr = append(arr, map[string]any{
			"sha":    c[1],
			"commit": map[string]any{"message": c[0]},
		})
	}
	body, _ := json.Marshal(map[string]any{"total_commits": total, "commits": arr})

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("request missing User-Agent (GitHub 403s without one)")
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q, want application/vnd.github+json", got)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestIncomingCommitsParsesNewestFirst(t *testing.T) {
	srv, hits := stubCompare(t, 3, [][2]string{
		{"oldest subject\n\nbody line", "aaaaaaa000"},
		{"middle subject", "bbbbbbb111"},
		{"newest subject\n\nlong body here", "ccccccc222"},
	})
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	ups, behind := incomingCommits("aaaaaaa", "ccccccc")
	if behind != 3 {
		t.Errorf("behind = %d, want 3 (total_commits)", behind)
	}
	if len(ups) != 3 {
		t.Fatalf("updates = %d, want 3", len(ups))
	}
	// Newest first, subject only (first line), 7-char short hash in New, no Old.
	if ups[0].Name != "newest subject" {
		t.Errorf("ups[0].Name = %q, want %q", ups[0].Name, "newest subject")
	}
	if ups[0].New != "ccccccc" {
		t.Errorf("ups[0].New = %q, want the 7-char short hash %q", ups[0].New, "ccccccc")
	}
	if ups[0].Old != "" {
		t.Errorf("a commit row has no from/to pair, want empty Old, got %q", ups[0].Old)
	}
	if ups[2].Name != "oldest subject" {
		t.Errorf("ups[2].Name = %q, want %q (oldest last)", ups[2].Name, "oldest subject")
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
}

func TestIncomingCommitsCaches(t *testing.T) {
	srv, hits := stubCompare(t, 1, [][2]string{{"only commit", "ddddddd333"}})
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	if _, b := incomingCommits("base111", "head222"); b != 1 {
		t.Fatalf("first lookup behind = %d, want 1", b)
	}
	if _, b := incomingCommits("base111", "head222"); b != 1 {
		t.Fatalf("second lookup behind = %d, want 1", b)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (same pair should serve from cache)", got)
	}
	// A new head is a cache miss: the network is consulted again.
	if _, b := incomingCommits("base111", "head999"); b != 1 {
		t.Fatalf("new-head lookup behind = %d, want 1", b)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Errorf("server hits = %d, want 2 (a new head refetches)", got)
	}
}

func TestIncomingCommitsNoUpdateSkipsFetch(t *testing.T) {
	srv, hits := stubCompare(t, 5, [][2]string{{"unused", "eeeeeee444"}})
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	for _, tc := range [][2]string{{"same", "same"}, {"", "head"}, {"base", ""}} {
		ups, behind := incomingCommits(tc[0], tc[1])
		if ups != nil || behind != 0 {
			t.Errorf("incomingCommits(%q,%q) = (%v,%d), want (nil,0)", tc[0], tc[1], ups, behind)
		}
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("server hits = %d, want 0 (no lookup when nothing is incoming)", got)
	}
}

func TestIncomingCommitsFailureIsBestEffort(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	ups, behind := incomingCommits("base", "head")
	if ups != nil || behind != 0 {
		t.Errorf("on API error want (nil,0) so the caller degrades, got (%v,%d)", ups, behind)
	}
	// A failed lookup must not poison the cache: the next release still resolves.
	if _, err := os.Stat(filepath.Join(cache, "ryoku", "commits.json")); !os.IsNotExist(err) {
		t.Errorf("a failed fetch must not write a cache file (stat err = %v)", err)
	}
}

// stubRecent stands in for GitHub's list-commits API, which returns a bare
// array of commits (not the compare API's object wrapper) newest first. It
// counts hits to prove the cache and asserts the headers GitHub requires.
func stubRecent(t *testing.T, commits [][2]string) (*httptest.Server, *int32) {
	t.Helper()
	arr := make([]map[string]any, 0, len(commits))
	for _, c := range commits {
		arr = append(arr, map[string]any{
			"sha":    c[1],
			"commit": map[string]any{"message": c[0]},
		})
	}
	body, _ := json.Marshal(arr)

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("User-Agent") == "" {
			t.Error("request missing User-Agent (GitHub 403s without one)")
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q, want application/vnd.github+json", got)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestRecentCommitsParsesNewestFirst(t *testing.T) {
	srv, hits := stubRecent(t, [][2]string{
		{"newest subject\n\nbody text", "aaaaaaa111"},
		{"middle subject", "bbbbbbb222"},
		{"oldest subject", "ccccccc333"},
	})
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	ups := recentCommits("aaaaaaa")
	if len(ups) != 3 {
		t.Fatalf("recent = %d, want 3", len(ups))
	}
	// Subject only (first line), 7-char short hash in New, no Old; the list API
	// is already newest-first, so no reversal.
	if ups[0].Name != "newest subject" {
		t.Errorf("ups[0].Name = %q, want %q", ups[0].Name, "newest subject")
	}
	if ups[0].New != "aaaaaaa" {
		t.Errorf("ups[0].New = %q, want the 7-char short hash %q", ups[0].New, "aaaaaaa")
	}
	if ups[0].Old != "" {
		t.Errorf("a commit row has no from/to pair, want empty Old, got %q", ups[0].Old)
	}
	if ups[2].Name != "oldest subject" {
		t.Errorf("ups[2].Name = %q, want %q (oldest last)", ups[2].Name, "oldest subject")
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
}

func TestRecentCommitsCaches(t *testing.T) {
	srv, hits := stubRecent(t, [][2]string{{"only commit", "ddddddd444"}})
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	if got := recentCommits("head111"); len(got) != 1 {
		t.Fatalf("first lookup len = %d, want 1", len(got))
	}
	if got := recentCommits("head111"); len(got) != 1 {
		t.Fatalf("second lookup len = %d, want 1", len(got))
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (same head should serve from cache)", got)
	}
	// A new head is a cache miss: the network is consulted again.
	if got := recentCommits("head999"); len(got) != 1 {
		t.Fatalf("new-head lookup len = %d, want 1", len(got))
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Errorf("server hits = %d, want 2 (a new head refetches)", got)
	}
}

func TestRecentCommitsEmptyHeadSkipsFetch(t *testing.T) {
	srv, hits := stubRecent(t, [][2]string{{"unused", "eeeeeee555"}})
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	if got := recentCommits(""); got != nil {
		t.Errorf("recentCommits(\"\") = %v, want nil (no head, no lookup)", got)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("server hits = %d, want 0 (empty head must not fetch)", got)
	}
}

func TestRecentCommitsFailureIsBestEffort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RYOKU_GITHUB_API", srv.URL)
	t.Setenv("RYOKU_REPO_SLUG", "owner/repo")
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	if got := recentCommits("head"); got != nil {
		t.Errorf("on API error want nil so the caller degrades, got %v", got)
	}
	// A failed lookup must not poison the cache: the next release still resolves.
	if _, err := os.Stat(filepath.Join(cache, "ryoku", "recent.json")); !os.IsNotExist(err) {
		t.Errorf("a failed fetch must not write a cache file (stat err = %v)", err)
	}
}
