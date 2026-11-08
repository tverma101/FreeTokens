// Package usage folds measured token receipts into daily totals and period summaries.
package usage

import (
	"math"
	"sort"
	"strings"
	"time"
)

// UnattributedModel names streams whose serving model is unknown.
const UnattributedModel = "Unattributed"

// Variant is one source spelling inside a model row (for example a Fast mode or build date).
type Variant struct {
	Name         string   `json:"name"`
	Tokens       int64    `json:"tokens"`
	CostUSD      *float64 `json:"costUsd,omitempty"`
	CostComplete bool     `json:"costComplete"`
}

// ModelRow groups variants of one model.
type ModelRow struct {
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Source       string    `json:"source"`
	Tokens       int64     `json:"tokens"`
	CostUSD      *float64  `json:"costUsd,omitempty"`
	CostComplete bool      `json:"costComplete"`
	Variants     []Variant `json:"variants,omitempty"`
}

// SourceFreebuff is the origin of Freebuff hosted and Desktop runs, whose model rows carry no
// origin prefix.
const SourceFreebuff = "Freebuff"

// SourceRow is the measured tokens one origin contributed to a period.
type SourceRow struct {
	Name   string `json:"name"`
	Tokens int64  `json:"tokens"`
}

// originSources maps the source label a history reader writes into its model rows to the name the
// popover shows. The IDE label is checked before the Antigravity app label, which is its prefix.
var originSources = []struct{ prefix, label string }{
	{"Antigravity IDE", "Antigravity IDE"},
	{"Agy CLI", "Agy CLI"},
	{"Antigravity", "Antigravity app"},
	{"Claude", "Claude"},
	{"Codex", "Codex"},
	{"OpenCode", "OpenCode"},
}

// sourceOrder is the display order of the per-source summary.
var sourceOrder = []string{SourceFreebuff, "Antigravity app", "Antigravity IDE", "Agy CLI", "Claude", "Codex", "OpenCode"}

// SourceOf names where a model row's tokens were measured.
func SourceOf(model string) string {
	for _, s := range originSources {
		if strings.HasPrefix(model, s.prefix+" · ") {
			return s.label
		}
	}
	return SourceFreebuff
}

// summarizeSources groups model rows by origin, in display order, skipping origins with no usage.
func summarizeSources(models []ModelRow) []SourceRow {
	totals := map[string]int64{}
	for _, m := range models {
		name := SourceOf(m.Name)
		totals[name] = satAdd(totals[name], m.Tokens)
	}
	var out []SourceRow
	for _, name := range sourceOrder {
		if totals[name] > 0 {
			out = append(out, SourceRow{Name: name, Tokens: totals[name]})
		}
	}
	return out
}

// Day is one local calendar day.
type Day struct {
	Date           string     `json:"date"`
	Tokens         int64      `json:"tokens"`
	CostUSD        *float64   `json:"costUsd,omitempty"`
	CostComplete   bool       `json:"costComplete"`
	Partial        bool       `json:"partial"`
	Models         []ModelRow `json:"models"`
	UnknownModels  []string   `json:"unknownModels,omitempty"`
	UnpricedModels []string   `json:"unpricedModels,omitempty"`
}

type variantAcc struct {
	name     string
	tokens   int64
	cost     float64
	priced   bool
	complete bool
}

type modelAcc struct {
	tokens   int64
	cost     float64
	priced   bool
	complete bool
	provider string
	fallback string
	variants map[string]*variantAcc
}

type dayAcc struct {
	tokens   int64
	cost     float64
	priced   bool
	complete bool
	models   map[string]*modelAcc
	unknown  map[string]bool
	unpriced map[string]bool
	partial  bool
}

// Accumulator collects per-day tokens and public-rate cost estimates.
type Accumulator struct {
	days map[string]*dayAcc
	// Vendor supplies the pricing catalog's vendor prefix for a model name. It is only consulted
	// when no provider keyword matches, and may be nil.
	Vendor func(string) string
}

func (a *Accumulator) day(d string) *dayAcc {
	if a.days == nil {
		a.days = map[string]*dayAcc{}
	}
	x := a.days[d]
	if x == nil {
		x = &dayAcc{complete: true, models: map[string]*modelAcc{}, unknown: map[string]bool{}, unpriced: map[string]bool{}}
		a.days[d] = x
	}
	return x
}

func satAdd(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

// Add records measured tokens; a nil cost marks the day and model incomplete.
func (a *Accumulator) Add(day string, tokens int64, cost *float64, model, group string) {
	a.add(day, tokens, cost, model, group, "", "")
}

// AddServed records measured tokens whose serving vendor the source recorded. A non-empty provider
// wins over the keyword table, because a measured vendor beats a guess from the model name.
func (a *Accumulator) AddServed(day string, tokens int64, cost *float64, model, group, provider string) {
	a.add(day, tokens, cost, model, group, provider, "")
}

// AddOwned records measured tokens for a model the source serves itself, such as an OpenCode Zen
// model. The provider is used only when neither the keyword table nor the pricing catalog names a
// vendor, so it never relabels another vendor's model.
func (a *Accumulator) AddOwned(day string, tokens int64, cost *float64, model, group, provider string) {
	a.add(day, tokens, cost, model, group, "", provider)
}

func (a *Accumulator) add(day string, tokens int64, cost *float64, model, group, provider, fallback string) {
	d := a.day(day)
	d.tokens = satAdd(d.tokens, tokens)
	if cost != nil {
		d.cost += *cost
		d.priced = true
	} else if tokens > 0 {
		d.complete = false
	}
	id := group
	if id == "" {
		id = model
	}
	m := d.models[id]
	if m == nil {
		m = &modelAcc{complete: true, variants: map[string]*variantAcc{}}
		d.models[id] = m
	}
	if provider != "" {
		m.provider = provider
	}
	if fallback != "" {
		m.fallback = fallback
	}
	m.tokens = satAdd(m.tokens, tokens)
	if cost != nil {
		m.cost += *cost
		m.priced = true
	} else if tokens > 0 {
		m.complete = false
	}
	key := strings.ToLower(model)
	v := m.variants[key]
	if v == nil {
		v = &variantAcc{name: model, complete: true}
		m.variants[key] = v
	}
	v.tokens = satAdd(v.tokens, tokens)
	if cost != nil {
		v.cost += *cost
		v.priced = true
	} else if tokens > 0 {
		v.complete = false
	}
}

// MarkPartial records a run or turn without a complete token receipt.
func (a *Accumulator) MarkPartial(day string) { a.day(day).partial = true }

// AddUnknownModel records a model nothing names (raises the unknown-pricing warning).
func (a *Accumulator) AddUnknownModel(day, model string) { a.day(day).unknown[model] = true }

// AddNamedUnpriced records a known model without a public route price (explained gap).
func (a *Accumulator) AddNamedUnpriced(day, model string) { a.day(day).unpriced[model] = true }

func costPtr(priced bool, v float64) *float64 {
	if !priced {
		return nil
	}
	return &v
}

func setList(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// Build returns days newest first.
func (a *Accumulator) Build() []Day {
	out := make([]Day, 0, len(a.days))
	for date, d := range a.days {
		day := Day{Date: date, Tokens: d.tokens, CostUSD: costPtr(d.priced, d.cost), CostComplete: d.complete,
			Partial: d.partial, UnknownModels: setList(d.unknown), UnpricedModels: setList(d.unpriced), Models: []ModelRow{}}
		for name, m := range d.models {
			row := ModelRow{Name: name, Provider: providerFor(m.provider, m.fallback, name, a.Vendor), Source: SourceOf(name), Tokens: m.tokens, CostUSD: costPtr(m.priced, m.cost), CostComplete: m.complete}
			if len(m.variants) > 1 || (len(m.variants) == 1 && firstVariant(m).name != name) {
				for _, v := range m.variants {
					row.Variants = append(row.Variants, Variant{v.name, v.tokens, costPtr(v.priced, v.cost), v.complete})
				}
				sort.Slice(row.Variants, func(i, j int) bool { return row.Variants[i].Tokens > row.Variants[j].Tokens })
			}
			day.Models = append(day.Models, row)
		}
		sortModels(day.Models)
		out = append(out, day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func firstVariant(m *modelAcc) *variantAcc {
	for _, v := range m.variants {
		return v
	}
	return nil
}

func sortModels(rows []ModelRow) {
	sort.Slice(rows, func(i, j int) bool {
		ci, cj := 0.0, 0.0
		if rows[i].CostUSD != nil {
			ci = *rows[i].CostUSD
		}
		if rows[j].CostUSD != nil {
			cj = *rows[j].CostUSD
		}
		if ci != cj {
			return ci > cj
		}
		if rows[i].Tokens != rows[j].Tokens {
			return rows[i].Tokens > rows[j].Tokens
		}
		return rows[i].Name < rows[j].Name
	})
}

// DayKey is the local calendar day of t.
func DayKey(t time.Time) string { return t.Local().Format("2006-01-02") }

// HistoryWindow is how far back each local reader keeps individual turns. The panel shows at most the
// last 30 days, so anything older is not retained: on an agent-heavy machine the alternative is
// keeping every turn ever written in memory and re-aggregating it on each refresh.
const HistoryWindow = 31 * 24 * time.Hour

// HistoryCutoff is the oldest instant a scan started at now needs to keep.
func HistoryCutoff(now time.Time) time.Time { return now.Add(-HistoryWindow) }

// KeepSince drops the items older than cutoff, in place, and returns the retained prefix. Items
// without a timestamp are dropped too: no reader can place them in the displayed window.
func KeepSince[T any](items []T, cutoff time.Time, at func(T) time.Time) []T {
	kept := items[:0]
	for _, item := range items {
		if t := at(item); !t.Before(cutoff) {
			kept = append(kept, item)
		}
	}
	return kept
}

// Period summarizes one or more days.
type Period struct {
	Label   string   `json:"label"`
	Tokens  int64    `json:"tokens"`
	CostUSD *float64 `json:"costUsd,omitempty"`
	// CostLowerBound means some measured tokens are unpriced or receipts are incomplete.
	CostLowerBound bool       `json:"costLowerBound"`
	Partial        bool       `json:"partial"`
	UnknownModels  []string   `json:"unknownModels,omitempty"`
	Models         []ModelRow `json:"models"`
	// Sources splits the period's tokens by where they were measured.
	Sources []SourceRow `json:"sources,omitempty"`
	HasData bool        `json:"hasData"`
}

// Summarize merges the given days into one period.
func Summarize(label string, days []Day, include func(string) bool) Period {
	p := Period{Label: label, Models: []ModelRow{}}
	var cost float64
	priced := false
	unknown := map[string]bool{}
	models := map[string]*ModelRow{}
	for _, d := range days {
		if !include(d.Date) {
			continue
		}
		p.HasData = p.HasData || d.Tokens > 0 || d.Partial
		p.Tokens = satAdd(p.Tokens, d.Tokens)
		if d.CostUSD != nil {
			cost += *d.CostUSD
			priced = true
		}
		if !d.CostComplete || d.Partial {
			p.CostLowerBound = true
		}
		p.Partial = p.Partial || d.Partial
		for _, u := range d.UnknownModels {
			unknown[u] = true
		}
		for _, m := range d.Models {
			row := models[m.Name]
			if row == nil {
				provider := m.Provider
				if provider == "" {
					provider = ProviderOf(m.Name)
				}
				row = &ModelRow{Name: m.Name, Provider: provider, Source: SourceOf(m.Name), CostComplete: true}
				models[m.Name] = row
			}
			row.Tokens = satAdd(row.Tokens, m.Tokens)
			if m.CostUSD != nil {
				c := *m.CostUSD
				if row.CostUSD != nil {
					c += *row.CostUSD
				}
				row.CostUSD = &c
			}
			row.CostComplete = row.CostComplete && m.CostComplete
			row.Variants = mergeVariants(row.Variants, m.Variants)
		}
	}
	if priced {
		p.CostUSD = &cost
	}
	p.UnknownModels = setList(unknown)
	for _, r := range models {
		p.Models = append(p.Models, *r)
	}
	sortModels(p.Models)
	p.Sources = summarizeSources(p.Models)
	return p
}

func mergeVariants(into, add []Variant) []Variant {
	for _, v := range add {
		found := false
		for i := range into {
			if strings.EqualFold(into[i].Name, v.Name) {
				into[i].Tokens = satAdd(into[i].Tokens, v.Tokens)
				if v.CostUSD != nil {
					c := *v.CostUSD
					if into[i].CostUSD != nil {
						c += *into[i].CostUSD
					}
					into[i].CostUSD = &c
				}
				into[i].CostComplete = into[i].CostComplete && v.CostComplete
				found = true
				break
			}
		}
		if !found {
			into = append(into, v)
		}
	}
	return into
}

// StandardPeriods returns Today, Yesterday, Week (today plus the 6 previous days) and
// Last 30 Days (today plus the 29 previous days).
func StandardPeriods(days []Day, now time.Time) []Period {
	today := DayKey(now)
	yesterday := DayKey(now.AddDate(0, 0, -1))
	week := DayKey(now.AddDate(0, 0, -6))
	first := DayKey(now.AddDate(0, 0, -29))
	return []Period{
		Summarize("Today", days, func(d string) bool { return d == today }),
		Summarize("Yesterday", days, func(d string) bool { return d == yesterday }),
		Summarize("Week", days, func(d string) bool { return d >= week && d <= today }),
		Summarize("Last 30 Days", days, func(d string) bool { return d >= first && d <= today }),
	}
}
