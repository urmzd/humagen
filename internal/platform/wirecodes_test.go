package platform_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/session"
	"github.com/urmzd/saige/sandbox/controlplane"

	_ "github.com/urmzd/humagen/internal/platform"
)

// A runtime error that crossed the wire still answers errors.Is for the
// sentinel it wrapped, because the platform registers the runtime's wire
// codes: the checks the platform makes on an answer, a session and the
// control plane hold on an error that was encoded and decoded.
func TestRuntimeErrorsKeepTheirSentinelAcrossTheWire(t *testing.T) {
	for name, sentinel := range map[string]error{
		"flow/runtime":         runtime.ErrNoQuestion,
		"flow/session":         session.ErrDenied,
		"sandbox/controlplane": controlplane.ErrFull,
	} {
		raw, err := json.Marshal(saigetypes.EncodeError(fmt.Errorf("a turn failed: %w", sentinel)))
		if err != nil {
			t.Fatal(err)
		}
		var wire saigetypes.EncodedError
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if got := saigetypes.DecodeError(&wire); !errors.Is(got, sentinel) {
			t.Errorf("%s: %v does not match %v after crossing the wire (%s)", name, got, sentinel, raw)
		}
	}
}
