package freebuff

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"
)

// Usage is one cumulative token receipt. Cached input is inside Input; reasoning is inside Output.
type Usage struct {
	InputTokens           int64  `json:"inputTokens"`
	OutputTokens          int64  `json:"outputTokens"`
	CachedInputTokens     *int64 `json:"cachedInputTokens,omitempty"`
	ReasoningOutputTokens *int64 `json:"reasoningOutputTokens,omitempty"`
	TotalTokens           *int64 `json:"totalTokens,omitempty"`
}

// Metrics is a hosted metrics event payload (only accounting fields are ever decoded).
type Metrics struct {
	Usage           *Usage `json:"usage,omitempty"`
	UsageIncomplete *bool  `json:"usageIncomplete,omitempty"`
}

// merging keeps previously measured fields when a later patch omits them.
func (m Metrics) merging(prev *Metrics) Metrics {
	if prev == nil {
		return m
	}
	out := m
	if out.Usage == nil {
		out.Usage = prev.Usage
	}
	if out.UsageIncomplete == nil {
		out.UsageIncomplete = prev.UsageIncomplete
	}
	return out
}

func (u *Usage) valid() bool {
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.InputTokens > math.MaxInt64-u.OutputTokens {
		return false
	}
	if u.CachedInputTokens != nil && (*u.CachedInputTokens < 0 || *u.CachedInputTokens > u.InputTokens) {
		return false
	}
	if u.ReasoningOutputTokens != nil && (*u.ReasoningOutputTokens < 0 || *u.ReasoningOutputTokens > u.OutputTokens) {
		return false
	}
	return u.TotalTokens == nil || *u.TotalTokens == u.InputTokens+u.OutputTokens
}

// Receipt keeps only accounting fields for one hosted run, never prompts or events.
type Receipt struct {
	ID          string  `json:"id"`
	TimestampMs float64 `json:"timestampMs"`
	Metrics     Metrics `json:"metrics"`
	Model       string  `json:"model,omitempty"`
}

type hostedRun struct {
	ID         string  `json:"id"`
	ChatID     string  `json:"chatId"`
	Status     string  `json:"status"`
	Model      *string `json:"model"`
	Agent      *string `json:"agent"`
	CreatedAt  string  `json:"createdAt"`
	FinishedAt *string `json:"finishedAt"`
}

func (r hostedRun) active() bool { return r.Status == "queued" || r.Status == "running" }

// Native Claude/Codex harness runs and BYOK runs are not Freebuff usage.
func (r hostedRun) freebuff() bool {
	return (r.Agent == nil || *r.Agent == "codebuff") && !(r.Model != nil && len(*r.Model) >= 5 && (*r.Model)[:5] == "byok:")
}

func (r hostedRun) timestamp() (time.Time, bool) {
	if r.FinishedAt != nil {
		return ParseTime(*r.FinishedAt)
	}
	return ParseTime(r.CreatedAt)
}

func (r hostedRun) model() string {
	if r.Model == nil {
		return ""
	}
	return *r.Model
}

type cachedRun struct {
	Run            hostedRun  `json:"run"`
	Cursor         string     `json:"cursor,omitempty"`
	Receipt        *Receipt   `json:"receipt,omitempty"`
	MetricsVersion int        `json:"metricsVersion,omitempty"`
	RetryAfter     *time.Time `json:"retryAfter,omitempty"`
}

func (c cachedRun) needsRefresh(now time.Time) bool {
	return c.MetricsVersion != hostedMetricsVersion || (c.RetryAfter != nil && !now.Before(*c.RetryAfter))
}

type hostedCache struct {
	AccountKey    string               `json:"accountKey"`
	WindowVersion int                  `json:"windowVersion"`
	Chats         map[string]string    `json:"chats"`
	Runs          map[string]cachedRun `json:"runs"`
}

const (
	hostedMetricsVersion = 2
	incompleteRetry      = 5 * time.Minute
	maxHostedRuns        = 10_000
	maxHostedCacheBytes  = 8 << 20
)

// HostedScanner incrementally reads the hosted run history. Metrics events are cumulative
// snapshots: each run's newest measured receipt replaces earlier values.
type HostedScanner struct {
	CachePath string
	mu        sync.Mutex
	cache     *hostedCache
}

// AccountKey identifies a login without storing it.
func AccountKey(a Auth) string {
	sum := sha256.Sum256([]byte(a.Token))
	return hex.EncodeToString(sum[:])
}

func (s *HostedScanner) load() {
	if s.cache != nil || s.CachePath == "" {
		return
	}
	data, err := os.ReadFile(s.CachePath)
	if err != nil || len(data) > maxHostedCacheBytes {
		return
	}
	var c hostedCache
	if json.Unmarshal(data, &c) == nil {
		s.cache = &c
	}
}

// Cached returns saved receipts for this login without network access.
func (s *HostedScanner) Cached(a Auth) []Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if s.cache == nil || s.cache.AccountKey != AccountKey(a) {
		return nil
	}
	return receiptsOf(s.cache)
}

// Scan refreshes hosted receipts and persists the accounting-only cache.
func (s *HostedScanner) Scan(ctx context.Context, a Auth, c *Client, now time.Time) ([]Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	key := AccountKey(a)
	initial := hostedCache{AccountKey: key, Chats: map[string]string{}, Runs: map[string]cachedRun{}}
	if s.cache != nil && s.cache.AccountKey == key {
		initial = cloneCache(*s.cache)
	}
	next, err := fetchHosted(ctx, a, c, now, initial)
	if err != nil {
		return nil, err
	}
	if s.cache == nil || !reflect.DeepEqual(*s.cache, next) {
		data, err := json.Marshal(next)
		if err != nil || len(data) > maxHostedCacheBytes {
			return nil, ErrInvalidResponse
		}
		if s.CachePath != "" {
			if err := writeFileAtomic(s.CachePath, data); err != nil {
				return nil, err
			}
		}
	}
	s.cache = &next
	return receiptsOf(&next), nil
}

func cloneCache(c hostedCache) hostedCache {
	out := hostedCache{AccountKey: c.AccountKey, WindowVersion: c.WindowVersion, Chats: map[string]string{}, Runs: map[string]cachedRun{}}
	for k, v := range c.Chats {
		out.Chats[k] = v
	}
	for k, v := range c.Runs {
		out.Runs[k] = v
	}
	return out
}

func receiptsOf(c *hostedCache) []Receipt {
	out := []Receipt{}
	for _, run := range c.Runs {
		if run.Receipt != nil {
			r := *run.Receipt
			r.Model = run.Run.model()
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func fetchHosted(ctx context.Context, a Auth, c *Client, now time.Time, result hostedCache) (hostedCache, error) {
	if result.WindowVersion != 1 {
		// Older versions picked the window by start time; recheck run lists once.
		result.Chats = map[string]string{}
		result.WindowVersion = 1
	}
	y, m, d := now.Date()
	cutoff := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -30)
	for id, run := range result.Runs {
		ts, ok := run.Run.timestamp()
		if !(run.Run.active() || (ok && !ts.Before(cutoff))) {
			delete(result.Runs, id)
		}
	}
	type chat struct {
		ID        string     `json:"id"`
		UpdatedAt string     `json:"updatedAt"`
		LatestRun *hostedRun `json:"latestRun"`
	}
	chats, err := hostedPages[chat](ctx, "chats", a, c)
	if err != nil {
		return result, err
	}
	ids := map[string]bool{}
	for _, ch := range chats {
		ids[ch.ID] = true
	}
	for id := range result.Chats {
		if !ids[id] {
			delete(result.Chats, id)
		}
	}
	activeChats := map[string]bool{}
	for _, run := range result.Runs {
		if run.Run.active() || run.needsRefresh(now) {
			activeChats[run.Run.ChatID] = true
		}
	}
	for _, ch := range chats {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		active := (ch.LatestRun != nil && ch.LatestRun.active()) || activeChats[ch.ID]
		if result.Chats[ch.ID] == ch.UpdatedAt && !active {
			continue
		}
		runs, err := hostedPages[hostedRun](ctx, "chats/"+url.PathEscape(ch.ID)+"/runs", a, c)
		if err != nil {
			return result, err
		}
		// A hidden or deleted run cannot undo measured consumption; keep receipts until the window expires.
		for _, run := range runs {
			if !run.freebuff() {
				delete(result.Runs, run.ID)
			}
		}
		for _, run := range runs {
			if !run.freebuff() {
				continue
			}
			ts, ok := run.timestamp()
			if !ok {
				return result, ErrInvalidResponse
			}
			if !run.active() && ts.Before(cutoff) {
				continue
			}
			prev, had := result.Runs[run.ID]
			if had && reflect.DeepEqual(prev.Run, run) && !run.active() && !prev.needsRefresh(now) {
				continue
			}
			var prevPtr *cachedRun
			if had {
				prevPtr = &prev
			}
			read, err := readHostedRun(ctx, run, prevPtr, a, c, now)
			if err != nil {
				return result, err
			}
			result.Runs[run.ID] = read
			if len(result.Runs) > maxHostedRuns {
				return result, ErrInvalidResponse
			}
		}
		result.Chats[ch.ID] = ch.UpdatedAt
	}
	return result, nil
}

func readHostedRun(ctx context.Context, run hostedRun, prev *cachedRun, a Auth, c *Client, now time.Time) (cachedRun, error) {
	result := cachedRun{Run: run}
	if prev != nil {
		result = *prev
		result.Run = run
	}
	if prev == nil || prev.MetricsVersion != hostedMetricsVersion {
		result.Cursor, result.Receipt = "", nil
	}
	ts, ok := run.timestamp()
	if !ok {
		return result, ErrInvalidResponse
	}
	stamp := float64(ts.UnixNano()) / 1e6
	cursor := result.Cursor
	seen := map[string]bool{}
	type event struct {
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	for i := 0; i < 1000; i++ {
		page, err := hostedPage[event](ctx, "runs/"+url.PathEscape(run.ID)+"/events", cursor, a, c)
		if err != nil {
			return result, err
		}
		for _, ev := range page.Items {
			if ev.Kind != "metrics" {
				continue // non-metric payloads include conversation content; never decode them
			}
			var patch Metrics
			dec := json.NewDecoder(bytes.NewReader(ev.Payload))
			if err := dec.Decode(&patch); err != nil {
				return result, ErrInvalidResponse
			}
			if patch.Usage != nil && !patch.Usage.valid() {
				return result, ErrInvalidResponse
			}
			var prior *Metrics
			if result.Receipt != nil {
				prior = &result.Receipt.Metrics
			}
			result.Receipt = &Receipt{ID: "cloud:" + run.ID, TimestampMs: stamp, Metrics: patch.merging(prior), Model: run.model()}
		}
		// endCursor advances across non-metric events too, so idle runs download nothing twice.
		switch {
		case page.EndCursor != nil:
			result.Cursor = *page.EndCursor
		case page.NextCursor != nil:
			result.Cursor = *page.NextCursor
		default:
			result.Cursor = cursor
		}
		if page.NextCursor == nil {
			if result.Receipt == nil {
				// No metrics is still a coverage gap; never manufacture a measured zero.
				result.Receipt = &Receipt{ID: "cloud:" + run.ID, TimestampMs: stamp, Model: run.model()}
			}
			result.Receipt.TimestampMs = stamp
			result.Receipt.Model = run.model()
			result.MetricsVersion = hostedMetricsVersion
			result.RetryAfter = nil
			mt := result.Receipt.Metrics
			if mt.Usage == nil || (mt.UsageIncomplete != nil && *mt.UsageIncomplete) {
				t := now.Add(incompleteRetry)
				result.RetryAfter = &t
			}
			return result, nil
		}
		if seen[*page.NextCursor] {
			return result, ErrInvalidResponse
		}
		seen[*page.NextCursor] = true
		cursor = *page.NextCursor
	}
	return result, ErrInvalidResponse
}

type page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	EndCursor  *string `json:"endCursor"`
}

func hostedPages[T any](ctx context.Context, path string, a Auth, c *Client) ([]T, error) {
	var items []T
	cursor := ""
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, err := hostedPage[T](ctx, path, cursor, a, c)
		if err != nil {
			return nil, err
		}
		items = append(items, p.Items...)
		if p.NextCursor == nil {
			return items, nil
		}
		if seen[*p.NextCursor] {
			return nil, ErrInvalidResponse
		}
		seen[*p.NextCursor] = true
		cursor = *p.NextCursor
	}
	return nil, ErrInvalidResponse
}

func hostedPage[T any](ctx context.Context, path, cursor string, a Auth, c *Client) (page[T], error) {
	var p page[T]
	if err := ctx.Err(); err != nil {
		return p, err
	}
	resp, err := c.FetchHistory(ctx, path, cursor, a)
	if err != nil {
		return p, err
	}
	if resp.Status == 401 {
		return p, ErrTokenInvalid
	}
	if resp.Status < 200 || resp.Status > 299 {
		return p, RequestFailed{resp.Status}
	}
	// nextCursor must be present (null is fine); its absence means an unknown response shape.
	var probe map[string]json.RawMessage
	if json.Unmarshal(resp.Body, &probe) != nil {
		return p, ErrInvalidResponse
	}
	if _, ok := probe["nextCursor"]; !ok {
		return p, ErrInvalidResponse
	}
	if _, ok := probe["items"]; !ok {
		return p, ErrInvalidResponse
	}
	if err := json.Unmarshal(resp.Body, &p); err != nil {
		return p, ErrInvalidResponse
	}
	return p, nil
}

// writeFileAtomic writes an owner-only staging file, syncs it and renames it over
// the cache. If replacing the destination fails (e.g. Windows file sharing
// permissions), the previous cache must stay intact. The next refresh can retry.
func writeFileAtomic(path string, data []byte) error {
	return writeFileAtomicWithRename(path, data, os.Rename)
}

// The rename seam lets tests prove failure safety without relying on host-specific
// permissions or Windows file locks.
func writeFileAtomicWithRename(path string, data []byte, rename func(string, string) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Never remove the destination on rename failure. A failed cache refresh
	// is recoverable; deleting the last valid snapshot is not.
	return rename(name, path)
}
