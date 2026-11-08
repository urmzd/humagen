package platform

import (
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/evals"
)

// Recorded output is private execution input, not merely a suite editing
// concern. A schedule acts only for the person who saved its current suite.
// Re-resolve explicit policy grants on every use; cached sign-in roles and
// system/agent labels never supply unattended replay authority.
func (p *Platform) checkEvalReplayAccess(tenant string, suite evals.Suite, by string) error {
	recorded := false
	for _, c := range suite.Cases {
		recorded = recorded || c.Trace != nil
	}
	if !recorded {
		return nil
	}
	person := by
	if by == "system:schedule" {
		person = suite.CreatedBy
	}
	if person == "" || strings.HasPrefix(person, "system:") || strings.HasPrefix(person, "agent:") || strings.HasPrefix(person, "eval:") {
		return auth.ErrForbidden
	}
	who := p.byPolicy(tenant, person)
	if !who.Can(auth.EvalsWrite) || !who.Can(auth.RunsRead) || !p.Seer(who).Suite(suite) {
		return auth.ErrForbidden
	}
	if suite.Target == evals.Agent && p.MayTry(who, suite.Subject) != nil {
		return auth.ErrForbidden
	}
	return nil
}
