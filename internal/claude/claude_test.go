package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
)

func writeTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, line := range lines {
		body += line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assistant(id, model string, at time.Time, input, output, cacheCreate, cacheRead int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":"s1","message":{"id":%q,"model":%q,`+
		`"usage":{"input_tokens":%d,"output_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d}}}`,
		at.Format(time.RFC3339Nano), id, model, input, output, cacheCreate, cacheRead)
}

func TestScanCountsEachResponseOnce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "session.jsonl")
	at := time.Date(2026, 11, 5, 10, 0, 5, 0, time.Local)
	writeTranscript(t, path,
		`{"type":"user","timestamp":"2026-11-05T10:00:00.000Z","message":{"role":"user"}}`,
		// One response written as two content blocks repeats its full usage.
		assistant("msg_1", "claude-sonnet-4-5-20250929", at, 100, 20, 300, 4000),
		assistant("msg_1", "claude-sonnet-4-5-20250929", at, 100, 20, 300, 4000),
		`{"type":"system","timestamp":"2026-11-05T10:00:06.000Z"}`,
		assistant("msg_2", "claude-sonnet-4-5-20250929", at.Add(time.Minute), 50, 10, 0, 1000),
	)
	s := &Scanner{Root: root}
	days, err := s.Scan(context.Background(), at.Add(24*time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	// 100+20+300+4000 once, plus 50+10+0+1000.
	if days[0].Tokens != 5480 {
		t.Fatalf("tokens = %d, want 5480", days[0].Tokens)
	}
	if len(days[0].Models) != 1 || days[0].Models[0].Name != "Claude · Sonnet 4.5" {
		t.Fatalf("models = %+v", days[0].Models)
	}
}

func TestScanSeparatesDays(t *testing.T) {
	root := t.TempDir()
	first := time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local)
	writeTranscript(t, filepath.Join(root, "p", "a.jsonl"),
		assistant("m1", "claude-opus-4-6", first, 10, 5, 0, 0),
		assistant("m2", "claude-opus-4-6", first.Add(24*time.Hour), 20, 5, 0, 0),
	)
	s := &Scanner{Root: root}
	days, err := s.Scan(context.Background(), first.Add(72*time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0].Date != "2026-11-05" || days[0].Tokens != 25 || days[1].Tokens != 15 {
		t.Fatalf("days = %+v", days)
	}
}

func TestScanReusesUnchangedTranscripts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "p", "a.jsonl")
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	writeTranscript(t, path, assistant("m1", "claude-sonnet-4-5", at, 10, 5, 0, 0))
	s := &Scanner{Root: root}
	now := at.Add(time.Hour)
	first, err := s.Scan(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Scan(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0].Tokens != second[0].Tokens {
		t.Fatalf("cached scan = %+v, want %+v", second, first)
	}
	if _, ok := s.cache[path]; !ok {
		t.Fatal("transcript was not cached")
	}
}

func TestFoldPricesSnapshotModelIDs(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	supplement := &pricing.Supplement{Pricing: map[string]pricing.Rates{
		"claude-sonnet-4-5": {Input: 3, Output: 15, CacheWrite: 3.75, CacheRead: 0.3, CacheReadExplicit: true},
	}}
	snap := pricing.NewSnapshot(supplement, nil)
	days := Fold([]Turn{{
		At: at, MessageID: "m", ModelID: "claude-sonnet-4-5-20250929", Model: "claude-sonnet-4-5-20250929",
		Input: 1_000_000, Output: 1_000_000, CacheCreate: 1_000_000, CacheRead: 1_000_000,
	}}, at.Add(time.Hour), snap)
	if len(days) != 1 || days[0].CostUSD == nil {
		t.Fatalf("days = %+v", days)
	}
	// 3 + 15 + 3.75 + 0.3 per million.
	if got := *days[0].CostUSD; got < 22.04 || got > 22.06 {
		t.Fatalf("cost = %v, want 22.05", got)
	}
	if !days[0].CostComplete {
		t.Fatal("fully priced day marked incomplete")
	}
}

func TestDisplayModel(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4-5-20250929": "Sonnet 4.5",
		"claude-opus-4-6":            "Opus 4.6",
		"claude-haiku-4-5":           "Haiku 4.5",
		"":                           "Unknown model",
	}
	for in, want := range cases {
		if got := displayModel(in); got != want {
			t.Fatalf("displayModel(%q) = %q, want %q", in, got, want)
		}
	}
}

// A transcript can hold years of responses, but the panel only shows the last 30 days, so older
// turns are dropped before they are retained or folded.
func TestScanKeepsOnlyTheDisplayedWindow(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "session.jsonl")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	writeTranscript(t, path,
		assistant("old", "claude-sonnet-4-5-20250929", now.AddDate(0, 0, -400), 1000, 1000, 0, 0),
		assistant("recent", "claude-sonnet-4-5-20250929", now.Add(-time.Hour), 10, 5, 0, 0),
	)
	s := &Scanner{Root: root}
	days, err := s.Scan(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Tokens != 15 {
		t.Fatalf("days = %+v, want only the recent 15 tokens", days)
	}
	if n := len(s.cache[path].turns); n != 1 {
		t.Fatalf("cached turns = %d, want the retained window only", n)
	}

	// The file does not change, so the next scan is served from the cache; the window moving past
	// the remaining turn must still empty it.
	if days, err = s.Scan(context.Background(), now.AddDate(0, 0, 40), nil); err != nil {
		t.Fatal(err)
	}
	if len(days) != 0 || len(s.cache[path].turns) != 0 {
		t.Fatalf("days = %+v cached = %d, want the turn aged out", days, len(s.cache[path].turns))
	}
}

// BenchmarkScanCachedHistory is the refresh cost on a large but typical history: 4000 responses over
// 20 days, served from the per-file cache the second time on.
func BenchmarkScanCachedHistory(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "project", "session.jsonl")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	var body strings.Builder
	for i := 0; i < 4000; i++ {
		at := now.Add(-time.Duration(i) * 7 * time.Minute)
		body.WriteString(assistant(fmt.Sprintf("msg_%d", i), "claude-sonnet-4-5-20250929", at, 1200, 300, 100, 8000))
		body.WriteString("\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	s := &Scanner{Root: root}
	if _, err := s.Scan(context.Background(), now, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Scan(context.Background(), now, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// Append-only updates must not parse the full prefix again, and a line interrupted across
// refreshes must be replayed exactly once after it becomes valid JSON.
func TestScanAppendsAndReplaysPartialJSONL(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "append.jsonl")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	first := assistant("one", "claude-sonnet-4-5", now.Add(-time.Hour), 10, 5, 0, 0)
	second := assistant("two", "claude-sonnet-4-5", now.Add(-time.Minute), 20, 5, 0, 0)
	writeTranscript(t, path, first)
	s := &Scanner{Root: root}
	assertTokens := func(want int64) {
		t.Helper()
		days, err := s.Scan(context.Background(), now, nil)
		if err != nil || len(days) != 1 || days[0].Tokens != want {
			t.Fatalf("tokens: days=%+v err=%v, want %d", days, err, want)
		}
	}
	assertTokens(15)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil { t.Fatal(err) }
	half := len(second)/2
	if _, err := f.WriteString(second[:half]); err != nil { t.Fatal(err) }
	if err := f.Close(); err != nil { t.Fatal(err) }
	assertTokens(15)
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil { t.Fatal(err) }
	if _, err := f.WriteString(second[half:]+"\n"); err != nil { t.Fatal(err) }
	if err := f.Close(); err != nil { t.Fatal(err) }
	assertTokens(40)
	assertTokens(40)
	if n := len(s.cache[path].turns); n != 2 { t.Fatalf("cached turns = %d, want 2", n) }
}

func BenchmarkScanAppendedTranscript(b *testing.B) {
	root := b.TempDir()
	path := filepath.Join(root, "project", "append.jsonl")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	var body strings.Builder
	for i := 0; i < 4000; i++ {
		body.WriteString(assistant(fmt.Sprintf("seed_%d", i), "claude-sonnet-4-5", now.Add(-time.Hour), 10, 5, 0, 0))
		body.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { b.Fatal(err) }
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil { b.Fatal(err) }
	s := &Scanner{Root: root}
	if _, err := s.Scan(context.Background(), now, nil); err != nil { b.Fatal(err) }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil { b.Fatal(err) }
		_, err = f.WriteString(assistant(fmt.Sprintf("append_%d", i), "claude-sonnet-4-5", now.Add(-time.Minute), 10, 5, 0, 0) + "\n")
		closeErr := f.Close()
		if err != nil || closeErr != nil { b.Fatalf("write=%v close=%v", err, closeErr) }
		if _, err := s.Scan(context.Background(), now, nil); err != nil { b.Fatal(err) }
	}
}

func TestReadTranscriptRangeHandlesLongLinesWithoutUnboundedBuffer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "p", "large.jsonl")
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	longModel := strings.Repeat("x", 130*1024)
	writeTranscript(t, path, assistant("long", longModel, now.Add(-time.Hour), 10, 5, 0, 0))
	turns, end, err := readTranscriptRange(context.Background(), path, 0)
	if err != nil || len(turns) != 1 || turns[0].Model != longModel {
		t.Fatalf("fragmented line lost: len=%d err=%v", len(turns), err)
	}
	info, err := os.Stat(path)
	if err != nil || end != info.Size() {
		t.Fatalf("last complete offset = %d, want %d (stat error: %v)", end, info.Size(), err)
	}
}
