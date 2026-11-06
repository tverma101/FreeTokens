package pricing

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

//go:embed pricing_supplement.json
var bundledSupplement []byte

type aliasRule struct {
	pattern   string
	re        *regexp.Regexp
	canonical string
}

// Supplement is the curated pricing feed shared with QuotaBar/OpenUsage.
type Supplement struct {
	Pricing        map[string]Rates
	aliases        []aliasRule
	retiredKeys    map[string]bool
	retiredAliases map[string]bool
	UpdatedAt      string
}

type supplementFile struct {
	UpdatedAt string `json:"updated_at"`
	Pricing   map[string]struct {
		Input      float64  `json:"input_per_million"`
		Output     float64  `json:"output_per_million"`
		CacheWrite *float64 `json:"cache_write_per_million"`
		CacheRead  *float64 `json:"cache_read_per_million"`
	} `json:"pricing"`
	AliasRules []struct {
		Pattern   string `json:"pattern"`
		Canonical string `json:"canonical"`
	} `json:"alias_rules"`
	RetiredPricingKeys   []string `json:"retired_pricing_keys"`
	RetiredAliasPatterns []string `json:"retired_alias_patterns"`
}

// DecodeSupplement parses the feed. Patterns RE2 cannot compile (lookaround) are skipped and logged;
// none of them are Freebuff routes.
func DecodeSupplement(data []byte) (*Supplement, error) {
	var f supplementFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	s := &Supplement{Pricing: map[string]Rates{}, retiredKeys: map[string]bool{}, retiredAliases: map[string]bool{}, UpdatedAt: f.UpdatedAt}
	for k, e := range f.Pricing {
		r := Rates{Input: e.Input, Output: e.Output, CacheWrite: e.Input, CacheRead: e.Input}
		if e.CacheWrite != nil {
			r.CacheWrite = *e.CacheWrite
		}
		if e.CacheRead != nil {
			r.CacheRead, r.CacheReadExplicit = *e.CacheRead, true
		}
		s.Pricing[k] = r
	}
	for _, rule := range f.AliasRules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue // RE2 has no lookaround; such rules cover non-Freebuff models (see README)
		}
		s.aliases = append(s.aliases, aliasRule{rule.Pattern, re, rule.Canonical})
	}
	for _, k := range f.RetiredPricingKeys {
		s.retiredKeys[k] = true
	}
	for _, p := range f.RetiredAliasPatterns {
		s.retiredAliases[p] = true
	}
	return s, nil
}

// BundledSupplement is the snapshot compiled into the binary.
func BundledSupplement() *Supplement {
	s, err := DecodeSupplement(bundledSupplement)
	if err != nil {
		panic("bundled pricing supplement is invalid: " + err.Error())
	}
	return s
}

// CanonicalName applies alias rules in order; first match wins.
func (s *Supplement) CanonicalName(model string) (string, bool) {
	if s == nil {
		return "", false
	}
	for _, r := range s.aliases {
		if r.re.MatchString(model) {
			return r.canonical, true
		}
	}
	return "", false
}

// Merging overlays other on s: other wins per key, base-only keys survive, retirements apply.
func (s *Supplement) Merging(other *Supplement) *Supplement {
	out := &Supplement{Pricing: map[string]Rates{}, retiredKeys: map[string]bool{}, retiredAliases: map[string]bool{}, UpdatedAt: s.UpdatedAt}
	if other.UpdatedAt > out.UpdatedAt {
		out.UpdatedAt = other.UpdatedAt
	}
	for k, v := range s.Pricing {
		out.Pricing[k] = v
	}
	for k, v := range other.Pricing {
		out.Pricing[k] = v
	}
	for k := range s.retiredKeys {
		out.retiredKeys[k] = true
	}
	for k := range other.retiredKeys {
		out.retiredKeys[k] = true
	}
	for k := range other.Pricing {
		if !other.retiredKeys[k] {
			delete(out.retiredKeys, k)
		}
	}
	for k := range out.retiredKeys {
		delete(out.Pricing, k)
	}
	for k := range s.retiredAliases {
		out.retiredAliases[k] = true
	}
	for k := range other.retiredAliases {
		out.retiredAliases[k] = true
	}
	seen := map[string]bool{}
	for _, r := range other.aliases {
		seen[r.pattern] = true
		out.aliases = append(out.aliases, r)
	}
	for _, r := range s.aliases {
		if !seen[r.pattern] {
			out.aliases = append(out.aliases, r)
		}
	}
	kept := out.aliases[:0]
	for _, r := range out.aliases {
		if !out.retiredAliases[r.pattern] {
			kept = append(kept, r)
		}
	}
	out.aliases = kept
	return out
}

// fingerprint changes only when rates or aliases change.
func (s *Supplement) fingerprint(b *strings.Builder) {
	keys := make([]string, 0, len(s.Pricing))
	for k := range s.Pricing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r, _ := json.Marshal(s.Pricing[k])
		b.WriteString(k + string(r) + "\n")
	}
	for _, a := range s.aliases {
		b.WriteString("alias:" + a.pattern + "->" + a.canonical + "\n")
	}
}
