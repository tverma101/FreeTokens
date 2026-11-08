// Package codex reads measured token usage from Codex's local session rollouts under
// ~/.codex/sessions, which both the Codex CLI and the Codex app write. The rollouts also hold
// prompts and tool payloads; this scanner keeps only per-response token counters, the model name
// and the response timestamp, never text.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"
)

// Source is the origin label Codex usage is reported under. It stays plain "Codex" because the
// rollouts cannot tell the CLI from the app, and both are the same ChatGPT account.
const Source = "Codex"

// DefaultRoot returns Codex's session directory, or "" when there is no home directory.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// Turn is one measured model response.
type Turn struct {
	At         time.Time
	ResponseID string
	Model      string
	Input      int64
	Cached     int64
	CacheWrite int64
	Output     int64
}

// Scanner caches parsed rollouts and re-reads only files that changed since the last scan.
type Scanner struct {
	Root string

	mu    sync.Mutex
	cache map[string]*cachedFile
	// unreadable is how many rollouts the last scan could not read, so a caller can say that its
	// totals are missing their usage instead of only writing a log line.
	unreadable int
}

type cachedFile struct {
	size     int64
	modified time.Time
	turns    []Turn
}

// NewScanner returns a scanner over Codex's default session directory.
func NewScanner() *Scanner { return &Scanner{Root: DefaultRoot()} }

// Transcripts lists the rollout files that exist under root.
func Transcripts(root string) []string {
	if root == "" {
		return nil
	}
	var out []string
	walkRollouts(root, func(path string) bool {
		out = append(out, path)
		return true
	})
	return out
}

func walkRollouts(root string, visit func(string) bool) {
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			return nil
		}
		if !visit(path) {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("codex history %s: %v", root, err)
	}
}

// Scan returns measured Codex days. A missing session directory is not an error.
func (s *Scanner) Scan(ctx context.Context, now time.Time, p *pricing.Snapshot) ([]usage.Day, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Root == "" {
		s.Root = DefaultRoot()
	}
	if s.cache == nil {
		s.cache = map[string]*cachedFile{}
	}
	if s.Root == "" {
		return nil, nil
	}

	present := map[string]bool{}
	var turns []Turn
	unreadable := 0
	cutoff := usage.HistoryCutoff(now)
	walkRollouts(s.Root, func(path string) bool {
		if ctx.Err() != nil {
			return false
		}
		present[path] = true
		st, err := os.Stat(path)
		if err != nil {
			delete(s.cache, path)
			return true
		}
		if c := s.cache[path]; c != nil && c.size == st.Size() && c.modified.Equal(st.ModTime()) {
			// Rollouts are re-read only when they change, so the retained turns are pruned here to
			// keep the cache bounded as days pass.
			c.turns = usage.KeepSince(c.turns, cutoff, turnTime)
			turns = append(turns, c.turns...)
			return true
		}
		parsed, err := readRollout(ctx, path)
		if err != nil {
			unreadable++
			delete(s.cache, path)
			return true
		}
		parsed = usage.KeepSince(parsed, cutoff, turnTime)
		turns = append(turns, parsed...)
		s.cache[path] = &cachedFile{size: st.Size(), modified: st.ModTime(), turns: parsed}
		return true
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for path := range s.cache {
		if !present[path] {
			delete(s.cache, path)
		}
	}
	s.unreadable = unreadable
	if unreadable > 0 {
		log.Printf("codex: %d session rollout(s) could not be read", unreadable)
	}
	return Fold(dedupe(turns), now, p), nil
}

// Unreadable reports how many session rollouts the last scan could not read.
func (s *Scanner) Unreadable() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreadable
}

func turnTime(t Turn) time.Time { return t.At }

func readRollout(ctx context.Context, path string) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// token_usage_record lines carry one usage per API response and name their turn, so the model
	// is resolved after the whole file is read. Older sessions only report the running total per
	// turn, and their per-turn deltas are used instead.
	var records, deltas []Turn
	var recordTurnIDs []string
	modelByTurn := map[string]string{}
	var activeModel string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var entry rolloutLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		at, hasTime := parseTime(entry.Timestamp)
		switch entry.Type {
		case "turn_context":
			if entry.Payload.Model != "" {
				activeModel = entry.Payload.Model
				if entry.Payload.TurnID != "" {
					modelByTurn[entry.Payload.TurnID] = entry.Payload.Model
				}
			}
		case "token_usage_record":
			if !hasTime || !valid(entry.Payload.Usage) {
				continue
			}
			if total, ok := tokens(entry.Payload.Usage); !ok || total <= 0 {
				continue
			}
			model := modelByTurn[entry.Payload.TurnID]
			if model == "" {
				// Older turn_context records can omit turn_id; attribute the receipt to
				// the model active *at this response*, never the last model in the file.
				model = activeModel
			}
			records = append(records, Turn{
				At: at, ResponseID: entry.Payload.ResponseID, Model: model,
				Input: entry.Payload.Usage.Input, Cached: entry.Payload.Usage.Cached,
				CacheWrite: entry.Payload.Usage.CacheWrite, Output: entry.Payload.Usage.Output,
			})
			recordTurnIDs = append(recordTurnIDs, entry.Payload.TurnID)
		case "event_msg":
			if entry.Payload.Type != "token_count" || !hasTime {
				continue
			}
			d := entry.Payload.Info.Last
			if !valid(d) {
				continue
			}
			if total, ok := tokens(d); !ok || total <= 0 {
				continue
			}
			deltas = append(deltas, Turn{
				At: at, Model: activeModel,
				Input: d.Input, Cached: d.Cached, CacheWrite: d.CacheWrite, Output: d.Output,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return deltas, nil
	}
	for i, turnID := range recordTurnIDs {
		// If a matching turn context was persisted after the receipt, its explicit
		// turn ID is stronger evidence than the current active-model fallback.
		if model := modelByTurn[turnID]; model != "" {
			records[i].Model = model
		}
	}
	// Without either a turn-scoped context or an active model at response time,
	// leave the model unknown rather than attributing it to an unrelated later turn.
	return records, nil
}

type rolloutLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string    `json:"type"`
		ResponseID string    `json:"response_id"`
		TurnID     string    `json:"turn_id"`
		Model      string    `json:"model"`
		Usage      usageJSON `json:"usage"`
		Info       struct {
			Last usageJSON `json:"last_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

type usageJSON struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
}

func valid(u usageJSON) bool {
	return u.Input >= 0 && u.Cached >= 0 && u.CacheWrite >= 0 && u.Output >= 0
}

func parseTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at, true
	}
	return time.Time{}, false
}

// dedupe keeps one turn per response. A resumed thread can repeat a response in a later rollout.
func dedupe(turns []Turn) []Turn {
	seen := map[string]bool{}
	out := turns[:0]
	for _, t := range turns {
		if t.ResponseID != "" {
			if seen[t.ResponseID] {
				continue
			}
			seen[t.ResponseID] = true
		}
		out = append(out, t)
	}
	return out
}

// Fold turns measured responses into the shared daily model rows.
func Fold(turns []Turn, now time.Time, p *pricing.Snapshot) []usage.Day {
	acc := usage.Accumulator{Vendor: p.Vendor}
	for _, t := range turns {
		if t.At.IsZero() || t.At.After(now) {
			continue
		}
		total, ok := totalTokens(t)
		if !ok || total <= 0 {
			continue
		}
		name := Source + " · " + displayModel(t.Model)
		day := usage.DayKey(t.At)
		var cost *float64
		if p != nil && t.Model != "" {
			if rates, ok := p.Resolve(t.Model); ok {
				uncached := t.Input - t.Cached - t.CacheWrite
				if uncached < 0 {
					uncached = 0
				}
				c := rates.Cost(pricing.Tokens{
					Input: uncached, CacheRead: t.Cached, CacheWrite: t.CacheWrite, Output: t.Output,
				}, t.At)
				cost = &c
			}
		}
		acc.Add(day, total, cost, name, "")
		if cost == nil {
			acc.AddNamedUnpriced(day, name)
		}
	}
	return acc.Build()
}

// tokens is Codex's own total: input already includes the cached tokens, output already includes
// reasoning tokens.
func tokens(u usageJSON) (int64, bool) {
	if u.Input < 0 || u.Output < 0 || u.Input > math.MaxInt64-u.Output {
		return 0, false
	}
	return u.Input + u.Output, true
}

func totalTokens(t Turn) (int64, bool) {
	return tokens(usageJSON{Input: t.Input, Output: t.Output})
}

func displayModel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Unknown model"
	}
	if strings.ContainsAny(name, `/\`) {
		name = filepath.Base(name)
	}
	parts := strings.Split(name, "-")
	for i, part := range parts {
		switch strings.ToLower(part) {
		case "":
		case "gpt":
			parts[i] = "GPT"
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
