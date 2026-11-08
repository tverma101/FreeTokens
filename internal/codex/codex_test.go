package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
)

func writeRollout(t *testing.T, path string, lines ...string) {
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

func turnContext(at time.Time, turnID, model string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"turn_context","payload":{"turn_id":%q,"model":%q}}`,
		at.Format(time.RFC3339Nano), turnID, model)
}

func record(at time.Time, responseID, turnID string, input, cached, cacheWrite, output int) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"token_usage_record","payload":{"response_id":%q,"turn_id":%q,`+
		`"usage":{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":%d,"output_tokens":%d}}}`,
		at.Format(time.RFC3339Nano), responseID, turnID, input, cached, cacheWrite, output)
}

func tokenCount(at time.Time, lastInput, lastOutput, totalInput, totalOutput int) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":`+
		`{"total_token_usage":{"input_tokens":%d,"output_tokens":%d},`+
		`"last_token_usage":{"input_tokens":%d,"output_tokens":%d}}}}`,
		at.Format(time.RFC3339Nano), totalInput, totalOutput, lastInput, lastOutput)
}

func TestScanUsesPerResponseRecords(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	writeRollout(t, filepath.Join(root, "2026", "11", "02", "rollout-a.jsonl"),
		`{"timestamp":"2026-11-02T09:59:00.000Z","type":"session_meta","payload":{"id":"s1"}}`,
		turnContext(at, "t1", "gpt-6-luna"),
		record(at.Add(time.Second), "resp_1", "t1", 20000, 0, 0, 50),
		// The running total repeats the same response, which must not be counted again.
		tokenCount(at.Add(2*time.Second), 20000, 50, 20000, 50),
		record(at.Add(3*time.Second), "resp_2", "t1", 24000, 20000, 0, 200),
	)
	s := &Scanner{Root: root}
	days, err := s.Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	// 20000+50 plus 24000+200; cached input is already inside input.
	if days[0].Tokens != 44250 {
		t.Fatalf("tokens = %d, want 44250", days[0].Tokens)
	}
	if len(days[0].Models) != 1 || days[0].Models[0].Name != "Codex · GPT 6 Luna" {
		t.Fatalf("models = %+v", days[0].Models)
	}
}

func TestScanFallsBackToTokenCountDeltas(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	writeRollout(t, filepath.Join(root, "2026", "11", "02", "rollout-b.jsonl"),
		turnContext(at, "t1", "gpt-5-codex"),
		tokenCount(at.Add(time.Second), 100, 10, 100, 10),
		tokenCount(at.Add(2*time.Second), 50, 5, 150, 15),
	)
	s := &Scanner{Root: root}
	days, err := s.Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Tokens != 165 {
		t.Fatalf("days = %+v", days)
	}
	if days[0].Models[0].Name != "Codex · GPT 5 Codex" {
		t.Fatalf("models = %+v", days[0].Models)
	}
}

func TestDedupeKeepsOneTurnPerResponse(t *testing.T) {
	at := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	turns := dedupe([]Turn{
		{At: at, ResponseID: "resp_1", Input: 10, Output: 1},
		{At: at, ResponseID: "resp_1", Input: 10, Output: 1},
		{At: at, ResponseID: "resp_2", Input: 10, Output: 1},
	})
	if len(turns) != 2 {
		t.Fatalf("turns = %+v", turns)
	}
}

func TestFoldPricesCachedInputSeparately(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	snap := pricing.NewSnapshot(&pricing.Supplement{Pricing: map[string]pricing.Rates{
		"gpt-6-luna": {Input: 2, Output: 10, CacheRead: 0.5, CacheReadExplicit: true},
	}}, nil)
	days := Fold([]Turn{{
		At: at, ResponseID: "resp_1", Model: "gpt-6-luna",
		Input: 1_000_000, Cached: 800_000, Output: 100_000,
	}}, at.Add(time.Hour), snap)
	if len(days) != 1 || days[0].CostUSD == nil {
		t.Fatalf("days = %+v", days)
	}
	// 0.2M at 2 + 0.8M at 0.5 + 0.1M at 10.
	if got := *days[0].CostUSD; got < 1.79 || got > 1.81 {
		t.Fatalf("cost = %v, want 1.8", got)
	}
	if days[0].Tokens != 1_100_000 {
		t.Fatalf("tokens = %d, want 1100000", days[0].Tokens)
	}
}


// Codex's turn_context.turn_id is optional in the persisted rollout protocol.
// A model switch in that format must not re-label earlier responses with the final model.
func TestScanAttributesMissingTurnIDsToModelAtResponseTime(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	writeRollout(t, filepath.Join(root, "2026", "11", "08", "rollout-switch.jsonl"),
		turnContext(at, "", "gpt-5-codex"),
		record(at.Add(time.Second), "resp_1", "t1", 100, 0, 0, 0),
		turnContext(at.Add(2*time.Second), "", "gpt-6-luna"),
		record(at.Add(3*time.Second), "resp_2", "t2", 200, 0, 0, 0),
	)
	days, err := (&Scanner{Root: root}).Scan(context.Background(), at.Add(time.Hour), nil)
	if err != nil || len(days) != 1 || len(days[0].Models) != 2 {
		t.Fatalf("model-switch history: days=%+v err=%v", days, err)
	}
	byName := map[string]int64{}
	for _, m := range days[0].Models {
		byName[m.Name] = m.Tokens
	}
	if byName["Codex · GPT 5 Codex"] != 100 || byName["Codex · GPT 6 Luna"] != 200 {
		t.Fatalf("wrong model attribution: %v", byName)
	}
}

// A response preceding its turn_context can be reattributed by an exact turn ID.
// A response with no matching context must not inherit a later unrelated model.
func TestReadRolloutLateContextAndUnattributedResponse(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "rollout-late-context.jsonl")
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)
	writeRollout(t, path,
		record(at, "resp_early", "t1", 50, 0, 0, 0),
		record(at.Add(time.Second), "resp_unknown", "t2", 70, 0, 0, 0),
		turnContext(at.Add(2*time.Second), "t1", "gpt-5-codex"),
	)
	turns, err := readRollout(context.Background(), path)
	if err != nil || len(turns) != 2 {
		t.Fatalf("rollout turns=%+v err=%v", turns, err)
	}
	if turns[0].Model != "gpt-5-codex" || turns[1].Model != "" {
		t.Fatalf("late context misattributed: turns=%+v", turns)
	}
}
