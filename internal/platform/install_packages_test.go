package platform

import (
	"context"
	"testing"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
)

// InstallPackagesForTest installs every package of the platform's agents in
// tenant's workspace on the offline model, as an administrator would on a
// real one. Tests that need the built-in agents call it where the server
// once installed them on its own.
func InstallPackagesForTest(t testing.TB, p *Platform, tenant string) {
	t.Helper()
	admin := auth.Principal{Tenant: tenant, Subject: "admin", Roles: []auth.Role{auth.Admin}}
	for _, name := range []string{"core", "review"} {
		if _, err := p.InstallPackage(context.Background(), admin, name, wire.ModelRef{Provider: "offline", Model: "offline"}); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
	if err := p.EnsureBuiltins(context.Background(), tenant); err != nil {
		t.Fatalf("ensure built-ins: %v", err)
	}
}
