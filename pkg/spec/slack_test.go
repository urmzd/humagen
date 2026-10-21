package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Slack app calls warp's API, so a sign-in that is not dev is refused
// unless the surface gives the app a credential for it.
func TestASlackSurfaceHasACredentialForTheSignIn(t *testing.T) {
	system := func(auth string) string {
		if auth == "" {
			return ""
		}
		return "api_version: warp/v1\nkind: System\nspec:\n  auth: " + auth + "\n---\n"
	}
	const oidc = "{kind: oidc, oidc: {issuer: 'https://login.example.com', client_id: warp, audience: warp}}"
	const slack = "api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  surfaces:\n    slack: {enabled: true, team: T1, bot_token: {ref: env:B}, app_token: {ref: env:A}"
	const client = ", warp_client: {token_url: 'https://login.example.com/token', client_id: warp-slack, client_secret: {ref: env:C}}"
	const token = ", warp_token: {ref: env:T}"
	for name, tc := range map[string]struct{ auth, extra, want string }{
		"no system is dev":           {"", "", ""},
		"dev needs none":             {"{kind: dev}", "", ""},
		"oidc with none":             {oidc, "", "needs a credential for warp's API"},
		"oidc with client creds":     {oidc, client, ""},
		"oidc with a token":          {oidc, token, ""},
		"mock with none":             {"{kind: mock}", "", "needs a credential"},
		"tailscale with a token":     {"{kind: tailscale}", token, "Tailscale Serve"},
		"both":                       {oidc, client + token, "not both"},
		"a token url that is no url": {oidc, ", warp_client: {token_url: login, client_id: c, client_secret: {ref: env:C}}", "token_url"},
		"a client with no secret":    {oidc, ", warp_client: {token_url: 'https://l.example.com/t', client_id: c}", "client_secret are required"},
		"a secret as a value":        {oidc, ", warp_token: xoxp-123", "reference"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "warp.yaml")
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
	for _, kind := range []string{"", "dev"} {
		if err := bare.CheckSignIn(kind); err != nil {
			t.Errorf("%q: %v", kind, err)
		}
	}
	for _, kind := range []string{"oidc", "okta", "mock", "tailscale"} {
		if err := bare.CheckSignIn(kind); err == nil {
			t.Errorf("%q: a Slack app with no credential was let through", kind)
		}
	}
	withToken := Slack{Enabled: true, Team: "T1", WarpToken: &Ref{Ref: "env:T"}}
	if err := withToken.CheckSignIn("okta"); err != nil {
		t.Errorf("okta with a token: %v", err)
	}
}
