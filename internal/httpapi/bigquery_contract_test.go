package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/catalog/bigquery"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

func bigQueryContractSeed(t *testing.T) platform.CatalogConnector {
	t.Helper()
	query, err := bigquery.New(bigquery.Config{Project: "cloud", Datasets: []string{"sales"}})
	if err != nil {
		t.Fatal(err)
	}
	// Querier only: startup cannot crawl the deployment fixture or use ADC.
	return platform.CatalogConnector{Tenant: "bigquery-contract", Connector: catalog.Connector{Name: "deployment-adc", System: "bigquery", Owner: "deployment-owner"}, Querier: query}
}

func exerciseBigQueryContract(t *testing.T, c *contract) {
	t.Helper()
	token := c.f.person(t, "bigquery-contract", "analyst", "Analyst", "humagen-admins")
	setup := "/api/catalog/connectors/bigquery/setup"
	create := "/api/catalog/connectors/bigquery"
	c.call(token, "GET", setup, setup, "", 200)
	body := `{"name":"local-sales","project":"cloud","datasets":["sales"],"credential":{"kind":"adc"},"max_bytes_billed":1048576,"max_rows":100,"timeout_seconds":30,"refresh_hours":24}`
	created := c.call(token, "POST", create, create, body, 201)
	if created.field("published") != nil || created.field("crawled_at") != nil {
		t.Fatal("new connector claims verified or published state")
	}
	c.call(token, "POST", create, create, body, 409)
	c.call(token, "POST", create, create, `{}`, 400)
	c.call(token, "POST", create, create, strings.Replace(body, `"cloud"`, `"ungranted-project"`, 1), 403)
	c.call(token, "POST", "/api/catalog/connectors/{name}/reconnect", "/api/catalog/connectors/missing/reconnect", "", 404)
	c.call(token, "POST", "/api/catalog/connectors/{name}/reconnect", "/api/catalog/connectors/local-sales/reconnect", "", 403)

	provider := platform.NativeConnectionProvider("bigquery")
	provider.ClientID, provider.ClientSecret = "registered", "fixture-secret"
	manager, err := connectionauth.New(state.NewMemory(), connectionauth.Config{Key: []byte(strings.Repeat("b", 32)), RedirectURL: "https://humagen.test/connections/callback", Providers: []connectionauth.Provider{provider}, HTTP: &http.Client{Transport: connectionContractTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "oauth2.googleapis.com" {
			t.Fatalf("unexpected external boundary: %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"offline-bigquery-token","token_type":"Bearer","expires_in":3600,"scope":"https://www.googleapis.com/auth/bigquery.readonly"}`))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	previous := c.f.p.ConnectionAuth
	c.f.p.ConnectionAuth = manager
	defer func() { c.f.p.ConnectionAuth = previous }()
	a := c.call(token, "POST", "/api/connections/{provider}/authorize", "/api/connections/bigquery/authorize", "", 200)
	u, err := url.Parse(a.str("url"))
	if err != nil {
		t.Fatal(err)
	}
	callback, _ := json.Marshal(ConnectionCallback{State: u.Query().Get("state"), Code: "offline-code"})
	c.call(token, "POST", "/api/connections/{provider}/callback", "/api/connections/bigquery/callback", string(callback), 204)
	c.call(token, "GET", setup, setup, "", 200)
	body = strings.Replace(body, `"local-sales"`, `"personal-sales"`, 1)
	body = strings.Replace(body, `"kind":"adc"`, `"kind":"oauth"`, 1)
	c.call(token, "POST", create, create, body, 201)
	c.call(token, "POST", "/api/catalog/connectors/{name}/reconnect", "/api/catalog/connectors/personal-sales/reconnect", "", 200)
	foreign := c.f.person(t, "bigquery-other", "analyst", "Analyst", "humagen-admins")
	c.call(foreign, "POST", "/api/catalog/connectors/{name}/reconnect", "/api/catalog/connectors/personal-sales/reconnect", "", 404)
}

func TestBigQuerySetupHTTPContract(t *testing.T) {
	f := newFixtureWith(t, platform.Config{Connectors: []platform.CatalogConnector{bigQueryContractSeed(t)}})
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	exerciseBigQueryContract(t, c)
}
