package spec

import (
	"errors"
	"github.com/urmzd/humagen/pkg/notifications"
	"net/mail"
	"strings"
)

type NotificationSettings struct {
	Email *NotificationEmail `json:"email,omitempty"`
}
type NotificationEmail struct {
	Address    string                       `json:"address"`
	From       string                       `json:"from"`
	Username   string                       `json:"username,omitempty"`
	Password   *Ref                         `json:"password,omitempty"`
	TLS        string                       `json:"tls,omitempty"`
	Recipients map[string]map[string]string `json:"recipients"`
}

func (s NotificationSettings) check() error {
	if s.Email == nil {
		return nil
	}
	e := s.Email
	password := ""
	if e.Password.Set() {
		password = "configured-reference"
	}
	if _, err := notifications.NewSMTP(notifications.SMTPConfig{Address: e.Address, From: e.From, Username: e.Username, Password: password, TLS: e.TLS}); err != nil {
		return err
	}
	count := 0
	for tenant, people := range e.Recipients {
		for subject, address := range people {
			count++
			if count > 1000 || tenant == "" || subject == "" || len(tenant) > 256 || len(subject) > 512 || strings.ContainsAny(tenant+subject+address, "\x00\r\n") {
				return errors.New("system: invalid notification email recipient mapping")
			}
			if _, err := mail.ParseAddress(address); err != nil {
				return errors.New("system: invalid notification email mailbox")
			}
		}
	}
	return nil
}
