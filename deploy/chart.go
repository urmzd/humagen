// Package deploy carries the Helm chart `humagen deploy` installs, so the
// binary that reads a spec can also put it in a cluster without a checkout.
package deploy

import "embed"

// Chart is deploy/helm/humagen.
//
//go:embed all:helm/humagen
var Chart embed.FS
