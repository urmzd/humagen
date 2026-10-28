package wire

import "github.com/urmzd/fiber/pkg/envelope"

// TextOf is the text a run was raised with.
func TextOf(tag envelope.Tag) string { return tag.Text }

// SetText replaces the text a run is raised with.
func SetText(tag *envelope.Tag, text string) { tag.Text = text }

// NewInput is tag raised with text. Every run Humagen raises, a call's child
// among them, is built here.
func NewInput(tag envelope.Tag, text string) envelope.Tag {
	tag.Text = text
	return tag
}

// The meta keys fiber's connectors write on a tag they raise.
const (
	MetaSlackEvent     = "slack_event"
	MetaSlackChannel   = "slack_channel"
	MetaSlackCommand   = "slack_command"
	MetaGitHubEvent    = "github_event"
	MetaGitHubOwner    = "github_owner"
	MetaGitHubRepo     = "github_repo"
	MetaGitHubLabels   = "github_labels"
	MetaGitHubAuthor   = "github_author"
	MetaGitHubHeadSHA  = "github_head_sha"
	MetaCronSchedule   = "cron_schedule"
	MetaCronExpression = "cron_expression"
)

// Surface is what a connector said about where a tag came from.
type Surface struct {
	SlackEvent    string
	SlackChannel  string
	SlackCommand  string
	GitHubEvent   string
	GitHubOwner   string
	GitHubRepo    string
	GitHubLabels  string
	GitHubAuthor  string
	GitHubHeadSHA string
	CronSchedule  string
}

// SurfaceOf reads the connector keys on a tag. A key it does not carry is
// empty.
func SurfaceOf(tag envelope.Tag) Surface {
	m := tag.Meta
	return Surface{
		SlackEvent:    m[MetaSlackEvent],
		SlackChannel:  m[MetaSlackChannel],
		SlackCommand:  m[MetaSlackCommand],
		GitHubEvent:   m[MetaGitHubEvent],
		GitHubOwner:   m[MetaGitHubOwner],
		GitHubRepo:    m[MetaGitHubRepo],
		GitHubLabels:  m[MetaGitHubLabels],
		GitHubAuthor:  m[MetaGitHubAuthor],
		GitHubHeadSHA: m[MetaGitHubHeadSHA],
		CronSchedule:  m[MetaCronSchedule],
	}
}

// SetGitHubHeadSHA records the head commit a GitHub tag is about. The tag's
// meta must be allocated.
func SetGitHubHeadSHA(tag *envelope.Tag, sha string) { tag.Meta[MetaGitHubHeadSHA] = sha }
