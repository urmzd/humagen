package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/catalog/bigquery"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/state"
)

func boundedBigQuery(name, kind string) BigQuerySetupInput {
	return BigQuerySetupInput{Name: name, Project: "cloud", Datasets: []string{"sales"}, Credential: BigQuerySetupCredential{Kind: kind}, MaxBytesBilled: 1 << 30, MaxRows: 200, TimeoutSeconds: 60, RefreshHours: 24}
}

func bigQuerySetupPolicy(t *testing.T) *auth.Policy {
	t.Helper()
	policy, err := auth.ParsePolicy("bigquery", `role admin { allow * on * } grant admin to subject "alice"`)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestBigQuerySetupPersistsPrivateBoundedConnectionWithoutCloudCalls(t *testing.T) {
	st := state.NewMemory()
	base, err := bigquery.New(bigquery.Config{Project: "cloud", Datasets: []string{"sales", "support"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Workspace: t.TempDir(), Policy: bigQuerySetupPolicy(t), Backends: Backends{State: st}, Connectors: []CatalogConnector{{Tenant: "t", Connector: catalog.Connector{Name: "deployment", System: "bigquery"}, Querier: base}}}
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := p.byPolicy("t", "alice")
	view, err := p.BigQuerySetup(who)
	if err != nil || view.OAuthAvailable || len(view.ADC) != 1 {
		t.Fatal(view, err)
	}
	created, err := p.CreateBigQueryConnector(t.Context(), who, boundedBigQuery("warehouse", "adc"))
	if err != nil || created.Owner != "alice" || created.CrawledAt != nil || created.Published != nil || created.Entries != 0 {
		t.Fatal(created, err)
	}
	if _, err := p.CreateBigQueryConnector(t.Context(), who, boundedBigQuery("warehouse", "adc")); !errors.Is(err, catalog.ErrExists) {
		t.Fatal("duplicate overwrote config", err)
	}
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	page, err := other.CatalogAs(who, "", 1)
	if err != nil || len(page.Connectors) != 1 || page.Connectors[0].Name != "warehouse" || len(page.Connectors[0].Capabilities) != 2 {
		t.Fatal("restart lost configured connector", page, err)
	}
	client, _, err := (managedBigQuery{p: other, tenant: "t", name: "warehouse"}).client()
	if err != nil || client.Config().MaxRows != 200 || client.Config().Datasets[0] != "sales" {
		t.Fatal("runtime lost bounds", err)
	}
	if got, err := other.BigQuerySetup(other.byPolicy("else", "alice")); err != nil || len(got.ADC) != 0 {
		t.Fatal("cross-tenant ADC grant", got, err)
	}
	cfg.Connectors = nil
	revoked, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = revoked.Close() })
	if _, err := revoked.CatalogStore.Connector("t", "warehouse"); err != nil {
		t.Fatal("revocation deleted retained private metadata", err)
	}
	if _, err := (managedBigQuery{p: revoked, tenant: "t", name: "warehouse"}).Query(t.Context(), "SELECT 1"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("removed ADC grant reached live query", err)
	}
}

func TestBigQuerySetupRejectsExpandedADCAndUnboundedOrForeignCredentials(t *testing.T) {
	base, _ := bigquery.New(bigquery.Config{Project: "cloud", Datasets: []string{"sales"}})
	p, err := New(Config{Workspace: t.TempDir(), Policy: bigQuerySetupPolicy(t), Connectors: []CatalogConnector{{Tenant: "t", Connector: catalog.Connector{Name: "deployment", System: "bigquery"}, Querier: base}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := p.byPolicy("t", "alice")
	for _, change := range []func(*BigQuerySetupInput){func(in *BigQuerySetupInput) { in.Project = "another" }, func(in *BigQuerySetupInput) { in.Datasets = []string{"secret"} }, func(in *BigQuerySetupInput) {
		in.Credential = BigQuerySetupCredential{Kind: "reference", Ref: "env:SERVER_SECRET"}
	}} {
		in := boundedBigQuery("refused", "adc")
		change(&in)
		if _, err := p.CreateBigQueryConnector(t.Context(), who, in); !errors.Is(err, auth.ErrForbidden) {
			t.Fatal("expanded authority accepted", err)
		}
	}
	for _, change := range []func(*BigQuerySetupInput){func(in *BigQuerySetupInput) { in.Datasets = nil }, func(in *BigQuerySetupInput) { in.MaxBytesBilled = 0 }, func(in *BigQuerySetupInput) { in.MaxRows = 10001 }, func(in *BigQuerySetupInput) { in.TimeoutSeconds = 601 }, func(in *BigQuerySetupInput) { in.RefreshHours = 0 }, func(in *BigQuerySetupInput) { in.Credential.Ref = "inline-key" }} {
		in := boundedBigQuery("invalid", "adc")
		change(&in)
		if _, err := p.CreateBigQueryConnector(t.Context(), who, in); !errors.Is(err, catalog.ErrInvalid) {
			t.Fatal("unbounded definition accepted", err)
		}
	}
	manager := auth.Principal{Tenant: "t", Subject: "manager", Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	if _, err := p.CreateBigQueryConnector(t.Context(), manager, boundedBigQuery("admin-only", "adc")); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("non-admin gained ADC", err)
	}
	if _, err := p.CatalogStore.Connector("t", "refused"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("refusal wrote connector", err)
	}
}

func TestBigQueryOAuthBindsOnlyDedicatedOwnerConsentAndRequiresExplicitReconnect(t *testing.T) {
	p, err := New(Config{Workspace: t.TempDir(), Policy: bigQuerySetupPolicy(t), ConnectionAuth: nativeConfig("bigquery")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	who := p.byPolicy("t", "alice")
	in := boundedBigQuery("warehouse", "oauth")
	if _, err = p.CreateBigQueryConnector(t.Context(), who, in); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("unconsented account accepted", err)
	}
	authorizeNative(t, p, who, "bigquery", "person")
	created, err := p.CreateBigQueryConnector(t.Context(), who, in)
	if err != nil {
		t.Fatal(err)
	}
	declared, ok := p.declaredConnector("t", created.Name)
	if !ok || declared.Crawler == nil || declared.Querier != nil {
		t.Fatal("read-only OAuth advertised unsupported SQL")
	}
	if _, err := (managedBigQuery{p: p, tenant: "t", name: created.Name}).Query(t.Context(), "SELECT 1"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("read-only OAuth reached jobs.insert", err)
	}
	_, def, err := p.managedBigQueryDefinition("t", created.Name)
	if err != nil || def.Binding == "" {
		t.Fatal(def, err)
	}
	client, _, err := (managedBigQuery{p: p, tenant: "t", name: created.Name}).client()
	if err != nil || client.Config().TokenSource == nil || client.Config().Credential != "" {
		t.Fatal("OAuth fell back to server identity", err)
	}
	token, err := client.Config().TokenSource(t.Context())
	if err != nil || token == "" {
		t.Fatal("bound token unavailable", err)
	}
	raw, _ := p.CatalogStore.ManagedDefinition("t", created.Name)
	if strings.Contains(string(raw), token) || strings.Contains(string(raw), "refresh-token") {
		t.Fatal("token copied into catalog")
	}
	authorizeNative(t, p, who, "bigquery", "person")
	if _, _, err = (managedBigQuery{p: p, tenant: "t", name: created.Name}).client(); !errors.Is(err, auth.ErrForbidden) {
		t.Fatal("new grant silently rebound old target", err)
	}
	foreign := who
	foreign.Subject = "bob"
	if _, err = p.ReconnectBigQueryConnector(t.Context(), foreign, created.Name); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("another owner rebound connection", err)
	}
	if _, err = p.ReconnectBigQueryConnector(t.Context(), who, created.Name); err != nil {
		t.Fatal(err)
	}
	if _, _, err = (managedBigQuery{p: p, tenant: "t", name: created.Name}).client(); err != nil {
		t.Fatal("explicit reconnect did not restore binding", err)
	}
	if err = p.ConnectionAuth.Disconnect("t", "alice", "bigquery"); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Config().TokenSource(t.Context()); !errors.Is(err, connectionauth.ErrReconnect) {
		t.Fatal("revoked consent still supplied a token", err)
	}
}
