// Command freetokens tracks Freebuff hours, sessions and measured token usage from the system tray
// or the command line.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tzlocal "github.com/thlib/go-timezone-local/tzlocal"

	"github.com/tverma101/FreeTokens/internal/autostart"
	"github.com/tverma101/FreeTokens/internal/config"
	"github.com/tverma101/FreeTokens/internal/freebuff"
	"github.com/tverma101/FreeTokens/internal/instance"
	"github.com/tverma101/FreeTokens/internal/tracker"
	"github.com/tverma101/FreeTokens/internal/update"
	"github.com/tverma101/FreeTokens/internal/view"
)

// Set by GoReleaser: -X main.version={{.Version}} -X main.commit={{.ShortCommit}} -X main.date={{.Date}}
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usageText = `FreeTokens — unofficial Freebuff usage tracker

Usage:
  freetokens [tray]                 Run in the menu bar / system tray (default with a desktop session)
  freetokens status [--json] [--prompt]
                                    Print hours, sessions and token usage once
  freetokens diagnose               Print setup diagnostics (never prints tokens)
  freetokens update [--check] [--json] [--scheduled]
                                    Check GitHub Releases and install a verified newer version
  freetokens config [get | set <key> <value>]
                                    Keys: auto-update (off|notify|auto), refresh-minutes, show-timers (true|false)
  freetokens autostart [on|off]     Start the tray at login
  freetokens version

Environment:
  FREEBUFF_CONFIG_DIR    Absolute directory holding Freebuff's credentials.json
  CODEBUFF_API_KEY       Token fallback used by the Freebuff CLI
  FREETOKENS_CONFIG_DIR  Override FreeTokens' settings directory
  FREETOKENS_CACHE_DIR   Override FreeTokens' cache directory
`

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if cmd == "" {
		if hasDesktop() {
			cmd = "tray"
		} else {
			cmd = "status"
		}
	}
	var err error
	switch cmd {
	case "tray":
		err = runTray()
	case "status":
		err = runStatus(args)
	case "diagnose", "diagnostics":
		err = runDiagnose()
	case "update":
		err = runUpdate(args)
	case "config":
		err = runConfig(args)
	case "autostart":
		err = runAutostart(args)
	case "version", "--version", "-v":
		fmt.Printf("freetokens %s (%s, %s) %s/%s\n", version, commit, date, runtime.GOOS, runtime.GOARCH)
	case "help", "--help", "-h":
		fmt.Print(usageText)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usageText)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "freetokens:", err)
		os.Exit(1)
	}
}

// hasDesktop guesses whether a tray can be shown.
func hasDesktop() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func userAgent() string {
	return fmt.Sprintf("FreeTokens/%s (%s; %s)", update.Normalize(version), runtime.GOOS, runtime.GOARCH)
}

func timeZone() string {
	if tz, err := tzlocal.RuntimeTZ(); err == nil && tz != "" {
		return tz
	}
	if name := time.Now().Location().String(); name != "" && name != "Local" {
		return name
	}
	return "UTC"
}

func newTracker() (*tracker.Tracker, string, error) {
	cache, err := config.CacheDir()
	if err != nil {
		return nil, "", fmt.Errorf("no cache directory: %w", err)
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return nil, "", err
	}
	tz := timeZone()
	return tracker.New(cache, userAgent(), func() string { return tz }), cache, nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	prompt := fs.Bool("prompt", false, "allow an OS keychain prompt if access needs approval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log.SetOutput(os.Stderr)
	if !*asJSON {
		log.SetOutput(io.Discard)
	}
	t, _, err := newTracker()
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	snap := t.Refresh(ctx, *prompt)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(snap)
	}
	fmt.Print(view.Text(snap, time.Now()))
	if snap.QuotaError != "" && snap.Quota == nil && len(snap.Periods) == 0 {
		os.Exit(3)
	}
	return nil
}

func runDiagnose() error {
	cfgStore, err := config.DefaultStore()
	if err != nil {
		return err
	}
	cache, _ := config.CacheDir()
	auth := freebuff.DefaultAuthStore()
	credPath, credErr := auth.CredentialsPath()
	_, statErr := os.Stat(credPath)
	a, authErr := auth.Load(false)
	dbs, _ := freebuff.DesktopDatabases(freebuff.DesktopProjectsDir())
	authState := "logged in"
	switch {
	case authErr != nil:
		authState = authErr.Error()
	case a == nil:
		authState = freebuff.ErrNotLoggedIn.Error()
	}
	d := map[string]any{
		"version":           version,
		"commit":            commit,
		"os":                runtime.GOOS,
		"arch":              runtime.GOARCH,
		"timeZone":          timeZone(),
		"configFile":        cfgStore.Path,
		"cacheDir":          cache,
		"credentialsPath":   credPath,
		"credentialsExists": credErr == nil && statErr == nil,
		"tokenFromEnv":      os.Getenv("CODEBUFF_API_KEY") != "",
		"auth":              authState,
		"desktopDatabases":  len(dbs),
		"autostart":         autostart.Enabled(),
		"settings":          cfgStore.Load(),
	}
	if a != nil {
		t, _, err := newTracker()
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			snap := t.Refresh(ctx, false)
			d["quotaError"] = snap.QuotaError
			d["historyWarning"] = snap.HistoryWarning
			d["observation"] = snap.Observation
			d["historyDays"] = len(snap.Days)
			if snap.Quota != nil {
				d["quotaStatus"] = snap.Quota.Status
				d["meters"] = len(snap.Quota.Meters)
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

// updateResult is the --json form of "freetokens update", read by the macOS app.
type updateResult struct {
	*update.Info
	Installed bool `json:"installed"`
	// Skipped is "off" or "not-due" when a --scheduled check didn't run.
	Skipped string `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := fs.Bool("check", false, "only report whether an update is available")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	scheduled := fs.Bool("scheduled", false, "follow the auto-update setting: check at most daily, install only with auto")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	// Plain-text scheduled checks must use the same policy gate as JSON callers.
	// Otherwise --scheduled without --json silently bypasses off/notify and may install.
	if *scheduled && !*asJSON {
		res := updateJSON(ctx, *checkOnly, true)
		if res.Error != "" {
			return errors.New(res.Error)
		}
		switch res.Skipped {
		case "off":
			fmt.Println("Scheduled update check skipped (automatic updates are off or this is a development build).")
			return nil
		case "not-due":
			fmt.Println("Scheduled update check skipped (the daily check is not due yet).")
			return nil
		}
		if res.Latest == "" {
			fmt.Println("No release for this platform was found.")
			return nil
		}
		if res.Installed {
			if res.App != "" {
				fmt.Printf("Updated %s to v%s. Quit and reopen FreeTokens to use it.\n", res.App, res.Latest)
			} else {
				fmt.Printf("Updated to v%s. Restart the tray to use it.\n", res.Latest)
			}
			return nil
		}
		if res.Available {
			fmt.Printf("FreeTokens v%s is available (you have v%s): %s\n", res.Latest, res.Current, res.URL)
			fmt.Println("Automatic installation was not requested; no update installed.")
			return nil
		}
		fmt.Printf("FreeTokens v%s is up to date.\n", res.Current)
		return nil
	}
	if *asJSON {
		log.SetOutput(io.Discard)
		res := updateJSON(ctx, *checkOnly, *scheduled)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
		if res.Error != "" {
			os.Exit(1)
		}
		return nil
	}
	info, err := update.Check(ctx, version)
	if err != nil {
		return err
	}
	if info.Latest == "" {
		fmt.Println("No release for this platform was found.")
		return nil
	}
	if !update.IsRelease(version) {
		fmt.Printf("This is a development build. The latest release is v%s: %s\n", info.Latest, info.URL)
		return nil
	}
	if !info.Available {
		fmt.Printf("FreeTokens v%s is up to date.\n", info.Current)
		return nil
	}
	fmt.Printf("FreeTokens v%s is available (you have v%s): %s\n", info.Latest, info.Current, info.URL)
	if *checkOnly {
		return nil
	}
	fmt.Printf("Installing %s…\n", info.AssetName)
	if err := update.Apply(ctx, info); err != nil {
		return err
	}
	if info.App != "" {
		fmt.Printf("Updated %s to v%s. Quit and reopen FreeTokens to use it.\n", info.App, info.Latest)
		return nil
	}
	fmt.Printf("Updated to v%s. Restart the tray (Quit, then reopen) to use it.\n", info.Latest)
	return nil
}

func updateJSON(ctx context.Context, checkOnly, scheduled bool) updateResult {
	res := updateResult{Info: &update.Info{Current: update.Normalize(version)}}
	var store *config.Store
	if scheduled {
		var err error
		if store, err = config.DefaultStore(); err != nil {
			res.Error = err.Error()
			return res
		}
		c := store.Load()
		if c.AutoUpdate == config.UpdateOff || !update.IsRelease(version) {
			res.Skipped = "off"
			return res
		}
		if since := time.Since(c.LastUpdateCheck); since >= 0 && since < update.CheckInterval {
			res.Skipped = "not-due"
			return res
		}
		checkOnly = c.AutoUpdate != config.UpdateAuto
	}
	info, err := update.Check(ctx, version)
	if store != nil {
		_, _ = store.Update(func(c *config.Config) { c.LastUpdateCheck = time.Now() })
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Info = info
	if checkOnly || !info.Available {
		return res
	}
	if err := update.Apply(ctx, info); err != nil {
		res.Error = err.Error()
		return res
	}
	res.Installed = true
	return res
}

func runConfig(args []string) error {
	store, err := config.DefaultStore()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "get" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(store.Load())
	}
	if args[0] != "set" || len(args) != 3 {
		return errors.New("usage: freetokens config set <auto-update|refresh-minutes|show-timers> <value>")
	}
	key, val := args[1], strings.ToLower(args[2])
	var setErr error
	_, err = store.Update(func(c *config.Config) {
		switch key {
		case "auto-update", "autoUpdate":
			switch val {
			case config.UpdateOff, config.UpdateNotify, config.UpdateAuto:
				c.AutoUpdate = val
			default:
				setErr = errors.New("auto-update must be off, notify or auto")
			}
		case "refresh-minutes", "refreshMinutes":
			var n int
			if _, e := fmt.Sscanf(val, "%d", &n); e != nil || n < 1 {
				setErr = errors.New("refresh-minutes must be a positive integer")
				return
			}
			c.RefreshMinutes = n
		case "show-timers", "showTimers":
			switch val {
			case "true", "on", "yes", "1":
				c.ShowTimers = true
			case "false", "off", "no", "0":
				c.ShowTimers = false
			default:
				setErr = errors.New("show-timers must be true or false")
			}
		default:
			setErr = fmt.Errorf("unknown key %q", key)
		}
	})
	if setErr != nil {
		return setErr
	}
	if err != nil {
		return err
	}
	fmt.Println("Saved. A running tray picks up changes after restart.")
	return nil
}

func runAutostart(args []string) error {
	if len(args) == 0 {
		fmt.Println(map[bool]string{true: "on", false: "off"}[autostart.Enabled()])
		return nil
	}
	switch args[0] {
	case "on":
		entry, err := autostart.TrayEntry()
		if err != nil {
			return err
		}
		if err := autostart.Enable(entry); err != nil {
			return err
		}
		fmt.Printf("FreeTokens will start at login: %s\n", strings.Join(entry.Exec, " "))
		return nil
	case "off":
		return autostart.Disable()
	}
	return errors.New("usage: freetokens autostart [on|off]")
}

func runTray() error {
	t, cache, err := newTracker()
	if err != nil {
		return err
	}
	lock, err := instance.Lock(cache)
	if errors.Is(err, instance.ErrRunning) {
		fmt.Fprintln(os.Stderr, "FreeTokens is already running in the tray.")
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	logPath := filepath.Join(cache, "freetokens.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(logPath, logPath+".1")
	}
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Printf("FreeTokens %s starting (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	cfg, err := config.DefaultStore()
	if err != nil {
		return err
	}
	detachOwnConsole()
	runTrayUI(t, cfg)
	return nil
}
