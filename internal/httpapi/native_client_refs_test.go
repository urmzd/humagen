package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/clients"
)

func TestNativeInstallationViewsHideDeploymentCredentialReferences(t *testing.T) {
	f := newFixture(t)
	admin := f.machine(t, "acme", everything...)
	const keyRef = "env:PRIVATE_INSTALLATION_KEY_REFERENCE"
	const signingRef = "env:PRIVATE_INSTALLATION_SIGNING_REFERENCE"
	_, err := f.p.Clients.Create("acme", clients.Client{
		ID: "native", Name: "Native GitHub", Surface: clients.GitHub, Managed: clients.ManagedInstall,
		Identity: clients.Identity{AppID: "123", Installation: "456", Owners: []string{"acme"}},
		Credentials: clients.Credentials{
			Token:         clients.SecretRef{Set: true, Ref: keyRef},
			SigningSecret: clients.SecretRef{Set: true, Ref: signingRef},
		},
	}, clients.DefaultCaps(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/clients", "/api/clients/native"} {
		response, body := f.do(t, nil, http.MethodGet, path, admin, "")
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %v", path, response.StatusCode, body)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), keyRef) || strings.Contains(string(raw), signingRef) {
			t.Fatalf("%s exposed a deployment credential reference", path)
		}
	}
	held, err := f.p.Clients.Get("acme", "native")
	if err != nil || held.Credentials.Token.Ref != keyRef || held.Credentials.SigningSecret.Ref != signingRef {
		t.Fatal("projection mutated the stored installation credentials", err)
	}
}
