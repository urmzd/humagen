package credref

import (
	"context"
	"testing"
)

// A reference written before the rename reads the renamed variable, and a
// new one reads a variable the operator has not renamed yet.
func TestAnEnvReferenceReadsEitherPrefix(t *testing.T) {
	t.Setenv("HUMAGEN_CREDREF_A", "new")
	t.Setenv("WARP_CREDREF_B", "old")
	for ref, want := range map[string]string{"env:WARP_CREDREF_A": "new", "env:HUMAGEN_CREDREF_B": "old"} {
		if got, err := Default.Resolve(context.Background(), ref); err != nil || got != want {
			t.Errorf("%s = %q, %v", ref, got, err)
		}
	}
	if _, err := Default.Resolve(context.Background(), "env:WARP_CREDREF_C"); err == nil {
		t.Error("an unset reference resolved")
	}
}
