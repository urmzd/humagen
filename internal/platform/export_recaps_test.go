package platform

import (
	"time"
)

// AnnouncementOf is what an agent's answer becomes when it is posted.
func AnnouncementOf(answer string) (subject, text string, post bool) { return announcementOf(answer) }

// ChangeSection is one section of project_changes as a test fills it.
type ChangeSection struct {
	Title string
	Rows  []string
}

// RenderChanges lays sections out as project_changes does, for a project
// with a subtree of n projects.
func RenderChanges(project string, n int, sections []ChangeSection) string {
	scope := changesScope{project: project, projects: make([]string, n), until: time.Unix(0, 0).UTC()}
	var secs []changeSection
	var rows [][]changeRow
	for _, s := range sections {
		secs = append(secs, changeSection{title: s.Title})
		var r []changeRow
		for _, text := range s.Rows {
			r = append(r, changeRow{text: text})
		}
		rows = append(rows, r)
	}
	return renderChanges(scope, secs, rows)
}
