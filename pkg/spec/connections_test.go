package spec

import "testing"

func TestConnectionAuthorizationConfiguration(t *testing.T) {
	valid := ConnectionAuthorization{EncryptionKey: &Ref{Ref: "env:HUMAGEN_CONNECTION_KEY"}, RedirectURL: "https://humagen.example.com/connections/callback"}
	if err := valid.check(""); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://humagen.example.com/callback?next=evil", "https://user:pass@humagen.example.com/callback", "http://humagen.example.com/callback"} {
		bad := valid
		bad.RedirectURL = raw
		if bad.check("") == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	valid.Clients = map[string]ConnectionOAuthClient{"linear": {Issuer: "https://auth.example.com", ClientID: "registered"}}
	if err := valid.check(""); err != nil {
		t.Fatal(err)
	}
	valid.Clients["linear"] = ConnectionOAuthClient{ClientID: "registered"}
	if valid.check("") == nil {
		t.Fatal("client not bound to issuer")
	}
	valid.Clients = map[string]ConnectionOAuthClient{"onedrive": {ClientID: "registered", ClientSecret: &Ref{Ref: "env:HUMAGEN_ONEDRIVE_SECRET"}}}
	if err := valid.check(""); err != nil {
		t.Fatal(err)
	}
	valid.Clients["onedrive"] = ConnectionOAuthClient{ClientID: "registered", ClientSecret: &Ref{Ref: "env:HUMAGEN_ONEDRIVE_SECRET"}, Scopes: []string{"Files.ReadWrite.All"}}
	if valid.check("") == nil {
		t.Fatal("OneDrive scopes widened by configuration")
	}
	valid.Clients = map[string]ConnectionOAuthClient{
		"google":    {ClientID: "registered", ClientSecret: &Ref{Ref: "env:HUMAGEN_GOOGLE_SECRET"}},
		"atlassian": {Issuer: "https://auth.atlassian.com/tenant", ClientID: "registered"},
	}
	if err := valid.check(""); err != nil {
		t.Fatal(err)
	}
	valid.Clients["google"] = ConnectionOAuthClient{ClientID: "registered", ClientSecret: &Ref{Ref: "env:HUMAGEN_GOOGLE_SECRET"}, Scopes: []string{"https://mail.google.com/"}}
	if valid.check("") == nil {
		t.Fatal("Google scopes widened by configuration")
	}
	valid.Clients = map[string]ConnectionOAuthClient{"gmail": {Issuer: "https://accounts.google.com", ClientID: "registered"}}
	if valid.check("") == nil {
		t.Fatal("a Google tool server took a client of its own; one google client serves them all")
	}
}

// With no redirect_url, the derived callback is checked: server.public_url's,
// so a public URL in plain HTTP off loopback is refused.
func TestAnEmptyRedirectURLIsThePublicURLsAndHeldToTheSameRule(t *testing.T) {
	c := ConnectionAuthorization{EncryptionKey: &Ref{Ref: "env:HUMAGEN_CONNECTION_KEY"}}
	for public, ok := range map[string]bool{
		"https://humagen.example.com/": true,
		"http://localhost:5173":        true,
		"http://127.0.0.1:8080":        true,
		"":                             true,
		"http://humagen.example.com":   false,
	} {
		if err := c.check(public); (err == nil) != ok {
			t.Errorf("public_url %q: %v", public, err)
		}
	}
	if got := c.Redirect("https://humagen.example.com/"); got != "https://humagen.example.com/connections/callback" {
		t.Fatalf("redirect %q", got)
	}
}
