package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/humagen/pkg/credref"
)

const personFile = `api_version: humagen/v1
kind: System
spec:
  auth: {kind: dev, tenant: {claim: org}}
  person_refs:
    default: ["op://People/{subject}/", "env:HUMAGEN_PERSON_{subject}_"]
    globex: ["keychain:globex-people-{subject}/"]
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  workspace:
    person_credentials: {values: true}
  tools:
    - name: issues
      kind: mcp
      mcp:
        transport: http
        endpoint: https://issues.example.com/mcp
        credential_scope: person
        accept:
          - name: search
            description: Search issues
            input_schema: {type: object, properties: {q: {type: string}}}
            schema_hash: 21877437383c4aa0e8fc28bbe3abc1d801bac704010bcdd62a93e30dafd20ab6
            class: read
---
api_version: humagen/v1
kind: Platform
metadata: {name: globex}
spec:
  tools:
    - {name: wiki, kind: mcp, mcp: {transport: http, endpoint: "https://wiki.example.com/mcp", credential: {ref: env:WIKI_TOKEN}}}
`

// The file's person-scoped tool reaches the platform as one, with each
// tool's whole form; where people may keep their credentials becomes the
// operator's flag; whether they may store values is each workspace's.
func TestTheFilesPersonScopedToolsAndWhatPeopleMayGive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	if err := os.WriteFile(path, []byte(personFile), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := loadFiles(t.Context(), Options{}, []string{path}, credref.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	const flag = "-person-refs=default=op://People/{subject}/,env:HUMAGEN_PERSON_{subject}_;globex=keychain:globex-people-{subject}/"
	if !slices.Contains(l.args, flag) {
		t.Fatalf("the System's person_refs became %q", l.args)
	}
	refs, err := personRefs(flag[len("-person-refs="):])
	if err != nil || len(refs["default"]) != 2 || refs["globex"][0] != "keychain:globex-people-{subject}/" {
		t.Fatalf("person refs = %v, %v", refs, err)
	}
	if values := personValues(l.cfg); !values["default"] || values["globex"] {
		t.Fatalf("person values = %v", values)
	}

	ext, err := fileExtensions(t.Context(), Options{}, l.cfg, credref.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	var issues, wiki toolprovider.Provider
	var accepted []toolprovider.Tool
	for _, s := range ext.ToolProviders {
		switch s.Provider.Name {
		case "issues":
			issues, accepted = s.Provider, s.Accept
		case "wiki":
			wiki = s.Provider
		}
	}
	if issues.Scope() != toolprovider.ScopePerson || issues.CredentialRef != "" || wiki.Scope() != toolprovider.ScopeShared || wiki.CredentialRef != "env:WIKI_TOKEN" {
		t.Fatalf("issues = %+v, wiki = %+v", issues, wiki)
	}
	if len(accepted) != 1 || accepted[0].Description != "Search issues" || len(accepted[0].InputSchema) == 0 || accepted[0].Class != toolprovider.ClassRead {
		t.Fatalf("the accepted tool lost its form: %+v", accepted)
	}
	full, err := toolprovider.NewTool(accepted[0].Name, accepted[0].Description, accepted[0].InputSchema)
	if err != nil || full.SchemaHash != accepted[0].SchemaHash {
		t.Fatalf("the declared form does not hash to its pin: %v", err)
	}
}

// Where people may keep their credentials is the operator's to say, only
// somewhere the server can confine, and only as each person's own place.
func TestPersonReferencesAreGivenToTenantsByTheOperator(t *testing.T) {
	if got, err := personRefs(""); err != nil || len(got) != 0 {
		t.Fatalf("with nothing configured, nobody may reference anything: %v, %v", got, err)
	}
	for _, raw := range []string{
		"acme", "=op://People/{subject}/", "acme=", "acme=env:ANTHROPIC_API_KEY", "acme=env:HUMAGEN_OIDC_CLIENT_SECRET", "acme=vault:x", "acme=op://",
		// A start that names nobody is everyone's.
		"acme=op://People/", "acme=env:HUMAGEN_PERSON_",
		// Two workspaces in one place.
		"acme=op://People/{subject}/;globex=op://People/{subject}/",
	} {
		if _, err := personRefs(raw); err == nil {
			t.Errorf("-person-refs %q was accepted", raw)
		}
	}
}
