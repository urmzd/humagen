package platform

import (
	"context"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/event"
)

// Commit evidence is a system observation, never a person's thumbs-up or an
// evaluation feedback candidate. Its durable envelope uses a reserved actor.
func (p *Platform) recordSurfaceEvidence(ctx context.Context, c clients.Client, tenant, id string, b event.Feedback) error {
	sha, ok := strings.CutPrefix(b.Actor.ID, "commit:")
	decoded, err := hex.DecodeString(sha)
	if !ok || err != nil || len(decoded) != 20 || len(sha) != 40 || !b.Actor.Bot || b.Removed || b.Rating != "up" || len(b.Reasons) != 1 || b.Reasons[0] != "suggestion_committed" || c.Surface != clients.GitHub || tenant != c.Tenant || !strings.HasPrefix(id, "github:suggestion:") || !strings.HasSuffix(id, ":"+sha) {
		return errors.New("feedback: invalid commit evidence")
	}
	target, err := address.Parse(b.MessageID)
	if err != nil {
		return err
	}
	comment, _ := target.Param("review_comment")
	commentID, err := strconv.ParseInt(comment, 10, 64)
	if err != nil || commentID <= 0 || b.Surface != "github" || id != "github:suggestion:"+comment+":"+sha {
		return errors.New("feedback: commit evidence does not identify a review comment")
	}
	record, found, err := p.resolveSurface(c, target)
	if err != nil {
		return err
	}
	if !found || record.Run != b.RunID || record.Finding == "" || record.Finding != b.FindingID {
		return errors.New("feedback: commit evidence has no matching delivered finding")
	}
	b.Actor = event.Actor{ID: "system:github:commit:" + sha, Display: "Verified commit evidence", Bot: true}
	b.SessionID = record.Session
	key := "evidence:" + surfaceHash(c.ID, id)
	stored, err := p.surfaceRecords.table.Update(tenant, key, func(old surfaceRecord, exists bool) (surfaceRecord, error) {
		if exists {
			return old, nil
		}
		return surfaceRecord{Run: b.RunID, Session: record.Session, Message: b.MessageID, Finding: b.FindingID, Updated: p.now(), Evidence: &b}, nil
	})
	if err != nil {
		return err
	}
	if stored.Evidence == nil {
		return errors.New("feedback: invalid persisted evidence")
	}
	return p.PublishFeedback(ctx, tenant, "native-"+key, stored.Updated, *stored.Evidence)
}
