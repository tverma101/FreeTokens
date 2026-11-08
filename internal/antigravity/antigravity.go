// Package antigravity reads measured token usage from local Antigravity stores. The stores also
// hold prompts and tool payloads; this scanner keeps only per-generation counters, model identity
// and the step timestamp, never text.
package antigravity

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"

	_ "modernc.org/sqlite"
)

// Root is one Antigravity data directory and the name shown for its usage.
type Root struct {
	Dir    string
	Source string
}

// DefaultRoots returns the app, IDE and Agy CLI stores under the user's home directory.
func DefaultRoots() []Root {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []Root{
		{Dir: filepath.Join(home, ".gemini", "antigravity"), Source: "Antigravity"},
		{Dir: filepath.Join(home, ".gemini", "antigravity-ide"), Source: "Antigravity IDE"},
		{Dir: filepath.Join(home, ".gemini", "antigravity-cli"), Source: "Agy CLI"},
	}
}

// Turn is one measured model generation.
type Turn struct {
	At             time.Time
	Source         string
	ModelID, Model string
	Input, Output  int64
	CacheRead      int64
	placement      placementKind // retained alongside cached turns so uncertainty survives refresh
}

type placementKind uint8

const (
	placementExact placementKind = iota
	placementApproximate
	placementReused
)

type usage3 struct{ input, output, cache int64 }

type genRow struct {
	modelID, model string
	usage3
}

// pairing counts how generations were placed on the timeline. Steps carry the only clock in the
// store, so a generation is dated by the step whose token counters match it; the other counters are
// the cases where that proof is unavailable and a neighbouring time is used instead.
type pairing struct {
	matched     int
	approximate int // no step's counters matched: dated at the next step's time
	reused      int // no step left: dated at the previous generation's time
}

func (p pairing) total() int { return p.matched + p.approximate + p.reused }

type stepRow struct {
	at time.Time
	usage3
}

// Scanner caches parsed conversations and re-reads only when the database or its WAL changes.
type Scanner struct {
	Roots []Root

	mu    sync.Mutex
	cache map[string]*cachedConversation
	// unreadable and paired describe the last scan, so the caller can report what its totals are
	// missing or where its per-day split is approximate.
	unreadable int
	paired     pairing
}

type cachedConversation struct {
	rev   []revision
	turns []Turn
}

type revision struct {
	size     int64
	modified time.Time
}

// NewScanner returns a scanner over the default Antigravity stores.
func NewScanner() *Scanner { return &Scanner{Roots: DefaultRoots()} }

// Databases lists conversation databases that exist, deduplicated by conversation ID.
func Databases(roots []Root) []string {
	seen := map[string]bool{}
	var out []string
	for _, root := range roots {
		files, _ := conversationFiles(root.Dir)
		for _, path := range files {
			id := conversationID(path)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, path)
		}
	}
	return out
}

func conversationID(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func conversationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "conversations"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".db") {
			continue
		}
		out = append(out, filepath.Join(dir, "conversations", entry.Name()))
	}
	return out, nil
}

// Scan returns measured Antigravity days. Missing stores are not an error.
func (s *Scanner) Scan(ctx context.Context, now time.Time, p *pricing.Snapshot) ([]usage.Day, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Roots) == 0 {
		s.Roots = DefaultRoots()
	}
	if s.cache == nil {
		s.cache = map[string]*cachedConversation{}
	}

	seenConversation := map[string]bool{}
	present := map[string]bool{}
	var turns []Turn
	var stats pairing
	unreadable := 0
	cutoff := usage.HistoryCutoff(now)
	for _, root := range s.Roots {
		files, err := conversationFiles(root.Dir)
		if err != nil {
			log.Printf("antigravity history %s: %v", root.Source, err)
			continue
		}
		for _, path := range files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			present[path] = true
			id := conversationID(path)
			if seenConversation[id] {
				continue
			}

			before := revisions(path)
			if c := s.cache[path]; c != nil && sameRevision(c.rev, before) {
				// Conversations are re-read only when they change, so the retained turns are pruned
				// here to keep the cache bounded as days pass.
				c.turns = usage.KeepSince(c.turns, cutoff, turnTime)
				turns = append(turns, c.turns...)
				seenConversation[id] = true
				continue
			}
			parsed, err := readConversation(ctx, path, root.Source, &stats)
			if err != nil {
				unreadable++
				delete(s.cache, path)
				continue
			}
			parsed = usage.KeepSince(parsed, cutoff, turnTime)
			turns = append(turns, parsed...)
			seenConversation[id] = true
			if sameRevision(revisions(path), before) {
				s.cache[path] = &cachedConversation{rev: before, turns: parsed}
			}
		}
	}
	for path := range s.cache {
		if !present[path] {
			delete(s.cache, path)
		}
	}
	// The fresh reader's counters do not include cache hits. Recompute from retained turns so
	// cached scans and the rolling history cutoff report the same attribution quality.
	stats = pairing{}
	for _, t := range turns {
		switch t.placement {
		case placementApproximate:
			stats.approximate++
		case placementReused:
			stats.reused++
		default:
			stats.matched++
		}
	}
	s.unreadable, s.paired = unreadable, stats
	if unreadable > 0 {
		log.Printf("antigravity: %d conversation database(s) could not be read", unreadable)
	}
	if stats.approximate+stats.reused > 0 {
		log.Printf("antigravity: %d of %d generation(s) had no matching step timestamp (%d dated at the next step, %d at the previous generation)",
			stats.approximate+stats.reused, stats.total(), stats.approximate, stats.reused)
	}
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].At.Before(turns[j].At) })
	return Fold(turns, now, p), nil
}

// Fold turns measured generations into the shared daily model rows.
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
		name := sourceModel(t)
		day := usage.DayKey(t.At)
		var cost *float64
		if p != nil {
			if route := priceRoute(t); route != "" {
				if rates, ok := p.Resolve(route); ok {
					c := rates.Cost(pricing.Tokens{Input: t.Input, CacheRead: t.CacheRead, Output: t.Output}, t.At)
					cost = &c
				}
			}
		}
		acc.Add(day, total, cost, name, "")
		if cost == nil {
			acc.AddNamedUnpriced(day, name)
		}
	}
	return acc.Build()
}

func totalTokens(t Turn) (int64, bool) {
	if t.Input < 0 || t.Output < 0 || t.CacheRead < 0 {
		return 0, false
	}
	total := t.Input
	for _, v := range []int64{t.Output, t.CacheRead} {
		if v > math.MaxInt64-total {
			return 0, false
		}
		total += v
	}
	return total, true
}

func sourceModel(t Turn) string {
	source := strings.TrimSpace(t.Source)
	if source == "" {
		source = "Antigravity"
	}
	model := strings.TrimSpace(t.Model)
	if model == "" {
		model = "Unknown model"
	}
	return source + " · " + model
}

func priceRoute(t Turn) string {
	id := strings.ToLower(strings.TrimSpace(t.ModelID))
	switch {
	case strings.HasPrefix(id, "gemini-3.8-flash"):
		return "gemini-3.8-flash"
	case strings.HasPrefix(id, "claude-sonnet-4-6"):
		return "claude-sonnet-4-6"
	case strings.HasPrefix(id, "claude-opus-4-6"):
		return "claude-opus-4-6"
	}
	name := strings.TrimSpace(t.Model)
	for _, suffix := range []string{" (High)", " (Medium)", " (Low)", " (Thinking)"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return name
}

func revisions(path string) []revision {
	var out []revision
	for _, p := range []string{path, path + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			out = append(out, revision{st.Size(), st.ModTime()})
		} else {
			out = append(out, revision{-1, time.Time{}})
		}
	}
	return out
}

func sameRevision(a, b []revision) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].size != b[i].size || !a[i].modified.Equal(b[i].modified) {
			return false
		}
	}
	return true
}

// Unreadable reports how many conversation databases the last scan could not read.
func (s *Scanner) Unreadable() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreadable
}

// Placement reports how the last scan dated its generations: how many had no step with matching
// token counts and were placed at the next step, how many reused a neighbour's time because no step
// was left, and how many generations it saw. Only the rest carry a measured timestamp.
func (s *Scanner) Placement() (approximate, reused, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paired.approximate, s.paired.reused, s.paired.total()
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

func readConversation(ctx context.Context, path, source string, stats *pairing) ([]Turn, error) {
	var lastErr error
	for _, mode := range []string{"ro", "rw"} {
		turns, err := queryConversation(ctx, path, source, mode, stats)
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

func queryConversation(ctx context.Context, path, source, mode string, stats *pairing) ([]Turn, error) {
	db, err := sql.Open("sqlite", sqliteURI(path, mode))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	steps, err := readSteps(ctx, db)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT data FROM gen_metadata ORDER BY idx`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []Turn
	nextStep := 0
	var lastAt time.Time
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		g, ok := parseGeneration(data)
		if !ok {
			continue
		}
		if total, ok := totalTokens(Turn{Input: g.input, Output: g.output, CacheRead: g.cache}); !ok || total <= 0 {
			continue
		}
		// A step is claimed only when its counters are exactly this generation's; that is the one
		// association the store actually records. Generations whose counters match nothing are dated
		// at the next step without consuming it, so an adjacent generation that does match still gets
		// its own measured time instead of being shifted.
		match := -1
		for k := nextStep; k < len(steps) && k < nextStep+128; k++ {
			if steps[k].usage3 == g.usage3 {
				match = k
				break
			}
		}
		at := lastAt
		placement := placementReused
		switch {
		case match >= 0:
			at = steps[match].at
			nextStep = match + 1
			placement = placementExact
			stats.matched++
		case nextStep < len(steps):
			at = steps[nextStep].at
			placement = placementApproximate
			stats.approximate++
		default:
			stats.reused++
		}
		if at.IsZero() {
			continue
		}
		lastAt = at
		turns = append(turns, Turn{
			At: at, Source: source,
			ModelID: g.modelID, Model: displayModel(g),
			Input: g.input, Output: g.output, CacheRead: g.cache, placement: placement,
		})
	}
	return turns, rows.Err()
}

func readSteps(ctx context.Context, db *sql.DB) ([]stepRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT metadata FROM steps WHERE step_type = 15 ORDER BY idx`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stepRow
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		at, u, ok := parseStep(data)
		if !ok {
			continue
		}
		out = append(out, stepRow{at: at, usage3: u})
	}
	return out, rows.Err()
}

func parseGeneration(data []byte) (genRow, bool) {
	top, ok := bytesField(data, 1)
	if !ok {
		return genRow{}, false
	}
	raw, ok := bytesField(top, 4)
	if !ok {
		return genRow{}, false
	}
	input, ok := tokenField(raw, 2)
	if !ok {
		return genRow{}, false
	}
	output, ok := tokenField(raw, 3)
	if !ok {
		return genRow{}, false
	}
	cache, ok := tokenField(raw, 5)
	if !ok {
		return genRow{}, false
	}
	return genRow{
		modelID: textField(top, 19),
		model:   textField(top, 21),
		usage3:  usage3{input, output, cache},
	}, true
}

func parseStep(data []byte) (time.Time, usage3, bool) {
	raw, ok := bytesField(data, 1)
	if !ok {
		return time.Time{}, usage3{}, false
	}
	sec, ok := varintField(raw, 1)
	if !ok || sec < 946684800 || sec > 4102444800 {
		return time.Time{}, usage3{}, false
	}
	nsec, _ := varintField(raw, 2)
	if nsec >= 1e9 {
		return time.Time{}, usage3{}, false
	}
	var u usage3
	if rawUsage, ok := bytesField(data, 9); ok {
		var valid bool
		if u.input, valid = tokenField(rawUsage, 2); !valid {
			return time.Time{}, usage3{}, false
		}
		if u.output, valid = tokenField(rawUsage, 3); !valid {
			return time.Time{}, usage3{}, false
		}
		if u.cache, valid = tokenField(rawUsage, 5); !valid {
			return time.Time{}, usage3{}, false
		}
	}
	return time.Unix(int64(sec), int64(nsec)), u, true
}

func tokenField(data []byte, num int) (int64, bool) {
	v, ok := varintField(data, num)
	if !ok {
		return 0, true
	}
	if v > math.MaxInt64 {
		return 0, false
	}
	return int64(v), true
}

func textField(data []byte, num int) string {
	raw, ok := bytesField(data, num)
	if !ok || len(raw) == 0 || len(raw) > 512 || !utf8.Valid(raw) {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func displayModel(g genRow) string {
	if g.model != "" {
		return g.model
	}
	id := strings.ToLower(g.modelID)
	switch {
	case strings.HasPrefix(id, "gemini-3.8-flash"):
		switch {
		case strings.HasSuffix(id, "-high"):
			return "Gemini 3.8 Flash (High)"
		case strings.HasSuffix(id, "-medium"):
			return "Gemini 3.8 Flash (Medium)"
		case strings.HasSuffix(id, "-low"):
			return "Gemini 3.8 Flash (Low)"
		}
		return "Gemini 3.8 Flash"
	case id == "claude-opus-4-6-thinking":
		return "Claude Opus 4.6 (Thinking)"
	case strings.HasPrefix(id, "claude-opus-4-6"):
		return "Claude Opus 4.6"
	case strings.HasPrefix(id, "claude-sonnet-4-6"):
		return "Claude Sonnet 4.6"
	case id == "":
		return "Unknown model"
	}
	parts := strings.Split(strings.ReplaceAll(id, "-", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
