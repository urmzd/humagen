package connectionauth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
)

// identitySetup is a manager with two native sign-ins, a clock the test
// moves, and counts of the token and identity requests it made.
func identitySetup(t *testing.T) (m *Manager, store state.Store, now *time.Time, exchanges, reads *atomic.Int32) {
	t.Helper()
	store = state.NewMemory()
	exchanges, reads = &atomic.Int32{}, &atomic.Int32{}
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now = &clock
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "one.test/token", "two.test/token":
			exchanges.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("code_verifier") == "" || r.Form.Get("redirect_uri") != "https://humagen.test/connections/callback" {
				t.Errorf("token request without the verifier or the configured redirect: %v", r.Form)
			}
			return response(`{"access_token":"identity-token","refresh_token":"identity-refresh","token_type":"Bearer","expires_in":3600}`), nil
		case "api.one.test/user", "api.two.test/user":
			reads.Add(1)
			if r.Header.Get("Authorization") != "Bearer identity-token" {
				t.Errorf("identity read carried %q", r.Header.Get("Authorization"))
			}
			return response(`{"id":42}`), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})}
	providers := []Provider{
		{Name: "one", Resource: "https://api.one.test/", Issuer: "https://one.test", ClientID: "c1", ClientSecret: "s1", AuthorizationURL: "https://one.test/authorize", TokenURL: "https://one.test/token"},
		{Name: "two", Resource: "https://api.two.test/", Issuer: "https://two.test", ClientID: "c2", ClientSecret: "s2", AuthorizationURL: "https://two.test/authorize", TokenURL: "https://two.test/token"},
	}
	m, err := New(store, Config{Key: []byte(strings.Repeat("k", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: providers, HTTP: client, Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return m, store, now, exchanges, reads
}

// A bound consent is for one person, one provider and one browser, for ten
// minutes, once; it reads who signed in and keeps nothing.
func TestABoundConsentIsCompletedOnceByWhoBeganItAndKeepsNothing(t *testing.T) {
	m, store, now, exchanges, reads := identitySetup(t)
	ctx := t.Context()
	begin := func(person, provider, binding string) string {
		t.Helper()
		a, err := m.BeginBound(ctx, "tenant", person, provider, "", binding)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(a.URL)
		if u.Host != provider+".test" || u.Query().Get("redirect_uri") != "https://humagen.test/connections/callback" || strings.Contains(a.URL, binding) {
			t.Fatalf("consent address %s", a.URL)
		}
		return nonceOf(t, a)
	}
	if _, err := m.BeginBound(ctx, "tenant", "ana", "one", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a bound consent with no binding: %v", err)
	}

	first, second := begin("ana", "one", "browser-a"), begin("ana", "one", "browser-a")
	if first == second || len(second) < 43 {
		t.Fatalf("states %q and %q", first, second)
	}
	// A state a later start replaced, one longer than any made here, and one
	// presented in another workspace name nothing and leave the consent be.
	for _, r := range []struct{ why, tenant, nonce string }{
		{"the state a later start replaced", "tenant", first},
		{"a state longer than any made here", "tenant", second + strings.Repeat("x", maxState)},
		{"another tenant", "elsewhere", second},
	} {
		if _, err := m.CompleteIdentity(ctx, r.tenant, "ana", "one", r.nonce, "code", "user", "browser-a"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", r.why, err)
		}
	}
	// A callback that does not match its consent ends it: the right callback
	// after it completes nothing.
	ended := []struct {
		why                     string
		person, provider, bound string
	}{
		{"this state at another provider", "ana", "two", "browser-a"},
		{"another person", "bob", "one", "browser-a"},
		{"another browser", "ana", "one", "browser-b"},
		{"no browser secret", "ana", "one", ""},
	}
	for _, r := range ended {
		nonce := begin("ana", "one", "browser-a")
		if _, err := m.CompleteIdentity(ctx, "tenant", r.person, r.provider, nonce, "code", "user", r.bound); !errors.Is(err, ErrConsentEnded) {
			t.Fatalf("%s: %v", r.why, err)
		}
		if _, err := m.CompleteIdentity(ctx, "tenant", "ana", "one", nonce, "code", "user", "browser-a"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("after %s, the right callback: %v", r.why, err)
		}
	}
	other := begin("ana", "two", "browser-a")
	if _, err := m.CompleteIdentity(ctx, "tenant", "ana", "one", other, "code", "user", "browser-a"); !errors.Is(err, ErrConsentEnded) {
		t.Fatalf("another provider's state: %v", err)
	}
	other = begin("ana", "two", "browser-a")
	// The call that keeps a credential ends a bound consent it is given.
	second = begin("ana", "one", "browser-a")
	if err := m.Complete(ctx, "tenant", "ana", "one", second, "code"); !errors.Is(err, ErrConsentEnded) {
		t.Fatalf("a bound consent completed as a connection: %v", err)
	}
	second = begin("ana", "one", "browser-a")
	if exchanges.Load() != 0 || reads.Load() != 0 {
		t.Fatalf("a refused callback reached the provider: %d exchanges, %d reads", exchanges.Load(), reads.Load())
	}

	raw, err := m.CompleteIdentity(ctx, "tenant", "ana", "one", second, "code", "user", "browser-a")
	if err != nil || string(raw) != `{"id":42}` {
		t.Fatalf("identity %q %v", raw, err)
	}
	if _, err := m.CompleteIdentity(ctx, "tenant", "ana", "one", second, "code", "user", "browser-a"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a spent state: %v", err)
	}
	if exchanges.Load() != 1 || reads.Load() != 1 {
		t.Fatalf("%d exchanges, %d reads", exchanges.Load(), reads.Load())
	}
	// Nothing of the sign-in is kept: no account, and no token in any record.
	if st, err := m.Status("tenant", "ana", "one"); err != nil || st.Connected {
		t.Fatalf("status %+v %v", st, err)
	}
	if all, err := m.Connections("tenant", "ana"); err != nil || len(all) != 0 {
		t.Fatalf("connections %+v %v", all, err)
	}
	rows, _, err := state.NewTable[string](store, "connection_oauth").List("tenant", state.Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.Contains(row.Value, "identity-token") || strings.Contains(row.Value, "identity-refresh") || strings.HasPrefix(row.Key, "account/") {
			t.Fatalf("kept %s", row.Key)
		}
	}

	// A consent ends ten minutes after it began.
	*now = now.Add(10 * time.Minute)
	if _, err := m.CompleteIdentity(ctx, "tenant", "ana", "two", other, "code", "user", "browser-a"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an expired state: %v", err)
	}
	// An unbound consent is not completed as an identity.
	plain, err := m.Begin(ctx, "tenant", "ana", "two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CompleteIdentity(ctx, "tenant", "ana", "two", nonceOf(t, plain), "code", "user", "browser-a"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unbound consent completed as an identity: %v", err)
	}
	if exchanges.Load() != 1 {
		t.Fatalf("%d exchanges", exchanges.Load())
	}
	// No consent is pending, so no state index is left.
	if rows, _, err = state.NewTable[string](store, "connection_oauth").List("tenant", state.Query{Prefix: "state/", Limit: 100}); err != nil || len(rows) != 0 {
		t.Fatalf("state indexes left: %d %v", len(rows), err)
	}
}
