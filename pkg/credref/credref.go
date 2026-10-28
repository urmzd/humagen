// Package credref resolves credential references: names for where a key is
// kept, never the key itself. A deployment writes "env:OPENAI_API_KEY",
// "keychain:humagen/openai" or "op://Private/OpenAI/credential" into its
// providers file, and the value is read when it is needed, on every call,
// and never written anywhere by humagen. Rotating a key is changing it where it
// lives; nothing here caches it.
//
// It is the one place a reference is parsed, so the providers file, the
// first-run setup and the MCP dialer all accept the same three schemes and
// refuse anything else.
package credref

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/mcptools"

	"github.com/urmzd/humagen/internal/legacy"
)

// Errors. A message never carries a resolved value.
var (
	// ErrInvalid is a reference that is not one of the supported schemes.
	ErrInvalid = errors.New("credref: invalid reference")
	// ErrUnavailable is a reference that names nothing, or a store that
	// could not be read.
	ErrUnavailable = errors.New("credref: credential unavailable")
)

// Scheme is where a credential is kept.
type Scheme string

const (
	// Env is this process's environment: "env:NAME".
	Env Scheme = "env"
	// Keychain is the operating system's keychain: "keychain:SERVICE" or
	// "keychain:SERVICE/ACCOUNT". macOS uses security(1), Linux secret-tool(1).
	Keychain Scheme = "keychain"
	// OnePassword is a 1Password secret reference read with op(1):
	// "op://VAULT/ITEM/FIELD".
	OnePassword Scheme = "op"
)

var (
	envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	part    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,127}$`)
)

// Ref is a parsed reference.
type Ref struct {
	Scheme Scheme
	// Name is the variable for Env, the service for Keychain.
	Name string
	// Account is the keychain account, optional.
	Account string
	// Raw is the reference as written.
	Raw string
}

// String returns the reference as written.
func (r Ref) String() string { return r.Raw }

// Parse checks a reference without reading it.
func Parse(raw string) (Ref, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "env:"):
		name := strings.TrimPrefix(raw, "env:")
		if !envName.MatchString(name) {
			return Ref{}, fmt.Errorf("%w: %q: env: names a variable", ErrInvalid, raw)
		}
		return Ref{Scheme: Env, Name: name, Raw: raw}, nil
	case strings.HasPrefix(raw, "keychain:"):
		service, account, _ := strings.Cut(strings.TrimPrefix(raw, "keychain:"), "/")
		if !part.MatchString(service) || (account != "" && !part.MatchString(account)) {
			return Ref{}, fmt.Errorf("%w: %q: keychain: is SERVICE or SERVICE/ACCOUNT", ErrInvalid, raw)
		}
		return Ref{Scheme: Keychain, Name: service, Account: account, Raw: raw}, nil
	case strings.HasPrefix(raw, "op://"):
		segs := strings.Split(strings.TrimPrefix(raw, "op://"), "/")
		if len(segs) < 3 || len(segs) > 4 {
			return Ref{}, fmt.Errorf("%w: %q: op:// is op://VAULT/ITEM/FIELD", ErrInvalid, raw)
		}
		for _, s := range segs {
			if !part.MatchString(s) {
				return Ref{}, fmt.Errorf("%w: %q: op:// is op://VAULT/ITEM/FIELD", ErrInvalid, raw)
			}
		}
		return Ref{Scheme: OnePassword, Raw: raw}, nil
	default:
		return Ref{}, fmt.Errorf("%w: %q: use env:NAME, keychain:SERVICE[/ACCOUNT] or op://VAULT/ITEM/FIELD", ErrInvalid, raw)
	}
}

// Resolver reads references. The zero value reads the real environment and
// runs the real tools; tests replace the fields.
type Resolver struct {
	// LookupEnv reads a variable. Nil means legacy.LookupEnv: a name under
	// HUMAGEN_ or the old WARP_ reads either, the new one winning.
	LookupEnv func(string) (string, bool)
	// Run runs a command and returns its standard output. Nil runs it.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// GOOS picks the keychain tool. Empty means this system.
	GOOS string
	// Timeout bounds one read from a keychain or 1Password. Zero is ten
	// seconds.
	Timeout time.Duration
}

// Default is the resolver for this process.
var Default = Resolver{}

// Resolve reads the value a reference names. It is called each time a value
// is needed, so the value is never held longer than the caller holds it.
func (r Resolver) Resolve(ctx context.Context, raw string) (string, error) {
	ref, err := Parse(raw)
	if err != nil {
		return "", err
	}
	switch ref.Scheme {
	case Env:
		// A reference under the old WARP_ prefix and one under HUMAGEN_ read
		// one variable, the new name winning (legacy.LookupEnv).
		look := r.LookupEnv
		if look == nil {
			look = legacy.LookupEnv
		}
		v, ok := look(ref.Name)
		if !ok || strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("%w: %s is not set", ErrUnavailable, ref.Raw)
		}
		return v, nil
	case Keychain:
		goos := r.GOOS
		if goos == "" {
			goos = goruntime.GOOS
		}
		switch goos {
		case "darwin":
			args := []string{"find-generic-password", "-s", ref.Name, "-w"}
			if ref.Account != "" {
				args = append(args, "-a", ref.Account)
			}
			return r.run(ctx, ref, "security", args...)
		case "linux", "freebsd", "openbsd":
			args := []string{"lookup", "service", ref.Name}
			if ref.Account != "" {
				args = append(args, "account", ref.Account)
			}
			return r.run(ctx, ref, "secret-tool", args...)
		default:
			return "", fmt.Errorf("%w: %s: no keychain on %s", ErrUnavailable, ref.Raw, goos)
		}
	default:
		return r.run(ctx, ref, "op", "read", "--no-newline", ref.Raw)
	}
}

func (r Resolver) run(ctx context.Context, ref Ref, name string, args ...string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	run := r.Run
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin = nil
			// The tool's error output may echo what it was asked for, never
			// the value, but it is not passed on either way.
			return cmd.Output()
		}
	}
	out, err := run(ctx, name, args...)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %s could not read it", ErrUnavailable, ref.Raw, name)
	}
	v := string(bytes.TrimRight(out, "\r\n"))
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrUnavailable, ref.Raw)
	}
	return v, nil
}

// MCP adapts the resolver to fiber's MCP dialer, which resolves a tool
// provider's credential when a session opens.
func (r Resolver) MCP() mcptools.Resolver {
	return mcptools.ResolverFunc(func(ctx context.Context, ref string) (mcptools.Secret, error) {
		v, err := r.Resolve(ctx, ref)
		if err != nil {
			return mcptools.Secret{}, fmt.Errorf("%w: %v", mcptools.ErrCredential, err)
		}
		return mcptools.NewSecret(v), nil
	})
}
