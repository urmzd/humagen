package app

import (
	"fmt"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/connectors/cron"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/spec"
)

// applySchedules merges the file's schedules into the workspace's, as the
// tree is merged: a schedule the file declares is created, or taken over from
// the console, filed in its project and marked as the file's, which the API
// then answers 409 for. Its paused state stays the console's. One that left
// the file stays, unmanaged. It runs once the file's agents and their homes
// are in, so an agent not available in its project fails the start, naming
// the schedule.
func applySchedules(p *platform.Platform, tenant string, w spec.Workspace) error {
	now := time.Now().UTC()
	declared := map[string]bool{}
	for i, pr := range w.Projects {
		for j, d := range pr.Schedules {
			at := fmt.Sprintf("platform %q: workspace.projects[%d].schedules[%d]", tenant, i, j)
			agent := wire.NormalizeName(d.Agent)
			if err := p.Available(tenant, pr.Name, agent); err != nil {
				return fmt.Errorf("%s.agent: %w", at, err)
			}
			meta := map[string]string{runs.MetaProject: pr.Name}
			if d.Deliver != "" {
				meta[platform.MetaDeliver] = d.Deliver
			}
			sc := cron.Schedule{Name: d.Name, Expression: d.Cron, Agent: agent, Text: d.Text, Tenant: tenant, Context: meta}
			if err := p.InProject(tenant, pr.Name, func() error {
				_, err := p.Schedules.Declare(sc, d.Description, now)
				return err
			}); err != nil {
				return fmt.Errorf("%s: %w", at, err)
			}
			declared[d.Name] = true
		}
	}
	scheds, err := p.Schedules.List(tenant)
	if err != nil {
		return fmt.Errorf("platform %q: schedules: %w", tenant, err)
	}
	for _, sc := range scheds {
		if !declared[sc.Name] && sc.About.Managed != "" {
			if err := p.Schedules.Release(tenant, sc.Name); err != nil {
				return fmt.Errorf("platform %q: schedule %q left the file: %w", tenant, sc.Name, err)
			}
		}
	}
	return nil
}
