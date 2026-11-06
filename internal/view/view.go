// Package view turns a tracker snapshot into short text lines shared by the CLI and the tray.
package view

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tverma101/FreeTokens/internal/freebuff"
	"github.com/tverma101/FreeTokens/internal/tracker"
	"github.com/tverma101/FreeTokens/internal/usage"
)

// Tokens formats a token count compactly (950, 12.3K, 4.56M, 1.2B).
func Tokens(n int64) string {
	f := float64(n)
	switch {
	case f >= 1e9:
		return trim(f/1e9, 2) + "B"
	case f >= 1e6:
		return trim(f/1e6, 2) + "M"
	case f >= 1e3:
		return trim(f/1e3, 1) + "K"
	}
	return fmt.Sprintf("%d", n)
}

func trim(v float64, places int) string {
	s := fmt.Sprintf("%.*f", places, v)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// USD formats dollars; lowerBound prefixes "≥" when some usage is unpriced or incomplete.
func USD(v float64, lowerBound bool) string {
	prefix := "~"
	if lowerBound {
		prefix = "≥"
	}
	if v > 0 && v < 0.01 {
		return "<$0.01"
	}
	return prefix + fmt.Sprintf("$%.2f", v)
}

// Hours formats an hour quantity.
func Hours(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-9 {
		return fmt.Sprintf("%.0f h", v)
	}
	return trim(v, 1) + " h"
}

// Duration formats a remaining time as "2h 05m", "4m" or "3d 4h".
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	days := int(d / (24 * time.Hour))
	h := int(d/time.Hour) % 24
	m := int(d/time.Minute) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// Clock is a compact countdown for the menu bar title ("1:05" or "42m").
func Clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	h, m := int(d/time.Hour), int(d/time.Minute)%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// Meter formats one quota meter.
func Meter(m freebuff.Meter, now time.Time) string {
	var s string
	switch m.Unit {
	case "hours":
		left := math.Max(0, m.Limit-m.Used)
		s = fmt.Sprintf("%s: %s left of %s", m.Label, Hours(left), Hours(m.Limit))
	case "usd":
		s = fmt.Sprintf("%s: $%.2f of $%.2f", m.Label, m.Used, m.Limit)
	default:
		s = fmt.Sprintf("%s: %s / %s sessions", m.Label, trim(m.Used, 1), trim(m.Limit, 1))
	}
	if m.ResetsAt != nil && m.ResetsAt.After(now) {
		s += " · resets in " + Duration(m.ResetsAt.Sub(now))
	}
	if m.Detail != "" {
		s += " (" + m.Detail + ")"
	}
	return s
}

// QuotaLines lists the live quota rows in display order.
func QuotaLines(q *freebuff.Quota, now time.Time) []string {
	if q == nil {
		return nil
	}
	var out []string
	for _, m := range q.Meters {
		if m.Label == "Daily Hours" {
			out = append(out, Meter(m, now))
		}
	}
	if q.HoursBalance != nil {
		out = append(out, "Hours Balance: "+Hours(*q.HoursBalance))
	}
	for _, m := range q.Meters {
		if m.Label != "Daily Hours" {
			out = append(out, Meter(m, now))
		}
	}
	if q.MonthlySpendUSD != nil && q.Meter("Monthly Spend") == nil {
		out = append(out, fmt.Sprintf("Monthly Spend: $%.2f", *q.MonthlySpendUSD))
	}
	return out
}

// Sessions lists active session countdowns.
func Sessions(q *freebuff.Quota, now time.Time) []string {
	if q == nil {
		return nil
	}
	var out []string
	n := 0
	for _, t := range q.ActiveSessions {
		if !t.After(now) {
			continue
		}
		n++
		out = append(out, fmt.Sprintf("Session %d: %s left", n, Duration(t.Sub(now))))
	}
	return out
}

// NextExpiry is the earliest future session expiry.
func NextExpiry(q *freebuff.Quota, now time.Time) (time.Time, bool) {
	if q == nil {
		return time.Time{}, false
	}
	for _, t := range q.ActiveSessions {
		if t.After(now) {
			return t, true
		}
	}
	return time.Time{}, false
}

// Period formats a period headline.
func Period(p usage.Period) string {
	s := PeriodCompact(p)
	if p.HasData && p.Partial {
		s += " (partial)"
	}
	return s
}

// PeriodCompact is Period without the partial marker, for menus that note incomplete history once.
func PeriodCompact(p usage.Period) string {
	if !p.HasData {
		return p.Label + ": no usage"
	}
	s := fmt.Sprintf("%s: %s tokens", p.Label, Tokens(p.Tokens))
	if p.CostUSD != nil {
		s += " · " + USD(*p.CostUSD, p.CostLowerBound)
	}
	return s
}

// Ago is a locale-neutral relative time ("just now", "4 min ago", "2 hr ago").
func Ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hr ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
}

// Model formats one model row.
func Model(m usage.ModelRow) string {
	s := fmt.Sprintf("%s: %s", m.Name, Tokens(m.Tokens))
	if m.CostUSD != nil {
		s += " · " + USD(*m.CostUSD, !m.CostComplete)
	} else {
		s += " · unpriced"
	}
	return s
}

// Text is the full human-readable status used by `freetokens status`.
func Text(s *tracker.Snapshot, now time.Time) string {
	var b strings.Builder
	plan := ""
	if s.Quota != nil && s.Quota.Plan != "" {
		plan = " · " + s.Quota.Plan
	}
	fmt.Fprintf(&b, "Freebuff%s\n", plan)
	if s.QuotaError != "" {
		fmt.Fprintf(&b, "  ! %s\n", s.QuotaError)
	}
	if s.Quota != nil && s.Quota.Warning != "" {
		fmt.Fprintf(&b, "  ! %s\n", s.Quota.Warning)
	}
	for _, l := range QuotaLines(s.Quota, now) {
		fmt.Fprintf(&b, "  %s\n", l)
	}
	for _, l := range Sessions(s.Quota, now) {
		fmt.Fprintf(&b, "  %s\n", l)
	}
	if len(s.Periods) > 0 {
		b.WriteString("\nTokens (estimated at public API rates)\n")
		for _, p := range s.Periods {
			fmt.Fprintf(&b, "  %s\n", Period(p))
			for _, m := range p.Models {
				fmt.Fprintf(&b, "    %s\n", Model(m))
			}
			if len(p.UnknownModels) > 0 {
				fmt.Fprintf(&b, "    unrecognized models: %s\n", strings.Join(p.UnknownModels, ", "))
			}
		}
	}
	if s.HistoryWarning != "" && s.HistoryWarning != s.QuotaError {
		fmt.Fprintf(&b, "\n! %s\n", s.HistoryWarning)
	}
	fmt.Fprintf(&b, "\nUpdated %s\n", s.RefreshedAt.Local().Format("Jan 2 15:04"))
	return b.String()
}
