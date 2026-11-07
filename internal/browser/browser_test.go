package browser

import (
	"errors"
	"fmt"
	"testing"
)

// lookOnly answers like exec.LookPath for the named commands and fails for everything else.
func lookOnly(available ...string) func(string) (string, error) {
	set := map[string]bool{}
	for _, name := range available {
		set[name] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestLinuxOpenersPrefersBrowser(t *testing.T) {
	got := linuxOpeners("firefox", lookOnly("firefox", "xdg-open", "gio"))
	want := "[[firefox] [xdg-open] [gio open]]"
	if fmt.Sprint(got) != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A $BROWSER that doesn't resolve must not hide a launcher that does.
func TestLinuxOpenersSkipsUnusableBrowser(t *testing.T) {
	got := linuxOpeners("firefox", lookOnly("xdg-open", "sensible-browser"))
	want := "[[xdg-open] [sensible-browser]]"
	if fmt.Sprint(got) != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// "firefox %s" and "a:b" are shell fragments or lists rather than one command.
func TestLinuxOpenersIgnoresShellFragments(t *testing.T) {
	for _, value := range []string{"firefox %s", "a:b", "  "} {
		got := linuxOpeners(value, lookOnly("firefox", "xdg-open"))
		want := "[[xdg-open]]"
		if fmt.Sprint(got) != want {
			t.Fatalf("%q: got %v, want %v", value, got, want)
		}
	}
}

func TestLinuxOpenersBareDesktop(t *testing.T) {
	if got := linuxOpeners("", lookOnly()); len(got) != 0 {
		t.Fatalf("got %v, want no openers", got)
	}
}

func TestOpenRejectsNonHTTPS(t *testing.T) {
	for _, raw := range []string{"http://github.com", "file:///etc/passwd", "javascript:alert(1)", "not a url"} {
		if err := Open(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}
