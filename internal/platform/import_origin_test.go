package platform

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// importedFixture is a made-up session, as an import sends it.
func importedFixture() knowledge.Imported {
	at := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	later := at.Add(time.Minute)
	zero, one := 0, 1
	return knowledge.Imported{Tool: knowledge.ToolClaudeAI, ID: "generated-origin", StartedAt: &at, EndedAt: &later, Branches: 1,
		Turns: []knowledge.ImportedTurn{
			{Speaker: knowledge.SpeakerUser, At: &at, Text: "generated question"},
			{Speaker: knowledge.SpeakerAssistant, At: &later, Text: "generated answer"},
		},
		Omitted: knowledge.ImportedOmitted{Turns: &zero, Results: &zero, CutTurns: &zero, Attachments: &one}}
}

// The structure of an imported document is read as the reader: only where
// they see the source, as what the import wrote with the document's current
// title and text, and otherwise as unknown with the reason.
func TestAnImportedDocumentsStructureIsReadAsTheReaderSeesIt(t *testing.T) {
	p := sourceGroundingPlatform(t)
	src, err := p.Knowledge.Get("tenant", "private")
	if err != nil {
		t.Fatal(err)
	}
	cfg := src.Lifecycle.SourceConfig
	want := importedFixture()
	doc, err := p.Knowledge.AddImportedForScope("tenant", "private", "A generated chat", "claude-ai:generated-origin", "# A generated chat", "owner", p.now(), &cfg, want)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := p.Knowledge.AddForScope("tenant", "private", "Written before structures", "claude-ai:generated-legacy", "# Written before structures", "owner", p.now(), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}

	got, err := p.ImportedOrigin(owner, "private", doc.ID)
	if err != nil || got.Session == nil || got.Unknown != "" || !reflect.DeepEqual(*got.Session, want) || got.Document.Digest != doc.Digest {
		t.Fatalf("origin = %+v, %v", got, err)
	}
	if got, err := p.ImportedOrigin(owner, "private", legacy.ID); err != nil || got.Session != nil || got.Unknown != knowledge.NotKept {
		t.Fatalf("legacy = %+v, %v", got, err)
	}

	// Not found alike: a reader outside the source's project, another
	// workspace, a reader who may not read knowledge, another source, a
	// document that is not there.
	for name, tt := range map[string]struct {
		who    auth.Principal
		source string
		id     string
	}{
		"outside the project": {auth.Principal{Tenant: "tenant", Subject: "outsider", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}, "private", doc.ID},
		"another workspace":   {auth.Principal{Tenant: "other", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}, "private", doc.ID},
		"no knowledge:read":   {auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.RunsWrite}}, "private", doc.ID},
		"another source":      {owner, "missing", doc.ID},
		"no such document":    {owner, "private", "missing"},
	} {
		if got, err := p.ImportedOrigin(tt.who, tt.source, tt.id); !errors.Is(err, knowledge.ErrNotFound) || got.Session != nil {
			t.Errorf("%s: origin = %+v, err = %v", name, got, err)
		}
	}

	// An edit leaves the structure untrusted, and says so.
	if _, err := p.Knowledge.ReplaceForScope("tenant", "private", doc.ID, "A generated chat, renamed", "# A generated chat", doc.Digest, p.now(), &cfg); err != nil {
		t.Fatal(err)
	}
	if got, err := p.ImportedOrigin(owner, "private", doc.ID); err != nil || got.Session != nil || got.Unknown != knowledge.Changed {
		t.Fatalf("edited = %+v, %v", got, err)
	}

	// Removed, it is not found.
	if err := p.Knowledge.RemoveForScope("tenant", "private", doc.ID, p.now(), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ImportedOrigin(owner, "private", doc.ID); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("removed: err = %v", err)
	}
}
