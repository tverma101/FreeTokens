// Package tracker orchestrates one Freebuff refresh: live quota, hosted receipts, model catalog,
// public pricing and Desktop history, mirroring QuotaBar's FreebuffProvider.refresh.
package tracker

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/tverma101/FreeTokens/internal/freebuff"
	"github.com/tverma101/FreeTokens/internal/pricing"
	"github.com/tverma101/FreeTokens/internal/usage"
)

// HistorySourceNote explains what the token and dollar figures mean.
const HistorySourceNote = "Measured Freebuff hosted runs and legacy Desktop tokens on this computer; dollars are estimated at public API rates, not Freebuff charges."

// Snapshot is one refresh result.
type Snapshot struct {
	RefreshedAt time.Time       `json:"refreshedAt"`
	Quota       *freebuff.Quota `json:"quota,omitempty"`
	// QuotaError is the live quota failure, if any.
	QuotaError string `json:"quotaError,omitempty"`
	// QuotaErrorCode classifies QuotaError for UIs: "signed-out", "expired", "approval" (the OS
	// needs the user to allow keychain access), "network" or "other".
	QuotaErrorCode string         `json:"quotaErrorCode,omitempty"`
	HistoryWarning string         `json:"historyWarning,omitempty"`
	Periods        []usage.Period `json:"periods,omitempty"`
	Days           []usage.Day    `json:"days,omitempty"`
	// Observation is the allowlisted numeric projection of the last session body (diagnostics only).
	Observation *freebuff.Observation `json:"observation,omitempty"`
	SourceNote  string                `json:"sourceNote"`
}

// Tracker holds caches across refreshes. Methods are safe for concurrent use; refreshes serialize.
type Tracker struct {
	Auth    freebuff.AuthStore
	Client  *freebuff.Client
	Hosted  *freebuff.HostedScanner
	Catalog *freebuff.ModelCatalog
	Pricing *pricing.Store
	Desktop *freebuff.DesktopScanner
	Now     func() time.Time

	mu       sync.Mutex
	loggedMu sync.Mutex
	logged   map[string]bool
}

// New builds a tracker with caches under cacheDir.
func New(cacheDir, userAgent string, tz func() string) *Tracker {
	return &Tracker{
		Auth:    freebuff.DefaultAuthStore(),
		Client:  freebuff.NewClient(userAgent, tz),
		Hosted:  &freebuff.HostedScanner{CachePath: filepath.Join(cacheDir, "freebuff-hosted-usage.json")},
		Catalog: &freebuff.ModelCatalog{CachePath: filepath.Join(cacheDir, "freebuff-models.json")},
		Pricing: pricing.NewStore(filepath.Join(cacheDir, "pricing"), userAgent),
		Desktop: &freebuff.DesktopScanner{Root: freebuff.DesktopProjectsDir()},
		Now:     time.Now,
	}
}

// logOnce logs a failure once until clear(key) is called, so a persistent outage doesn't spam.
func (t *Tracker) logOnce(key, format string, args ...any) {
	t.loggedMu.Lock()
	defer t.loggedMu.Unlock()
	if t.logged == nil {
		t.logged = map[string]bool{}
	}
	if !t.logged[key] {
		t.logged[key] = true
		log.Printf(format, args...)
	}
}

func (t *Tracker) clear(key string) {
	t.loggedMu.Lock()
	delete(t.logged, key)
	t.loggedMu.Unlock()
}

// Refresh runs one full refresh. interactive allows an OS credential prompt (manual refresh only).
func (t *Tracker) Refresh(ctx context.Context, interactive bool) *Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.Now()
	snap := &Snapshot{RefreshedAt: now, SourceNote: HistorySourceNote}

	auth, authErr := t.Auth.Load(interactive)
	if auth == nil && authErr == nil {
		authErr = freebuff.ErrNotLoggedIn
	}
	if auth != nil {
		t.refreshQuota(ctx, *auth, snap)
	} else {
		snap.setQuotaError(authErr)
	}
	if ctx.Err() != nil {
		return snap
	}

	if err := t.Pricing.Refresh(ctx, now, false); err != nil {
		t.logOnce("pricing", "pricing feed refresh failed: %v", err)
	} else {
		t.clear("pricing")
	}

	var hosted []freebuff.Receipt
	incomplete := auth == nil
	if auth == nil {
		snap.HistoryWarning = authErr.Error()
	} else {
		var err error
		hosted, err = t.Hosted.Scan(ctx, *auth, t.Client, now)
		if err != nil {
			if ctx.Err() != nil {
				return snap
			}
			t.logOnce("hosted", "hosted token history read failed: %v", err)
			hosted = t.Hosted.Cached(*auth)
			incomplete = true
			snap.HistoryWarning = "Freebuff's hosted token history couldn't be refreshed. Totals may be incomplete."
			if errors.Is(err, freebuff.ErrTokenInvalid) {
				snap.HistoryWarning = err.Error()
			}
		} else {
			t.clear("hosted")
		}
		if len(hosted) > 0 || t.Desktop.HasHistory() {
			if err := t.Catalog.Refresh(ctx, *auth, t.Client, now); err != nil {
				if ctx.Err() != nil {
					return snap
				}
				t.logOnce("catalog", "model catalog refresh failed: %v", err)
				if snap.HistoryWarning == "" {
					snap.HistoryWarning = "Freebuff model names couldn't be refreshed. Saved model data is still available."
				}
			} else {
				t.clear("catalog")
			}
		}
	}

	fold, err := t.Desktop.Scan(ctx, now, t.Pricing.Current(), t.Catalog.Current(), hosted)
	if err != nil {
		if ctx.Err() != nil {
			return snap
		}
		t.logOnce("history", "token history read failed: %v", err)
		snap.HistoryWarning = freebuff.ErrLocalHistory.Error()
		return snap
	}
	t.clear("history")
	days := fold.Days
	first := usage.DayKey(now.AddDate(0, 0, -29))
	today := usage.DayKey(now)
	for _, d := range days {
		if d.Partial && d.Date >= first && d.Date <= today {
			if snap.HistoryWarning == "" {
				snap.HistoryWarning = "Some Freebuff runs have no complete token receipt. Totals include only measured usage."
			}
			break
		}
	}
	if incomplete {
		for i := range days {
			days[i].Partial = true
		}
	}
	snap.Days = days
	snap.Periods = usage.StandardPeriods(days, now)
	return snap
}

func (t *Tracker) refreshQuota(ctx context.Context, auth freebuff.Auth, snap *Snapshot) {
	resp, err := t.Client.FetchSession(ctx, auth)
	if err != nil {
		t.logOnce("quota", "usage refresh failed: %v", err)
		snap.setQuotaError(freebuff.ErrConnection)
		return
	}
	switch {
	case resp.Status == 401:
		snap.setQuotaError(freebuff.ErrTokenInvalid)
		return
	case (resp.Status < 200 || resp.Status >= 300) && resp.Status != 404:
		t.logOnce("quota", "usage refresh failed: HTTP %d", resp.Status)
		snap.setQuotaError(freebuff.RequestFailed{Status: resp.Status})
		return
	}
	q, err := freebuff.MapQuota(resp.Status, resp.Body)
	if err != nil {
		t.logOnce("quota", "usage refresh failed: %v", err)
		snap.setQuotaError(err)
		return
	}
	t.clear("quota")
	obs := freebuff.Observe(resp.Body)
	snap.Quota, snap.Observation = q, &obs
}

func (s *Snapshot) setQuotaError(err error) {
	s.QuotaError = err.Error()
	s.QuotaErrorCode = ErrorCode(err)
}

// ErrorCode classifies a quota or auth error (see Snapshot.QuotaErrorCode).
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, freebuff.ErrNotLoggedIn), errors.Is(err, freebuff.ErrMissingDefaultProfile),
		errors.Is(err, freebuff.ErrInvalidCredentials):
		return "signed-out"
	case errors.Is(err, freebuff.ErrTokenInvalid):
		return "expired"
	case errors.Is(err, freebuff.ErrPermissionRequired):
		return "approval"
	case errors.Is(err, freebuff.ErrConnection):
		return "network"
	}
	return "other"
}
