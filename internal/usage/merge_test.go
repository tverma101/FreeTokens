package usage

import "testing"

func TestMergeDaysSumsOverlappingModels(t *testing.T) {
	cost := 1.5
	base := []Day{{
		Date: "2026-11-05", Tokens: 100, CostUSD: &cost, CostComplete: true,
		Models: []ModelRow{{Name: "Freebuff model", Tokens: 100, CostUSD: &cost, CostComplete: true}},
	}}
	extra := []Day{{
		Date: "2026-11-05", Tokens: 50,
		Models: []ModelRow{{Name: "Antigravity · Gemini 3.8 Flash", Tokens: 50}},
	}}
	days := MergeDays(base, extra)
	if len(days) != 1 {
		t.Fatalf("days = %+v", days)
	}
	day := days[0]
	if day.Tokens != 150 {
		t.Fatalf("tokens = %d, want 150", day.Tokens)
	}
	if day.CostComplete {
		t.Fatal("day with an unpriced model was marked complete")
	}
	if day.CostUSD == nil || *day.CostUSD != cost {
		t.Fatalf("cost = %v, want %v", day.CostUSD, cost)
	}
	if len(day.Models) != 2 {
		t.Fatalf("models = %+v", day.Models)
	}
}

func TestMergeDaysKeepsSeparateDatesNewestFirst(t *testing.T) {
	first := []Day{{Date: "2026-11-04", Tokens: 10}}
	second := []Day{{Date: "2026-11-05", Tokens: 20}}
	days := MergeDays(first, second)
	if len(days) != 2 || days[0].Date != "2026-11-05" || days[1].Date != "2026-11-04" {
		t.Fatalf("days = %+v", days)
	}
}

func TestMergeDaysCombinesSameModelAndVariants(t *testing.T) {
	first := []Day{{Date: "2026-11-05", Tokens: 10, Models: []ModelRow{{
		Name: "Model", Tokens: 10, CostComplete: true,
		Variants: []Variant{{Name: "Fast", Tokens: 10, CostComplete: true}},
	}}}}
	second := []Day{{Date: "2026-11-05", Tokens: 5, Models: []ModelRow{{
		Name: "Model", Tokens: 5, CostComplete: true,
		Variants: []Variant{{Name: "Fast", Tokens: 5, CostComplete: true}},
	}}}}
	days := MergeDays(first, second)
	if len(days) != 1 || len(days[0].Models) != 1 {
		t.Fatalf("days = %+v", days)
	}
	model := days[0].Models[0]
	if model.Tokens != 15 || len(model.Variants) != 1 || model.Variants[0].Tokens != 15 {
		t.Fatalf("model = %+v", model)
	}
}
