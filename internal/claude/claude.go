// Package claude reads measured token usage from Claude Code's local transcripts under
// ~/.claude/projects. The transcripts also hold prompts and tool payloads; this scanner keeps only
// per-response token counters, the model id and the response timestamp, never text.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
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

// Source is the origin label Claude Code usage is reported under. It stays plain "Claude" because
// Claude Code can also be pointed at non-Anthropic models, so a session is the source, not the model
// family.
const Source = "Claude"

// DefaultRoot returns Claude Code's transcript directory, or "" when there is no home directory.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// Turn is one measured model response.
type Turn struct {
	At          time.Time
	MessageID   string
	ModelID     string
	Model       string
	Input       int64
	Output      int64
	CacheCreate int64
	CacheRead   int64
}

// Scanner caches parsed transcripts and re-reads only files that changed since the last scan.
type Scanner struct {
	Root string

	mu    sync.Mutex
	cache map[string]*cachedFile
	// unreadable is how many transcripts the last scan could not read, so a caller can say that
	// its totals are missing their usage instead of only writing a log line.
	unreadable int
}

type cachedFile struct {
	size     int64
	modified time.Time
	completeUntil int64 // last newline; incomplete tails are replayed on append
	info     os.FileInfo // distinguish append from delete-and-replace at the same path
	turns    []Turn
}

// NewScanner returns a scanner over Claude Code's default transcript directory.
func NewScanner() *Scanner { return &Scanner{Root: DefaultRoot()} }

// Transcripts lists the JSONL transcript files that exist under root.
func Transcripts(root string) []string {
	if root == "" {
		return nil
	}
	var out []string
	walkTranscripts(root, func(path string) bool {
		out = append(out, path)
		return true
	})
	return out
}

func walkTranscripts(root string, visit func(string) bool) {
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
		log.Printf("claude history %s: %v", root, err)
	}
}

// Scan returns measured Claude Code days. A missing transcript directory is not an error.
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
	walkTranscripts(s.Root, func(path string) bool {
		if ctx.Err() != nil {
			return false
		}
		present[path] = true
		st, err := os.Stat(path)
		if err != nil {
			delete(s.cache, path)
			return true
		}
		if c := s.cache[path]; c != nil && c.info != nil && os.SameFile(c.info, st) && c.size == st.Size() && c.modified.Equal(st.ModTime()) {
			// An unchanged file is only re-read when it changes, so its cached turns are pruned here
			// to keep the retained history bounded as days pass.
			c.turns = usage.KeepSince(c.turns, cutoff, turnTime)
			turns = append(turns, c.turns...)
			return true
		}
		if c := s.cache[path]; c != nil && c.info != nil && os.SameFile(c.info, st) && st.Size() > c.size && c.completeUntil <= st.Size() {
			// JSONL transcripts are append-heavy. Read only the new suffix, replaying the
			// last unterminated line if the previous scan ended during a write.
			more, completeUntil, err := readTranscriptRange(ctx, path, c.completeUntil)
			if err == nil {
				parsed := usage.KeepSince(dedupe(append(c.turns, more...)), cutoff, turnTime)
				turns = append(turns, parsed...)
				s.cache[path] = &cachedFile{size: st.Size(), modified: st.ModTime(), completeUntil: completeUntil, info: st, turns: parsed}
				return true
			}
			// An unusable tail falls through to a full scan, which also detects rewrites.
		}
		parsed, completeUntil, err := readTranscriptRange(ctx, path, 0)
		if err != nil {
			unreadable++
			delete(s.cache, path)
			return true
		}
		parsed = usage.KeepSince(parsed, cutoff, turnTime)
		turns = append(turns, parsed...)
		s.cache[path] = &cachedFile{size: st.Size(), modified: st.ModTime(), completeUntil: completeUntil, info: st, turns: parsed}
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
		log.Printf("claude: %d transcript(s) could not be read", unreadable)
	}
	return Fold(dedupe(turns), now, p), nil
}

// Unreadable reports how many transcripts the last scan could not read.
func (s *Scanner) Unreadable() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unreadable
}

func turnTime(t Turn) time.Time { return t.At }

func readTranscript(ctx context.Context, path string) ([]Turn, error) {
	turns, _, err := readTranscriptRange(ctx, path, 0)
	return turns, err
}

// readTranscriptRange returns parsed receipts and the last complete newline offset. Unlike
// starting at the previous file size, this preserves JSONL lines whose writes were interrupted.
func readTranscriptRange(ctx context.Context, path string, start int64) ([]Turn, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, start, err
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, start, err
	}
	reader := bufio.NewReaderSize(f, 64*1024)
	var turns []Turn
	position, completeUntil := start, start
	var line []byte
	for {
		if err := ctx.Err(); err != nil {
			return nil, completeUntil, err
		}
		// ReadSlice caps each fragment at the reader buffer size. ReadBytes can allocate an
		// unbounded tool-payload line before checking its length; do not do that on agent logs.
		chunk, readErr := reader.ReadSlice('\n')
		position += int64(len(chunk))
		if len(line)+len(chunk) > 16*1024*1024 {
			return nil, completeUntil, errors.New("Claude transcript line exceeds 16 MiB")
		}
		line = append(line, chunk...)
		if errors.Is(readErr, bufio.ErrBufferFull) {
			continue
		}
		if turn, ok := parseLine(line); ok {
			turns = append(turns, turn)
		}
		line = line[:0]
		if readErr == nil {
			completeUntil = position
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return turns, completeUntil, nil
		}
		return nil, completeUntil, readErr
	}
}

type transcriptLine struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	SessionID string `json:"sessionId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			Input       int64 `json:"input_tokens"`
			Output      int64 `json:"output_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// parseLine keeps assistant responses that carry a token receipt. Every content block of one
// response is written as its own line with the same usage, so lines are keyed by message id later.
func parseLine(line []byte) (Turn, bool) {
	if len(line) == 0 || line[0] != '{' {
		return Turn{}, false
	}
	var entry transcriptLine
	if err := json.Unmarshal(line, &entry); err != nil {
		return Turn{}, false
	}
	if entry.Type != "assistant" || entry.Message.ID == "" {
		return Turn{}, false
	}
	u := entry.Message.Usage
	if u.Input < 0 || u.Output < 0 || u.CacheCreate < 0 || u.CacheRead < 0 {
		return Turn{}, false
	}
	if u.Input+u.Output+u.CacheCreate+u.CacheRead <= 0 {
		return Turn{}, false
	}
	at, ok := parseTime(entry.Timestamp)
	if !ok {
		return Turn{}, false
	}
	return Turn{
		At: at, MessageID: entry.Message.ID, ModelID: entry.Message.Model, Model: entry.Message.Model,
		Input: u.Input, Output: u.Output, CacheCreate: u.CacheCreate, CacheRead: u.CacheRead,
	}, true
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

// dedupe keeps one turn per response. Claude Code writes one line per content block, each repeating
// the full usage of the response, and resumed sessions can repeat a response in more than one file.
func dedupe(turns []Turn) []Turn {
	seen := map[string]bool{}
	out := turns[:0]
	for _, t := range turns {
		if t.MessageID != "" {
			if seen[t.MessageID] {
				continue
			}
			seen[t.MessageID] = true
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
		if p != nil {
			if route := priceRoute(t); route != "" {
				if rates, ok := p.Resolve(route); ok {
					c := rates.Cost(pricing.Tokens{
						Input: t.Input, CacheRead: t.CacheRead, CacheWrite: t.CacheCreate, Output: t.Output,
					}, t.At)
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
	var total int64
	for _, v := range []int64{t.Input, t.Output, t.CacheCreate, t.CacheRead} {
		if v < 0 || v > math.MaxInt64-total {
			return 0, false
		}
		total += v
	}
	return total, true
}

// priceRoute maps a transcript model id to a public pricing key. Claude Code writes snapshot ids
// such as claude-sonnet-4-5-20250929 while the price keys stop at the version.
func priceRoute(t Turn) string {
	id := strings.ToLower(strings.TrimSpace(t.ModelID))
	if id == "" {
		return ""
	}
	if n := len(id); n > 9 && id[n-9] == '-' && digits(id[n-8:]) {
		id = id[:n-9]
	}
	return id
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
	slug := strings.ToLower(name)
	if !strings.HasPrefix(slug, "claude-") {
		return name
	}
	parts := strings.Split(strings.TrimPrefix(slug, "claude-"), "-")
	if n := len(parts); n > 0 && len(parts[n-1]) == 8 && digits(parts[n-1]) {
		parts = parts[:n-1]
	}
	var words, version []string
	for _, part := range parts {
		if digits(part) {
			version = append(version, part)
			continue
		}
		words = append(words, strings.ToUpper(part[:1])+part[1:])
	}
	out := strings.Join(words, " ")
	if len(version) > 0 {
		if out != "" {
			out += " "
		}
		out += strings.Join(version, ".")
	}
	if out == "" {
		return name
	}
	return out
}
