package usage

import (
	"reflect"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

func TestSourcesSplitFreebuffAndAntigravityStores(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.Local)
	today := DayKey(now)
	var a Accumulator
	a.Add(today, 10, f(1), "DeepSeek V4.1 Flash", "")
	a.Add(today, 20, nil, "Antigravity · Gemini 3.8 Flash", "")
	a.Add(today, 30, nil, "Antigravity IDE · Gemini 3.8 Flash", "")
	a.Add(today, 40, nil, "Agy CLI · Claude Sonnet 5", "")
	a.Add(today, 50, nil, "Claude · Sonnet 4.5", "")
	a.Add(today, 60, nil, "Codex · GPT 6 Luna", "")
	period := StandardPeriods(a.Build(), now)[0]
	want := []SourceRow{
		{Name: "Freebuff", Tokens: 10},
		{Name: "Antigravity app", Tokens: 20},
		{Name: "Antigravity IDE", Tokens: 30},
		{Name: "Agy CLI", Tokens: 40},
		{Name: "Claude", Tokens: 50},
		{Name: "Codex", Tokens: 60},
	}
	if !reflect.DeepEqual(period.Sources, want) {
		t.Fatalf("sources = %+v, want %+v", period.Sources, want)
	}
	if period.Tokens != 210 {
		t.Fatalf("period tokens = %d, want 210", period.Tokens)
	}
}

func TestSourcesSkipOriginsWithoutUsage(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.Local)
	var a Accumulator
	a.Add(DayKey(now), 5, nil, "Agy CLI · Gemini 3.8 Flash", "")
	period := StandardPeriods(a.Build(), now)[0]
	want := []SourceRow{{Name: "Agy CLI", Tokens: 5}}
	if !reflect.DeepEqual(period.Sources, want) {
		t.Fatalf("sources = %+v, want %+v", period.Sources, want)
	}
}

func TestPeriodsAndLowerBounds(t *testing.T) {
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.Local)
	today, yesterday, old := DayKey(now), DayKey(now.AddDate(0, 0, -1)), DayKey(now.AddDate(0, 0, -40))
	mid := DayKey(now.AddDate(0, 0, -10))
	var a Accumulator
	a.Add(today, 100, f(0.5), "A 10/17", "A")
	a.Add(today, 50, nil, "B", "")
	a.AddNamedUnpriced(today, "B")
	a.Add(yesterday, 10, f(0.1), "A Fast", "A")
	a.Add(mid, 20, f(2), "A", "A")
	a.Add(old, 999, f(9), "A", "A")
	a.MarkPartial(yesterday)
	days := a.Build()
	if days[0].Date != today {
		t.Fatalf("days must be newest first: %+v", days)
	}
	p := StandardPeriods(days, now)
	if p[0].Tokens != 150 || p[0].CostUSD == nil || *p[0].CostUSD != 0.5 || !p[0].CostLowerBound {
		t.Fatalf("today = %+v", p[0])
	}
	if !p[1].Partial || p[1].Tokens != 10 {
		t.Fatalf("yesterday = %+v", p[1])
	}
	if p[2].Tokens != 160 {
		t.Fatalf("week = %+v (10-day-old usage must be excluded)", p[2])
	}
	if p[3].Tokens != 180 {
		t.Fatalf("30 days = %+v (40-day-old usage must be excluded)", p[3])
	}
	var aRow *ModelRow
	for i := range p[3].Models {
		if p[3].Models[i].Name == "A" {
			aRow = &p[3].Models[i]
		}
	}
	if aRow == nil || aRow.Tokens != 130 || len(aRow.Variants) != 2 {
		t.Fatalf("grouped row = %+v", aRow)
	}
}

func TestKeepSinceDropsOldAndUndated(t *testing.T) {
	type item struct {
		at time.Time
	}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	items := []item{
		{at: now.AddDate(0, 0, -60)},
		{},
		{at: now.Add(-time.Hour)},
		{at: HistoryCutoff(now)},
	}
	kept := KeepSince(items, HistoryCutoff(now), func(i item) time.Time { return i.at })
	if len(kept) != 2 {
		t.Fatalf("kept = %d, want the recent turn and the cutoff instant", len(kept))
	}
	if !kept[0].at.Equal(now.Add(-time.Hour)) || !kept[1].at.Equal(HistoryCutoff(now)) {
		t.Fatalf("kept = %+v", kept)
	}
}
