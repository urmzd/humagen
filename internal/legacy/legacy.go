package legacy

import (
	"path/filepath"
	"slices"
)

// Names the product had as warp that are still read, each with a note.
const (
	// APIVersion is the spec language's old version. Parse accepts it.
	APIVersion = "warp/v1"
	// SlackClient and SlackToken are surfaces.slack's old field names.
	SlackClient = "warp_client"
	SlackToken  = "warp_token"
	// ScopePrefix is the old prefix of a machine client's scopes. Tokens an
	// identity provider mints with it keep their permissions.
	ScopePrefix = "warp:"
	// ReviewFormat is the old id of the GitHub review format. A review an
	// agent writes under it is still rendered.
	ReviewFormat = "warp.github.review.v1"
	// ConfigDir and ConfigFile are where start and setup kept the spec:
	// os.UserConfigDir()/warp/warp.yaml, or warp.yaml in $WARP_HOME.
	ConfigDir  = "warp"
	ConfigFile = "warp.yaml"
	// Command is the old name of the binary. The image keeps a link by it.
	Command = "warp"
	// ComposeDir is where deploy wrote compose projects beside a spec.
	ComposeDir = ".warp"
	// ShareDir is the old install directory beside the binary, share/warp.
	ShareDir = "warp"
	// RepoURL is the repository's old address. GitHub redirects it, and
	// CHANGELOG.md links to it.
	RepoURL = "https://github.com/urmzd/warp"
	// SystemSender is the system sender's name in inbox records written
	// before the rename.
	SystemSender = "warp"
	// ChartName is the chart that installed releases before the rename.
	ChartName = "warp"
)

// Groups are the old default identity provider groups, by role. They are
// still granted while the default groups are in force.
var Groups = map[string]string{
	"warp-admins":    "admin",
	"warp-managers":  "manager",
	"warp-operators": "operator",
	"warp-viewers":   "viewer",
}

// GroupNames are Groups' keys, sorted.
func GroupNames() []string {
	names := make([]string, 0, len(Groups))
	for g := range Groups {
		names = append(names, g)
	}
	slices.Sort(names)
	return names
}

// Kept names: stored data and running deployments are kept under these, so
// they did not change with the product's name.
const (
	// DeployName is the System document's metadata.name default and deploy's
	// Kubernetes namespace default. It names the compose project, the Helm
	// release and its Secret, the compose service and the bundled Postgres
	// user and database of deployments made before the rename.
	DeployName = "warp"
	// DataVolume is the compose volume mounted at DataDir.
	DataVolume = "warp-data"
	// DataDir, SpecDir and WebDir are paths in the image that volumes,
	// generated compose files and specs people wrote bind.
	DataDir = "/var/lib/warp"
	SpecDir = "/etc/warp"
	WebDir  = "/usr/share/warp/web"
	// CacheDir is the directory under os.UserCacheDir() whose workspace/
	// holds the files stored records point at.
	CacheDir = "warp"
)

// WorkspaceDir is where agents write their files when nothing else is set:
// CacheDir/workspace under the user cache directory.
func WorkspaceDir(userCacheDir string) string {
	return filepath.Join(userCacheDir, CacheDir, "workspace")
}
