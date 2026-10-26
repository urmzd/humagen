package platform

import (
	"cmp"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"slices"
	"strings"
)

// CurrentPlace resolves the authoritative current audience of a conversation.
// Run and artifact attribution remains immutable in the execution stores.
func (p *Platform) CurrentPlace(tenant string, at Place) Place {
	if at.Thread != "" {
		if th, err := p.Threads.Get(tenant, at.Thread); err == nil {
			at.Project = th.Project
		}
	}
	return at
}

// RunPlace returns the run's current conversation audience, without changing
// its immutable execution and accounting attribution.
func (p *Platform) RunPlace(tenant string, run runs.Run) Place {
	return p.CurrentPlace(tenant, placeOfRun(run))
}

// ShownRun projects current conversation placement for operational reads. The
// execution record, accounting ancestry and replay input remain untouched.
func (p *Platform) ShownRun(tenant string, run runs.Run) runs.Run {
	if th, err := p.Threads.Get(tenant, run.Thread); err == nil && (th.Project != run.Project || th.Channel != cmp.Or(run.Channel, projects.General)) {
		if run.Outcome != nil {
			outcome := *run.Outcome
			outcome.Decisions = slices.Clone(outcome.Decisions)
			run.Outcome = &outcome
		}
		// Retained evidence can carry an original private source title or
		// passage even after its journal event expired. A placement move does
		// not grant those original capabilities to the new audience.
		run.Citations = nil
		hidden := append([]string{run.Project}, run.Within...)
		hidden = append(hidden, runs.SourcesOf(run.Tag().Meta)...)
		for _, old := range hidden {
			if old == "" {
				continue
			}
			run.Text = strings.ReplaceAll(run.Text, old, "[Private context]")
			run.Answer = strings.ReplaceAll(run.Answer, old, "[Private context]")
			run.Error = strings.ReplaceAll(run.Error, old, "[Private context]")
			run.Reason = strings.ReplaceAll(run.Reason, old, "[Private context]")
			run.Cause = strings.ReplaceAll(run.Cause, old, "[Private context]")
			if run.Outcome != nil {
				run.Outcome.Summary = strings.ReplaceAll(run.Outcome.Summary, old, "[Private context]")
				run.Outcome.NextAction = strings.ReplaceAll(run.Outcome.NextAction, old, "[Private context]")
				for i, decision := range run.Outcome.Decisions {
					run.Outcome.Decisions[i] = strings.ReplaceAll(decision, old, "[Private context]")
				}
			}
		}
		run.Project, run.Channel = th.Project, th.Channel
		run.Within = nil
		run.Input = nil
	}
	return run
}

func (p *Platform) ShownRuns(tenant string, list []runs.Run) []runs.Run {
	out := make([]runs.Run, len(list))
	for i, run := range list {
		out[i] = p.ShownRun(tenant, run)
	}
	return out
}

// ShownArtifact projects a file's operational placement while its original
// storage accounting attribution remains immutable.
func (p *Platform) ShownArtifact(tenant string, a artifacts.Artifact) artifacts.Artifact {
	original := a.Project
	a.Project = p.CurrentPlace(tenant, Place{Project: a.Project, Thread: a.Thread}).Project
	if original != "" && original != a.Project {
		a.Title = strings.ReplaceAll(a.Title, original, "published")
		a.Description = strings.ReplaceAll(a.Description, original, "[Previous project]")
		a.Preview = strings.ReplaceAll(a.Preview, original, "[Previous project]")
	}
	return a
}

func (p *Platform) inboxProject(tenant string) func(inbox.Source) string {
	return func(source inbox.Source) string {
		at := Place{Project: source.Project, Thread: source.Thread}
		if at.Thread == "" && source.Run != "" {
			if run, err := p.Runs.Get(tenant, source.Run); err == nil {
				at = p.RunPlace(tenant, run)
			}
		}
		return p.CurrentPlace(tenant, at).Project
	}
}
