package notifications

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
	"unicode/utf8"
)

// EmailSender delivers one notification with a stable message identity.
// SMTP acceptance is delivery to the configured relay, not a read receipt.
type EmailSender interface {
	Send(context.Context, EmailMessage) error
}
type EmailMessage struct{ ID, To, Subject, Body string }
type SMTPConfig struct {
	Address  string
	From     string
	Username string
	Password string
	// TLS is starttls (default), implicit, or insecure_loopback for a local test relay without credentials.
	TLS string
}
type SMTPSender struct {
	cfg  SMTPConfig
	host string
	from *mail.Address
}

func NewSMTP(cfg SMTPConfig) (*SMTPSender, error) {
	host, port, err := net.SplitHostPort(cfg.Address)
	if err != nil || len(cfg.Address) > 512 || len(cfg.From) > 1024 || host == "" || port == "" || strings.ContainsAny(host, "\r\n\x00 ") {
		return nil, errors.New("notifications: invalid SMTP address")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil || strings.ContainsAny(cfg.From, "\r\n\x00") {
		return nil, errors.New("notifications: invalid SMTP sender")
	}
	if cfg.TLS == "" {
		cfg.TLS = "starttls"
	}
	switch cfg.TLS {
	case "starttls", "implicit":
	case "insecure_loopback":
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() || cfg.Username != "" || cfg.Password != "" {
			return nil, errors.New("notifications: plaintext SMTP requires a loopback address and no credentials")
		}
	default:
		return nil, errors.New("notifications: invalid SMTP TLS mode")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return nil, errors.New("notifications: SMTP authentication requires both username and password")
	}
	return &SMTPSender{cfg: cfg, host: host, from: from}, nil
}

func (s *SMTPSender) Send(ctx context.Context, message EmailMessage) error {
	recipient, err := mail.ParseAddress(message.To)
	if err != nil || strings.ContainsAny(message.To+message.Subject+message.ID, "\r\n\x00") || len(message.Subject) > 500 || !utf8.ValidString(message.Subject) || message.ID == "" || len(message.ID) > 256 || strings.IndexFunc(message.ID, func(r rune) bool {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		return !allowed
	}) >= 0 {
		return errors.New("notifications: invalid email header")
	}
	if len(message.Body) > 65536 || !utf8.ValidString(message.Body) || strings.ContainsRune(message.Body, 0) {
		return errors.New("notifications: invalid email body")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("notifications: SMTP connection: %w", err)
	}
	defer func() { _ = conn.Close() }()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	secured := s.cfg.TLS == "implicit"
	tlsConfig := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
	if secured {
		protected := tls.Client(conn, tlsConfig)
		if err = protected.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("notifications: SMTP TLS: %w", err)
		}
		conn = protected
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("notifications: SMTP greeting: %w", err)
	}
	defer func() { _ = client.Close() }()
	if s.cfg.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("notifications: SMTP server requires STARTTLS support")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("notifications: SMTP TLS: %w", err)
		}
		secured = true
	}
	if s.cfg.Username != "" {
		if !secured {
			return errors.New("notifications: SMTP authentication requires TLS")
		}
		if err = client.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.host)); err != nil {
			return errors.New("notifications: SMTP authentication failed")
		}
	}
	if err = client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("notifications: SMTP sender refused: %w", err)
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return errors.New("notifications: SMTP recipient refused")
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("notifications: SMTP data: %w", err)
	}
	body := strings.ReplaceAll(strings.ReplaceAll(message.Body, "\r\n", "\n"), "\n", "\r\n")
	var encoded strings.Builder
	quoted := quotedprintable.NewWriter(&encoded)
	if _, err := io.WriteString(quoted, body); err != nil {
		return err
	}
	if err := quoted.Close(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%s@humagen>\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s\r\n", s.from.String(), recipient.String(), mime.QEncoding.Encode("UTF-8", message.Subject), message.ID, time.Now().UTC().Format(time.RFC1123Z), encoded.String())
	if err != nil {
		return fmt.Errorf("notifications: SMTP body: %w", err)
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("notifications: SMTP acceptance: %w", err)
	}
	// DATA's final 250 acknowledged the message. A failed QUIT must not cause a duplicate retry.
	_ = client.Quit()
	return nil
}
