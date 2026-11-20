package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Slack app calls humagen's API, so a sign-in that is not dev is refused
// unless the surface gives the app a credential for it.
func TestASlackSurfaceHasACredentialForTheSignIn(t *testing.T) {
	system := func(auth string) string {
		if auth == "" {
			return ""
		}
		return "api_version: humagen/v1\nkind: System\nspec:\n  auth: " + auth + "\n---\n"
	}
	const oidc = "{kind: oidc, oidc: {issuer: 'https://login.example.com', client_id: humagen, audience: humagen}}"
	const slack = "api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  surfaces:\n    slack: {enabled: true, team: T1, bot_token: {ref: env:B}, app_token: {ref: env:A}"
	const client = ", humagen_client: {token_url: 'https://login.example.com/token', client_id: humagen-slack, client_secret: {ref: env:C}}"
	const token = ", humagen_token: {ref: env:T}"
	for name, tc := range map[string]struct{ auth, extra, want string }{
		"no system is dev":           {"", "", ""},
		"dev needs none":             {"{kind: dev}", "", ""},
		"oidc with none":             {oidc, "", "needs a credential for humagen's API"},
		"oidc with client creds":     {oidc, client, ""},
		"oidc with a token":          {oidc, token, ""},
		"mock with none":             {"{kind: mock}", "", "needs a credential"},
		"tailscale needs none":       {"{kind: tailscale}", "", ""},
		"tailscale with a token":     {"{kind: tailscale}", token, "remove humagen_client and humagen_token"},
		"tailscale with a client":    {"{kind: tailscale}", client, "remove humagen_client and humagen_token"},
		"both":                       {oidc, client + token, "not both"},
		"a token url that is no url": {oidc, ", humagen_client: {token_url: login, client_id: c, client_secret: {ref: env:C}}", "token_url"},
		"a client with no secret":    {oidc, ", humagen_client: {token_url: 'https://l.example.com/t', client_id: c}", "client_secret are required"},
		"a secret as a value":        {oidc, ", humagen_token: xoxp-123", "reference"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "humagen.yaml")
			if err := os.WriteFile(path, []byte(system(tc.auth)+slack+tc.extra+"}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A flag may choose the sign-in after the file is checked, so the check is
// one the server makes again with the kind it runs.
func TestCheckSignInTakesTheKindTheServerRuns(t *testing.T) {
	bare := Slack{Enabled: true, Team: "T1"}
	// Under tailscale the server gives the app it starts its credential.
	for _, kind := range []string{"", "dev", "tailscale"} {
		if err := bare.CheckSignIn(kind); err != nil {
			t.Errorf("%q: %v", kind, err)
		}
	}
	for _, kind := range []string{"oidc", "okta", "mock"} {
		if err := bare.CheckSignIn(kind); err == nil {
			t.Errorf("%q: a Slack app with no credential was let through", kind)
		}
	}
	withToken := Slack{Enabled: true, Team: "T1", HumagenToken: &Ref{Ref: "env:T"}}
	if err := withToken.CheckSignIn("okta"); err != nil {
		t.Errorf("okta with a token: %v", err)
	}
	// One in the file could not be verified, so it is refused, not ignored.
	if err := withToken.CheckSignIn("tailscale"); err == nil {
		t.Error("tailscale took a token nothing could verify")
	}
}

// Whom the app acts for is authority, so the file is checked for who is who
// and for which credential may act for them: the server's own under
// tailscale, and one the file must name under every other sign-in.
func TestASlackSurfaceMapsPeopleForACredentialTheServerCanName(t *testing.T) {
	system := func(auth string) string {
		if auth == "" {
			return ""
		}
		return "api_version: humagen/v1\nkind: System\nspec:\n  auth: " + auth + "\n---\n"
	}
	const oidc = "{kind: oidc, oidc: {issuer: 'https://login.example.com', client_id: humagen, audience: humagen}}"
	const tailscale = "{kind: tailscale}"
	surface := func(team, extra string) string {
		return "api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  surfaces:\n    slack: {enabled: true, team: " + team + ", bot_token: {ref: env:B}, app_token: {ref: env:A}" + extra + "}\n"
	}
	const token = ", humagen_token: {ref: env:T}"
	const people = ", people: {U0123ANA: 'tailscale:ana@github', W0123BEN: ben}"
	for name, tc := range map[string]struct{ auth, team, extra, want string }{
		"tailscale with people":            {tailscale, "T0123ACME", people, ""},
		"tailscale with a machine":         {tailscale, "T0123ACME", people + ", machine: slack-app", "remove machine"},
		"tailscale, a machine, no people":  {tailscale, "T0123ACME", ", machine: slack-app", "remove machine"},
		"oidc with people and a machine":   {oidc, "T0123ACME", token + people + ", machine: humagen-slack", ""},
		"oidc with people and no machine":  {oidc, "T0123ACME", token + people, "people need machine: under oidc sign-in the server cannot know"},
		"oidc, a machine, no people":       {oidc, "T0123ACME", token + ", machine: humagen-slack", ""},
		"mock with people and no machine":  {"{kind: mock}", "T0123ACME", token + people, "people need machine: under mock sign-in"},
		"dev with people and no machine":   {"", "T0123ACME", people, "people need machine: under dev sign-in"},
		"dev with people and a machine":    {"", "T0123ACME", people + ", machine: dev-app", ""},
		"no people":                        {tailscale, "T1", "", ""},
		"an empty map":                     {tailscale, "T1", ", people: {}", ""},
		"a user that is no id":             {tailscale, "T0123ACME", ", people: {ana: ana}", `"ana" is not a Slack user id`},
		"a channel for a user":             {tailscale, "T0123ACME", ", people: {C0123ANA: ana}", "is not a Slack user id"},
		"an empty subject":                 {tailscale, "T0123ACME", ", people: {U0123ANA: ' '}", "maps to a subject of 1 to 200 characters"},
		"a subject too long":               {tailscale, "T0123ACME", ", people: {U0123ANA: " + strings.Repeat("a", 201) + "}", "maps to a subject of 1 to 200 characters"},
		"people in a team that is no id":   {tailscale, "acme", people, "is not a team id"},
		"a machine too long":               {oidc, "T0123ACME", token + ", machine: " + strings.Repeat("m", 201), "machine is a credential's subject"},
		"a subject that is not a string":   {tailscale, "T0123ACME", ", people: {U0123ANA: [ana]}", "people"},
		"an unknown field beside people":   {tailscale, "T0123ACME", people + ", persons: {}", "persons"},
		"a default the file does not have": {tailscale, "T0123ACME", people + ", default_agent: lead", "default_agent"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "humagen.yaml")
			if err := os.WriteFile(path, []byte(system(tc.auth)+surface(tc.team, tc.extra)), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if s := cfg.Platforms[0].Spec.Surfaces.Slack; strings.Contains(tc.extra, "U0123ANA") && (len(s.People) != 2 || s.People["U0123ANA"] != "tailscale:ana@github") {
					t.Fatalf("people = %v", s.People)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}

	// A flag may choose the sign-in after the file is checked.
	mapped := Slack{Enabled: true, Team: "T0123ACME", People: map[string]string{"U0123ANA": "ana"}}
	if err := mapped.CheckSignIn("tailscale"); err != nil {
		t.Errorf("tailscale with people: %v", err)
	}
	for _, kind := range []string{"", "dev", "oidc", "okta", "mock"} {
		if err := mapped.CheckSignIn(kind); err == nil {
			t.Errorf("%q: people with no machine were let through", kind)
		}
	}
	mapped.Machine, mapped.HumagenToken = "humagen-slack", &Ref{Ref: "env:T"}
	if err := mapped.CheckSignIn("okta"); err != nil {
		t.Errorf("okta with people and a machine: %v", err)
	}
}
