// Package browser opens https URLs in the default browser.
package browser

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Open launches the default browser. Only https URLs are accepted.
func Open(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("refusing to open a non-https URL")
	}
	for _, argv := range openers() {
		cmd := exec.Command(argv[0], append(argv[1:], u.String())...)
		if err := cmd.Start(); err == nil {
			// Reap the launcher so a long-lived tray process doesn't keep a zombie per click.
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	return errors.New("no browser opener found")
}

// openers lists the launcher commands to try, in order.
func openers() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"open"}}
	case "windows":
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler"}}
	default:
		return linuxOpeners(os.Getenv("BROWSER"), exec.LookPath)
	}
}

// linuxOpeners keeps the launchers this session can actually run. $BROWSER comes first because the
// user set it deliberately; the rest cover minimal images where xdg-open isn't installed. A value
// carrying arguments or a colon-separated list isn't a single command, so it is ignored.
func linuxOpeners(browser string, look func(string) (string, error)) [][]string {
	var out [][]string
	if b := strings.TrimSpace(browser); b != "" && !strings.ContainsAny(b, " \t:") {
		if _, err := look(b); err == nil {
			out = append(out, []string{b})
		}
	}
	for _, c := range [][]string{{"xdg-open"}, {"gio", "open"}, {"sensible-browser"}, {"x-www-browser"}} {
		if _, err := look(c[0]); err == nil {
			out = append(out, c)
		}
	}
	return out
}
