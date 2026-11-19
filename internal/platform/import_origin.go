package platform

import (
	"fmt"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// ImportedOrigin reads, as who, one document and the structure of the
// session it was imported from: the tool, the conversation's id, its main
// path with who said what and when, its start and end, and what the import
// left out or cut. The structure is what an import sent through the import
// routes in the write of the document's revision that is read
// (knowledge.Imported), never something read out of the document's text,
// title or URI, which a person may edit.
//
// It is the importer's assertion, not a verified record: the server cannot
// tell that a session came from the tool it names. Origin.ImportedBy and
// Origin.ImportedAt say who sent it, holding knowledge:import, and when; a
// caller that shows the structure names them, and never calls it verified.
//
// A document with no trusted structure answers with Origin.Session nil and
// Origin.Unknown saying why: knowledge.NotKept for one no import wrote,
// knowledge.Changed for one edited since its import (even back to the same
// text), knowledge.Malformed for a kept structure this version does not
// accept. A caller says the structure is unknown, and does not guess one.
//
// who must be able to read knowledge and see the source: a source of a
// project the reader does not see, another tenant's, an excluded document
// and a missing one are all knowledge.ErrNotFound. Origin.Document.Digest
// names the revision read, for a caller that pins what it read.
func (p *Platform) ImportedOrigin(who auth.Principal, source, id string) (knowledge.Origin, error) {
	src, err := p.SourceAs(who, source)
	if err != nil {
		return knowledge.Origin{}, err
	}
	var expected *knowledge.SourceConfig
	if src.Lifecycle != nil {
		cfg := src.Lifecycle.SourceConfig
		expected = &cfg
	}
	return p.Knowledge.OriginForScope(who.Tenant, source, id, expected)
}

// ErrImportNeedsWrite refuses an import by a caller without knowledge:write.
// Its words are the ones a route's own refusal uses ("you need PERMISSION"),
// so a client tells it from a refusal of knowledge:import by the permission
// it names.
var ErrImportNeedsWrite = fmt.Errorf("%w: you need knowledge:write", auth.ErrForbidden)

// ImportDocument adds, as who, a session's document with the session's
// structure, through the import route. The route checks knowledge:import;
// writing a document's text is also writing knowledge, so who must hold
// knowledge:write as well, or nothing is written (auth.ErrForbidden). The
// structure is who's assertion and names them.
func (p *Platform) ImportDocument(who auth.Principal, source, title, uri, text string, at time.Time, expected *knowledge.SourceConfig, session knowledge.Imported) (knowledge.Document, error) {
	if !who.Can(auth.KnowledgeWrite) {
		return knowledge.Document{}, ErrImportNeedsWrite
	}
	return p.Knowledge.AddImportedForScope(who.Tenant, source, title, uri, text, who.Subject, at, expected, session)
}

// ReimportDocument replaces, as who, a session's document and its
// structure in place, from the digest last read, under the same rule as
// ImportDocument. It may replace any document of the source whose uri names
// the session's tool and id, one a person added through the ordinary route
// included: holding knowledge:write, who could edit that document anyway,
// so this is no wider than an ordinary edit, and the structure it leaves
// names who as its importer.
func (p *Platform) ReimportDocument(who auth.Principal, source, id, title, text, digest string, at time.Time, expected *knowledge.SourceConfig, session knowledge.Imported) (knowledge.Document, error) {
	if !who.Can(auth.KnowledgeWrite) {
		return knowledge.Document{}, ErrImportNeedsWrite
	}
	return p.Knowledge.ReplaceImportedForScope(who.Tenant, source, id, title, text, digest, who.Subject, at, expected, session)
}
