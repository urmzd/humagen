package spec

import (
	"strings"
	"testing"
)

func TestBrandingTextReads(t *testing.T) {
	docs, err := Parse([]byte(`api_version: humagen/v1
kind: System
spec:
  branding:
    welcome: {title: "Welcome to {name}", text: "Ask the field team anything."}
    copy: {navigation.agents: Assistants}
    email: {subject: "{name} updates", multiple: "{count} updates wait in {name}."}
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  branding:
    starters:
      - {agent: lead, label: Plan the week, text: "Plan this week's visits for "}
    copy: {navigation.projects: Accounts}
    email: {footer: "Sent by {name}."}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := docs.check(); err != nil {
		t.Fatal(err)
	}
	m := MergeBranding(docs.System.Spec.Branding, docs.Platforms[0].Spec.Branding)
	if m.Welcome.Title != "Welcome to {name}" || len(m.Starters) != 1 || m.Copy["navigation.agents"] != "Assistants" || m.Copy["navigation.projects"] != "Accounts" {
		t.Fatalf("merged = %+v", m)
	}
	if m.Email.Subject != "{name} updates" || m.Email.Footer != "Sent by {name}." {
		t.Fatalf("email = %+v", m.Email)
	}
	if len(CopyKeys()) < 100 || !copyKeys["navigation.agents"] {
		t.Fatalf("copy keys: %d", len(CopyKeys()))
	}
}

func TestEmptyStartersShowNone(t *testing.T) {
	docs, err := Parse([]byte("api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  branding: {starters: []}\n"))
	if err != nil {
		t.Fatal(err)
	}
	b := docs.Platforms[0].Spec.Branding
	if b.IsZero() || b.Starters == nil || len(b.Starters) != 0 {
		t.Fatalf("starters: [] is set, and empty: %#v", b.Starters)
	}
}

func TestBrandingTextRefuses(t *testing.T) {
	for name, tc := range map[string]struct{ branding, want string }{
		"an unknown copy key":          {`copy: {navigation.agentz: X}`, `no text called "navigation.agentz"`},
		"an empty copy value":          {`copy: {navigation.agents: ""}`, "is empty"},
		"a starter with no agent":      {`starters: [{label: X, text: Y}]`, "starters[0].agent"},
		"a starter with no text":       {`starters: [{agent: lead, label: X}]`, "label and text are required"},
		"too many starters":            {`starters: [` + strings.Repeat(`{agent: a, label: b, text: c},`, MaxStarters+1) + `]`, "at most 6"},
		"an unknown email field":       {`email: {body: X}`, "body"},
		"an unknown placeholder":       {`email: {subject: "{count} for {user}"}`, "{count} is not a placeholder here"},
		"a count with no {count}":      {`email: {multiple: Some updates}`, "put {count} in it"},
		"a welcome title on two lines": {`welcome: {title: "a\nb"}`, "welcome.title: one line"},
	} {
		t.Run(name, func(t *testing.T) {
			docs, err := Parse([]byte("api_version: humagen/v1\nkind: System\nspec:\n  branding:\n    " + tc.branding + "\n"))
			if err == nil {
				err = docs.check()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
