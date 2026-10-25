package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/auth/mockidp"
	"github.com/urmzd/humagen/pkg/authz"
)

// policyEnv names the file holding the authorization policy. Unset means the
// built-in policy. See docs/authorization.md.
const policyEnv = "HUMAGEN_AUTHZ_POLICY"

// mockAudience is the API audience the mock provider issues access tokens for.
const mockAudience = "api://humagen-dev"

// groupFlags are the -admin-group, -manager-group, -operator-group and
// -viewer-group flags,
// which predate the policy language and become grants.
type groupFlags struct {
	admin, manager, operator, viewer *string
}

func addGroupFlags(fs *flag.FlagSet) groupFlags {
	return groupFlags{
		admin:    fs.String("admin-group", "humagen-admins", "identity provider group granted the admin role"),
		manager:  fs.String("manager-group", "humagen-managers", "identity provider group granted the manager role"),
		operator: fs.String("operator-group", "humagen-operators", "identity provider group granted the operator role"),
		viewer:   fs.String("viewer-group", "humagen-viewers", "identity provider group granted the viewer role"),
	}
}

// noteOldGroups notes the old default groups granted, which an identity
// provider may hold for another product of the same name.
func noteOldGroups(groupRoles map[string]auth.Role, n *legacy.Notes) {
	var old []string
	for _, g := range legacy.GroupNames() {
		if _, ok := groupRoles[g]; ok {
			old = append(old, g)
		}
	}
	if len(old) > 0 {
		n.Add("the default groups also grant %s, the old names; name your groups in auth.groups or a policy file", strings.Join(old, ", "))
	}
}

// loadPolicy reads the policy named by policyEnv, or returns the built-in
// one, and the group table to add to it. Without a policy file the group
// flags apply with their defaults, as they always have. With one, the file
// says who gets what, and a group flag applies only if it was set on the
// command line. Any problem stops the server.
func loadPolicy(fs *flag.FlagSet, g groupFlags, getenv func(string) string) (*auth.Policy, map[string]auth.Role, error) {
	all := map[string]auth.Role{*g.admin: auth.Admin, *g.manager: auth.Manager, *g.operator: auth.Operator, *g.viewer: auth.Viewer}
	path := strings.TrimSpace(getenv(policyEnv))
	if path == "" {
		// The old default groups keep their roles while the defaults are in
		// force: for each role whose flag was not set.
		set := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
		for group, role := range legacy.Groups {
			if !set[role+"-group"] {
				all[group] = auth.Role(role)
			}
		}
		return auth.DefaultPolicy(), all, nil
	}
	pol, err := auth.LoadPolicyFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", policyEnv, err)
	}
	set := map[string]auth.Role{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "admin-group":
			set[*g.admin] = auth.Admin
		case "manager-group":
			set[*g.manager] = auth.Manager
		case "operator-group":
			set[*g.operator] = auth.Operator
		case "viewer-group":
			set[*g.viewer] = auth.Viewer
		}
	})
	return pol, set, nil
}

// seeGranted records whom the policy grants a role by subject in the
// workspace -tenant names, so they are in its directory before they first
// sign in. With tenants from a claim (authConfig.claimed: -tenant-claim, or
// the mock's) it records nobody: the one policy is every tenant's and a grant names no
// tenant, so a person enters a workspace's directory only through a
// Principal of it, or a client of it mapping them.
func seeGranted(p *platform.Platform, tenant, tenantClaim string, pol *auth.Policy, log *slog.Logger) {
	if tenantClaim != "" {
		return
	}
	if err := p.SeeGranted(tenant, granted(pol)); err != nil {
		log.Warn("people the policy names could not all be recorded", "error", err)
	}
}

// granted is the roles the policy grants to subjects by name, one entry per
// subject.
func granted(pol *auth.Policy) []platform.Grant {
	by := map[string][]string{}
	var order []string
	for _, g := range pol.Compiled().Grants() {
		if g.Subject.Kind != authz.SubjectID || g.Subject.Value == "" {
			continue
		}
		if _, seen := by[g.Subject.Value]; !seen {
			order = append(order, g.Subject.Value)
		}
		for _, r := range g.Roles {
			if !slices.Contains(by[g.Subject.Value], r.Text) {
				by[g.Subject.Value] = append(by[g.Subject.Value], r.Text)
			}
		}
	}
	out := make([]platform.Grant, 0, len(order))
	for _, subject := range order {
		out = append(out, platform.Grant{Subject: subject, Roles: by[subject]})
	}
	return out
}

// authConfig is what the sign-in modes are built from.
type authConfig struct {
	mode, addr, publicURL       string
	tenant, tenantClaim, groups string
	policy                      *auth.Policy
	groupRoles                  map[string]auth.Role
	mockAddr, mockPersonas      string
	getenv                      func(string) string
	log                         *slog.Logger
}

// claimed is the claim each caller's tenant is read from, or "" when every
// caller is in the one workspace -tenant names. The mock always reads one:
// personas may name their own tenant, and one without gets -tenant.
func (c authConfig) claimed() string {
	if c.mode == "mock" && c.tenantClaim == "" {
		return "tenant"
	}
	return c.tenantClaim
}

// newAuthn builds the authenticator for a mode. stop releases what it started.
func newAuthn(ctx context.Context, c authConfig) (authn *httpapi.Authn, stop func(), err error) {
	stop = func() {}
	switch c.mode {
	case "dev":
		if !loopback(c.addr) {
			return nil, stop, fmt.Errorf("-auth=dev grants everyone admin, so it only listens on loopback, not %q", c.addr)
		}
		if !c.policy.HasRole(auth.Admin) {
			return nil, stop, fmt.Errorf("-auth=dev signs everyone in as admin, and the policy declares no admin role")
		}
		c.log.Warn("dev auth: every request is an admin. Do not expose this server.")
		dev := auth.Principal{
			Subject: "dev", Tenant: c.tenant, Kind: auth.User, Name: "Developer", Roles: []auth.Role{auth.Admin},
		}.WithPolicy(c.policy)
		return &httpapi.Authn{Dev: &dev}, stop, nil

	case "tailscale":
		if c.tenantClaim != "" {
			return nil, stop, errors.New("-auth=tailscale uses -tenant, not -tenant-claim")
		}
		authn, err := httpapi.NewTailscaleAuthn(c.addr, c.publicURL, c.tenant, c.policy)
		return authn, stop, err

	case "oidc", "okta":
		v, err := auth.NewVerifier(ctx, auth.OIDCConfig{
			Issuer:      c.getenv("HUMAGEN_OIDC_ISSUER"),
			ClientID:    c.getenv("HUMAGEN_OIDC_CLIENT_ID"),
			Audience:    c.getenv("HUMAGEN_OIDC_AUDIENCE"),
			Tenant:      c.tenant,
			TenantClaim: c.tenantClaim,
			GroupsClaim: c.groups,
			Policy:      c.policy,
			GroupRoles:  c.groupRoles,
		})
		if err != nil {
			return nil, stop, fmt.Errorf("%w (set HUMAGEN_OIDC_ISSUER, HUMAGEN_OIDC_CLIENT_ID, HUMAGEN_OIDC_CLIENT_SECRET and HUMAGEN_OIDC_AUDIENCE, or use -auth=dev or -auth=mock locally)", err)
		}
		authn, err := httpapi.NewAuthn(v, c.getenv("HUMAGEN_OIDC_CLIENT_ID"), c.getenv("HUMAGEN_OIDC_CLIENT_SECRET"), c.publicURL,
			[]string{"openid", "profile", "email", c.groups})
		return authn, stop, err

	case "mock":
		return mockAuthn(ctx, c)
	}
	return nil, stop, fmt.Errorf("-auth must be oidc, okta, tailscale, mock or dev, not %q", c.mode)
}

// mockAuthn starts the development identity provider on loopback and signs in
// against it through the same verifier and sign-in flow as a real provider.
func mockAuthn(ctx context.Context, c authConfig) (*httpapi.Authn, func(), error) {
	stop := func() {}
	if !loopback(c.addr) {
		return nil, stop, fmt.Errorf("-auth=mock lets anyone sign in as anyone, so it only listens on loopback, not %q", c.addr)
	}
	people, machines := mockidp.DefaultPeople(), mockidp.DefaultMachines()
	if c.mockPersonas != "" {
		var err error
		if people, machines, err = readPersonas(c.mockPersonas); err != nil {
			return nil, stop, err
		}
	}
	tenantClaim := c.claimed()
	idp, err := mockidp.Start(mockidp.Config{
		Enabled:      true,
		Addr:         c.mockAddr,
		ClientID:     mockidp.DevClientID,
		ClientSecret: mockidp.DevClientSecret,
		RedirectURIs: []string{strings.TrimSuffix(c.publicURL, "/") + "/auth/callback"},
		Audience:     mockAudience,
		Tenant:       c.tenant,
		TenantClaim:  tenantClaim,
		GroupsClaim:  c.groups,
		People:       people,
		Machines:     machines,
		Log:          c.log,
	})
	if err != nil {
		return nil, stop, err
	}
	stop = func() { _ = idp.Close() }
	v, err := auth.NewVerifier(ctx, auth.OIDCConfig{
		Issuer:      idp.Issuer(),
		ClientID:    mockidp.DevClientID,
		Audience:    mockAudience,
		TenantClaim: tenantClaim,
		GroupsClaim: c.groups,
		Policy:      c.policy,
		GroupRoles:  c.groupRoles,
	})
	if err != nil {
		stop()
		return nil, func() {}, err
	}
	authn, err := httpapi.NewAuthn(v, mockidp.DevClientID, mockidp.DevClientSecret, c.publicURL,
		[]string{"openid", "profile", "email", c.groups})
	if err != nil {
		stop()
		return nil, func() {}, err
	}
	c.log.Warn("mock sign-in: HUMAGEN_OIDC_* is ignored. Sign in with no password as any persona.",
		"sign_in", strings.TrimSuffix(c.publicURL, "/")+"/auth/login", "issuer", idp.Issuer(), "audience", mockAudience)
	return authn, stop, nil
}

// personas is the file -mock-personas reads.
type personas struct {
	People   []mockidp.Person  `json:"people"`
	Machines []mockidp.Machine `json:"machines"`
}

func readPersonas(path string) ([]mockidp.Person, []mockidp.Machine, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("-mock-personas: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p personas
	if err := dec.Decode(&p); err != nil {
		return nil, nil, fmt.Errorf("-mock-personas %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("-mock-personas %s: more than one JSON value", path)
	}
	return p.People, p.Machines, nil
}

// runAuthz is "humagen authz": checking a policy file, and explaining a
// decision for a token's claims the way the server would make it.
func runAuthz(args []string, stdout io.Writer, getenv func(string) string) error {
	const usage = "usage: humagen authz check FILE | authz explain [-claims FILE] [-tenant-claim C] [-groups-claim C] [-*-group G] PERMISSION"
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "check":
		if len(args) != 2 {
			return errors.New(usage)
		}
		pol, err := auth.LoadPolicyFile(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: ok, roles %s, %d grants\n", args[1], strings.Join(pol.Compiled().RoleNames(), ", "), len(pol.Compiled().Grants()))
		return nil
	case "explain":
		fs := flag.NewFlagSet("authz explain", flag.ContinueOnError)
		claimsFile := fs.String("claims", "", "JSON file of a token's claims, such as a decoded ID token's payload. Empty means no caller")
		tenant := fs.String("tenant", "default", "as for serve")
		tenantClaim := fs.String("tenant-claim", "", "as for serve")
		groupsClaim := fs.String("groups-claim", "groups", "as for serve")
		groups := addGroupFlags(fs)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New(usage)
		}
		perm := auth.Permission(fs.Arg(0))
		if !isPermission(perm) {
			return fmt.Errorf("unknown permission %q", perm)
		}
		pol, groupRoles, err := loadPolicy(fs, groups, getenv)
		if err != nil {
			return err
		}
		if pol, err = pol.WithGroupRoles(groupRoles); err != nil {
			return err
		}
		claims := map[string]any{}
		if *claimsFile != "" {
			raw, err := os.ReadFile(*claimsFile)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &claims); err != nil {
				return fmt.Errorf("%s: %w", *claimsFile, err)
			}
		}
		sub, _ := claims["sub"].(string)
		cfg := auth.OIDCConfig{Tenant: *tenant, TenantClaim: *tenantClaim, GroupsClaim: *groupsClaim}
		id, err := cfg.Identity(sub, claims)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, pol.Compiled().Explain(id, string(perm)))
		return nil
	}
	return errors.New(usage)
}

func isPermission(p auth.Permission) bool {
	for _, q := range auth.Permissions() {
		if q == p {
			return true
		}
	}
	return false
}
