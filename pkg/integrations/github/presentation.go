package github

import (
	presentation "github.com/urmzd/humagen/internal/presentation/github"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// Review contract aliases keep presentation owned by humagen.
type ReviewReport = presentation.ReviewReport
type ReviewFinding = presentation.ReviewFinding
type ReviewEvidence = presentation.ReviewEvidence
type ReviewLink = presentation.ReviewLink

const ReviewFormat = presentation.ReviewFormat

var NewHTTPAPIAt = transport.NewHTTPAPIAt
