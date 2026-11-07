package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Scheduled plain-text invocations must return without network access or installation when
// automatic updates are disabled. --check keeps the release-build regression test safe
// even if the policy routing is accidentally removed.
func TestScheduledTextUpdateRespectsOffPolicy(t *testing.T) {
	t.Setenv("FREETOKENS_CONFIG_DIR", t.TempDir())
	for _, tc := range []struct {
		name    string
		version string
		args    []string
	}{
		{name: "development build", version: "dev", args: []string{"--scheduled"}},
		{name: "release check-only", version: "0.1.0", args: []string{"--scheduled", "--check"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousVersion := version
			version = tc.version
			defer func() { version = previousVersion }()

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			oldStdout := os.Stdout
			os.Stdout = w
			defer func() {
				os.Stdout = oldStdout
				_ = w.Close()
				_ = r.Close()
			}()

			runErr := runUpdate(tc.args)
			_ = w.Close()
			os.Stdout = oldStdout
			output, readErr := io.ReadAll(r)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if runErr != nil {
				t.Fatalf("scheduled update returned %v (output %q)", runErr, output)
			}
			if !strings.Contains(string(output), "Scheduled update check skipped") {
				t.Fatalf("off-policy scheduled check was not skipped: %q", output)
			}
		})
	}
}
