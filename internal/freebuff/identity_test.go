package freebuff

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"
)

const (
	normalRoute = "deepseek/deepseek-v4.1-flash-1017"
	legacyRoute = "deepseek/deepseek-v4-flash"
	fastRoute   = "deepseek-v4.1-flash-fast"
)

var testNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)

func testCatalog() map[string]CatalogEntry {
	return map[string]CatalogEntry{
		"m-normal": {Key: "m-normal", DisplayName: "DeepSeek V4.1 Flash 10/17", LegacyDigests: []string{"1e303ac563a6f9cc"}},
		"m-fast":   {Key: "m-fast", DisplayName: "DeepSeek V4.1 Flash Fast 10/17"},
		"m-slow":   {Key: "m-slow", DisplayName: "DeepSeek V4.1 Flash Slow"},
	}
}

func testPricing(t *testing.T) *pricing.Snapshot {
	t.Helper()
	sup, err := pricing.DecodeSupplement([]byte(`{"pricing":{"` + fastRoute + `":{"input_per_million":4,"output_per_million":8,"cache_write_per_million":4,"cache_read_per_million":0.4}},
		"alias_rules":[{"pattern":"^DeepSeek V4\\.1 Flash Fast 10/17$","canonical":"` + fastRoute + `"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	normal := pricing.Rates{Input: 1, Output: 2, CacheWrite: 1, CacheRead: 0.1, CacheReadExplicit: true}
	return pricing.NewSnapshot(sup, map[string]pricing.Rates{normalRoute: normal, legacyRoute: normal})
}

func receipt(id, model string) Receipt {
	in, out, cached, total := int64(100), int64(50), int64(80), int64(150)
	return Receipt{ID: id, TimestampMs: float64(testNow.Add(-time.Minute).UnixMilli()), Model: model,
		Metrics: Metrics{Usage: &Usage{InputTokens: in, OutputTokens: out, CachedInputTokens: &cached, TotalTokens: &total}}}
}

func scanReceipts(t *testing.T, catalog map[string]CatalogEntry, p *pricing.Snapshot, rs ...Receipt) []usage.Day {
	t.Helper()
	s := &DesktopScanner{Roots: []string{t.TempDir()}}
	f, err := s.Scan(context.Background(), testNow, p, catalog, rs)
	if err != nil {
		t.Fatal(err)
	}
	return f.Days
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func TestLegacyDigestMatchesPublishedCatalog(t *testing.T) {
	if got := LegacyDigest(legacyRoute); got != "1e303ac563a6f9cc" {
		t.Fatalf("digest = %s", got)
	}
	if LegacyDigest("DEEPSEEK/DEEPSEEK-V4-FLASH") == "1e303ac563a6f9cc" {
		t.Fatal("digest must be case sensitive")
	}
}

func TestKeyLegacySlugDatedRouteAndModesShareOneRow(t *testing.T) {
	days := scanReceipts(t, testCatalog(), testPricing(t),
		receipt("key", "m-normal"), receipt("legacy", legacyRoute), receipt("dated", normalRoute),
		receipt("fast", "m-fast"), receipt("slow", "m-slow"))
	if len(days) != 1 || len(days[0].Models) != 1 {
		t.Fatalf("days = %+v", days)
	}
	d, m := days[0], days[0].Models[0]
	if m.Name != "DeepSeek V4.1 Flash" || m.Tokens != 750 || d.Tokens != 750 {
		t.Fatalf("model = %+v", m)
	}
	// Normal: (20*1 + 80*0.1 + 50*2)/1M = 0.000128 each ×3; Fast costs four times that.
	if m.CostUSD == nil || !near(*m.CostUSD, 0.000896) || d.CostUSD == nil || !near(*d.CostUSD, 0.000896) {
		t.Fatalf("cost = %v / %v", m.CostUSD, d.CostUSD)
	}
	if m.CostComplete {
		t.Fatal("the named Slow variant has no public rate, so cost is a lower bound")
	}
	if len(d.UnknownModels) != 0 {
		t.Fatalf("known but unpriced is not unknown: %v", d.UnknownModels)
	}
	if len(m.Variants) != 3 {
		t.Fatalf("variants = %+v", m.Variants)
	}
	for _, v := range m.Variants {
		switch v.Name {
		case "DeepSeek V4.1 Flash 10/17":
			if v.Tokens != 450 || v.CostUSD == nil || !near(*v.CostUSD, 0.000384) {
				t.Fatalf("normal = %+v", v)
			}
		case "DeepSeek V4.1 Flash Fast 10/17":
			if v.CostUSD == nil || !near(*v.CostUSD, 0.000512) {
				t.Fatalf("fast = %+v", v)
			}
		}
	}
}

func TestDifferentVersionsAndUnidentifiedSlugsStaySeparate(t *testing.T) {
	c := testCatalog()
	c["m-new"] = CatalogEntry{Key: "m-new", DisplayName: "DeepSeek V4.2 Flash Fast 11/01"}
	c["m-pro"] = CatalogEntry{Key: "m-pro", DisplayName: "DeepSeek V4.1 Pro"}
	c["m-sol"] = CatalogEntry{Key: "m-sol", DisplayName: "GPT-6.1 Sol"}
	c["m-terra"] = CatalogEntry{Key: "m-terra", DisplayName: "GPT-6.1 Terra"}
	var rs []Receipt
	for i, m := range []string{"m-normal", "m-new", "m-pro", "m-sol", "m-terra", "unknown/flash-fast", "fbm1.private"} {
		rs = append(rs, receipt("cloud:"+string(rune('0'+i)), m))
	}
	days := scanReceipts(t, c, testPricing(t), rs...)
	got := map[string]bool{}
	for _, m := range days[0].Models {
		got[m.Name] = true
	}
	for _, want := range []string{"DeepSeek V4.1 Flash", "DeepSeek V4.2 Flash", "DeepSeek V4.1 Pro", "GPT-6.1 Sol", "GPT-6.1 Terra", "unknown/flash-fast", "Unattributed"} {
		if !got[want] {
			t.Errorf("missing row %q in %v", want, got)
		}
	}
	if len(got) != 7 || days[0].Tokens != 1050 {
		t.Fatalf("rows=%v tokens=%d", got, days[0].Tokens)
	}
	if u := days[0].UnknownModels; len(u) != 2 {
		t.Fatalf("unknown = %v", u)
	}
}

func TestSameNamedEntriesDoNotMakeRouteDisplayAmbiguous(t *testing.T) {
	c := testCatalog()
	c["m-copy"] = CatalogEntry{Key: "m-copy", DisplayName: "DeepSeek V4.1 Flash 10/17"}
	p := testPricing(t)
	r := NewIdentity(c, p).Resolve(normalRoute)
	if r.DisplayName != "DeepSeek V4.1 Flash 10/17" || r.Group != "DeepSeek V4.1 Flash" {
		t.Fatalf("resolved = %+v", r)
	}
}

func TestAmbiguousLegacyDigestAssignsNothing(t *testing.T) {
	c := testCatalog()
	c["m-collision"] = CatalogEntry{Key: "m-collision", DisplayName: "Different Model", LegacyDigests: []string{"1e303ac563a6f9cc"}}
	r := NewIdentity(c, pricing.NewSnapshot(nil, nil)).Resolve(legacyRoute)
	if r.DisplayName != "" || r.Route != "" {
		t.Fatalf("resolved = %+v", r)
	}
}

func TestMissingLegacyLabelsAreExplainedLowerBound(t *testing.T) {
	days := scanReceipts(t, testCatalog(), testPricing(t),
		receipt("old:missing", "m-retired"), receipt("old:missing2", "fbm1.expired"), receipt("cloud:known", "m-normal"))
	d := days[0]
	if d.Tokens != 450 || d.CostComplete || len(d.UnknownModels) != 0 {
		t.Fatalf("day = %+v", d)
	}
	for _, m := range d.Models {
		if m.Name == OlderUsageName && m.Tokens == 300 {
			return
		}
	}
	t.Fatalf("no %q row of 300 tokens: %+v", OlderUsageName, d.Models)
}

func TestInvalidCountsFailInsteadOfUndercounting(t *testing.T) {
	r := receipt("bad", "m-normal")
	cached := int64(500) // more cached than input
	r.Metrics.Usage.CachedInputTokens = &cached
	s := &DesktopScanner{Roots: []string{t.TempDir()}}
	if _, err := s.Scan(context.Background(), testNow, testPricing(t), testCatalog(), []Receipt{r}); err != ErrLocalHistory {
		t.Fatalf("err = %v, want ErrLocalHistory", err)
	}
}

func TestFutureAndPlaceholderReceipts(t *testing.T) {
	future := receipt("cloud:future", "m-normal")
	future.TimestampMs = float64(testNow.Add(time.Hour).UnixMilli())
	placeholder := Receipt{ID: "cloud:gap", TimestampMs: float64(testNow.Add(-time.Minute).UnixMilli())}
	days := scanReceipts(t, testCatalog(), testPricing(t), future, placeholder)
	if len(days) != 1 || days[0].Tokens != 0 || !days[0].Partial {
		t.Fatalf("days = %+v", days)
	}
}
