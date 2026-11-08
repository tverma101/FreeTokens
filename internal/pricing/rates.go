// Package pricing estimates public API dollar costs for measured Freebuff tokens. It ports the
// subset of QuotaBar's pricing that Freebuff routes use: a curated supplement (bundled, refreshed
// from the upstream feed) and OpenRouter's public model list with its UTC weekday/time windows.
// These are public API price estimates, never Freebuff's own Freebucks charges.
package pricing

import "time"

// UTCOverride is an OpenRouter price lane selected by UTC weekday and minute of day.
type UTCOverride struct {
	Days        []string `json:"days,omitempty"`
	StartMinute *int     `json:"start,omitempty"`
	EndMinute   *int     `json:"end,omitempty"`
	Input       *float64 `json:"in,omitempty"`
	Output      *float64 `json:"out,omitempty"`
	CacheWrite  *float64 `json:"cw,omitempty"`
	CacheRead   *float64 `json:"cr,omitempty"`
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (o UTCOverride) matches(weekday, previous string, minute int) bool {
	current := o.Days == nil || contains(o.Days, weekday)
	switch {
	case o.StartMinute != nil && o.EndMinute != nil:
		s, e := *o.StartMinute, *o.EndMinute
		if s < e {
			return current && minute >= s && minute < e
		}
		if s > e {
			prev := o.Days == nil || contains(o.Days, previous)
			return (current && minute >= s) || (prev && minute < e)
		}
		return false
	case o.StartMinute != nil:
		return current && minute >= *o.StartMinute
	case o.EndMinute != nil:
		return current && minute < *o.EndMinute
	default:
		return current
	}
}

// Rates are USD per million tokens.
type Rates struct {
	Input             float64       `json:"in"`
	Output            float64       `json:"out"`
	CacheWrite        float64       `json:"cw"`
	CacheRead         float64       `json:"cr"`
	CacheReadExplicit bool          `json:"crx"`
	UTC               []UTCOverride `json:"utc,omitempty"`
}

var weekdays = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// Tokens are the separately priced buckets of one receipt.
type Tokens struct {
	Input      int64 // uncached input
	CacheWrite int64
	CacheRead  int64
	Output     int64
}

// Cost prices one aggregated receipt. Long-context tiers are never applied: a receipt aggregates
// several requests, so per-request prompt sizes are unknown.
func (r Rates) Cost(t Tokens, at time.Time) float64 {
	in, out, cw, cr := r.Input, r.Output, r.CacheWrite, r.CacheRead
	if o := r.override(at); o != nil {
		if o.Input != nil {
			in = *o.Input
		}
		if o.Output != nil {
			out = *o.Output
		}
		if o.CacheWrite != nil {
			cw = *o.CacheWrite
		}
		if o.CacheRead != nil {
			cr = *o.CacheRead
		}
	}
	if !r.CacheReadExplicit {
		cr = in
	}
	return float64(t.Input)*in/1e6 + float64(t.CacheWrite)*cw/1e6 +
		float64(t.Output)*out/1e6 + float64(t.CacheRead)*cr/1e6
}

func (r Rates) override(at time.Time) *UTCOverride {
	if len(r.UTC) == 0 || at.IsZero() {
		return nil
	}
	u := at.UTC()
	idx := int(u.Weekday())
	prev := (idx + 6) % 7
	minute := u.Hour()*60 + u.Minute()
	for i := range r.UTC {
		if r.UTC[i].matches(weekdays[idx], weekdays[prev], minute) {
			return &r.UTC[i]
		}
	}
	return nil
}
