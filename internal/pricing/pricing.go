package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Snapshot is an immutable pricing view used for one accounting pass.
type Snapshot struct {
	Supplement *Supplement
	OpenRouter map[string]Rates
	token      string
}

// NewSnapshot builds a snapshot and its content token.
func NewSnapshot(s *Supplement, openRouter map[string]Rates) *Snapshot {
	if s == nil {
		s = &Supplement{Pricing: map[string]Rates{}}
	}
	if openRouter == nil {
		openRouter = map[string]Rates{}
	}
	var b strings.Builder
	b.WriteString("pricing-v1\n")
	s.fingerprint(&b)
	keys := make([]string, 0, len(openRouter))
	for k := range openRouter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r, _ := json.Marshal(openRouter[k])
		b.WriteString("or:" + k + string(r) + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return &Snapshot{Supplement: s, OpenRouter: openRouter, token: hex.EncodeToString(sum[:])}
}

// Token changes only when prices or aliases change.
func (p *Snapshot) Token() string { return p.token }

// HasExactRoute reports whether model is itself an OpenRouter or supplement key.
func (p *Snapshot) HasExactRoute(model string) bool {
	_, a := p.OpenRouter[model]
	_, b := p.Supplement.Pricing[model]
	return a || b
}

// NormalizedKey replaces '.', '@', '_' and ' ' with '-'.
func NormalizedKey(v string) string {
	return strings.NewReplacer(".", "-", "@", "-", " ", "-", "_", "-").Replace(v)
}

func (p *Snapshot) lookup(name string) (Rates, bool) {
	key, ok := p.key(name)
	if !ok {
		return Rates{}, false
	}
	if r, ok := p.Supplement.Pricing[key]; ok {
		return r, true
	}
	return p.OpenRouter[key], true
}

// key names the catalog entry that prices name, preferring the supplement the way lookup does.
func (p *Snapshot) key(name string) (string, bool) {
	if _, ok := p.Supplement.Pricing[name]; ok {
		return name, true
	}
	if _, ok := p.OpenRouter[name]; ok {
		return name, true
	}
	if n := NormalizedKey(name); n != name {
		if _, ok := p.Supplement.Pricing[n]; ok {
			return n, true
		}
		if _, ok := p.OpenRouter[n]; ok {
			return n, true
		}
	}
	return "", false
}

// Vendor returns the vendor prefix of the catalog entry that prices model, for example "x-ai" for
// "x-ai/grok-4". Bare keys and models no catalog prices return "".
func (p *Snapshot) Vendor(model string) string {
	if p == nil || model == "" {
		return ""
	}
	if canonical, ok := p.Supplement.CanonicalName(model); ok && canonical != model {
		if key, ok := p.key(canonical); ok {
			return vendorPrefix(key)
		}
	}
	if key, ok := p.key(model); ok {
		return vendorPrefix(key)
	}
	return ""
}

func vendorPrefix(key string) string {
	if i := strings.IndexByte(key, '/'); i > 0 && i < len(key)-1 {
		return key[:i]
	}
	return ""
}

// Resolve follows QuotaBar's order for exact routes: a supplement alias's canonical key first,
// then the raw name; supplement before OpenRouter.
func (p *Snapshot) Resolve(model string) (Rates, bool) {
	if canonical, ok := p.Supplement.CanonicalName(model); ok && canonical != model {
		if r, ok := p.lookup(canonical); ok {
			return r, true
		}
	}
	return p.lookup(model)
}
