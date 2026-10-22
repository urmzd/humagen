package auth_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/auth/authtest"
	"github.com/urmzd/warp/pkg/authz"
)

// legacyRolePermissions is the hardcoded table the policy language replaced,
// copied as it stood, with the projects permissions added since: a viewer
// reads projects, an operator also makes them (anyone who can start a run
// can make a project), and only an admin governs the tree. The built-in
// policy must reproduce it exactly, so that a deployment with no policy file
// behaves as it did.
var legacyRolePermissions = map[auth.Role][]auth.Permission{
	auth.Viewer: {auth.AgentsRead, auth.RunsRead, auth.SchedulesRead, auth.AlertsRead, auth.KnowledgeRead, auth.EvalsRead, auth.BillingRead, auth.ArtifactsRead, auth.ClientsRead, auth.ProjectsRead,
		// Added with the directory: "read on *" has always covered it.
		auth.PeopleRead},
	auth.Operator: {
		auth.AgentsRead, auth.RunsRead, auth.RunsWrite, auth.SchedulesRead, auth.SchedulesWrite,
		auth.AlertsRead, auth.AlertsWrite, auth.KnowledgeRead, auth.EvalsRead, auth.EvalsWrite,
		auth.BillingRead, auth.ArtifactsRead, auth.ArtifactsWrite, auth.ClientsRead,
		auth.ProjectsRead, auth.ProjectsWrite, auth.PeopleRead,
	},
	auth.Admin: auth.Permissions(),
}

// managerPermissions is the manager role, added after the legacy table:
// everything an operator has, and writing agents and knowledge.
var managerPermissions = append(slices.Clone(legacyRolePermissions[auth.Operator]), auth.AgentsWrite, auth.KnowledgeWrite)

func sorted(ps []auth.Permission) []auth.Permission {
	out := slices.Clone(ps)
	slices.Sort(out)
	return out
}

func TestDefaultPolicyReproducesTheLegacyRoles(t *testing.T) {
	pol := auth.DefaultPolicy()
	for role, want := range legacyRolePermissions {
		if got := pol.Permissions([]auth.Role{role}); !slices.Equal(sorted(got), sorted(want)) {
			t.Errorf("%s: policy grants %v, legacy table %v", role, got, want)
		}
		who := auth.Principal{Subject: "u", Tenant: "acme", Roles: []auth.Role{role}}
		if got := who.Granted(); !slices.Equal(sorted(got), sorted(want)) {
			t.Errorf("%s: Granted %v, legacy table %v", role, got, want)
		}
	}
	if got := pol.Compiled().RoleNames(); !slices.Equal(got, []string{"viewer", "operator", "manager", "admin"}) {
		t.Errorf("roles %v: the built-in policy must declare exactly the four roles", got)
	}
	if len(pol.Compiled().Grants()) != 0 {
		t.Error("the built-in policy grants a role by itself; roles must come from configuration")
	}
}

func TestPolicyFileErrorsStopLoading(t *testing.T) {
	for name, src := range map[string]string{
		"syntax":           "role viewer { allow read on }",
		"unknown resource": "role viewer { allow read on rockets }",
		"empty":            "",
	} {
		if _, err := auth.ParsePolicy("p.authz", src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := auth.LoadPolicyFile(t.TempDir() + "/missing.authz"); err == nil {
		t.Error("a missing file was accepted")
	}
}

func TestGroupRolesBecomeGrants(t *testing.T) {
	pol, err := auth.DefaultPolicy().WithGroupRoles(map[string]auth.Role{"ops": auth.Operator})
	if err != nil {
		t.Fatal(err)
	}
	g := pol.Compiled().Grants()
	if len(g) != 1 || g[0].String() != `grant operator to group "ops"` {
		t.Fatalf("grants %v", g)
	}
	if _, err := auth.DefaultPolicy().WithGroupRoles(map[string]auth.Role{"ops": "root"}); err == nil {
		t.Fatal("a group mapped to an undeclared role was accepted")
	}
	iss := authtest.New(t)
	_, err = auth.NewVerifier(context.Background(), auth.OIDCConfig{
		Issuer: iss.URL, ClientID: client, Audience: audience, Tenant: "acme",
		GroupRoles: map[string]auth.Role{"ops": "root"},
	})
	if err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("verifier started with a group mapped to an undeclared role: %v", err)
	}
}

const custom = `
role viewer   { allow read on * }
role operator extends viewer { allow write on runs }
role auditor  extends viewer { deny read on billing }
grant viewer   to anyone
grant operator to group "eng-oncall"
grant auditor  to claim department = "finance"
grant operator to email domain "acme.test"
`

func customVerifier(t *testing.T, iss *authtest.Issuer) *auth.Verifier {
	t.Helper()
	pol, err := auth.ParsePolicy("custom.authz", custom)
	if err != nil {
		t.Fatal(err)
	}
	return verifier(t, iss, func(c *auth.OIDCConfig) {
		c.Policy = pol
		c.GroupRoles = map[string]auth.Role{"warp-operators": auth.Operator}
	})
}

func TestVerifierAssignsRolesThroughThePolicy(t *testing.T) {
	iss := authtest.New(t)
	v := customVerifier(t, iss)
	for _, tc := range []struct {
		name   string
		claims map[string]any
		roles  []auth.Role
	}{
		{"anyone", map[string]any{}, []auth.Role{auth.Viewer}},
		{"group in the policy", map[string]any{"groups": []string{"eng-oncall"}}, []auth.Role{auth.Viewer, auth.Operator}},
		{"group from GroupRoles", map[string]any{"groups": []string{"warp-operators"}}, []auth.Role{auth.Viewer, auth.Operator}},
		{"claim", map[string]any{"department": "finance"}, []auth.Role{auth.Viewer, "auditor"}},
		{"verified email", map[string]any{"email": "ada@acme.test", "email_verified": true}, []auth.Role{auth.Viewer, auth.Operator}},
		{"unverified email", map[string]any{"email": "ada@acme.test"}, []auth.Role{auth.Viewer}},
		{"email_verified as a string", map[string]any{"email": "ada@acme.test", "email_verified": "true"}, []auth.Role{auth.Viewer}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := map[string]any{"nonce": "n1"}
			for k, val := range tc.claims {
				claims[k] = val
			}
			p, err := v.IDToken(context.Background(), iss.Sign(t, "00u1", client, claims), "n1")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(p.Roles, tc.roles) {
				t.Fatalf("roles %v, want %v", p.Roles, tc.roles)
			}
		})
	}
}

func TestPrincipalIsDecidedByItsVerifiersPolicy(t *testing.T) {
	iss := authtest.New(t)
	v := customVerifier(t, iss)
	p, err := v.IDToken(context.Background(), iss.Sign(t, "00u1", client, map[string]any{"nonce": "n1", "department": "finance"}), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Can(auth.RunsRead) || p.Can(auth.BillingRead) {
		t.Fatalf("granted %v; the auditor's deny must remove billing:read", p.Granted())
	}
	d := v.Policy().Explain(p, auth.BillingRead)
	if d.Allowed || d.Policy.Role != "auditor" || !strings.Contains(d.String(), "deny read on billing") {
		t.Fatalf("explain:\n%s", d)
	}
	// The sign-in kept the grant behind the role, though not the claims, so
	// the explanation names it.
	if d.Policy.Grant == nil || d.Policy.Grant.String() != `grant auditor to claim department = "finance"` {
		t.Fatalf("explain names no grant:\n%s", d)
	}
	// Under another policy the kept grants mean nothing, so none is named.
	if d := auth.DefaultPolicy().Explain(p.WithPolicy(nil), auth.RunsRead); d.Policy.Grant != nil {
		t.Fatalf("a grant of another policy was named:\n%s", d)
	}
	// The same roles under the built-in policy would be decided differently,
	// which is why a principal carries the policy that assigned them.
	if !p.WithPolicy(nil).Can(auth.BillingRead) {
		t.Fatal("WithPolicy(nil) should fall back to the built-in policy")
	}
}

// "anyone" is a grant for people. A machine is not given roles, so a policy
// that grants everyone admin still leaves a machine with only its scopes.
func TestPolicyDoesNotWidenAMachine(t *testing.T) {
	iss := authtest.New(t)
	pol, err := auth.ParsePolicy("open.authz", "role admin { allow * on * }\ngrant admin to anyone")
	if err != nil {
		t.Fatal(err)
	}
	v := verifier(t, iss, func(c *auth.OIDCConfig) { c.Policy = pol; c.GroupRoles = nil })
	p, err := v.Bearer(context.Background(), iss.Sign(t, "svc", audience, map[string]any{"scp": []string{"warp:runs:read"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Roles) != 0 || !slices.Equal(p.Granted(), []auth.Permission{auth.RunsRead}) {
		t.Fatalf("machine roles %v, granted %v", p.Roles, p.Granted())
	}
	d := pol.Explain(p, auth.RunsRead)
	if !d.Allowed || !d.Scope {
		t.Fatalf("explain: %+v", d)
	}
}

// A client relays people: the Slack app asks as the person it maps and
// relays their clicks. Their roles come from the policy the deployment set,
// so the machine carries that policy. Before, it fell back to the built-in
// one, which grants nobody anything, and every mapped person was refused.
func TestAMachineCarriesThePolicyForThePeopleItMaps(t *testing.T) {
	iss := authtest.New(t)
	pol, err := auth.ParsePolicy("people.authz", "role operator { allow read, write on runs }\ngrant operator to subject \"ana\"")
	if err != nil {
		t.Fatal(err)
	}
	v := verifier(t, iss, func(c *auth.OIDCConfig) { c.Policy = pol; c.GroupRoles = nil })
	p, err := v.Bearer(context.Background(), iss.Sign(t, "slack-app", audience, map[string]any{"scp": []string{"warp:runs:write"}}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Policy() != pol {
		t.Fatal("the machine does not carry the deployment's policy")
	}
	roles := p.Policy().Roles(authz.Identity{Subject: "ana", Tenant: p.Tenant})
	if len(roles) != 1 || roles[0] != "operator" {
		t.Fatalf("ana's roles through the machine = %v, want [operator]", roles)
	}
	if len(p.Roles) != 0 || !slices.Equal(p.Granted(), []auth.Permission{auth.RunsWrite}) {
		t.Fatalf("the machine itself was widened: roles %v, granted %v", p.Roles, p.Granted())
	}
}

func TestExplainTheZeroPrincipal(t *testing.T) {
	d := auth.DefaultPolicy().Explain(auth.Principal{Roles: []auth.Role{auth.Admin}}, auth.RunsRead)
	if d.Allowed {
		t.Fatal("the zero principal was allowed")
	}
}

func TestIdentityFromClaims(t *testing.T) {
	cfg := auth.OIDCConfig{TenantClaim: "org", GroupsClaim: "roles"}
	id, err := cfg.Identity("u", map[string]any{"org": "globex", "roles": []any{"a", 3, "b"}, "email": "x@y.z", "email_verified": true})
	if err != nil {
		t.Fatal(err)
	}
	if id.Tenant != "globex" || !slices.Equal(id.Groups, []string{"a", "b"}) || id.Email != "x@y.z" {
		t.Fatalf("got %+v", id)
	}
	if _, err := cfg.Identity("u", map[string]any{}); err == nil {
		t.Fatal("an identity with no tenant claim was accepted")
	}
}

func TestManagerIsAnOperatorWhoWritesAgentsAndKnowledge(t *testing.T) {
	pol := auth.DefaultPolicy()
	if r, ok := auth.ParseRole("manager"); !ok || r != auth.Manager {
		t.Fatalf("manager is not a built-in role: %v %v", r, ok)
	}
	got := pol.Permissions([]auth.Role{auth.Manager})
	if !slices.Equal(sorted(got), sorted(managerPermissions)) {
		t.Fatalf("manager grants %v, want %v", got, managerPermissions)
	}
	who := auth.Principal{Subject: "m", Tenant: "acme", Kind: auth.User, Roles: []auth.Role{auth.Manager}}
	for _, p := range []auth.Permission{auth.AgentsWrite, auth.KnowledgeWrite, auth.RunsWrite, auth.SchedulesWrite} {
		if !who.Can(p) {
			t.Errorf("a manager cannot %s", p)
		}
	}
	for _, p := range []auth.Permission{auth.BillingWrite, auth.ClientsWrite, auth.ProjectsGovern} {
		if who.Can(p) {
			t.Errorf("a manager can %s, which stays an admin's", p)
		}
	}
}

// Anyone reads the tree, whoever starts runs makes projects, and only an
// admin governs it: the workspace itself and company projects. A machine
// holds govern only when its token names it, and a policy file can grant it
// by name.
func TestOnlyAnAdminGovernsTheProjectTree(t *testing.T) {
	for _, tc := range []struct {
		role               auth.Role
		read, write, govrn bool
	}{
		{auth.Viewer, true, false, false},
		{auth.Operator, true, true, false},
		{auth.Manager, true, true, false},
		{auth.Admin, true, true, true},
	} {
		who := auth.Principal{Subject: "u", Tenant: "acme", Kind: auth.User, Roles: []auth.Role{tc.role}}
		if who.Can(auth.ProjectsRead) != tc.read || who.Can(auth.ProjectsWrite) != tc.write || who.Can(auth.ProjectsGovern) != tc.govrn {
			t.Errorf("%s: read %v write %v govern %v, want %v %v %v", tc.role,
				who.Can(auth.ProjectsRead), who.Can(auth.ProjectsWrite), who.Can(auth.ProjectsGovern), tc.read, tc.write, tc.govrn)
		}
	}
	machine := auth.Principal{Subject: "ci", Tenant: "acme", Kind: auth.Machine, Scopes: []auth.Permission{auth.ProjectsWrite}}
	if machine.Can(auth.ProjectsGovern) || machine.Can(auth.ProjectsRead) {
		t.Error("a machine scoped to projects:write holds more than its scope")
	}
	pol, err := auth.ParsePolicy("p.authz", "role keeper { allow read, govern on projects }")
	if err != nil {
		t.Fatalf("a policy cannot grant govern by name: %v", err)
	}
	if got := pol.Permissions([]auth.Role{"keeper"}); !slices.Equal(sorted(got), sorted([]auth.Permission{auth.ProjectsRead, auth.ProjectsGovern})) {
		t.Fatalf("keeper holds %v", got)
	}
}
