package freebuff

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/tverma101/FreeTokens/internal/secrets"
)

// KeychainService is the credential-store service the Freebuff CLI writes through Bun.secrets.
// The account (Bun's "name") is the absolute credentials.json path.
const KeychainService = "freebuff-cli"

// Auth is the bearer token for Freebuff's API. It is never written to disk or logs by FreeTokens.
type Auth struct {
	Token string
}

// Typed auth failures. Each has a repair message suitable for the tray and CLI.
var (
	ErrNotLoggedIn           = errors.New("Not signed in to Freebuff. Run \"freebuff login\" in a terminal, then refresh")
	ErrInvalidConfigDir      = errors.New("FREEBUFF_CONFIG_DIR must be an absolute path")
	ErrInvalidCredentials    = errors.New("Freebuff's credentials file is invalid. Sign in again with the Freebuff CLI")
	ErrCredentialsUnreadable = errors.New("FreeTokens couldn't read Freebuff's local credentials. Check file or keychain access")
	ErrPermissionRequired    = errors.New("The OS needs permission to read Freebuff's keychain item. Use Refresh Now and allow access when prompted")
	ErrTokenInvalid          = errors.New("Your Freebuff login has expired. Sign in again with the Freebuff CLI")
	ErrMissingDefaultProfile = errors.New("Freebuff credentials were found, but they have no \"default\" profile. Sign in again with the Freebuff CLI, or point FREEBUFF_CONFIG_DIR at the directory that holds it")
)

// AuthStore reads the Freebuff CLI's existing `default` profile. Current CLI versions keep the token
// in the OS credential store (service `freebuff-cli`, account = credentials file path) and leave
// `tokenStore: "keychain"` in credentials.json; older versions keep `authToken` in the file.
// CODEBUFF_API_KEY is the CLI's own environment fallback. FreeTokens never launches the CLI and
// never admits a session.
type AuthStore struct {
	Getenv   func(string) string
	HomeDir  func() (string, error)
	ReadFile func(string) ([]byte, error)
	Secrets  secrets.Store
}

// DefaultAuthStore uses the process environment, the real home directory and the OS credential store.
func DefaultAuthStore() AuthStore {
	return AuthStore{Getenv: os.Getenv, HomeDir: os.UserHomeDir, ReadFile: os.ReadFile, Secrets: secrets.Default()}
}

// CredentialsPath mirrors the CLI's config-dir resolution.
func (s AuthStore) CredentialsPath() (string, error) {
	if configured := strings.TrimSpace(s.Getenv("FREEBUFF_CONFIG_DIR")); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", ErrInvalidConfigDir
		}
		return filepath.Clean(filepath.Join(configured, "credentials.json")), nil
	}
	home, err := s.HomeDir()
	if err != nil {
		return "", ErrCredentialsUnreadable
	}
	env := s.Getenv("NEXT_PUBLIC_CB_ENVIRONMENT")
	suffix := ""
	if env != "" && env != "prod" {
		suffix = "-" + env
	}
	return filepath.Clean(filepath.Join(home, ".config", "manicode"+suffix, "credentials.json")), nil
}

// Load returns the token, or (nil, nil) when nothing is configured anywhere. interactive allows a
// credential-store prompt (manual refresh only); background refreshes stay silent.
func (s AuthStore) Load(interactive bool) (*Auth, error) {
	path, err := s.CredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := s.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		if fb := s.envToken(); fb != nil {
			return fb, nil
		}
		return nil, ErrCredentialsUnreadable
	}
	if len(strings.TrimSpace(string(data))) > 0 {
		var root map[string]json.RawMessage
		if json.Unmarshal(data, &root) != nil {
			if fb := s.envToken(); fb != nil {
				return fb, nil
			}
			return nil, ErrInvalidCredentials
		}
		rawProfile, ok := root["default"]
		var profile map[string]any
		if !ok || json.Unmarshal(rawProfile, &profile) != nil || profile == nil {
			if fb := s.envToken(); fb != nil {
				return fb, nil
			}
			return nil, ErrMissingDefaultProfile
		}
		if tok := trimmedString(profile["authToken"]); tok != "" {
			return &Auth{Token: tok}, nil
		}
		if store, _ := profile["tokenStore"].(string); store == "keychain" && s.Secrets != nil {
			fallback := s.envToken()
			// With an environment fallback, never open a prompt; still prefer a silently readable item.
			allowPrompt := interactive && fallback == nil
			tok, kerr := s.Secrets.Get(KeychainService, path, allowPrompt)
			switch {
			case kerr == nil && strings.TrimSpace(tok) != "":
				return &Auth{Token: strings.TrimSpace(tok)}, nil
			case errors.Is(kerr, secrets.ErrInteractionRequired):
				if fallback != nil {
					return fallback, nil
				}
				return nil, ErrPermissionRequired
			case kerr != nil && !errors.Is(kerr, secrets.ErrNotFound):
				if fallback != nil {
					return fallback, nil
				}
				return nil, ErrCredentialsUnreadable
			}
			if fallback != nil {
				return fallback, nil
			}
		}
	}
	return s.envToken(), nil
}

// HasLocalSetup reports whether Freebuff looks configured on this machine, including broken setups
// that should show a repair message rather than look absent.
func (s AuthStore) HasLocalSetup() bool {
	a, err := s.Load(false)
	switch {
	case err == nil:
		return a != nil
	case errors.Is(err, ErrNotLoggedIn):
		return false
	default:
		return true
	}
}

func (s AuthStore) envToken() *Auth {
	if tok := strings.TrimSpace(s.Getenv("CODEBUFF_API_KEY")); tok != "" {
		return &Auth{Token: tok}
	}
	return nil
}

func trimmedString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
