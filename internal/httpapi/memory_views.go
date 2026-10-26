package httpapi

import (
	"net/url"
	"strconv"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/memory"
)

// Memory provenance is projected through current resource visibility. Sharing
// reviewed text does not grant access to the private source it was drawn from.
func (s *Server) viewNoteAs(who auth.Principal, n memory.Note) noteView {
	view := viewNote(s.P.Names(who.Tenant), n)
	seer := s.P.Seer(who)
	switch n.Source {
	case memory.Consolidated:
		run, err := s.P.Runs.Get(who.Tenant, n.By)
		if err != nil || !seer.Run(run) {
			view.By, view.ByName = "", ""
			return view
		}
		view.SourceURL = "/conversations/" + url.PathEscape(s.P.RunPlace(who.Tenant, run).Thread)
		view.SourceName = run.Agent
	case memory.Artifact:
		reference, err := url.Parse(n.By)
		if err != nil || reference.Scheme != "humagen" || reference.Host != "artifacts" {
			view.By, view.ByName = "", ""
			return view
		}
		if len(reference.Path) < 2 {
			view.By, view.ByName = "", ""
			return view
		}
		version, err := strconv.Atoi(reference.Query().Get("version"))
		if err != nil || version < 1 {
			view.By, view.ByName = "", ""
			return view
		}
		id := reference.Path[1:]
		artifact, err := s.P.Artifacts.Get(who.Tenant, id)
		if err != nil || artifact.Version != version || !seer.Artifact(artifact) {
			view.By, view.ByName = "", ""
			return view
		}
		view.SourceURL = "/conversations/" + url.PathEscape(artifact.Thread) + "/deliverables/" + url.PathEscape(artifact.ID) + "/provenance"
		view.SourceName = artifact.Title
	}
	return view
}
func (s *Server) viewNotesAs(who auth.Principal, notes []memory.Note) []noteView {
	out := make([]noteView, 0, len(notes))
	for _, note := range notes {
		out = append(out, s.viewNoteAs(who, note))
	}
	return out
}
