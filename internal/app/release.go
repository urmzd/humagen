package app

import (
	"log/slog"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/internal/release"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/spec"
)

// postReleaseNotes posts what the release this process runs brings, its
// breaking changes and features in the changelog, in the inbox of each
// workspace whose Platform document sets workspace.release_notes. Each
// workspace is posted a release once (Platform.PostReleaseNotes). A build
// that is not a release, or a release of fixes alone, posts nothing. A post that fails is logged and
// tried again at the next start, and the server starts either way.
func postReleaseNotes(p *platform.Platform, cfg spec.Config, version, changelog string, log *slog.Logger) {
	var tenants []string
	for _, doc := range cfg.Platforms {
		if doc.Spec.Workspace.ReleaseNotes {
			tenants = append(tenants, doc.Metadata.Name)
		}
	}
	if len(tenants) == 0 {
		return
	}
	n, ok := release.Find(changelog, version)
	if !ok || !n.News() {
		return
	}
	a := platform.Announcement{Subject: n.Subject(), Text: n.Text(inbox.MaxText)}
	for _, tenant := range tenants {
		m, posted, err := p.PostReleaseNotes(tenant, n.Version, a)
		switch {
		case err != nil:
			log.Warn("release notes not posted; the next start tries again", "tenant", tenant, "version", n.Version, "error", err)
		case posted:
			log.Info("release notes posted", "tenant", tenant, "version", n.Version, "message", m.ID)
		}
	}
}
