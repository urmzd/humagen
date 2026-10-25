package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/auth/authtest"
	"github.com/urmzd/humagen/pkg/call"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestTailscaleServeAuth(t *testing.T) {
	base := authConfig{mode: "tailscale", addr: "127.0.0.1:8080", publicURL: "https://humagen.example.ts.net", tenant: "acme", policy: auth.DefaultPolicy()}
	a, stop, err := newAuthn(context.Background(), base)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.NewRequest("GET", base.publicURL+"/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Tailscale-User-Login", "alice@example.com")
	who, err := a.Authenticate(r)
	if err != nil || who.Tenant != "acme" || len(who.Granted()) != 0 {
		t.Fatalf("default deny: %+v, %v", who, err)
	}
	base.tenantClaim = "tenant"
	if _, _, err := newAuthn(context.Background(), base); err == nil {
		t.Fatal("accepted a tenant claim in Tailscale mode")
	}
}

func TestOktaAlias(t *testing.T) {
	issuer := authtest.New(t)
	a, stop, err := newAuthn(context.Background(), authConfig{
		mode: "okta", publicURL: "https://humagen.example.com", tenant: "acme", groups: "groups", policy: auth.DefaultPolicy(),
		getenv: env(map[string]string{
			"HUMAGEN_OIDC_ISSUER":        issuer.URL,
			"HUMAGEN_OIDC_CLIENT_ID":     "web",
			"HUMAGEN_OIDC_CLIENT_SECRET": "secret",
			"HUMAGEN_OIDC_AUDIENCE":      "api://humagen",
		}),
	})
	stop()
	if err != nil {
		t.Fatal(err)
	}
	if a.Verifier == nil || a.OAuth.ClientID != "web" || a.OAuth.RedirectURL != "https://humagen.example.com/auth/callback" {
		t.Fatalf("Okta did not use the OIDC flow: %+v", a)
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// server starts the platform behind -auth=mock exactly as serve wires it, and
// returns its public URL.
func mockServer(t *testing.T, policyFile string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	public := "http://" + ln.Addr().String()

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	groups := addGroupFlags(fs)
	pol, groupRoles, err := loadPolicy(fs, groups, env(map[string]string{policyEnv: policyFile}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	authn, stop, err := newAuthn(ctx, authConfig{
		mode: "mock", addr: ln.Addr().String(), publicURL: public,
		tenant: "acme", groups: "groups", policy: pol, groupRoles: groupRoles,
		mockAddr: "127.0.0.1:0", getenv: env(nil), log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	srv := &http.Server{Handler: (&httpapi.Server{P: p, Authn: authn, Log: slog.New(slog.DiscardHandler)}).Handler()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		stop()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("platform shutdown: %v", err)
		}
	})
	return public
}

type me struct {
	Principal struct {
		Subject string      `json:"subject"`
		Name    string      `json:"name"`
		Tenant  string      `json:"tenant"`
		Kind    string      `json:"kind"`
		Roles   []auth.Role `json:"roles"`
	} `json:"principal"`
	Permissions []auth.Permission `json:"permissions"`
}

// signIn drives a browser through the server's own sign-in: /auth/login,
// the provider's persona page, the callback and the session cookie.
func signIn(t *testing.T, public, persona string) me {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar}
	res, err := browser.Get(public + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(page), "Development identity provider") {
		t.Fatalf("login landed on %s (%d)", res.Request.URL, res.StatusCode)
	}
	form := res.Request.URL.Query()
	form.Set("persona", persona)
	authorize := *res.Request.URL
	authorize.RawQuery = ""
	res, err = browser.PostForm(authorize.String(), form)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if !strings.HasPrefix(res.Request.URL.String(), public) {
		t.Fatalf("sign-in ended at %s", res.Request.URL)
	}
	res, err = browser.Get(public + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("/api/me %d: %s", res.StatusCode, body)
	}
	var m me
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMockSignInForEachDefaultPersona(t *testing.T) {
	public := mockServer(t, "")
	viewer := []auth.Permission{auth.AgentsRead, auth.RunsRead, auth.SchedulesRead, auth.AlertsRead, auth.KnowledgeRead, auth.EvalsRead, auth.BillingRead, auth.ArtifactsRead, auth.ClientsRead, auth.ProjectsRead, auth.PeopleRead}
	for _, tc := range []struct {
		persona string
		roles   []auth.Role
		want    []auth.Permission
	}{
		{"mock-viewer", []auth.Role{auth.Viewer}, viewer},
		{"mock-operator", []auth.Role{auth.Operator}, auth.DefaultPolicy().Permissions([]auth.Role{auth.Operator})},
		{"mock-manager", []auth.Role{auth.Manager}, auth.DefaultPolicy().Permissions([]auth.Role{auth.Manager})},
		{"mock-admin", []auth.Role{auth.Admin}, auth.Permissions()},
		{"mock-nobody", nil, nil},
	} {
		t.Run(tc.persona, func(t *testing.T) {
			m := signIn(t, public, tc.persona)
			if m.Principal.Subject != tc.persona || m.Principal.Tenant != "acme" || m.Principal.Kind != "user" {
				t.Fatalf("principal %+v", m.Principal)
			}
			if !slices.Equal(m.Principal.Roles, tc.roles) || !slices.Equal(m.Permissions, tc.want) {
				t.Fatalf("roles %v, permissions %v; want %v, %v", m.Principal.Roles, m.Permissions, tc.roles, tc.want)
			}
		})
	}
}

func TestMockSignInUnderAPolicyFile(t *testing.T) {
	path := writeFile(t, "p.authz", `
role viewer  { allow read on * }
role auditor extends viewer { deny read on billing }
grant viewer  to anyone
grant auditor to group "humagen-admins"
`)
	m := signIn(t, mockServer(t, path), "mock-admin")
	// With a policy file, the default group flags no longer map
	// humagen-admins to admin: the file decides.
	if !slices.Equal(m.Principal.Roles, []auth.Role{auth.Viewer, "auditor"}) {
		t.Fatalf("roles %v", m.Principal.Roles)
	}
	if slices.Contains(m.Permissions, auth.BillingRead) || !slices.Contains(m.Permissions, auth.RunsRead) || slices.Contains(m.Permissions, auth.RunsWrite) {
		t.Fatalf("permissions %v", m.Permissions)
	}
}

func TestServeRefusesABadPolicy(t *testing.T) {
	for name, src := range map[string]string{
		"syntax":       "role viewer { allow read on }",
		"unknown role": "role a { allow read on * }\ngrant b to anyone",
		"empty":        "# nothing here",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(policyEnv, writeFile(t, "p.authz", src))
			err := run(Options{}, []string{"serve", "-auth=dev", "-addr", "127.0.0.1:0"})
			if err == nil || !strings.Contains(err.Error(), policyEnv) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("missing file", func(t *testing.T) {
		t.Setenv(policyEnv, filepath.Join(t.TempDir(), "missing.authz"))
		if err := run(Options{}, []string{"serve", "-auth=dev", "-addr", "127.0.0.1:0"}); err == nil {
			t.Fatal("started")
		}
	})
}

func TestGroupFlagsWithAPolicyFile(t *testing.T) {
	path := writeFile(t, "p.authz", "role viewer { allow read on * }")
	for name, tc := range map[string]struct {
		file    string
		args    []string
		want    int
		wantErr bool
	}{
		// The four defaults and their four old names.
		"without a file, the defaults apply": {"", nil, 8, false},
		"with a file, the defaults do not":   {path, nil, 0, false},
		"with a file, an explicit flag does": {path, []string{"-viewer-group", "readers"}, 1, false},
		"and must name a declared role":      {path, []string{"-admin-group", "ops"}, 1, true},
	} {
		t.Run(name, func(t *testing.T) {
			fs := flag.NewFlagSet("serve", flag.ContinueOnError)
			g := addGroupFlags(fs)
			if err := fs.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			pol, roles, err := loadPolicy(fs, g, env(map[string]string{policyEnv: tc.file}))
			if err != nil {
				t.Fatal(err)
			}
			if len(roles) != tc.want {
				t.Fatalf("group roles %v, want %d", roles, tc.want)
			}
			if _, err := pol.WithGroupRoles(roles); (err != nil) != tc.wantErr {
				t.Fatalf("err %v", err)
			}
		})
	}
}

// The old default groups keep their roles while the defaults are in force:
// for each role whose flag was not set, and never with a policy file.
func TestTheOldDefaultGroupsKeepTheirRoles(t *testing.T) {
	path := writeFile(t, "p.authz", "role viewer { allow read on * }\nrole admin { allow * on * }")
	for name, tc := range map[string]struct {
		file string
		args []string
		want map[string]auth.Role
	}{
		"no flags":    {"", nil, map[string]auth.Role{"humagen-admins": auth.Admin, "warp-admins": auth.Admin, "warp-viewers": auth.Viewer, "humagen-viewers": auth.Viewer}},
		"admin flag":  {"", []string{"-admin-group", "ops"}, map[string]auth.Role{"ops": auth.Admin, "warp-viewers": auth.Viewer}},
		"policy file": {path, nil, map[string]auth.Role{}},
	} {
		t.Run(name, func(t *testing.T) {
			fs := flag.NewFlagSet("serve", flag.ContinueOnError)
			g := addGroupFlags(fs)
			if err := fs.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			_, roles, err := loadPolicy(fs, g, env(map[string]string{policyEnv: tc.file}))
			if err != nil {
				t.Fatal(err)
			}
			for group, role := range tc.want {
				if roles[group] != role {
					t.Errorf("%s is %q, want %q (%v)", group, roles[group], role, roles)
				}
			}
			if tc.file != "" && len(roles) != 0 {
				t.Errorf("with a policy file, the defaults granted %v", roles)
			}
			if _, ok := roles["warp-admins"]; ok && slices.Contains(tc.args, "-admin-group") {
				t.Error("an explicit -admin-group still granted warp-admins")
			}
		})
	}
	var buf bytes.Buffer
	n := legacy.NewNotes(&buf)
	noteOldGroups(map[string]auth.Role{"warp-admins": auth.Admin, "humagen-admins": auth.Admin}, n)
	noteOldGroups(map[string]auth.Role{"warp-admins": auth.Admin}, n)
	if got := strings.Count(buf.String(), "\n"); got != 1 || !strings.Contains(buf.String(), "warp-admins") {
		t.Errorf("notes %q", buf.String())
	}
	buf.Reset()
	noteOldGroups(map[string]auth.Role{"humagen-admins": auth.Admin}, legacy.NewNotes(&buf))
	if buf.Len() != 0 {
		t.Errorf("noted %q with no old group", buf.String())
	}
}

func TestMockAndDevRefusals(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	noAdmin, err := auth.ParsePolicy("p", "role viewer { allow read on * }")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]authConfig{
		"mock on every interface":          {mode: "mock", addr: "0.0.0.0:8080", publicURL: "http://127.0.0.1:8080", mockAddr: "127.0.0.1:0"},
		"mock provider on every interface": {mode: "mock", addr: "127.0.0.1:8080", publicURL: "http://127.0.0.1:8080", mockAddr: "0.0.0.0:0"},
		"mock with a public url":           {mode: "mock", addr: "127.0.0.1:8080", publicURL: "https://humagen.example.com", mockAddr: "127.0.0.1:0"},
		"dev without an admin role":        {mode: "dev", addr: "127.0.0.1:8080", policy: noAdmin},
		"unknown mode":                     {mode: "open"},
	} {
		t.Run(name, func(t *testing.T) {
			if c.policy == nil {
				c.policy = auth.DefaultPolicy()
			}
			c.tenant, c.groups, c.getenv, c.log = "acme", "groups", env(nil), log
			_, stop, err := newAuthn(ctx, c)
			stop()
			if err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestMockPersonasFile(t *testing.T) {
	path := writeFile(t, "people.json", `{"people": [{"subject": "s1", "name": "Sam", "groups": ["humagen-admins"]}]}`)
	people, machines, err := readPersonas(path)
	if err != nil || len(people) != 1 || people[0].Subject != "s1" || len(machines) != 0 {
		t.Fatalf("got %v %v %v", people, machines, err)
	}
	for name, src := range map[string]string{
		"unknown field": `{"people": [{"subject": "s1", "password": "x"}]}`,
		"two values":    `{} {}`,
		"not json":      `people:`,
	} {
		if _, _, err := readPersonas(writeFile(t, "p.json", src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAuthzCommand(t *testing.T) {
	path := writeFile(t, "p.authz", `
role viewer  { allow read on * }
role auditor extends viewer { deny read on billing }
grant viewer  to anyone
grant auditor to claim department = "finance"
`)
	var out bytes.Buffer
	if err := runAuthz([]string{"check", path}, &out, env(nil)); err != nil || !strings.Contains(out.String(), "ok, roles viewer, auditor, 2 grants") {
		t.Fatalf("check: %v %s", err, out.String())
	}
	if err := runAuthz([]string{"check", writeFile(t, "bad.authz", "role a {")}, &out, env(nil)); err == nil {
		t.Fatal("check accepted a bad policy")
	}

	claims := writeFile(t, "claims.json", `{"sub": "00u1", "department": "finance"}`)
	out.Reset()
	err := runAuthz([]string{"explain", "-claims", claims, "billing:read"}, &out, env(map[string]string{policyEnv: path}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"billing:read: denied", "deny read on billing", `grant auditor to claim department = "finance"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("explain is missing %q:\n%s", want, out.String())
		}
	}
	if err := runAuthz([]string{"explain", "rockets:launch"}, &out, env(nil)); err == nil {
		t.Fatal("explain accepted an unknown permission")
	}

	// With no policy file it explains the built-in policy and the group flags.
	admin := writeFile(t, "admin.json", `{"sub": "00u2", "groups": ["humagen-admins"]}`)
	out.Reset()
	if err := runAuthz([]string{"explain", "-claims", admin, "agents:write"}, &out, env(nil)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "agents:write: allowed") || !strings.Contains(out.String(), `grant admin to group "humagen-admins"`) {
		t.Fatalf("explain:\n%s", out.String())
	}
}

// A grant names a subject and no tenant. With one tenant, whom the policy
// grants a role is in its directory before they first sign in. With tenants
// from a claim the one policy is every tenant's, so a grant puts nobody in
// the directory of the workspace -tenant names: they could belong to any.
func TestAPolicyGrantEntersNoDirectoryWhenTenantsComeFromAClaim(t *testing.T) {
	pol, err := auth.ParsePolicy("test", `
role viewer { allow read on * }
role admin extends viewer { allow * on * }
grant viewer to anyone
grant admin to subject "okta|bob-of-other"
`)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	claimed := newProjectsPlatform(t)
	seeGranted(claimed, "home", "tenant", pol, log)
	if n := claimed.People.Len("home"); n != 0 {
		t.Fatalf("with tenants from a claim, the grant put %d people in home's directory", n)
	}
	// The mock reads every caller's tenant from a claim, -tenant-claim set
	// or not, since its personas may name their own.
	mock := newProjectsPlatform(t)
	seeGranted(mock, "home", authConfig{mode: "mock"}.claimed(), pol, log)
	if n := mock.People.Len("home"); n != 0 {
		t.Fatalf("under the mock, the grant put %d people in home's directory", n)
	}
	one := newProjectsPlatform(t)
	seeGranted(one, "home", authConfig{mode: "oidc"}.claimed(), pol, log)
	if bob, err := one.People.Get("home", "okta|bob-of-other"); err != nil || !slices.Equal(bob.Roles, []string{"admin"}) {
		t.Fatalf("with one tenant, the granted subject = %+v, %v", bob, err)
	}
}
