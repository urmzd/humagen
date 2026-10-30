package memory

import (
	"github.com/urmzd/humagen/pkg/blob"
	"github.com/urmzd/humagen/pkg/drive"
	"github.com/urmzd/humagen/pkg/state"
)

// openOn opens a store whose files are in blobs, so a test can open it again
// on the same records and bytes.
func openOn(st state.Store, blobs blob.Store) (*Store, error) {
	d, err := drive.Open(st, blobs, drive.WithScreen(Screen))
	if err != nil {
		return nil, err
	}
	return Open(st, d)
}

// OpenOn is openOn for the external tests.
var OpenOn = openOn
