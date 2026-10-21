// Package deploy carries the Helm chart `warp deploy` installs, so the
// binary that reads a spec can also put it in a cluster without a checkout.
package deploy

import "embed"

// Chart is deploy/helm/warp.
//
//go:embed all:helm/warp
var Chart embed.FS
