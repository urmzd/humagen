package platform

import (
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// ImportedOrigin reads, as who, one document and the structure of the
// session it was imported from: the tool, the conversation's id, its main
// path with who said what and when, its start and end, and what the import
// left out or cut. The structure is what an import sent in the write of the
// document's revision that is read (knowledge.Imported), never something
// read out of the document's text, title or URI, which a person may edit.
//
// A document with no trusted structure answers with Origin.Session nil and
// Origin.Unknown saying why: knowledge.NotKept for one written before
// structures were kept or not by an import, knowledge.Changed for one whose
// title or text changed since its import, knowledge.Malformed for a kept
// structure this version does not accept. A caller says the structure is
// unknown, and does not guess one.
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
