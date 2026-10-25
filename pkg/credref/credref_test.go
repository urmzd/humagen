package credref

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseAcceptsOnlyTheThreeSchemes(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"env:OPENAI_API_KEY", true},
		{"keychain:humagen", true},
		{"keychain:humagen/openai", true},
		{"op://Private/OpenAI/credential", true},
		{"op://Private/OpenAI/section/credential", true},
		{"sk-live-1234", false},
		{"env:", false},
		{"env:1BAD", false},
		{"keychain:", false},
		{"keychain:a;rm -rf/", false},
		{"op://Private/OpenAI", false},
		{"vault:secret/x", false},
	} {
		_, err := Parse(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("Parse(%q) = %v, want ok=%v", tc.raw, err, tc.ok)
		}
	}
}

func TestResolveReadsEveryTimeAndNeverEchoesTheValue(t *testing.T) {
	val := "first"
	r := Resolver{LookupEnv: func(string) (string, bool) { return val, true }}
	got, err := r.Resolve(context.Background(), "env:KEY")
	if err != nil || got != "first" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	val = "rotated"
	if got, _ := r.Resolve(context.Background(), "env:KEY"); got != "rotated" {
		t.Fatalf("a rotated key was not picked up: %q", got)
	}
}

func TestResolveRunsTheRightTool(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return []byte("secret\n"), nil
	}
	for _, tc := range []struct {
		goos, ref string
		want      []string
	}{
		{"darwin", "keychain:humagen/openai", []string{"security", "find-generic-password", "-s", "humagen", "-w", "-a", "openai"}},
		{"linux", "keychain:humagen", []string{"secret-tool", "lookup", "service", "humagen"}},
		{"linux", "op://V/I/F", []string{"op", "read", "--no-newline", "op://V/I/F"}},
	} {
		calls = nil
		r := Resolver{Run: run, GOOS: tc.goos}
		got, err := r.Resolve(context.Background(), tc.ref)
		if err != nil || got != "secret" {
			t.Fatalf("%s: Resolve = %q, %v", tc.ref, got, err)
		}
		if !slices.Equal(calls[0], tc.want) {
			t.Errorf("%s ran %v, want %v", tc.ref, calls[0], tc.want)
		}
	}
}

func TestAFailedReadSaysWhichReferenceAndNothingElse(t *testing.T) {
	r := Resolver{Run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("leaked"), errors.New("exit 1: leaked")
	}}
	_, err := r.Resolve(context.Background(), "op://V/I/F")
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "leaked") {
		t.Fatalf("err = %v", err)
	}
	_, err = Resolver{LookupEnv: func(string) (string, bool) { return "", false }}.Resolve(context.Background(), "env:NOPE")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unset variable: %v", err)
	}
}
