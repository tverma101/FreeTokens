// Package opencode reads measured token usage from OpenCode's local SQLite session database. The
// database also holds prompts, tool output and diffs; this scanner keeps only per-message token
// counters, the provider and model ids and the message timestamp, never text.
package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"

	_ "modernc.org/sqlite"
)

// Source is the origin label OpenCode usage is reported under. Like Claude Code and Codex, OpenCode
// can be pointed at any provider's models, so the tool is the source.
const Source = "OpenCode"

// Zen is the vendor label for OpenCode's own models. Its Zen gateway serves them and no public
// catalog lists them, so the gateway is the only vendor a row can name.
const Zen = "OpenCode Zen"

// DatabasePath returns OpenCode's session database, or "" when there is none. OpenCode follows XDG
// on Unix, and older Windows builds still write under the home directory.
func DatabasePath() string {
	var candidates []string
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		candidates = append(candidates, filepath.Join(dir, "opencode", "opencode.db"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "share", "opencode", "opencode.db"))
	}
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "opencode", "opencode.db"))
	}
	for _, path := range candidates {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path
		}
	}
	return ""
}

// Turn is one measured assistant message.
type Turn struct {
	At         time.Time
	ProviderID string
	ModelID    string
	Input      int64
	Output     int64
	Reasoning  int64
	CacheRead  int64
	CacheWrite int64
}

// Scanner caches the decoded messages and re-reads only when the database or its WAL changes.
type Scanner struct {
	Path string

	mu    sync.Mutex
	cache *cachedScan
}

type cachedScan struct {
	rev   revision
	turns []Turn
}

type revision struct {
	size     int64
	modified time.Time
}

// NewScanner returns a scanner over OpenCode's default database.
func NewScanner() *Scanner { return &Scanner{Path: DatabasePath()} }

// Scan returns measured OpenCode days. A missing database is not an error.
func (s *Scanner) Scan(ctx context.Context, now time.Time, p *pricing.Snapshot) ([]usage.Day, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Path == "" {
		s.Path = DatabasePath()
	}
	if s.Path == "" {
		return nil, nil
	}
	before := revisionOf(s.Path)
	if before.size < 0 {
		return nil, nil
	}
	cutoff := usage.HistoryCutoff(now)
	if s.cache != nil && s.cache.rev == before {
		// The decoded messages are reused until the database changes, so the retained turns are
		// pruned here to keep the cache bounded as days pass.
		s.cache.turns = usage.KeepSince(s.cache.turns, cutoff, turnTime)
		return Fold(s.cache.turns, now, p), nil
	}
	turns, err := readMessages(ctx, s.Path, cutoff)
	if err != nil {
		return nil, err
	}
	turns = usage.KeepSince(turns, cutoff, turnTime)
	if revisionOf(s.Path) == before {
		s.cache = &cachedScan{rev: before, turns: turns}
	}
	return Fold(turns, now, p), nil
}

// revisionOf describes the database together with its write-ahead log, so a message committed
// while OpenCode has the file open still invalidates the cache.
func revisionOf(path string) revision {
	st, err := os.Stat(path)
	if err != nil {
		return revision{size: -1}
	}
	rev := revision{size: st.Size(), modified: st.ModTime()}
	if wal, err := os.Stat(path + "-wal"); err == nil {
		rev.size += wal.Size()
		if wal.ModTime().After(rev.modified) {
			rev.modified = wal.ModTime()
		}
	}
	return rev
}

func turnTime(t Turn) time.Time { return t.At }

func sqliteURI(path, mode string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String() + "?mode=" + mode + "&_pragma=busy_timeout(1000)&_pragma=query_only(1)"
}

// readMessages opens the database read-only, falling back to a writable handle only when a running
// OpenCode has left it in a state SQLite refuses to open read-only. Nothing is ever written.
func readMessages(ctx context.Context, path string, cutoff time.Time) ([]Turn, error) {
	var lastErr error
	for _, mode := range []string{"ro", "rw"} {
		turns, err := queryMessages(ctx, path, mode, cutoff)
		if err == nil {
			return turns, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "unable to open") && !strings.Contains(msg, "(14)") &&
			!strings.Contains(msg, "database is locked") && !strings.Contains(msg, "(5)") {
			break
		}
	}
	return nil, lastErr
}

// messageTables lists the storage OpenCode has used for one conversation message. Sessions from
// before its v2 session schema live in message; newer ones live in session_message, which keeps the
// message kind in its own column instead of inside the JSON. Both are read for compatibility.
//
// Filter before retrieving JSON payloads, not after decoding them. OpenCode's newer message table
// has a time_created index, so a long-running user only pays to read recent receipts.
// The <= 0 branch preserves legacy rows whose timestamp lives inside the JSON instead of the
// time_created column (parseMessage already supports that fallback).
var messageTables = []struct{ query string }{
	{`SELECT id, data, time_created, '' FROM message WHERE time_created >= ? OR time_created <= 0`},
	{`SELECT id, data, time_created, type FROM session_message WHERE time_created >= ? OR time_created <= 0`},
}

func queryMessages(ctx context.Context, path, mode string, cutoff time.Time) ([]Turn, error) {
	db, err := sql.Open("sqlite", sqliteURI(path, mode))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var turns []Turn
	seen := map[string]bool{}
	foundTable := false
	for _, table := range messageTables {
		got, err := queryTable(ctx, db, table.query, cutoff.UnixMilli(), seen)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no such table") {
				continue
			}
			return nil, err
		}
		foundTable = true
		turns = append(turns, got...)
	}
	if !foundTable {
		return nil, errors.New("OpenCode history schema has no supported message table")
	}
	return turns, nil
}

func queryTable(ctx context.Context, db *sql.DB, query string, cutoffMillis int64, seen map[string]bool) ([]Turn, error) {
	rows, err := db.QueryContext(ctx, query, cutoffMillis)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []Turn
	for rows.Next() {
		var id string
		var data []byte
		var created int64
		var kind string
		if err := rows.Scan(&id, &data, &created, &kind); err != nil {
			return nil, err
		}
		// A migration can leave a receipt in both legacy and new tables. Message IDs are
		// stable across the migration, so count each receipt once rather than both copies.
		if seen[id] && id != "" {
			continue
		}
		if turn, ok := parseMessage(data, created, kind); ok {
			turns = append(turns, turn)
			if id != "" {
				seen[id] = true
			}
		}
	}
	return turns, rows.Err()
}

// message is one stored message in either schema: the older one names the message role and model at
// the top level, the v2 one nests the model under model.
type message struct {
	Role       string `json:"role"`
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Model      struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	} `json:"model"`
	Time struct {
		Created int64 `json:"created"`
	} `json:"time"`
	Tokens struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// parseMessage keeps assistant messages that carry a token receipt.
func parseMessage(data []byte, created int64, kind string) (Turn, bool) {
	var m message
	if err := json.Unmarshal(data, &m); err != nil {
		return Turn{}, false
	}
	role := m.Role
	if role == "" {
		role = kind
	}
	if role != "assistant" {
		return Turn{}, false
	}
	providerID, modelID := m.ProviderID, m.ModelID
	if modelID == "" {
		providerID, modelID = m.Model.ProviderID, m.Model.ID
	}
	ms := created
	if ms <= 0 {
		ms = m.Time.Created
	}
	if ms <= 0 {
		return Turn{}, false
	}
	t := Turn{
		At: time.UnixMilli(ms), ProviderID: providerID, ModelID: modelID,
		Input: m.Tokens.Input, Output: m.Tokens.Output, Reasoning: m.Tokens.Reasoning,
		CacheRead: m.Tokens.Cache.Read, CacheWrite: m.Tokens.Cache.Write,
	}
	if total, ok := totalTokens(t); !ok || total <= 0 {
		return Turn{}, false
	}
	return t, true
}

// Fold turns measured messages into the shared daily model rows.
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
		name := Source + " · " + displayModel(t.ModelID)
		day := usage.DayKey(t.At)
		// OpenCode records the provider per message, which is measured rather than inferred. Its own
		// id names the tool, so it only stands in for models nothing else attributes.
		provider := usage.ProviderHint(t.ProviderID)
		var cost *float64
		if p != nil {
			if route := priceRoute(t.ModelID); route != "" {
				if rates, ok := p.Resolve(route); ok {
					c := rates.Cost(pricing.Tokens{
						Input: t.Input, CacheRead: t.CacheRead, CacheWrite: t.CacheWrite,
						Output: t.Output + t.Reasoning,
					}, t.At)
					cost = &c
				}
			}
		}
		switch {
		case provider != "":
			acc.AddServed(day, total, cost, name, "", provider)
		case ownProvider(t.ProviderID):
			acc.AddOwned(day, total, cost, name, "", Zen)
		default:
			acc.Add(day, total, cost, name, "")
		}
		if cost == nil {
			acc.AddNamedUnpriced(day, name)
		}
	}
	return acc.Build()
}

// ownProvider reports whether a provider id names OpenCode itself. Its Zen models exist in no public
// catalog, so the tool is the only vendor a row can name.
func ownProvider(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "opencode", "opencode-zen":
		return true
	}
	return false
}

// totalTokens is OpenCode's own total: reasoning is reported alongside output rather than inside
// it, and input excludes cache reads.
func totalTokens(t Turn) (int64, bool) {
	var total int64
	for _, v := range []int64{t.Input, t.Output, t.Reasoning, t.CacheRead, t.CacheWrite} {
		if v < 0 || v > math.MaxInt64-total {
			return 0, false
		}
		total += v
	}
	return total, true
}

// priceRoute maps a models.dev id to a public pricing key, dropping a build-date suffix.
func priceRoute(id string) string {
	route := strings.ToLower(strings.TrimSpace(id))
	if route == "" {
		return ""
	}
	if n := len(route); n > 9 && route[n-9] == '-' && digits(route[n-8:]) {
		route = route[:n-9]
	}
	return route
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func displayModel(id string) string {
	name := strings.TrimSpace(id)
	if name == "" {
		return "Unknown model"
	}
	if strings.ContainsAny(name, `/\`) {
		name = filepath.Base(name)
	}
	var words, version []string
	flush := func() {
		if len(version) > 0 {
			words = append(words, strings.Join(version, "."))
			version = nil
		}
	}
	for _, part := range strings.Split(name, "-") {
		switch {
		case part == "":
		case digits(part):
			version = append(version, part)
		default:
			flush()
			if strings.EqualFold(part, "gpt") {
				words = append(words, "GPT")
				continue
			}
			words = append(words, strings.ToUpper(part[:1])+part[1:])
		}
	}
	flush()
	return strings.Join(words, " ")
}
