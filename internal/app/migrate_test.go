package app

import (
	"bytes"
	"strings"
	"testing"
)

// humagen migrate sessions is a dry run unless -apply is given, and says so.
func TestMigrateSessionsIsADryRunByDefault(t *testing.T) {
	var out bytes.Buffer
	if err := migrateSessions(t.Context(), nil, &out, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "dry run: nothing is written") || !strings.Contains(got, "would write 0 changes") {
		t.Fatalf("the dry run printed:\n%s", got)
	}
	out.Reset()
	if err := migrateSessions(t.Context(), []string{"-apply"}, &out, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "(applied)") || !strings.Contains(got, "wrote 0 changes") {
		t.Fatalf("the apply printed:\n%s", got)
	}
	if err := migrateCmd([]string{"threads"}, &out); err == nil {
		t.Fatal("an unknown migration was accepted")
	}
}
