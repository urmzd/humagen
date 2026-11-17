package platform

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/attachment"
	"github.com/urmzd/saige/flow/attachment/intake"
)

const (
	MaxUploads     = 5
	MaxUploadBytes = 25 << 20
)

// Upload carries bytes supplied with this message, never a caller-selected
// digest or a remote URL. The server sniffs and stores them under its tenant.
type Upload struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
	Kind string `json:"kind,omitempty"`
}

func checkUploads(uploads []Upload) error {
	if len(uploads) > MaxUploads {
		return fmt.Errorf("%w: at most %d uploaded files", threads.ErrInvalid, MaxUploads)
	}
	total := 0
	for _, f := range uploads {
		total += len(f.Data)
		if f.Name == "" || len(f.Name) > 512 || len(f.Data) == 0 || total > MaxUploadBytes {
			return fmt.Errorf("%w: uploads need a name and nonempty bytes, at most %d bytes in total", threads.ErrInvalid, MaxUploadBytes)
		}
		if f.Kind != "" && f.Kind != string(attachment.KindOf(attachment.Sniff(f.Data, f.Name))) {
			return fmt.Errorf("%w: uploaded bytes do not match their content kind", threads.ErrInvalid)
		}
	}
	return nil
}

// storeUploads is called only after the target conversation's posting checks.
// Stored references survive retries and carry the same bytes into later turns.
func (p *Platform) storeUploads(ctx context.Context, tenant string, uploads []Upload) ([]attachment.Attachment, error) {
	if err := checkUploads(uploads); err != nil {
		return nil, err
	}
	var out []attachment.Attachment
	for _, f := range uploads {
		a, err := intake.Store(ctx, p.files, tenant, bytes.NewReader(f.Data), intake.Meta{Name: f.Name, MaxBytes: MaxUploadBytes})
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func uploadText(text string, uploads []Upload) string {
	if strings.TrimSpace(text) != "" || len(uploads) == 0 {
		return text
	}
	return "Please inspect the attached files."
}
