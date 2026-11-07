package freebuff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tverma101/FreeTokens/internal/secrets"
)

type fakeSecrets struct {
	tok   string
	err   error
	asked *bool
}

func (f fakeSecrets) Get(service, account string, interactive bool) (string, error) {
	if f.asked != nil {
		*f.asked = interactive
	}
	return f.tok, f.err
}

func store(file string, env map[string]string, sec secrets.Store) AuthStore {
	return AuthStore{
		Getenv:  func(k string) string { return env[k] },
		HomeDir: func() (string, error) { return "/home/u", nil },
		ReadFile: func(p string) ([]byte, error) {
			if file == "" {
				return nil, os.ErrNotExist
			}
			return []byte(file), nil
		},
		Secrets: sec,
	}
}

func TestAuthSources(t *testing.T) {
	if a, err := store(`{"default":{"authToken":" file-tok "}}`, nil, nil).Load(false); err != nil || a.Token != "file-tok" {
		t.Fatalf("file: %v %v", a, err)
	}
	if a, err := store(``, map[string]string{"CODEBUFF_API_KEY": "env"}, nil).Load(false); err != nil || a.Token != "env" {
		t.Fatalf("env: %v %v", a, err)
	}
	if a, err := store(``, nil, nil).Load(false); err != nil || a != nil {
		t.Fatalf("none: %v %v", a, err)
	}
	asked := false
	kc := `{"default":{"tokenStore":"keychain"}}`
	if a, err := store(kc, nil, fakeSecrets{tok: "kc", asked: &asked}).Load(true); err != nil || a.Token != "kc" || !asked {
		t.Fatalf("keychain: %v %v asked=%v", a, err, asked)
	}
	if _, err := store(kc, nil, fakeSecrets{err: secrets.ErrInteractionRequired}).Load(false); !errors.Is(err, ErrPermissionRequired) {
		t.Fatalf("locked: %v", err)
	}
	// With an env fallback the keychain is never allowed to prompt.
	asked = true
	if a, err := store(kc, map[string]string{"CODEBUFF_API_KEY": "env"}, fakeSecrets{err: secrets.ErrInteractionRequired, asked: &asked}).Load(true); err != nil || a.Token != "env" || asked {
		t.Fatalf("fallback: %v %v asked=%v", a, err, asked)
	}
	if _, err := store(`{"other":{}}`, nil, nil).Load(false); !errors.Is(err, ErrMissingDefaultProfile) {
		t.Fatalf("profile: %v", err)
	}
	if _, err := store(`{bad`, nil, nil).Load(false); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("invalid: %v", err)
	}
	if _, err := store(``, map[string]string{"FREEBUFF_CONFIG_DIR": "relative"}, nil).Load(false); !errors.Is(err, ErrInvalidConfigDir) {
		t.Fatalf("config dir: %v", err)
	}
}

func TestCredentialsPath(t *testing.T) {
	p, _ := store("", map[string]string{"NEXT_PUBLIC_CB_ENVIRONMENT": "dev"}, nil).CredentialsPath()
	if p != filepath.Join("/home/u", ".config", "manicode-dev", "credentials.json") {
		t.Fatalf("path = %s", p)
	}
}

// The CLI follows the XDG base directory spec, so a desktop that exports XDG_CONFIG_HOME keeps its
// credentials (and the token in them) somewhere ~/.config never sees.
func TestCredentialsPathHonoursXDGConfigHome(t *testing.T) {
	xdg := t.TempDir()
	p, err := store("", map[string]string{"XDG_CONFIG_HOME": xdg}, nil).CredentialsPath()
	if err != nil || p != filepath.Join(xdg, "manicode", "credentials.json") {
		t.Fatalf("path = %s err = %v", p, err)
	}
	// A relative value is not a base directory; the spec and Go's own resolution both ignore it.
	p, _ = store("", map[string]string{"XDG_CONFIG_HOME": "relative"}, nil).CredentialsPath()
	if p != filepath.Join("/home/u", ".config", "manicode", "credentials.json") {
		t.Fatalf("relative XDG path = %s", p)
	}
}
