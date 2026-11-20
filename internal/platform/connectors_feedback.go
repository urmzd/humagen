package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/connectors"
	"github.com/urmzd/saige/flow/event"
)

// surfaceRecords stores only verified native delivery handles. Keys include the
// installation; a comment in another installation never resolves by coincidence.
type surfaceRecords struct{ table *state.Table[surfaceRecord] }
type surfaceRecord struct {
	Run      string          `json:"run,omitempty"`
	Session  string          `json:"session,omitempty"`
	Message  string          `json:"message,omitempty"`
	Finding  string          `json:"finding,omitempty"`
	Channel  string          `json:"channel,omitempty"`
	Updated  time.Time       `json:"updated,omitempty"`
	Created  time.Time       `json:"created,omitempty"`
	Cursor   string          `json:"cursor,omitempty"`
	Page     int             `json:"page,omitempty"`
	Evidence *event.Feedback `json:"evidence,omitempty"`
}

func newSurfaceRecords(st state.Store) *surfaceRecords {
	if st == nil {
		st = state.NewMemory()
	}
	return &surfaceRecords{table: state.NewTable[surfaceRecord](st, "surface-deliveries")}
}
func surfaceHash(parts ...string) string {
	h := sha256.New()
	for _, s := range parts {
		fmt.Fprintf(h, "%d:%s", len(s), s)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func surfaceAddress(a address.Address) string {
	b := address.Address{Connector: a.Connector, Workspace: a.Workspace, Path: append([]string(nil), a.Path...)}
	if a.Connector == "github" {
		b.Workspace = strings.ToLower(b.Workspace)
		if len(b.Path) > 0 {
			b.Path[0] = strings.ToLower(b.Path[0])
		}
		if len(b.Path) > 1 && b.Path[1] == "pull" {
			b.Path[1] = "pulls"
		}
	}
	for _, key := range []string{"thread", "comment", "review_comment"} {
		if v, _ := a.Param(key); v != "" {
			b = b.WithParam(key, v)
		}
	}
	return b.String()
}
func surfaceKey(c clients.Client, a address.Address) string {
	return "handle:" + surfaceHash(c.ID, surfaceAddress(a))
}
func (p *Platform) deliveredFor(c clients.Client) connectors.DeliveryObserver {
	return func(_ context.Context, d connectors.Delivery) error {
		current, ok := p.currentInstallation(c)
		if !ok || d.Tenant != c.Tenant || d.Message.Connector != string(c.Surface) {
			return errors.New("connector delivery: installation unavailable")
		}
		run, err := p.Runs.Get(c.Tenant, d.Run)
		if err != nil {
			return err
		}
		// Cross-surface delivery legitimately uses a different client than ingress.
		if d.Message.IsZero() || !p.surfaceOwns(current, d.Message) {
			return errors.New("connector delivery: destination outside installation")
		}
		r := surfaceRecord{Run: run.ID, Session: run.Thread, Message: d.Message.String(), Finding: d.Finding, Channel: d.Channel, Updated: p.now(), Created: run.CreatedAt}
		batch := p.surfaceRecords.table.Batch()
		batch.Put(c.Tenant, surfaceKey(c, d.Message), r)
		// A stable per-answer index bounds each poll and permits cursor pagination.
		batch.Put(c.Tenant, "poll:"+surfaceHash(c.ID)+":"+surfaceHash(d.Message.String()), r)
		if err := batch.Commit(); err != nil {
			return err
		}
		if surfaceKey(c, d.Target) == surfaceKey(c, d.Message) {
			return nil
		}
		_, err = p.surfaceRecords.table.Update(c.Tenant, surfaceKey(c, d.Target), func(old surfaceRecord, exists bool) (surfaceRecord, error) {
			if exists && (old.Created.After(r.Created) || old.Created.Equal(r.Created) && old.Run > r.Run) {
				return old, nil
			}
			return r, nil
		})
		return err
	}
}
func (p *Platform) surfaceOwns(c clients.Client, a address.Address) bool {
	if a.Connector != string(c.Surface) {
		return false
	}
	if c.Surface == clients.Slack {
		return c.Identity.Team != "" && c.Identity.Team == a.Workspace
	}
	if c.Surface == clients.GitHub {
		for _, owner := range c.Identity.Owners {
			if strings.EqualFold(owner, a.Workspace) {
				return true
			}
		}
	}
	return false
}
func (p *Platform) resolveSurface(c clients.Client, a address.Address) (surfaceRecord, bool, error) {
	current, enabled := p.currentInstallation(c)
	if !enabled || !p.surfaceOwns(current, a) {
		return surfaceRecord{}, false, nil
	}
	it, err := p.surfaceRecords.table.Lookup(c.Tenant, surfaceKey(c, a))
	if errors.Is(err, state.ErrNotFound) {
		return surfaceRecord{}, false, nil
	}
	if err != nil {
		return surfaceRecord{}, false, err
	}
	if _, err = p.Runs.Get(c.Tenant, it.Value.Run); err != nil {
		return surfaceRecord{}, false, err
	}
	return it.Value, true, nil
}
func (p *Platform) continueSurface(c clients.Client, a address.Address) bool {
	_, ok, err := p.resolveSurface(c, a)
	return err == nil && ok
}
func (p *Platform) feedbackFor(c clients.Client) connectors.FeedbackHandler {
	return connectors.FeedbackHandler{
		Resolve: func(_ context.Context, tenant string, a address.Address) (connectors.FeedbackTarget, error) {
			if tenant != c.Tenant {
				return connectors.FeedbackTarget{}, nil
			}
			r, ok, err := p.resolveSurface(c, a)
			return connectors.FeedbackTarget{Found: ok, Run: r.Run, Session: r.Session, Message: r.Message, Finding: r.Finding, Channel: r.Channel}, err
		},
		Record: func(ctx context.Context, tenant, id string, b event.Feedback) error {
			return p.recordSurfaceFeedback(ctx, c, tenant, id, "", b)
		},
		Withdraw: func(ctx context.Context, tenant, id, source string, b event.Feedback) error {
			if source == "" {
				return errors.New("feedback: withdrawal needs an original observation")
			}
			b.Removed = true
			return p.recordSurfaceFeedback(ctx, c, tenant, id, source, b)
		},
		Evidence: func(ctx context.Context, tenant, id string, b event.Feedback) error {
			return p.recordSurfaceEvidence(ctx, c, tenant, id, b)
		},
		Stop: func(ctx context.Context, tenant, run string, actor event.Actor) error {
			if _, enabled := p.currentInstallation(c); !enabled || tenant != c.Tenant || actor.ID == "" || actor.Bot {
				return errors.New("stop: installation or actor refused")
			}
			return p.stopFromSurface(ctx, tenant, c.ID, string(c.Surface), run, actor.ID)
		},
	}
}

func (p *Platform) recordSurfaceFeedback(ctx context.Context, c clients.Client, tenant, id, source string, b event.Feedback) error {
	current, enabled := p.currentInstallation(c)
	if !enabled || tenant != c.Tenant || b.Actor.ID == "" || b.Actor.Bot {
		return errors.New("feedback: installation or actor refused")
	}
	run, err := p.Runs.Get(tenant, b.RunID)
	if err != nil {
		return err
	}
	nativeID := surfaceHash(c.ID, id)
	tag := event.Envelope{Data: event.MessagePosted{Request: event.Request{Actor: b.Actor}}}
	subject := p.surfacePerson(current, tag)
	if subject == "" {
		subject = "native:" + string(c.Surface) + ":" + surfaceHash(c.ID, b.Actor.ID)
	}
	r := feedback.Record{Run: run.ID, Subject: subject, FindingID: b.FindingID, Rating: b.Rating, Comment: b.Comment, Reason: strings.Join(b.Reasons, ",")}
	if b.Removed {
		if source != "" {
			var applied bool
			r, applied, err = p.Feedback.RemoveNativeSource(tenant, nativeID, surfaceHash(c.ID, source), r)
			if err == nil && !applied {
				return nil
			}
		} else {
			r, err = p.Feedback.RemoveNative(tenant, nativeID, r)
		}
	} else {
		r, err = p.Feedback.SaveNative(tenant, nativeID, r)
	}
	if err != nil {
		return err
	}
	b.SessionID = run.Thread
	b.Actor = event.Actor{ID: subject}
	b.Rating, b.Comment = r.Rating, r.Comment
	if r.Reason != "" {
		b.Reasons = []string{r.Reason}
	}
	if err = p.PublishFeedback(ctx, tenant, "native:"+nativeID, r.UpdatedAt, b); err != nil {
		return err
	}
	if !b.Removed {
		return p.RecordFeedbackCandidate(tenant, r)
	}
	p.RefreshGoodAsk(tenant, run.ID)
	return nil
}

// PublishFeedback publishes a persisted assessment without using the execution
// sequence. ID and timestamp must be stable on retries of the same write.
func (p *Platform) PublishFeedback(ctx context.Context, tenant, id string, at time.Time, b event.Feedback) error {
	run, err := p.Runs.Get(tenant, b.RunID)
	if err != nil {
		return err
	}
	if id == "" || at.IsZero() {
		return errors.New("feedback: missing persisted identity")
	}
	b.SessionID = run.Thread
	return p.broker.Publish(ctx, event.Envelope{ID: id, Source: "humagen://feedback", Type: event.TypeFeedback, Channel: run.Tag().Channel, Time: at, Tenant: tenant, Run: run.ID, Agent: run.Agent, Session: run.Thread, Data: b})
}

// pollSurfaceFeedback visits one bounded page per installation and tick. The
// cursor is persistent; process restarts cannot starve older delivered answers.
func (p *Platform) pollSurfaceFeedback(ctx context.Context, st *seat) error {
	poller, ok := st.peer.(interface {
		PollFeedback(context.Context, address.Address, int) (int, error)
	})
	if !ok || st.client.Surface != clients.GitHub {
		return nil
	}
	c := st.client
	if current, enabled := p.currentInstallation(c); !enabled || !current.AllowsEvent("reaction") {
		return nil
	}
	key := "cursor:" + surfaceHash(c.ID)
	cursor, err := p.surfaceRecords.table.Lookup(c.Tenant, key)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return err
	}
	if p.now().Before(cursor.Value.Updated.Add(15 * time.Second)) {
		return nil
	}
	rows, next, err := p.surfaceRecords.table.List(c.Tenant, state.Query{Prefix: "poll:" + surfaceHash(c.ID) + ":", After: cursor.Value.Cursor, Limit: 1})
	if err != nil {
		return err
	}
	v := surfaceRecord{Updated: p.now(), Cursor: next}
	if len(rows) > 0 {
		r := rows[0].Value
		if p.now().Sub(r.Updated) < 30*24*time.Hour {
			target, err := address.Parse(r.Message)
			if err != nil {
				return err
			}
			page := cursor.Value.Page
			if page < 1 {
				page = 1
			}
			more, err := poller.PollFeedback(ctx, target, page)
			if err != nil {
				return err
			}
			if more > 0 {
				v.Cursor = cursor.Value.Cursor
				v.Page = more
			}
		}
	}
	return p.surfaceRecords.table.Put(c.Tenant, key, v)
}
