package platform

import (
	"strconv"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

func sourceAuditFields(src knowledge.Source) map[string]string {
	fields := map[string]string{"documents": strconv.Itoa(src.Documents), "content_version": strconv.FormatUint(src.ContentVersion, 10)}
	if src.Lifecycle != nil {
		fields["mode"] = string(src.Lifecycle.Mode)
		fields["provider"] = src.Lifecycle.Provider
		fields["origin"] = src.Lifecycle.Origin
		fields["project"] = src.Lifecycle.Project
		fields["status"] = string(src.Lifecycle.Status)
	}
	return fields
}
func (p *Platform) beginSourceAudit(who auth.Principal, action string, src knowledge.Source, after map[string]string) (*AuditAttempt, error) {
	project := ""
	if src.Lifecycle != nil {
		project = src.Lifecycle.Project
	}
	return p.BeginAudit(who, audit.Change{Action: action, Resource: "source", ID: src.Name, Scope: "project", Project: project, Before: sourceAuditFields(src), After: after})
}
func (p *Platform) finishSourceAudit(attempt *AuditAttempt, result error) {
	if err := attempt.Finish(result); err != nil {
		p.log.Error("platform: source audit completion remains pending", "audit", attempt.ID(), "error", err)
	}
}
