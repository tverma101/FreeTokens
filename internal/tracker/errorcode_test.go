package tracker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/tverma101/FreeTokens/internal/freebuff"
)

func TestErrorCode(t *testing.T) {
	cases := map[error]string{
		nil:                               "",
		freebuff.ErrNotLoggedIn:           "signed-out",
		freebuff.ErrMissingDefaultProfile: "signed-out",
		fmt.Errorf("x: %w", freebuff.ErrTokenInvalid): "expired",
		freebuff.ErrPermissionRequired:                "approval",
		freebuff.ErrConnection:                        "network",
		errors.New("boom"):                            "other",
	}
	for err, want := range cases {
		if got := ErrorCode(err); got != want {
			t.Errorf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
}
