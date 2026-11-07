package platform

import (
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
)

const metaDryRun = "_humagen.dry_run"

// IsEvaluation separates setup and eval activity from ordinary work.
func IsEvaluation(run runs.Run) bool {
	return run.Origin == OriginEval || wire.Meta(run.Tag())[metaDryRun] == "true"
}
