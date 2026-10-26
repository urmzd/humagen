package app

import (
	"context"
	"errors"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/spec"
)

type configuredEmail struct {
	spec spec.NotificationEmail
	keys credref.Resolver
}

func (s configuredEmail) Send(ctx context.Context, message notifications.EmailMessage) error {
	password := ""
	if s.spec.Password.Set() {
		var err error
		password, err = s.keys.Resolve(ctx, s.spec.Password.Ref)
		if err != nil {
			return errors.New("notification email credential unavailable")
		}
	}
	sender, err := notifications.NewSMTP(notifications.SMTPConfig{Address: s.spec.Address, From: s.spec.From, Username: s.spec.Username, Password: password, TLS: s.spec.TLS})
	if err != nil {
		return err
	}
	return sender.Send(ctx, message)
}
func emailConfiguration(system spec.System, publicURL string, keys credref.Resolver) *platform.EmailConfig {
	email := system.Spec.Notifications.Email
	if email == nil {
		return nil
	}
	return &platform.EmailConfig{Sender: configuredEmail{spec: *email, keys: keys}, PublicURL: publicURL, Recipients: email.Recipients}
}
