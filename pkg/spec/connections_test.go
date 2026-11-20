package spec

import "testing"

func TestConnectionAuthorizationConfiguration(t *testing.T) {
	valid := ConnectionAuthorization{EncryptionKey: &Ref{Ref: "env:HUMAGEN_CONNECTION_KEY"}, RedirectURL: "https://humagen.example.com/connections/callback"}
	if err := valid.check(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://humagen.example.com/callback?next=evil", "https://user:pass@humagen.example.com/callback", "http://humagen.example.com/callback"} {
		bad := valid
		bad.RedirectURL = raw
		if bad.check() == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	valid.Clients = map[string]ConnectionOAuthClient{"linear": {Issuer: "https://auth.example.com", ClientID: "registered"}}
	if err := valid.check(); err != nil {
		t.Fatal(err)
	}
	valid.Clients["linear"] = ConnectionOAuthClient{ClientID: "registered"}
	if valid.check() == nil {
		t.Fatal("client not bound to issuer")
	}
}
