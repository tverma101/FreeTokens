//go:build linux || freebsd || openbsd || netbsd || dragonfly

package secrets

import (
	"strings"
	"time"

	dbus "github.com/godbus/dbus/v5"
	ss "github.com/zalando/go-keyring/secret_service"
)

// promptWait bounds how long an unlock prompt may stay unanswered. The tracker refreshes under a
// three minute deadline, so a desktop that shows no prompt (a locked session, or a session with no
// prompt agent at all) must not park the refresh forever.
const promptWait = 2 * time.Minute

type platformStore struct{}

// Bun's libsecret schema "com.oven-sh.bun.Secret" stores attributes {service, account}. The
// service-level search covers every collection, not only "login".
func (platformStore) Get(service, account string, interactive bool) (string, error) {
	svc, err := ss.NewSecretService()
	if err != nil {
		return "", ErrUnavailable
	}
	root := svc.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")
	attrs := map[string]string{"service": service, "account": account}
	var unlocked, locked []dbus.ObjectPath
	if err := root.Call("org.freedesktop.Secret.Service.SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return "", ErrUnavailable
	}
	if len(unlocked) == 0 && len(locked) > 0 {
		if !interactive {
			return "", ErrInteractionRequired
		}
		var done []dbus.ObjectPath
		var prompt dbus.ObjectPath
		if err := root.Call("org.freedesktop.Secret.Service.Unlock", 0, locked[:1]).Store(&done, &prompt); err != nil {
			return "", ErrInteractionRequired
		}
		if prompt != "/" && prompt != "" {
			if err := runPrompt(svc, prompt); err != nil {
				return "", ErrInteractionRequired
			}
		}
		unlocked = locked[:1]
	}
	if len(unlocked) == 0 {
		return "", ErrNotFound
	}
	session, err := svc.OpenSession()
	if err != nil {
		return "", ErrUnavailable
	}
	defer svc.Close(session)
	secret, err := svc.GetSecret(unlocked[0], session.Path())
	if err != nil {
		return "", ErrInteractionRequired
	}
	return strings.TrimSpace(string(secret.Value)), nil
}

func runPrompt(svc *ss.SecretService, prompt dbus.ObjectPath) error {
	signals := make(chan *dbus.Signal, 1)
	svc.Signal(signals)
	defer svc.RemoveSignal(signals)
	if err := svc.AddMatchSignal(dbus.WithMatchObjectPath(prompt), dbus.WithMatchInterface("org.freedesktop.Secret.Prompt")); err != nil {
		return err
	}
	if err := svc.Object("org.freedesktop.secrets", prompt).Call("org.freedesktop.Secret.Prompt.Prompt", 0, "").Err; err != nil {
		return err
	}
	timer := time.NewTimer(promptWait)
	defer timer.Stop()
	for {
		select {
		case sig, ok := <-signals:
			if !ok {
				return ErrInteractionRequired
			}
			if sig.Path == prompt && len(sig.Body) > 0 {
				if dismissed, _ := sig.Body[0].(bool); dismissed {
					return ErrInteractionRequired
				}
				return nil
			}
		case <-timer.C:
			return ErrInteractionRequired
		}
	}
}
