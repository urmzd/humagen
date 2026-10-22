package github

import (
	transport "github.com/urmzd/fiber/pkg/connectors/github"
	presentation "github.com/urmzd/humagen/internal/presentation/github"
)

// Review contract aliases keep presentation owned by humagen.
type ReviewReport = presentation.ReviewReport
type ReviewFinding = presentation.ReviewFinding
type ReviewEvidence = presentation.ReviewEvidence
type ReviewLink = presentation.ReviewLink

const ReviewFormat = presentation.ReviewFormat

var NewHTTPAPIAt = transport.NewHTTPAPIAt
