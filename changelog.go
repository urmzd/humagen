package warp

import _ "embed"

// changelog is warp's CHANGELOG.md, which the release tool writes at each
// release. A workspace that sets workspace.release_notes is posted the
// running release's features from it. It is embedded here because the
// module root is the one package that can reach it.
//
//go:embed CHANGELOG.md
var changelog string
