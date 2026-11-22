package platform

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/attachment"
	filesblob "github.com/urmzd/saige/flow/blob"
)

// maxAttachmentText bounds the text of one attached file a reader opens.
const maxAttachmentText = 1 << 20

// AttachmentText is a text file attached to a message, as a reader opens it.
type AttachmentText struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Text      string `json:"text"`
	// Truncated is set when the file is longer than what is shown.
	Truncated bool `json:"truncated,omitempty"`
}

// MessageAttachment reads the text of the file at index n attached to a
// message, for a reader of its session. Only a text file is read here.
func (p *Platform) MessageAttachment(ctx context.Context, who auth.Principal, sessionID, messageID string, n int) (AttachmentText, error) {
	th, err := p.SessionRecord(ctx, who, sessionID)
	if err != nil {
		return AttachmentText{}, err
	}
	m, err := p.Threads.Message(who.Tenant, th.ID, messageID)
	if err != nil {
		return AttachmentText{}, threads.ErrNotFound
	}
	if n < 0 || n >= len(m.Attachments) {
		return AttachmentText{}, threads.ErrNotFound
	}
	a := m.Attachments[n]
	if attachment.KindOf(a.MediaType) != attachment.KindText {
		return AttachmentText{}, fmt.Errorf("%w: only a text file can be opened here", threads.ErrInvalid)
	}
	r, _, err := p.files.Get(ctx, who.Tenant, filesblob.Digest(a.Digest))
	if err != nil {
		return AttachmentText{}, err
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, maxAttachmentText+1))
	if err != nil {
		return AttachmentText{}, err
	}
	out := AttachmentText{Name: a.Label(), MediaType: a.MediaType}
	if len(data) > maxAttachmentText {
		data, out.Truncated = data[:maxAttachmentText], true
	}
	out.Text = strings.ToValidUTF8(string(data), string(utf8.RuneError))
	return out, nil
}
