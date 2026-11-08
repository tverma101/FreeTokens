package freebuff

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"

	_ "modernc.org/sqlite"
)

// Current Desktop can clear threads.model during migration while keeping the selected model in
// world_snapshot. Project just that scalar; never fetch the rest of the thread snapshot.
const selectedModelSQL = `COALESCE(NULLIF(t.model, ''), CASE WHEN json_valid(t.world_snapshot)
  THEN json_extract(t.world_snapshot, '$.model') END, '')`

// Assistant usage plus the separate auto-run manager call. The outcome receipt is excluded because
// it repeats the completed assistant turn's usage. Every column is COALESCEd so one NULL marks one
// unreadable row instead of failing the history.
const messagesAndManagersSQL = `SELECT id, timestamp, metrics, selectedModel FROM (
  SELECT
    COALESCE(CASE WHEN m.request_id LIKE 'cloud:%' THEN m.request_id
      ELSE m.thread_id || ':' || m.seq END, '') AS id,
    COALESCE(CAST(m.ts AS TEXT), '') AS timestamp,
    COALESCE(NULLIF(m.metrics_json, ''), '{}') AS metrics,
    ` + selectedModelSQL + ` AS selectedModel
  FROM messages m JOIN threads t ON t.id = m.thread_id
  WHERE t.harness_id = 'codebuff' AND m.role = 'assistant'
  UNION ALL
  SELECT
    COALESCE('mission-manager:' || r.thread_id || ':' || CAST(r.id AS TEXT), '') AS id,
    COALESCE(CAST(json_extract(r.manager_json, '$.startedAt') AS TEXT), CAST(r.created_at AS TEXT), '') AS timestamp,
    json_object(
      'usage', json(json_set(json_extract(r.manager_json, '$.usage'), '$.model', json_extract(r.manager_json, '$.model'))),
      'usageIncomplete', COALESCE(json_extract(r.manager_json, '$.usageIncomplete'), 0)
    ) || '' AS metrics,
    ` + selectedModelSQL + ` AS selectedModel
  FROM auto_run_decision_receipts r JOIN threads t ON t.id = r.thread_id
  WHERE t.harness_id = 'codebuff'
    AND json_extract(r.manager_json, '$.harnessId') = 'codebuff'
    AND json_type(r.manager_json, '$.usage') = 'object'
)`

// Compatibility query for stores created before mission-manager receipts existed.
const assistantMessagesSQL = `SELECT
  COALESCE(CASE WHEN m.request_id LIKE 'cloud:%' THEN m.request_id ELSE m.thread_id || ':' || m.seq END, ''),
  COALESCE(CAST(m.ts AS TEXT), ''),
  COALESCE(NULLIF(m.metrics_json, ''), '{}'),
  ` + selectedModelSQL + `
FROM messages m JOIN threads t ON t.id = m.thread_id
WHERE t.harness_id = 'codebuff' AND m.role = 'assistant'`

// DesktopRoots lists the per-user directories Freebuff may keep its data in, most likely first.
// The app keeps its data beside its CLI credentials in the XDG config directory on every platform:
// $XDG_CONFIG_HOME when set, otherwise ~/.config. Linux builds call the directory "Freebuff" and
// earlier ones called it "freebuff-desktop". Older FreeTokens releases looked only in
// ~/.config/freebuff-desktop, so a Linux install using XDG_CONFIG_HOME was never read at all.
//
// Some installs keep history in the workspace instead (<project>/.freebuff/desktop-v2.db), which is
// outside every directory above. The app records no readable list of workspaces and walking a home
// directory would be unbounded, so those are opt-in through FREETOKENS_HISTORY_DIRS: a path-list of
// project roots or .freebuff directories. Anything not listed there is not searched, and
// `freetokens diagnose` prints the roots actually used.
func DesktopRoots() []string {
	names := []string{"Freebuff", "freebuff", "freebuff-desktop"}
	home, _ := os.UserHomeDir()
	var roots []string
	// add covers a base directory both by the names a release has used and by anything already on
	// disk whose name starts with "freebuff", so a spelling difference doesn't hide the data.
	add := func(base string) {
		if base == "" {
			return
		}
		for _, name := range names {
			roots = append(roots, filepath.Join(base, name))
		}
		roots = append(roots, freebuffDirsIn(base)...)
	}
	// $XDG_CONFIG_HOME first, then ~/.config: the app follows the XDG spec, and a directory it no
	// longer uses is worth one cheap look. configHome is relative (and skipped) without a home.
	if dir := configHome(home, os.Getenv); filepath.IsAbs(dir) {
		add(dir)
	}
	if home != "" {
		add(filepath.Join(home, ".config"))
	}
	switch runtime.GOOS {
	case "windows":
		add(os.Getenv("APPDATA"))
		add(os.Getenv("LOCALAPPDATA"))
	case "darwin":
		add(filepath.Join(home, "Library", "Application Support"))
	}
	if config, err := os.UserConfigDir(); err == nil {
		add(config)
	}
	if data := os.Getenv("XDG_DATA_HOME"); data != "" && filepath.IsAbs(data) {
		add(data)
	} else {
		add(filepath.Join(home, ".local", "share"))
	}
	roots = append(roots, workspaceRoots()...)
	return uniquePaths(roots)
}

// HistoryDirsEnv names the opt-in list of extra directories searched for Freebuff history.
const HistoryDirsEnv = "FREETOKENS_HISTORY_DIRS"

// workspaceRoots returns the extra roots the user pointed at, in order. Each entry is either a
// project root (its .freebuff directory is searched) or a .freebuff directory itself. Relative
// entries are skipped: the working directory of a login item is not the user's project.
func workspaceRoots() []string {
	var out []string
	for _, entry := range filepath.SplitList(os.Getenv(HistoryDirsEnv)) {
		entry = strings.TrimSpace(entry)
		if entry == "" || !filepath.IsAbs(entry) {
			continue
		}
		if strings.EqualFold(filepath.Base(filepath.Clean(entry)), ".freebuff") {
			out = append(out, filepath.Clean(entry))
			continue
		}
		out = append(out, filepath.Join(filepath.Clean(entry), ".freebuff"))
	}
	return out
}

// freebuffDirsIn lists the directories in base that belong to this app, whatever spelling and
// suffix its release used ("Freebuff", "freebuff-desktop", "Freebuff Desktop").
func freebuffDirsIn(base string) []string {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), "freebuff") {
			out = append(out, filepath.Join(base, e.Name()))
		}
	}
	return out
}

// Electron keeps caches and profile stores beside the app's own files; none of them holds a SQLite
// usage database, and walking them is the expensive part of a search.
var electronDirs = map[string]bool{
	"Cache": true, "Code Cache": true, "DawnCache": true, "DawnGraphiteCache": true,
	"DawnWebGPUCache": true, "GPUCache": true, "Crashpad": true, "Partitions": true,
	"Local Storage": true, "Session Storage": true, "blob_storage": true, "shared_proto_db": true,
	"IndexedDB": true, "Service Worker": true, "CacheStorage": true, "File System": true,
	"Network": true,
}

// maxScanDepth covers both layouts the app has used: <root>/<project>/db and <root>/data/<x>/db.
// The walk is bounded by depth and by the Electron directory skip list instead of by a result cap:
// a cap would silently drop the projects over it and publish totals that look complete.
const maxScanDepth = 3

// DesktopDatabases lists Freebuff's per-project databases under the given roots. Electron's state
// files live in the same tree and share extensions, so a candidate counts only when it carries the
// messages and threads tables the usage projection reads.
func DesktopDatabases(roots []string) ([]string, error) {
	var paths []string
	for _, root := range roots {
		paths = append(paths, discoverDatabases(root)...)
	}
	var out []string
	for _, path := range uniquePaths(paths) {
		if hasUsageSchema(path) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}

// discoverDatabases walks one root a few levels deep for SQLite files.
func discoverDatabases(root string) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxScanDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if e.IsDir() {
				if !electronDirs[e.Name()] {
					walk(path, depth+1)
				}
				continue
			}
			if !isDatabaseName(e.Name()) {
				continue
			}
			if st, err := e.Info(); err == nil && st.Mode().IsRegular() {
				out = append(out, path)
			}
		}
	}
	walk(root, 0)
	return out
}

func isDatabaseName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	}
	return false
}

// hasUsageSchema reports whether path holds the two tables the usage projection reads, so a
// Chromium state file is skipped instead of failing the whole history read.
func hasUsageSchema(path string) bool {
	valid, err := inspectUsageSchema(path)
	// A corrupt/locked known Freebuff database must reach the scanner's coverage warning,
	// not vanish as though it were an unrelated Chromium state file.
	return valid || (err != nil && strings.EqualFold(filepath.Base(path), "desktop-v2.db"))
}

func inspectUsageSchema(path string) (bool, error) {
	var lastErr error
	for _, mode := range []string{"ro", "rw"} {
		db, err := sql.Open("sqlite", sqliteURI(path, mode))
		if err != nil {
			lastErr = err
			continue
		}
		var tables int
		err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('messages', 'threads')`).Scan(&tables)
		_ = db.Close()
		if err == nil {
			return tables == 2, nil
		}
		lastErr = err
	}
	return false, lastErr
}

// uniquePaths drops repeats (Windows' APPDATA and the user config dir are the same directory) and
// keeps the order in which they were found.
func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = filepath.Clean(p)
		key := p
		if runtime.GOOS == "windows" {
			key = strings.ToLower(p)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

type rawRow struct{ id, timestamp, metrics, selectedModel string }

func sqliteURI(path string, mode string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String() + "?mode=" + mode + "&_pragma=busy_timeout(1000)&_pragma=query_only(1)"
}

// readDesktopRows runs the usage projection, retrying only proven missing-schema errors. A WAL
// database under an active writer can refuse a read-only open, so it retries read-write (the
// statements are SELECTs and the file is known to exist).
func readDesktopRows(ctx context.Context, path string) ([]rawRow, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}
	var lastErr error
	for _, mode := range []string{"ro", "rw"} {
		rows, err := queryDesktop(ctx, sqliteURI(path, mode))
		if err == nil {
			return rows, nil
		}
		lastErr = err
		if !strings.Contains(strings.ToLower(err.Error()), "unable to open") && !strings.Contains(err.Error(), "(14)") {
			break
		}
	}
	return nil, lastErr
}

func queryDesktop(ctx context.Context, dsn string) ([]rawRow, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	includeManagers, includeSnapshot := true, true
	for {
		q := assistantMessagesSQL
		if includeManagers {
			q = messagesAndManagersSQL
		}
		if !includeSnapshot {
			q = strings.ReplaceAll(q, selectedModelSQL, "COALESCE(t.model, '')")
		}
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			msg := strings.ToLower(err.Error())
			switch {
			case includeManagers && strings.Contains(msg, "no such table: auto_run_decision_receipts"):
				includeManagers = false
				continue
			case includeSnapshot && strings.Contains(msg, "no such column: t.world_snapshot"):
				includeSnapshot = false
				continue
			}
			return nil, err
		}
		var out []rawRow
		for rows.Next() {
			var id, ts, metrics, model sql.NullString
			if err := rows.Scan(&id, &ts, &metrics, &model); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, rawRow{id.String, ts.String, metrics.String, model.String})
		}
		err = rows.Err()
		rows.Close()
		return out, err
	}
}

// count is absent, a valid non-negative integer, or invalid.
type count struct {
	present, valid bool
	v              int64
}

func countOf(m map[string]any, key string) count {
	raw, ok := m[key]
	if !ok {
		return count{}
	}
	n, ok := raw.(json.Number)
	if !ok {
		return count{present: true}
	}
	f, err := n.Float64()
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) || f < 0 || f >= math.MaxInt64 || f != math.Trunc(f) {
		return count{present: true}
	}
	return count{true, true, int64(f)}
}

type turn struct {
	id                                      string
	timestampMs                             float64
	hasUsage                                bool
	input, output, cached, reasoning, total count
	savedModel                              string
	fromReceipt                             bool
	partial                                 bool
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case json.Number:
		return x.String() == "1"
	}
	return false
}

func deriveTurn(r rawRow) (*turn, error) {
	if r.id == "" {
		return nil, nil
	}
	stamp, err := strconv.ParseFloat(strings.TrimSpace(r.timestamp), 64)
	if err != nil || math.IsInf(stamp, 0) || math.IsNaN(stamp) {
		return nil, nil
	}
	metrics, ok := decodeObject([]byte(r.metrics))
	if !ok {
		return nil, ErrLocalHistory
	}
	u, hasUsage := obj(metrics["usage"])
	recorded := str(u["model"])
	t := &turn{id: r.id, timestampMs: stamp, savedModel: r.selectedModel, hasUsage: hasUsage,
		fromReceipt: recorded != "", partial: !hasUsage || truthy(metrics["usageIncomplete"])}
	if recorded != "" {
		t.savedModel = recorded
	}
	if hasUsage {
		t.input, t.output = countOf(u, "inputTokens"), countOf(u, "outputTokens")
		t.cached, t.reasoning, t.total = countOf(u, "cachedInputTokens"), countOf(u, "reasoningOutputTokens"), countOf(u, "totalTokens")
	}
	return t, nil
}

func hostedTurn(r Receipt) *turn {
	t := &turn{id: r.ID, timestampMs: r.TimestampMs, savedModel: r.Model,
		partial: r.Metrics.Usage == nil || (r.Metrics.UsageIncomplete != nil && *r.Metrics.UsageIncomplete)}
	if u := r.Metrics.Usage; u != nil {
		t.hasUsage = true
		t.input, t.output = count{true, true, u.InputTokens}, count{true, true, u.OutputTokens}
		opt := func(p *int64) count {
			if p == nil {
				return count{}
			}
			return count{true, true, *p}
		}
		t.cached, t.reasoning, t.total = opt(u.CachedInputTokens), opt(u.ReasoningOutputTokens), opt(u.TotalTokens)
	}
	return t
}

type revision struct {
	size     int64
	modified time.Time
}

type cachedDB struct {
	rev        []revision
	turns      []*turn
	unreadable int
}

// DesktopScanner reads Freebuff Desktop's measured usage (never conversation text) and folds it
// with hosted receipts. Unchanged database+WAL revisions reuse the previously derived turns.
type DesktopScanner struct {
	// Roots are the directories searched for the app's databases (see DesktopRoots).
	Roots    []string
	MaxTurns int
	mu       sync.Mutex
	dbs      map[string]*cachedDB
	order    []string
	retained int
	// unreadable counts malformed rows; unreadableDBs counts known Freebuff stores that failed
	// to open. Both are surfaced separately rather than silently undercounting usage.
	unreadable int
	unreadableDBs int
}

// Unreadable reports how many local history rows the last scan skipped for having no identifier
// or timestamp, so a caller can say that its totals are a lower bound.
func (s *DesktopScanner) Unreadable() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreadable
}

// UnreadableDatabases reports known Freebuff history stores skipped due to read errors.
func (s *DesktopScanner) UnreadableDatabases() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreadableDBs
}

// HasHistory reports whether any project database exists.
func (s *DesktopScanner) HasHistory() bool {
	paths, _ := DesktopDatabases(s.Roots)
	return len(paths) > 0
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

// Fold is the measured token history plus coverage flags.
type Fold struct {
	Days []usage.Day `json:"days"`
	// HasSources is false when there is no hosted or local history at all.
	HasSources bool `json:"hasSources"`
}

// Scan reads changed databases and folds every turn. Hosted receipts take precedence over an
// older Desktop copy of the same cloud:<run>.
func (s *DesktopScanner) Scan(ctx context.Context, now time.Time, p *pricing.Snapshot, catalog map[string]CatalogEntry, hosted []Receipt) (*Fold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dbs == nil {
		s.dbs = map[string]*cachedDB{}
	}
	maxTurns := s.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 200_000
	}
	paths, err := DesktopDatabases(s.Roots)
	if err != nil {
		return nil, ErrLocalHistory
	}
	known := map[string]bool{}
	for _, p := range paths {
		known[p] = true
	}
	for p, c := range s.dbs {
		if !known[p] {
			s.retained -= len(c.turns)
			delete(s.dbs, p)
			s.removeOrder(p)
		}
	}
	if len(paths) == 0 && len(hosted) == 0 {
		s.unreadable, s.unreadableDBs = 0, 0
		return &Fold{Days: []usage.Day{}}, nil
	}
	groups := [][]*turn{}
	h := make([]*turn, 0, len(hosted))
	for _, r := range hosted {
		h = append(h, hostedTurn(r))
	}
	groups = append(groups, h)
	unreadable := 0
	unreadableDBs := 0
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		before := revisions(path)
		if c := s.dbs[path]; c != nil && sameRevision(c.rev, before) {
			groups = append(groups, c.turns)
			unreadable += c.unreadable
			continue
		}
		rows, err := readDesktopRows(ctx, path)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			log.Printf("freebuff desktop history %s: %v", filepath.Base(filepath.Dir(path)), err)
			unreadableDBs++
			delete(s.dbs, path)
			s.removeOrder(path)
			continue
		}
		var turns []*turn
		bad := 0
		for _, r := range rows {
			t, err := deriveTurn(r)
			if err != nil {
				return nil, err
			}
			if t == nil {
				bad++
				continue
			}
			turns = append(turns, t)
		}
		groups = append(groups, turns)
		unreadable += bad
		// One query is a consistent snapshot; cache it only if nothing committed meanwhile.
		if sameRevision(revisions(path), before) {
			s.store(path, &cachedDB{before, turns, bad}, maxTurns)
		}
	}
	s.unreadable, s.unreadableDBs = unreadable, unreadableDBs
	if unreadable > 0 {
		log.Printf("freebuff: %d local history row(s) had no identifier or timestamp and were skipped", unreadable)
	}
	return fold(groups, now, p, catalog)
}

func (s *DesktopScanner) removeOrder(path string) {
	for i, p := range s.order {
		if p == path {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

func (s *DesktopScanner) store(path string, c *cachedDB, max int) {
	if old := s.dbs[path]; old != nil {
		s.retained -= len(old.turns)
		delete(s.dbs, path)
		s.removeOrder(path)
	}
	if len(c.turns) > max {
		return
	}
	for s.retained > max-len(c.turns) && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		if d := s.dbs[oldest]; d != nil {
			s.retained -= len(d.turns)
			delete(s.dbs, oldest)
		}
	}
	s.order = append(s.order, path)
	s.dbs[path] = c
	s.retained += len(c.turns)
}

func required(c count) (int64, error) {
	if !c.present || !c.valid {
		return 0, ErrLocalHistory
	}
	return c.v, nil
}

func optional(c count) (int64, error) {
	if !c.present {
		return 0, nil
	}
	if !c.valid {
		return 0, ErrLocalHistory
	}
	return c.v, nil
}

func fold(groups [][]*turn, now time.Time, p *pricing.Snapshot, catalog map[string]CatalogEntry) (*Fold, error) {
	identity := NewIdentity(catalog, p)
	acc := usage.Accumulator{Vendor: p.Vendor}
	seen := map[string]bool{}
	for _, g := range groups {
		for _, t := range g {
			if seen[t.id] {
				continue
			}
			ms := t.timestampMs
			at := time.Unix(0, int64(ms*1e6))
			if at.After(now) {
				continue
			}
			day := usage.DayKey(at)
			if !t.hasUsage {
				if t.partial {
					acc.MarkPartial(day)
				}
				continue
			}
			// A hosted placeholder without usage must not hide a measured older Desktop copy.
			seen[t.id] = true
			input, err := required(t.input)
			if err != nil {
				return nil, err
			}
			output, err := required(t.output)
			if err != nil {
				return nil, err
			}
			if input > math.MaxInt64-output {
				return nil, ErrLocalHistory
			}
			total := input + output
			if t.total.present && (!t.total.valid || t.total.v != total) {
				return nil, ErrLocalHistory
			}
			if total <= 0 {
				continue
			}
			cached, err := optional(t.cached)
			if err != nil {
				return nil, err
			}
			reasoning, err := optional(t.reasoning)
			if err != nil {
				return nil, err
			}
			if cached > input || reasoning > output {
				return nil, ErrLocalHistory
			}
			var res Resolved
			if t.savedModel != "" {
				res = identity.Resolve(t.savedModel)
			}
			saved := t.savedModel
			legacy := res.DisplayName == "" && !t.fromReceipt && !strings.HasPrefix(t.id, "cloud:") &&
				(saved == "" || strings.HasPrefix(saved, "m-") || strings.HasPrefix(saved, "fbm1."))
			model := res.DisplayName
			switch {
			case legacy:
				model = OlderUsageName
			case model != "":
			case saved != "" && (t.fromReceipt || strings.Contains(saved, "/")) && !strings.HasPrefix(saved, "fbm1."):
				model = saved
			default:
				model = usage.UnattributedModel
			}
			var cost *float64
			if res.Route != "" {
				if rates, ok := p.Resolve(res.Route); ok {
					c := rates.Cost(pricing.Tokens{Input: input - cached, CacheRead: cached, Output: output}, at)
					cost = &c
				}
			}
			group := res.Group
			if legacy {
				group = ""
			}
			acc.Add(day, total, cost, model, group)
			switch {
			case cost != nil:
			case legacy:
				acc.AddNamedUnpriced(day, model) // explained history gap, not a pricing failure
			case res.DisplayName == "":
				acc.AddUnknownModel(day, model)
			default:
				acc.AddNamedUnpriced(day, model) // known model without a public route price
			}
			if t.partial {
				acc.MarkPartial(day)
			}
		}
	}
	return &Fold{Days: acc.Build(), HasSources: true}, nil
}
