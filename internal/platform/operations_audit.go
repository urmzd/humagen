package platform

import "github.com/urmzd/humagen/pkg/operations"

func (p *Platform) finishOperationsAudit(attempt *AuditAttempt, result error) {
	if err := attempt.Finish(result); err != nil {
		p.log.Error("platform: operation audit outcome remains pending", "attempt", attempt.ID(), "error", err)
	}
}
func operationPolicyFields(rules map[operations.Category]operations.Level) map[string]string {
	out := map[string]string{}
	for category, level := range rules {
		out[string(category)] = string(level)
	}
	return out
}
