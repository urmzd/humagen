package clients

import (
	"fmt"
	"slices"
)

// Capabilities only tightens the installation's existing adapter, routing and
// runtime grants. Nil preserves existing configured installations; explicit
// empty lists allow no event or native action.
type Capabilities struct {
	Events          []string `json:"events"`
	Actions         []string `json:"actions"`
	DeliverOutcomes bool     `json:"deliver_outcomes"`
}

func EventIDs(surface Surface) []string {
	if surface == Slack {
		return []string{"app_mention", "message", "slash_command"}
	}
	if surface == GitHub {
		return []string{"issue_comment", "pull_request_review_comment", "pull_request_review", "issues", "pull_request"}
	}
	return []string{}
}
func ActionIDs(surface Surface) []string {
	if surface == Slack {
		return []string{"slack_post_message", "slack_add_reaction"}
	}
	if surface == GitHub {
		return []string{"github_read_context", "github_comment", "github_request_review", "github_add_labels", "github_apply_changes", "github_run_checks"}
	}
	return []string{}
}
func (c Client) AllowsEvent(id string) bool {
	return c.Capabilities == nil || slices.Contains(c.Capabilities.Events, id)
}
func (c Client) AllowsAction(id string) bool {
	return c.Capabilities == nil || slices.Contains(c.Capabilities.Actions, id)
}
func (c Client) AllowsOutcomes() bool { return c.Capabilities == nil || c.Capabilities.DeliverOutcomes }
func (c Capabilities) validate(surface Surface) error {
	if c.Events == nil || c.Actions == nil || len(c.Events) > 16 || len(c.Actions) > 32 {
		return fmt.Errorf("%w: capabilities require bounded explicit event and action lists", ErrInvalid)
	}
	for _, pair := range []struct{ got, allowed []string }{{c.Events, EventIDs(surface)}, {c.Actions, ActionIDs(surface)}} {
		for i, id := range pair.got {
			if !slices.Contains(pair.allowed, id) || slices.Contains(pair.got[:i], id) {
				return fmt.Errorf("%w: unknown or duplicate installation capability", ErrInvalid)
			}
		}
	}
	return nil
}
