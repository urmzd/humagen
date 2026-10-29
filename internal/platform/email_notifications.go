package platform

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/prefs"
)

const MaxEmailRecipients = 1000
const emailBatch = 5

type EmailConfig struct {
	Sender    notifications.EmailSender
	PublicURL string
	// Recipients is an operator-owned tenant -> subject -> mailbox mapping.
	// Delivery still needs the subject's current standing policy and project access.
	Recipients map[string]map[string]string
}

type emailRecipient struct{ tenant, subject, address string }

func emailRecipients(cfg *EmailConfig) []emailRecipient {
	out := []emailRecipient{}
	if cfg != nil {
		for tenant, people := range cfg.Recipients {
			for subject, address := range people {
				out = append(out, emailRecipient{tenant, subject, address})
			}
		}
	}
	slices.SortFunc(out, func(a, b emailRecipient) int {
		if n := strings.Compare(a.tenant, b.tenant); n != 0 {
			return n
		}
		return strings.Compare(a.subject, b.subject)
	})
	return out
}
func validateEmail(cfg *EmailConfig) error {
	if cfg == nil {
		return nil
	}
	u, err := url.Parse(cfg.PublicURL)
	if cfg.Sender == nil || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("platform: email needs a sender and an absolute public HTTP(S) URL")
	}
	recipients := emailRecipients(cfg)
	if len(recipients) > MaxEmailRecipients {
		return errors.New("platform: too many email recipients")
	}
	for _, r := range recipients {
		if r.tenant == "" || r.subject == "" || len(r.tenant) > 256 || len(r.subject) > 512 || strings.ContainsAny(r.tenant+r.subject+r.address, "\x00\r\n") {
			return errors.New("platform: invalid email recipient mapping")
		}
		if _, err := mail.ParseAddress(r.address); err != nil {
			return errors.New("platform: invalid email recipient mailbox")
		}
	}
	return nil
}

// EmailAvailable discloses only whether this human has a configured delivery
// target and standing permissions. It never returns someone else's mailbox.
func (p *Platform) EmailAvailable(who auth.Principal) bool {
	if p.cfg.Email == nil || who.Kind != auth.User || who.Subject == "" || p.cfg.Email.Recipients[who.Tenant][who.Subject] == "" {
		return false
	}
	return len(p.byPolicy(who.Tenant, who.Subject).Granted()) > 0
}

// DeliverEmailNotifications polls a bounded set of recipients independently of
// browser sessions. Queue identities survive replay; SMTP failures remain
// unacknowledged. Stable Message-ID permits relay deduplication after a crash.
func (p *Platform) DeliverEmailNotifications(ctx context.Context) (int, error) {
	p.emailMu.Lock()
	defer p.emailMu.Unlock()
	if p.cfg.Email == nil {
		return 0, nil
	}
	recipients := emailRecipients(p.cfg.Email)
	if len(recipients) == 0 {
		return 0, nil
	}
	sent := 0
	checked := min(emailBatch, len(recipients))
	for i := 0; i < checked; i++ {
		r := recipients[p.emailCursor%len(recipients)]
		p.emailCursor = (p.emailCursor + 1) % len(recipients)
		if ctx.Err() != nil {
			return sent, ctx.Err()
		}
		who := p.emailReader(r.tenant, r.subject)
		if !p.EmailAvailable(who) || !p.Prefs.Get(r.tenant, r.subject).Personal.Email {
			continue
		}
		if err := p.EmailNotifications.RecheckEmailPolicy(r.tenant, r.subject, p.Prefs.Get(r.tenant, r.subject).Personal, p.now()); err != nil {
			return sent, err
		}
		queue, err := p.notificationsFor(who, p.EmailNotifications)
		if err != nil {
			return sent, err
		}
		for _, delivery := range queue {
			if sent >= emailBatch {
				return sent, nil
			}
			fresh := p.emailReader(r.tenant, r.subject)
			if !p.EmailAvailable(fresh) || !p.Prefs.Get(r.tenant, r.subject).Personal.Email {
				break
			}
			dispatch := p.Prefs.Get(r.tenant, r.subject).Personal
			if !notifications.QuietUntil(dispatch, p.now()).IsZero() {
				continue
			}
			reader := p.inboxReader(p.Projects.Tree(r.tenant), fresh)
			seer := p.Seer(fresh)
			visible := func(id string) bool {
				m, err := p.Inbox.GetAs(r.tenant, id, reader)
				mode := dispatch.NotificationDelivery(string(m.Kind), p.now())
				return err == nil && mode != prefs.Off && (mode != prefs.Digest || delivery.Mode != prefs.Immediate) && fresh.Can(reads(m.Kind)) && seer.Message(m) && m.Unread && !m.Hidden && m.State == inbox.Open
			}
			count := 0
			for _, item := range delivery.Items {
				if visible(item.Inbox) {
					count++
				}
			}
			if count == 0 {
				continue
			}
			w := p.wording(r.tenant)
			message := notifications.EmailMessage{ID: delivery.ID, To: r.address, Subject: w.EmailSubject, Body: fmt.Sprintf("%s\n\n%s/inbox\n\n%s", emailIntro(w, count), strings.TrimRight(p.cfg.Email.PublicURL, "/"), w.EmailFooter)}
			if err := p.cfg.Email.Sender.Send(ctx, message); err != nil {
				return sent, err
			}
			sent++
			if err := p.EmailNotifications.Ack(r.tenant, r.subject, delivery.ID, visible); err != nil {
				return sent, err
			}
		}
	}
	return sent, nil
}
func (p *Platform) runEmailNotifications(ctx context.Context) error {
	if p.cfg.Email == nil {
		return nil
	}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if _, err := p.DeliverEmailNotifications(ctx); err != nil && ctx.Err() == nil {
			p.log.Warn("email notification delivery failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func emailIntro(w Wording, count int) string {
	if count == 1 {
		return w.EmailSingle
	}
	return strings.ReplaceAll(w.EmailMultiple, "{count}", fmt.Sprint(count))
}

// Attribute exceptional reads to this worker, while preserving Seer’s own
// membership-first and fail-closed governance rule.
func (p *Platform) emailReader(tenant, subject string) auth.Principal {
	who := p.byPolicy(tenant, subject)
	reader := who
	seen := map[[2]string]bool{}
	reader.AuditRead = func(actor, project, thread string) bool {
		if actor != subject {
			return false
		}
		at := Place{Project: project, Thread: thread}
		key := [2]string{project, thread}
		if allowed, ok := seen[key]; ok {
			return allowed
		}
		if len(seen) >= 1000 {
			return false
		}
		allowed := p.recordRead(who, at, "worker:email-notifications")
		seen[key] = allowed
		return allowed
	}
	return reader
}
