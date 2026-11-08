package freebuff

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHostedScannerReadsCumulativeMetricsAndCaches(t *testing.T) {
	var eventsCalls atomic.Int32
	finished := testNow.Add(-time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("x-freebuff-device-chats") != "1" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v1/cloud/history/")
		switch {
		case p == "chats":
			_, _ = w.Write([]byte(`{"items":[{"id":"c1","updatedAt":"u1","latestRun":null}],"nextCursor":null}`))
		case p == "chats/c1/runs":
			_, _ = w.Write([]byte(`{"items":[
				{"id":"r1","chatId":"c1","status":"completed","model":"m-normal","agent":"codebuff","createdAt":"` + finished + `","finishedAt":"` + finished + `"},
				{"id":"r2","chatId":"c1","status":"completed","model":"byok:x","agent":"codebuff","createdAt":"` + finished + `","finishedAt":"` + finished + `"},
				{"id":"r3","chatId":"c1","status":"completed","model":"m-normal","agent":"claude","createdAt":"` + finished + `","finishedAt":"` + finished + `"}],"nextCursor":null}`))
		case p == "runs/r1/events":
			eventsCalls.Add(1)
			_, _ = w.Write([]byte(`{"items":[
				{"kind":"metrics","payload":{"usage":{"inputTokens":10,"outputTokens":5}}},
				{"kind":"message","payload":{"text":"secret conversation"}},
				{"kind":"metrics","payload":{"usage":{"inputTokens":100,"outputTokens":50,"cachedInputTokens":80,"totalTokens":150}}}],
				"nextCursor":null,"endCursor":"e1"}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := NewClient("test", func() string { return "UTC" })
	c.BaseOverride = srv.URL
	cache := filepath.Join(t.TempDir(), "hosted.json")
	s := &HostedScanner{CachePath: cache}
	rs, err := s.Scan(context.Background(), Auth{Token: "tok"}, c, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].ID != "cloud:r1" || rs[0].Metrics.Usage == nil || rs[0].Metrics.Usage.InputTokens != 100 {
		t.Fatalf("receipts = %+v", rs)
	}
	// Unchanged chat: no further event reads, and a fresh scanner reuses the disk cache.
	s2 := &HostedScanner{CachePath: cache}
	if got := s2.Cached(Auth{Token: "tok"}); len(got) != 1 {
		t.Fatalf("cached = %+v", got)
	}
	if got := s2.Cached(Auth{Token: "other"}); len(got) != 0 {
		t.Fatal("cache must be keyed by account")
	}
	if _, err := s2.Scan(context.Background(), Auth{Token: "tok"}, c, testNow); err != nil {
		t.Fatal(err)
	}
	if n := eventsCalls.Load(); n != 1 {
		t.Fatalf("events fetched %d times, want 1", n)
	}
	if _, err := s2.Scan(context.Background(), Auth{Token: "bad"}, c, testNow); err != ErrTokenInvalid {
		t.Fatalf("err = %v, want ErrTokenInvalid", err)
	}
}


func TestWriteFileAtomicPreservesLastValidCacheOnRenameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosted.json")
	const previous = "{\"accountKey\":\"previous\"}"
	if err := os.WriteFile(path, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	renames := 0
	failure := errors.New("destination in use")
	err := writeFileAtomicWithRename(path, []byte("{\"accountKey\":\"new\"}"), func(from, to string) error {
		renames++
		if to != path {
			t.Fatalf("unexpected rename destination %q", to)
		}
		// Simulate a locked destination without deleting or modifying it.
		return failure
	})
	if !errors.Is(err, failure) || renames != 1 {
		t.Fatalf("rename error = %v, attempts = %d; want original failure and one attempt", err, renames)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != previous {
		t.Fatalf("valid cached history lost after rename failure: content=%q err=%v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "hosted.json" {
		t.Fatalf("failed staging file leaked: entries=%+v err=%v", entries, err)
	}
	if err := writeFileAtomic(path, []byte("{\"accountKey\":\"new\"}")); err != nil {
		t.Fatalf("later retry failed: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "{\"accountKey\":\"new\"}" {
		t.Fatalf("retry did not replace cached history: content=%q err=%v", got, err)
	}
}
