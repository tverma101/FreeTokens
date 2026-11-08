//go:build gui && (windows || (linux && cgo))

package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"os"
	goruntime "runtime"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/tverma101/FreeTokens/internal/autostart"
	"github.com/tverma101/FreeTokens/internal/browser"
	"github.com/tverma101/FreeTokens/internal/config"
	"github.com/tverma101/FreeTokens/internal/freebuff"
	"github.com/tverma101/FreeTokens/internal/icon"
	"github.com/tverma101/FreeTokens/internal/tracker"
	"github.com/tverma101/FreeTokens/internal/update"
	"github.com/tverma101/FreeTokens/internal/view"
)

//go:embed all:web/dist
var assets embed.FS

// hasPopover reports whether this build embeds the Wails popover instead of only the tray menu.
const hasPopover = true

const (
	windowWidth  = 380
	windowHeight = 560
	// hideDelay lets a tray click cancel the blur that the click itself causes.
	hideDelay = 220 * time.Millisecond
)

func runGUIUI(t *tracker.Tracker, cfg *config.Store) {
	a := &App{tracker: t, cfg: cfg, version: version, wake: make(chan struct{}, 1)}
	opts := &options.App{
		Title:            "FreeTokens",
		Width:            windowWidth,
		Height:           windowHeight,
		Frameless:        true,
		DisableResize:    true,
		AlwaysOnTop:      true,
		StartHidden:      true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        a.startup,
		OnDomReady:       a.domReady,
		OnBeforeClose:    a.beforeClose,
		OnShutdown:       a.shutdown,
		Bind:             []interface{}{a},
	}
	applyPlatformOptions(opts)
	// macOS owns one AppKit event loop and Wails runs it on the main thread. systray.Run would
	// start a second [NSApp run] from a background goroutine, so on macOS the tray joins Wails'
	// loop and its menu bar item is created in domReady, on the main thread.
	if goruntime.GOOS == "darwin" {
		a.startTray, _ = systray.RunWithExternalLoop(a.trayReady, nil)
	}
	if err := wails.Run(opts); err != nil {
		log.Printf("gui: %v", err)
		fmt.Fprintln(os.Stderr, "FreeTokens:", err)
	}
}

// App is the Wails bridge. The tracker serializes refreshes; this type serializes the
// snapshot that the frontend reads and the window state that the tray toggles.
type App struct {
	ctx     context.Context
	tracker *tracker.Tracker
	cfg     *config.Store
	version string

	mu         sync.Mutex
	snap       *tracker.Snapshot
	refreshing bool
	visible    bool
	quitting   bool
	hideTimer  *time.Timer
	showItem   *systray.MenuItem
	wake       chan struct{}

	// startTray creates the native tray on platforms where it can't own the event loop.
	startTray func()
	trayOnce  sync.Once
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.startTray == nil {
		go a.tray()
	}
	go a.refreshLoop()
	go a.refresh(false)
}

// domReady places the hidden popover before the first paint, so it never flashes
// in the middle of the screen.
func (a *App) domReady(ctx context.Context) {
	// macOS: the tray joins this AppKit loop, so it is added here on the main thread.
	a.trayOnce.Do(func() {
		if a.startTray != nil {
			a.startTray()
		}
	})
	x, y := windowPosition(ctx)
	runtime.WindowSetPosition(ctx, x, y)
	runtime.WindowShow(ctx)
	a.setVisible(true)
}

func (a *App) shutdown(context.Context) {
	systray.Quit()
}

// beforeClose keeps the process in the tray instead of ending it.
func (a *App) beforeClose(context.Context) bool {
	a.mu.Lock()
	quitting := a.quitting
	a.mu.Unlock()
	if quitting {
		return false
	}
	a.hide()
	return true
}

func (a *App) tray() {
	systray.Run(a.trayReady, func() {})
}

// trayReady builds the tray menu once the platform has created the icon.
func (a *App) trayReady() {
	t, r := icon.Tray(false)
	systray.SetTemplateIcon(t, r)
	systray.SetTooltip("FreeTokens")
	systray.SetOnTapped(a.ToggleWindow)

	a.showItem = systray.AddMenuItem("Show FreeTokens", "Open the usage popover")
	refresh := systray.AddMenuItem("Refresh Now", "Refresh now (may ask for keychain access)")
	dashboard := systray.AddMenuItem("Open Freebuff Account", freebuff.DashboardURL)
	settings := systray.AddMenuItem("Settings", "Refresh interval and login item")
	systray.AddSeparator()
	quit := systray.AddMenuItem(guiQuitLabel(), "")

	go func() {
		for {
			select {
			case <-a.showItem.ClickedCh:
				a.ToggleWindow()
			case <-refresh.ClickedCh:
				go a.refresh(true)
			case <-dashboard.ClickedCh:
				_ = browser.Open(freebuff.DashboardURL)
			case <-settings.ClickedCh:
				a.openSettings()
			case <-quit.ClickedCh:
				a.Quit()
				return
			}
		}
	}()
}

func (a *App) refreshLoop() {
	for {
		minutes := a.cfg.Load().RefreshMinutes
		select {
		case <-time.After(time.Duration(minutes) * time.Minute):
		case <-a.wake:
		}
		a.mu.Lock()
		quitting := a.quitting
		a.mu.Unlock()
		if quitting {
			return
		}
		a.refresh(false)
	}
}

func (a *App) refresh(interactive bool) *tracker.Snapshot {
	a.mu.Lock()
	if a.refreshing {
		snap := a.snap
		a.mu.Unlock()
		return snap
	}
	a.refreshing = true
	ctx := a.ctx
	a.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	snap := a.tracker.Refresh(ctx, interactive)
	cancel()

	a.mu.Lock()
	a.snap = snap
	a.refreshing = false
	ctx = a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "snapshot", snap)
	}
	a.updateTray(snap)
	return snap
}

func guiQuitLabel() string {
	if goruntime.GOOS == "windows" {
		return "Exit FreeTokens"
	}
	return "Quit FreeTokens"
}

func (a *App) updateTray(snap *tracker.Snapshot) {
	if snap == nil || a.showItem == nil {
		return
	}
	line := "FreeTokens"
	if q := snap.Quota; q != nil {
		if m := q.Meter("Daily Hours"); m != nil {
			line = "FreeTokens · " + view.Hours(max0(m.Limit-m.Used)) + " left"
		}
	}
	systray.SetTooltip(line)
}

func max0(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

// GetSnapshot returns the latest refresh, or nil while the first one is running.
func (a *App) GetSnapshot() *tracker.Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snap
}

// RefreshNow runs a manual refresh. A manual refresh may show a keychain prompt.
func (a *App) RefreshNow() *tracker.Snapshot { return a.refresh(true) }

func (a *App) OpenDashboard() error { return browser.Open(freebuff.DashboardURL) }

// Preferences holds the settings the popover exposes.
type Preferences struct {
	RefreshMinutes int    `json:"refreshMinutes"`
	StartAtLogin   bool   `json:"startAtLogin"`
	Version        string `json:"version"`
	Platform       string `json:"platform"`
}

func (a *App) GetPreferences() Preferences {
	return Preferences{
		RefreshMinutes: a.cfg.Load().RefreshMinutes,
		StartAtLogin:   autostart.Enabled(),
		Version:        a.version,
		Platform:       goruntime.GOOS,
	}
}

func (a *App) SaveRefreshMinutes(minutes int) error {
	if minutes < 1 || minutes > 24*60 {
		return errors.New("refresh interval must be between 1 and 1440 minutes")
	}
	_, err := a.cfg.Update(func(c *config.Config) { c.RefreshMinutes = minutes })
	if err != nil {
		return err
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return nil
}

func (a *App) SetStartAtLogin(on bool) error {
	if on {
		entry, err := autostart.TrayEntry()
		if err != nil {
			return err
		}
		return autostart.Enable(entry)
	}
	return autostart.Disable()
}

// UpdateStatus is the result of a manual release check.
type UpdateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	DevBuild  bool   `json:"devBuild"`
	URL       string `json:"url,omitempty"`
}

func (a *App) CheckForUpdates() (*UpdateStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	info, err := update.Check(ctx, a.version)
	if err != nil {
		return nil, err
	}
	return &UpdateStatus{
		Current:   info.Current,
		Latest:    info.Latest,
		Available: info.Available,
		DevBuild:  !update.IsRelease(a.version),
		URL:       info.URL,
	}, nil
}

func (a *App) OpenReleases() error {
	return browser.Open("https://github.com/" + update.Repo + "/releases")
}

// ToggleWindow opens the popover next to the tray icon, or closes it if it is open.
func (a *App) ToggleWindow() {
	a.mu.Lock()
	if a.hideTimer != nil {
		a.hideTimer.Stop()
		a.hideTimer = nil
	}
	visible := a.visible
	a.visible = !visible
	ctx := a.ctx
	a.mu.Unlock()
	a.syncShowItem(!visible)
	if ctx == nil {
		return
	}
	if visible {
		runtime.WindowHide(ctx)
		return
	}
	x, y := windowPosition(ctx)
	runtime.WindowSetPosition(ctx, x, y)
	runtime.WindowShow(ctx)
	runtime.WindowSetAlwaysOnTop(ctx, true)
}

// RequestHide hides after a short delay so a tray click can cancel it.
func (a *App) RequestHide() {
	a.mu.Lock()
	if a.hideTimer != nil {
		a.hideTimer.Stop()
	}
	a.hideTimer = time.AfterFunc(hideDelay, func() {
		a.mu.Lock()
		a.hideTimer = nil
		a.mu.Unlock()
		a.hide()
	})
	a.mu.Unlock()
}

func (a *App) HideWindow() { a.hide() }

func (a *App) hide() {
	a.mu.Lock()
	if a.hideTimer != nil {
		a.hideTimer.Stop()
		a.hideTimer = nil
	}
	a.visible = false
	ctx := a.ctx
	a.mu.Unlock()
	a.syncShowItem(false)
	if ctx != nil {
		runtime.WindowHide(ctx)
	}
}

func (a *App) setVisible(v bool) {
	a.mu.Lock()
	a.visible = v
	a.mu.Unlock()
	a.syncShowItem(v)
}

func (a *App) syncShowItem(visible bool) {
	if a.showItem == nil {
		return
	}
	if visible {
		a.showItem.SetTitle("Hide FreeTokens")
	} else {
		a.showItem.SetTitle("Show FreeTokens")
	}
}

func (a *App) openSettings() {
	a.showWindow()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "open-settings")
	}
}

func (a *App) showWindow() {
	a.mu.Lock()
	if a.hideTimer != nil {
		a.hideTimer.Stop()
		a.hideTimer = nil
	}
	a.visible = true
	ctx := a.ctx
	a.mu.Unlock()
	a.syncShowItem(true)
	if ctx == nil {
		return
	}
	x, y := windowPosition(ctx)
	runtime.WindowSetPosition(ctx, x, y)
	runtime.WindowShow(ctx)
	runtime.WindowSetAlwaysOnTop(ctx, true)
}

// Quit ends the process from either the popover or the tray.
func (a *App) Quit() {
	a.mu.Lock()
	a.quitting = true
	ctx := a.ctx
	a.mu.Unlock()
	systray.Quit()
	if ctx != nil {
		runtime.Quit(ctx)
	}
}
