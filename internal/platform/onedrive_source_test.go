package platform

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/connectionauth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/state"
)

func TestNativeOneDriveConsentUsesMicrosoftCommonEndpoints(t *testing.T) {
	onedrive, drive := NativeConnectionProvider("onedrive"), NativeConnectionProvider("drive")
	if onedrive.Resource == drive.Resource || onedrive.Offline || !slices.Equal(onedrive.Scopes, []string{"Files.Read", "offline_access", "User.Read"}) ||
		onedrive.AuthorizationURL != "https://login.microsoftonline.com/common/oauth2/v2.0/authorize" || onedrive.TokenURL != "https://login.microsoftonline.com/common/oauth2/v2.0/token" {
		t.Fatalf("OneDrive consent broadened or reused another audience: %+v", onedrive)
	}
	f := newSourceQueryFixture(t, knowledge.Hybrid, &selectedQueryProvider{})
	f.p.PersonContexts = personctx.NewStore()
	f.p.Audit, _ = audit.Open(nil)
	var err error
	f.p.ConnectionAuth, err = connectionauth.New(state.NewMemory(), *nativeConfig("onedrive"))
	if err != nil {
		t.Fatal(err)
	}
	admin := f.who
	admin.Scopes = append(slices.Clone(admin.Scopes), auth.ClientsWrite)
	if _, err = f.p.AuthorizeConnection(t.Context(), admin, "onedrive", ConnectionAuthorizationInput{Scope: "workspace"}); !errors.Is(err, personctx.ErrInvalid) {
		t.Fatal("OneDrive accepted a workspace connection", err)
	}
	a, err := f.p.AuthorizeConnection(t.Context(), f.who, "onedrive", ConnectionAuthorizationInput{Scope: "person"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(a.URL)
	q := u.Query()
	if u.Host != "login.microsoftonline.com" || u.Path != "/common/oauth2/v2.0/authorize" || q.Get("scope") != "Files.Read offline_access User.Read" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Has("access_type") || q.Has("resource") {
		t.Fatalf("authorization request %s", a.URL)
	}
	if err = f.p.CompleteConnection(t.Context(), f.who, "onedrive", q.Get("state"), "code"); err != nil {
		t.Fatal(err)
	}
	presets, err := f.p.ConnectionPresets(f.who)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(presets, func(c ConnectionPreset) bool { return c.ID == "onedrive" })
	if i < 0 || presets[i].Name != "OneDrive" || presets[i].Kind != "source_oauth" || presets[i].Scope != "person" || !presets[i].Connected {
		t.Fatalf("OneDrive preset %+v", presets)
	}
	if name, ok := connectionAlertName("onedrive"); !ok || name != "OneDrive" {
		t.Fatal("OneDrive reconnect alert unnamed")
	}
}

func TestNativeOneDriveSourceRequiresExactConsentAndRevocationDropsResults(t *testing.T) {
	provider := &selectedQueryProvider{docs: []knowledge.ImportDocument{{ID: "selected", Title: "Selected", Text: "release source"}}}
	f := newSourceQueryFixture(t, knowledge.Hybrid, provider)
	f.p.PersonContexts = personctx.NewStore()
	f.p.Audit, _ = audit.Open(nil)
	if _, ok := f.p.sourceProvider("onedrive").(knowledge.HTTPProvider); !ok {
		t.Fatal("OneDrive provider is not registered")
	}
	f.p.cfg.Ext.SourceProviders["onedrive"] = provider
	cfg := f.src.Lifecycle.SourceConfig
	cfg.Provider = "onedrive"
	cfg.Resources = []string{"selected"}
	if _, err := f.p.Knowledge.Create("tenant", "onedrive-docs", "", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	src, err := f.p.Knowledge.Configure("tenant", "onedrive-docs", cfg, f.p.now())
	if err != nil {
		t.Fatal(err)
	}
	f.src = src
	if r := sourceResource(src); r.Provider != "onedrive" || !slices.Equal(r.Attributes["folder"], []string{"folder"}) {
		t.Fatalf("suggestion resource %+v", r)
	}
	f.p.ConnectionAuth, err = connectionauth.New(state.NewMemory(), *nativeConfig("onedrive"))
	if err != nil {
		t.Fatal(err)
	}
	authorizeNative(t, f.p, f.who, "onedrive", "person")
	if _, err = f.p.sourceSecret(context.Background(), f.who, src); knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("source used unbound account")
	}
	if err = f.p.ConnectSourceOAuth(f.who, src.Name, "drive"); !errors.Is(err, knowledge.ErrInvalid) {
		t.Fatal("OneDrive source bound to another provider's account", err)
	}
	if err = f.p.ConnectSourceOAuth(f.who, src.Name, "onedrive"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.sourceSecret(context.Background(), f.who, src); err != nil {
		t.Fatal(err)
	}
	other := f.who
	other.Subject = "other"
	if _, err = f.p.sourceSecret(context.Background(), other, src); knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("source borrowed another person's account")
	}
	detail, err := f.p.GetConnection(t.Context(), f.who, "onedrive")
	if err != nil || !slices.ContainsFunc(detail.Mappings, func(m ConnectionMapping) bool { return m.Kind == "source" && m.ID == src.Name }) {
		t.Fatalf("connection detail does not show the bound source: %+v %v", detail.Mappings, err)
	}
	provider.afterFetch = func() {
		if err := f.p.ConnectionAuth.Disconnect("tenant", "owner", "onedrive"); err != nil {
			t.Fatal(err)
		}
	}
	if rows, err := f.p.searchSourceLive(t.Context(), f.who, src, "release"); len(rows) != 0 || knowledge.FailureCause(err) != knowledge.AuthExpired {
		t.Fatal("revoked source content released", err)
	}
}
