package platform

import (
	"slices"

	"github.com/urmzd/warp/pkg/artifacts"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/runs"
)

// Read confinement: one rule decides whether a reader sees something that
// lives in a project, and every read path asks it. What a reader may not see
// answers as what does not exist, 404, so trying learns nothing.
//
// The rule is a pure function, seesPlace. A place is where an object lives:
// its project, and the thread it belongs to when it belongs to one. Runs,
// inbox messages and artifacts each name theirs.

// threadAccess is what reading a thread turns on: whether only its
// participants read it, and who they are, by subject.
type threadAccess struct {
	Private      bool
	Participants []string
}

// seesPlace reports whether a reader sees what lives in a place. viewer is
// whether the reader's role in the place's project is at least viewer, and
// th is the place's thread, nil when it has none or none is recorded, which
// reads as shared. A reader holding projects:govern sees everything. Anyone
// else needs a role in the project, and in a private thread to take part in
// it. A reader with no subject takes part in nothing.
func seesPlace(viewer, govern bool, th *threadAccess, subject string) bool {
	switch {
	case govern:
		return true
	case !viewer:
		return false
	case th == nil || !th.Private:
		return true
	}
	return subject != "" && slices.Contains(th.Participants, subject)
}

// placeOfRun is where a run lives: its conversation, in the project it is
// filed in. A run another run called has its root's.
func placeOfRun(r runs.Run) Place { return Place{Project: r.Project, Thread: r.Thread} }

// placeOfSource is where an inbox message's subject lives.
func placeOfSource(s inbox.Source) Place { return Place{Project: s.Project, Thread: s.Thread} }

// placeOfArtifact is where a published file lives: the conversation that
// published it, in its project.
func placeOfArtifact(a artifacts.Artifact) Place { return Place{Project: a.Project, Thread: a.Thread} }
