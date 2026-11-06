package freebuff

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	DayMs            = int64(24 * time.Hour / time.Millisecond)
	WeekMs           = 7 * DayMs
	FiveDayMs        = 5 * DayMs
	FreebucksPerHour = 5.0
)

var (
	ErrConnection      = errors.New("Couldn't reach Freebuff. Check your connection and try again")
	ErrInvalidResponse = errors.New("Freebuff returned unreadable usage data. Try again later")
	ErrLocalHistory    = errors.New("FreeTokens couldn't read Freebuff Desktop's measured token history. Refresh again after the current turn finishes")
)

// RequestFailed is a non-2xx, non-401 HTTP status.
type RequestFailed struct{ Status int }

func (e RequestFailed) Error() string {
	return fmt.Sprintf("Freebuff usage request failed (HTTP %d)", e.Status)
}

// Meter is a used/limit progress row.
type Meter struct {
	Label    string     `json:"label"`
	Used     float64    `json:"used"`
	Limit    float64    `json:"limit"`
	Unit     string     `json:"unit"` // "hours", "sessions" or "usd"
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
	PeriodMs int64      `json:"periodMs,omitempty"`
	Detail   string     `json:"detail,omitempty"`
}

// Quota is the mapped read-only session endpoint.
type Quota struct {
	Status string `json:"status"`
	Plan   string `json:"plan,omitempty"`
	// Meters holds Daily Hours, legacy session windows and Monthly Spend (when it has a limit).
	Meters []Meter `json:"meters"`
	// HoursBalance is the separate wallet in whole hours; nil means unavailable (never zero-filled).
	HoursBalance *float64 `json:"hoursBalance"`
	// MonthlySpendUSD is set only when the server reports actual USD.
	MonthlySpendUSD *float64 `json:"monthlySpendUsd,omitempty"`
	// ActiveSessions are occupied session expiries, earliest first.
	ActiveSessions []time.Time `json:"activeSessions"`
	Warning        string      `json:"warning,omitempty"`
}

// Meter returns the named meter.
func (q *Quota) Meter(label string) *Meter {
	for i := range q.Meters {
		if q.Meters[i].Label == label {
			return &q.Meters[i]
		}
	}
	return nil
}

// Observation keeps only allowlisted numbers for diagnostics.
type Observation struct {
	CombinedFreebucks       *float64 `json:"combinedFreebucks"`
	DailyLimitFreebucks     *float64 `json:"dailyLimitFreebucks"`
	DailySpentFreebucks     *float64 `json:"dailySpentFreebucks"`
	DailyRemainingFreebucks *float64 `json:"dailyRemainingFreebucks"`
	WalletFreebucks         *float64 `json:"walletFreebucks"`
}

func ptr(v any) *float64 {
	if f, ok := number(v); ok {
		return &f
	}
	return nil
}

// Observe extracts the allowlisted numeric projection from a session response body.
func Observe(body []byte) Observation {
	root, _ := decodeObject(body)
	bucks, _ := obj(root["freebucks"])
	daily, _ := obj(bucks["daily"])
	wallet, _ := obj(bucks["wallet"])
	return Observation{ptr(bucks["balance"]), ptr(daily["limit"]), ptr(daily["spent"]), ptr(daily["remaining"]), ptr(wallet["balance"])}
}

// MapQuota ports QuotaBar's FreebuffUsageMapper. The current hours meter supersedes legacy session
// caps; wire values are Freebucks and five Freebucks are one hour.
func MapQuota(status int, body []byte) (*Quota, error) {
	var root map[string]any
	if status == 404 {
		root = map[string]any{"status": "none"}
	} else {
		var ok bool
		if root, ok = decodeObject(body); !ok {
			return nil, ErrInvalidResponse
		}
	}
	st := str(root["status"])
	if st == "" {
		return nil, ErrInvalidResponse
	}
	q := &Quota{Status: st, ActiveSessions: []time.Time{}}
	subscription, _ := obj(root["subscription"])
	subUsage, _ := obj(subscription["usage"])
	freeWindows, hasFreeWindows := obj(root["freeWindows"])
	rawBucks, hasBucksKey := root["freebucks"]
	bucks, bucksIsObject := obj(rawBucks)
	if hasBucksKey && rawBucks != nil && !bucksIsObject {
		return nil, ErrInvalidResponse
	}
	if hasBucksKey && rawBucks == nil {
		q.Warning = "Freebuff couldn't refresh its hours balances. Refresh again to get current hours."
	}

	add := func(label string, used, limit any, unit string, reset any, period int64, detail string) {
		u, ok1 := nonnegative(used)
		l, ok2 := nonnegative(limit)
		if !ok1 || !ok2 {
			return
		}
		m := Meter{Label: label, Used: u, Limit: l, Unit: unit, PeriodMs: period, Detail: detail}
		if t, ok := ParseTime(reset); ok {
			m.ResetsAt = &t
		}
		q.Meters = append(q.Meters, m)
	}

	switch {
	case hasBucksKey:
		// Legacy windows no longer describe the budget when the hours meter exists.
	case subUsage != nil:
		add("Daily", subUsage["dayUsed"], subUsage["dayLimit"], "sessions", subUsage["dayResetAt"], DayMs, "")
		add("5-Day", subUsage["fiveDayUsed"], subUsage["fiveDayLimit"], "sessions", nil, FiveDayMs, "")
		add("Monthly", subUsage["monthUsed"], subUsage["monthLimit"], "sessions", subUsage["periodEndsAt"], 0, "")
		add("Premium Sessions", subUsage["dayPremiumUsed"], subUsage["dayPremiumLimit"], "sessions", subUsage["dayResetAt"], DayMs, "Luna / DeepSeek V4 Pro")
		add("Free Premium Sessions", subUsage["freeDayUsed"], subUsage["freeDayLimit"], "sessions", subUsage["dayResetAt"], DayMs, "")
	case hasFreeWindows:
		add("Daily", freeWindows["dayUsed"], freeWindows["dayLimit"], "sessions", freeWindows["dayResetAt"], DayMs, "")
		add("Weekly", freeWindows["weekUsed"], freeWindows["weekLimit"], "sessions", nil, WeekMs, "")
		add("Monthly", freeWindows["monthUsed"], freeWindows["monthLimit"], "sessions", freeWindows["monthResetAt"], 0, "")
	default:
		models, _ := obj(root["rateLimitsByModel"])
		if pool := mostUsedPool(models); pool != nil {
			detail := ""
			if pool.label != "" {
				detail = "Most used pool: " + pool.label
			}
			add("Session Pool", pool.recent, pool.limit, "sessions", pool.reset, pool.period, detail)
		}
	}

	if daily, ok := obj(bucks["daily"]); ok {
		spent, ok1 := nonnegative(daily["spent"])
		limit, ok2 := nonnegative(daily["limit"])
		if !ok1 || !ok2 {
			return nil, ErrInvalidResponse
		}
		if r, present := daily["resetAt"]; present && r != nil {
			if _, ok := ParseTime(r); !ok {
				return nil, ErrInvalidResponse
			}
		}
		add("Daily Hours", spent/FreebucksPerHour, limit/FreebucksPerHour, "hours", daily["resetAt"], DayMs, "")
	} else if bucksIsObject {
		return nil, ErrInvalidResponse
	}
	if bucksIsObject {
		var wallet float64
		if rawWallet, present := bucks["wallet"]; present {
			w, ok := obj(rawWallet)
			if !ok {
				return nil, ErrInvalidResponse
			}
			b, ok := nonnegative(w["balance"])
			if !ok {
				return nil, ErrInvalidResponse
			}
			wallet = b
		} else {
			// Older servers supplied only the combined total; remove the daily remainder.
			total, ok := nonnegative(bucks["balance"])
			daily, ok2 := obj(bucks["daily"])
			if !ok || !ok2 {
				return nil, ErrInvalidResponse
			}
			spent, ok3 := nonnegative(daily["spent"])
			limit, ok4 := nonnegative(daily["limit"])
			if !ok3 || !ok4 {
				return nil, ErrInvalidResponse
			}
			remaining, ok := nonnegative(daily["remaining"])
			if !ok {
				remaining = math.Max(0, limit-spent)
			}
			if total+1e-9 < remaining {
				return nil, ErrInvalidResponse
			}
			wallet = math.Max(0, total-remaining)
		}
		hours := math.Floor(wallet/FreebucksPerHour + 1e-9)
		q.HoursBalance = &hours
	}

	monthly, _ := obj(bucks["monthly"])
	spentRaw, limitRaw, resetRaw := firstPresent(monthly["spentUsd"], subUsage["monthSpendUsd"]), firstPresent(monthly["limitUsd"], subUsage["monthSpendLimitUsd"]), firstPresent(monthly["resetAt"], subUsage["periodEndsAt"])
	if spent, ok := nonnegative(spentRaw); ok {
		if limit, ok := number(limitRaw); ok && limit > 0 {
			add("Monthly Spend", spent, limit, "usd", resetRaw, 0, "")
		}
		q.MonthlySpendUSD = &spent
	}

	expiries, err := sessionExpiries(root)
	if err != nil {
		return nil, err
	}
	q.ActiveSessions = expiries
	if tier := str(subscription["tierId"]); tier != "" {
		q.Plan = tier
	} else if access := str(root["accessTier"]); access != "" {
		q.Plan = strings.ToUpper(access[:1]) + access[1:]
	}
	return q, nil
}

func firstPresent(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

func sessionExpiries(root map[string]any) ([]time.Time, error) {
	sessions := map[string]time.Time{}
	if str(root["status"]) == "active" {
		t, ok := ParseTime(root["expiresAt"])
		if !ok {
			return nil, ErrInvalidResponse
		}
		id := str(root["instanceId"])
		if id == "" {
			id = "single"
		}
		sessions[id] = t
	}
	if raw, present := root["desktopPurchases"]; present && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return nil, ErrInvalidResponse
		}
		for _, item := range list {
			p, ok := obj(item)
			if !ok {
				return nil, ErrInvalidResponse
			}
			holder := str(p["holderInstanceId"])
			if holder == "" {
				continue // unoccupied purchases are reusable hours, not running sessions
			}
			t, ok := ParseTime(p["expiresAt"])
			if !ok {
				return nil, ErrInvalidResponse
			}
			sessions[holder] = t
		}
	}
	out := make([]time.Time, 0, len(sessions))
	for _, t := range sessions {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

type pool struct {
	key, label    string
	limit, recent float64
	reset         any
	period        int64
}

func mostUsedPool(models map[string]any) *pool {
	pools := map[string]*pool{}
	for model, raw := range models {
		v, ok := obj(raw)
		if !ok {
			continue
		}
		limit, ok1 := number(v["limit"])
		recent, ok2 := number(v["recentCount"])
		if !ok1 || limit <= 0 || !ok2 || recent < 0 {
			continue
		}
		key := str(v["pool"])
		if key == "" {
			key = "model:" + model
		}
		var period int64
		switch str(v["period"]) {
		case "pacific_day":
			period = DayMs
		case "pacific_week":
			period = WeekMs
		case "pacific_month":
			period = 30 * DayMs
		}
		c := &pool{key: key, label: str(v["poolLabel"]), limit: limit, recent: recent, reset: v["resetAt"], period: period}
		if e := pools[key]; e == nil || c.recent/c.limit > e.recent/e.limit {
			pools[key] = c
		}
	}
	var best *pool
	for _, p := range pools {
		if best == nil || p.recent/p.limit > best.recent/best.limit || (p.recent/p.limit == best.recent/best.limit && p.key < best.key) {
			best = p
		}
	}
	return best
}
