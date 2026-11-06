//go:build (darwin && cgo) || windows || linux || freebsd || openbsd || netbsd

// Package tray is the menu bar / system tray front end. Menu items are created once and updated in
// place (hidden when unused), which behaves consistently across macOS, Windows and Linux SNI hosts.
package tray

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/tverma101/FreeTokens/internal/autostart"
	"github.com/tverma101/FreeTokens/internal/browser"
	"github.com/tverma101/FreeTokens/internal/config"
	"github.com/tverma101/FreeTokens/internal/freebuff"
	"github.com/tverma101/FreeTokens/internal/icon"
	"github.com/tverma101/FreeTokens/internal/tracker"
	"github.com/tverma101/FreeTokens/internal/update"
	"github.com/tverma101/FreeTokens/internal/view"
)

const (
	repoURL       = "https://github.com/" + update.Repo
	releasesURL   = repoURL + "/releases"
	quotaSlots    = 8
	sessionSlots  = 6
	modelSlots    = 15
	updateEvery   = 24 * time.Hour
	timerInterval = 15 * time.Second
)

// Options configures the tray app.
type Options struct {
	Version string
	Tracker *tracker.Tracker
	Config  *config.Store
}

type periodItems struct {
	head   *systray.MenuItem
	models []*systray.MenuItem
	more   *systray.MenuItem
}

type app struct {
	opt Options

	mu       sync.Mutex
	snap     *tracker.Snapshot
	cfg      config.Config
	pending  *update.Info
	updating bool

	refreshCh chan bool // true = interactive
	wakeCh    chan struct{}

	header, status, warning, updated *systray.MenuItem
	usageLabel                       *systray.MenuItem
	quota                            []*systray.MenuItem
	sessions                         []*systray.MenuItem
	periods                          []periodItems
	refresh, dashboard               *systray.MenuItem
	settings                         *systray.MenuItem
	showTimers, login                *systray.MenuItem
	updOff, updNotify, updAuto       *systray.MenuItem
	intervals                        map[int]*systray.MenuItem
	checkUpdate, installUpdate       *systray.MenuItem
	about, quit                      *systray.MenuItem
	lastWarn                         *bool
}

// Run blocks until the user quits.
func Run(opt Options) {
	a := &app{opt: opt, cfg: opt.Config.Load(), refreshCh: make(chan bool, 1), wakeCh: make(chan struct{}, 1)}
	systray.Run(a.onReady, func() {})
}

func (a *app) setIcon(warn bool) {
	if a.lastWarn != nil && *a.lastWarn == warn {
		return
	}
	a.lastWarn = &warn
	t, r := icon.Tray(warn)
	systray.SetTemplateIcon(t, r)
}

func disabled(title string) *systray.MenuItem {
	it := systray.AddMenuItem(title, "")
	it.Disable()
	return it
}

func sub(parent *systray.MenuItem, title string) *systray.MenuItem {
	it := parent.AddSubMenuItem(title, "")
	it.Disable()
	return it
}

func (a *app) onReady() {
	a.setIcon(false)
	systray.SetTooltip("FreeTokens")
	a.header = disabled("Freebuff")
	a.status = disabled("Loading…")
	for i := 0; i < quotaSlots; i++ {
		it := disabled("")
		it.Hide()
		a.quota = append(a.quota, it)
	}
	for i := 0; i < sessionSlots; i++ {
		it := disabled("")
		it.Hide()
		a.sessions = append(a.sessions, it)
	}
	systray.AddSeparator()
	a.usageLabel = disabled("Token usage · cost at public API rates")
	for _, label := range []string{"Today", "Yesterday", "Last 30 Days"} {
		p := periodItems{head: systray.AddMenuItem(label, "Models used in this period")}
		for i := 0; i < modelSlots; i++ {
			it := sub(p.head, "")
			it.Hide()
			p.models = append(p.models, it)
		}
		p.more = sub(p.head, "")
		p.more.Hide()
		a.periods = append(a.periods, p)
	}
	a.warning = disabled("")
	a.warning.Hide()
	a.updated = disabled("")
	a.updated.Hide()
	systray.AddSeparator()
	a.refresh = systray.AddMenuItem("Refresh Now", "Refresh now (may ask for keychain access)")
	a.dashboard = systray.AddMenuItem("Open Freebuff Account", freebuff.DashboardURL)
	a.installUpdate = systray.AddMenuItem("", "Download, verify and install this release")
	a.installUpdate.Hide()

	a.settings = systray.AddMenuItem("Settings", "")
	// Only the macOS menu bar shows a title next to the icon; elsewhere the timer is in the tooltip.
	// (The item is not created at all: Windows re-shows hidden items when their check state changes.)
	if runtime.GOOS == "darwin" {
		a.showTimers = a.settings.AddSubMenuItemCheckbox("Show Session Timer in Menu Bar", "", a.cfg.ShowTimers)
	}
	a.login = a.settings.AddSubMenuItemCheckbox(loginLabel(), "", autostart.Enabled())
	a.settings.AddSeparator()
	sub(a.settings, "Updates from GitHub Releases")
	a.updOff = a.settings.AddSubMenuItemCheckbox("Never Check", "FreeTokens never contacts GitHub", false)
	a.updNotify = a.settings.AddSubMenuItemCheckbox("Check Daily and Notify", "", false)
	a.updAuto = a.settings.AddSubMenuItemCheckbox("Check Daily and Install", "Installs only checksum-verified releases", false)
	a.settings.AddSeparator()
	sub(a.settings, "Refresh Every")
	a.intervals = map[int]*systray.MenuItem{}
	for _, m := range []int{1, 5, 15, 30, 60} {
		a.intervals[m] = a.settings.AddSubMenuItemCheckbox(fmt.Sprintf("%d min", m), "", false)
	}
	a.checkUpdate = systray.AddMenuItem("Check for Updates…", "Ask GitHub for the newest release")
	a.about = systray.AddMenuItem("About FreeTokens ("+displayVersion(a.opt.Version)+")", repoURL)
	systray.AddSeparator()
	a.quit = systray.AddMenuItem(quitLabel(), "")
	a.syncSettings()

	go a.clicks()
	go a.refreshLoop()
	go a.timerLoop()
	go a.updateLoop()
	a.requestRefresh(false)
}

// loginLabel and quitLabel follow each platform's wording.
func loginLabel() string {
	if runtime.GOOS == "windows" {
		return "Start with Windows"
	}
	return "Start at Login"
}

func quitLabel() string {
	if runtime.GOOS == "windows" {
		return "Exit FreeTokens"
	}
	return "Quit FreeTokens"
}

func displayVersion(v string) string {
	if update.IsRelease(v) {
		return "v" + update.Normalize(v)
	}
	return "development build"
}

func setCheck(it *systray.MenuItem, on bool) {
	if on {
		it.Check()
	} else {
		it.Uncheck()
	}
}

func (a *app) syncSettings() {
	a.mu.Lock()
	c := a.cfg
	a.mu.Unlock()
	if a.showTimers != nil {
		setCheck(a.showTimers, c.ShowTimers)
	}
	setCheck(a.login, autostart.Enabled())
	setCheck(a.updOff, c.AutoUpdate == config.UpdateOff)
	setCheck(a.updNotify, c.AutoUpdate == config.UpdateNotify)
	setCheck(a.updAuto, c.AutoUpdate == config.UpdateAuto)
	for m, it := range a.intervals {
		setCheck(it, c.RefreshMinutes == m)
	}
}

func (a *app) setConfig(f func(*config.Config)) {
	c, err := a.opt.Config.Update(f)
	if err != nil {
		log.Printf("saving settings: %v", err)
	}
	a.mu.Lock()
	a.cfg = c
	a.mu.Unlock()
	a.syncSettings()
}

func (a *app) requestRefresh(interactive bool) {
	select {
	case a.refreshCh <- interactive:
	default:
	}
}

func (a *app) clicks() {
	intervalCh := make(chan int)
	for m, it := range a.intervals {
		go func(m int, it *systray.MenuItem) {
			for range it.ClickedCh {
				intervalCh <- m
			}
		}(m, it)
	}
	var timersCh chan struct{}
	if a.showTimers != nil {
		timersCh = a.showTimers.ClickedCh
	}
	for {
		select {
		case <-a.refresh.ClickedCh:
			a.requestRefresh(true)
		case <-a.dashboard.ClickedCh:
			_ = browser.Open(freebuff.DashboardURL)
		case <-a.about.ClickedCh:
			_ = browser.Open(repoURL)
		case <-timersCh:
			a.setConfig(func(c *config.Config) { c.ShowTimers = !c.ShowTimers })
			a.render()
		case <-a.login.ClickedCh:
			a.toggleLogin()
		case <-a.updOff.ClickedCh:
			a.setConfig(func(c *config.Config) { c.AutoUpdate = config.UpdateOff })
		case <-a.updNotify.ClickedCh:
			a.setConfig(func(c *config.Config) { c.AutoUpdate = config.UpdateNotify })
			a.wake()
		case <-a.updAuto.ClickedCh:
			a.setConfig(func(c *config.Config) { c.AutoUpdate = config.UpdateAuto })
			a.wake()
		case m := <-intervalCh:
			a.setConfig(func(c *config.Config) { c.RefreshMinutes = m })
			a.requestRefresh(false)
		case <-a.checkUpdate.ClickedCh:
			go a.checkForUpdates(true)
		case <-a.installUpdate.ClickedCh:
			a.mu.Lock()
			pending := a.pending
			a.mu.Unlock()
			if pending != nil {
				go a.installPending()
			} else {
				_ = browser.Open(releasesURL)
			}
		case <-a.quit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (a *app) wake() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}

func (a *app) toggleLogin() {
	var err error
	if autostart.Enabled() {
		err = autostart.Disable()
	} else {
		var entry autostart.Entry
		if entry, err = autostart.TrayEntry(); err == nil {
			err = autostart.Enable(entry)
		}
	}
	if err != nil {
		log.Printf("start at login: %v", err)
	}
	setCheck(a.login, autostart.Enabled())
}

func (a *app) refreshLoop() {
	interactive := <-a.refreshCh
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		a.mu.Lock()
		loaded := a.snap != nil
		a.mu.Unlock()
		if loaded {
			a.status.SetTitle("Refreshing…")
			a.status.Show()
		}
		snap := a.opt.Tracker.Refresh(ctx, interactive)
		cancel()
		a.mu.Lock()
		a.snap = snap
		minutes := a.cfg.RefreshMinutes
		a.mu.Unlock()
		a.render()
		timer := time.NewTimer(time.Duration(minutes) * time.Minute)
		select {
		case interactive = <-a.refreshCh:
			timer.Stop()
		case <-timer.C:
			interactive = false
		}
	}
}

func (a *app) timerLoop() {
	t := time.NewTicker(timerInterval)
	defer t.Stop()
	for range t.C {
		a.renderTimers()
	}
}

func (a *app) renderTimers() {
	a.mu.Lock()
	snap, show := a.snap, a.cfg.ShowTimers
	a.mu.Unlock()
	now := time.Now()
	var q *freebuff.Quota
	if snap != nil {
		q = snap.Quota
	}
	lines := view.Sessions(q, now)
	for i, it := range a.sessions {
		if i < len(lines) {
			it.SetTitle(lines[i])
			it.Show()
		} else {
			it.Hide()
		}
	}
	title := ""
	tooltip := []string{"FreeTokens"}
	if exp, ok := view.NextExpiry(q, now); ok {
		if show {
			title = view.Clock(exp.Sub(now))
		}
		tooltip = append(tooltip, "Session: "+view.Duration(exp.Sub(now))+" left")
	}
	if snap != nil {
		if lines := view.QuotaLines(snap.Quota, now); len(lines) > 0 {
			tooltip = append(tooltip, lines[0])
		}
		if len(snap.Periods) > 0 && snap.Periods[0].HasData {
			tooltip = append(tooltip, view.PeriodCompact(snap.Periods[0]))
		}
		footer := "Updated " + view.Ago(now.Sub(snap.RefreshedAt))
		if partial(snap) {
			footer += " · some history is incomplete"
		}
		a.updated.SetTitle(footer)
		a.updated.Show()
	}
	systray.SetTitle(title)
	// Windows truncates tray tooltips at 127 characters.
	systray.SetTooltip(shorten(strings.Join(tooltip, "\n"), 127))
}

func partial(s *tracker.Snapshot) bool {
	for _, p := range s.Periods {
		if p.HasData && p.Partial {
			return true
		}
	}
	return false
}

func (a *app) render() {
	a.mu.Lock()
	snap := a.snap
	a.mu.Unlock()
	if snap == nil {
		return
	}
	now := time.Now()
	header := "Freebuff"
	if snap.Quota != nil && snap.Quota.Plan != "" {
		header += " · " + snap.Quota.Plan
	}
	a.header.SetTitle(header)

	var problems []string
	if snap.QuotaError != "" {
		problems = append(problems, snap.QuotaError)
	}
	if snap.Quota != nil && snap.Quota.Warning != "" {
		problems = append(problems, snap.Quota.Warning)
	}
	if len(problems) > 0 {
		a.status.SetTitle("⚠ " + shorten(problems[0], 90))
		a.status.SetTooltip(strings.Join(problems, "\n"))
		a.status.Show()
	} else {
		a.status.Hide()
	}
	a.setIcon(snap.QuotaError != "")

	lines := view.QuotaLines(snap.Quota, now)
	for i, it := range a.quota {
		if i < len(lines) {
			it.SetTitle(lines[i])
			it.Show()
		} else {
			it.Hide()
		}
	}
	for i, p := range a.periods {
		if i >= len(snap.Periods) {
			p.head.Hide()
			continue
		}
		per := snap.Periods[i]
		p.head.SetTitle(view.PeriodCompact(per))
		p.head.Show()
		for j, it := range p.models {
			if j < len(per.Models) {
				it.SetTitle(view.Model(per.Models[j]))
				it.Show()
			} else {
				it.Hide()
			}
		}
		if extra := len(per.Models) - len(p.models); extra > 0 {
			p.more.SetTitle(fmt.Sprintf("…and %d more (run freetokens status)", extra))
			p.more.Show()
		} else if len(per.Models) == 0 {
			p.more.SetTitle("No measured usage")
			p.more.Show()
		} else {
			p.more.Hide()
		}
	}
	if snap.HistoryWarning != "" && snap.HistoryWarning != snap.QuotaError {
		a.warning.SetTitle("⚠ " + shorten(snap.HistoryWarning, 90))
		a.warning.SetTooltip(snap.HistoryWarning)
		a.warning.Show()
	} else {
		a.warning.Hide()
	}
	a.usageLabel.SetTooltip(snap.SourceNote)
	a.updated.SetTooltip("Last refreshed " + snap.RefreshedAt.Local().Format("Jan 2, 15:04") + ". " + snap.SourceNote)
	a.renderTimers()
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// updateLoop performs the opt-in daily check. With "off" it never contacts GitHub.
func (a *app) updateLoop() {
	for {
		a.mu.Lock()
		c := a.cfg
		a.mu.Unlock()
		wait := updateEvery
		if c.AutoUpdate != config.UpdateOff && update.IsRelease(a.opt.Version) {
			if since := time.Since(c.LastUpdateCheck); since >= updateEvery || since < 0 {
				a.checkForUpdates(false)
			} else {
				wait = updateEvery - since
			}
		}
		select {
		case <-time.After(wait):
		case <-a.wakeCh:
		}
	}
}

func (a *app) checkForUpdates(manual bool) {
	if manual {
		a.checkUpdate.SetTitle("Checking for Updates…")
		a.checkUpdate.Disable()
		defer func() {
			a.checkUpdate.Enable()
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	info, err := update.Check(ctx, a.opt.Version)
	a.setConfig(func(c *config.Config) { c.LastUpdateCheck = time.Now() })
	if err != nil {
		log.Printf("update check: %v", err)
		if manual {
			a.checkUpdate.SetTitle("Update Check Failed — Try Again")
		}
		return
	}
	if !info.Available {
		if manual {
			msg := "FreeTokens Is Up to Date"
			if !update.IsRelease(a.opt.Version) {
				msg = "Development Build — Latest Release v" + info.Latest
			}
			a.checkUpdate.SetTitle(msg)
		}
		return
	}
	a.mu.Lock()
	a.pending = info
	auto := a.cfg.AutoUpdate == config.UpdateAuto
	a.mu.Unlock()
	a.installUpdate.SetTitle("Install FreeTokens v" + info.Latest)
	a.installUpdate.Show()
	a.checkUpdate.SetTitle("v" + info.Latest + " Available — Release Notes")
	if auto && !manual {
		a.installPending()
	} else if manual {
		_ = browser.Open(info.URL)
	}
}

func (a *app) installPending() {
	a.mu.Lock()
	info := a.pending
	if a.updating || info == nil {
		a.mu.Unlock()
		return
	}
	a.updating = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.updating = false
		a.mu.Unlock()
	}()
	a.installUpdate.SetTitle("Installing v" + info.Latest + "…")
	a.installUpdate.Disable()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := update.Apply(ctx, info); err != nil {
		log.Printf("update: %v", err)
		a.installUpdate.SetTitle("Update Failed — Open Releases Page")
		a.installUpdate.Enable()
		a.mu.Lock()
		a.pending = nil
		a.mu.Unlock()
		return
	}
	if err := update.Restart(); err != nil {
		log.Printf("restart after update: %v", err)
		a.installUpdate.SetTitle("Updated — Quit and Reopen FreeTokens")
		return
	}
	systray.Quit()
}
