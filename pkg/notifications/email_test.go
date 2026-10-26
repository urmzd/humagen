package notifications

import (
	"context"
	"io"
	"net"
	"net/mail"
	"strings"
	"testing"
)

func TestSMTPRejectsUnsafeConfigurationAndHeaders(t *testing.T) {
	for _, cfg := range []SMTPConfig{
		{Address: "mail.example.com:25", From: "humagen@example.test", TLS: "insecure_loopback"},
		{Address: "127.0.0.1:25", From: "humagen@example.test", TLS: "insecure_loopback", Username: "user", Password: "secret"},
		{Address: "mail.example.com:25", From: "bad\r\nBcc:other@example.test", TLS: "starttls"},
	} {
		if _, err := NewSMTP(cfg); err == nil {
			t.Fatal("accepted unsafe SMTP configuration")
		}
	}
	sender, err := NewSMTP(SMTPConfig{Address: "127.0.0.1:1", From: "humagen@example.test", TLS: "insecure_loopback"})
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.Send(context.Background(), EmailMessage{ID: "n", To: "person@example.test", Subject: "Hi\r\nBcc:other@example.test", Body: "hello"}); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("header rejected before dial: %v", err)
	}
}

func TestSMTPDeliversThroughRealLoopbackProtocol(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			received <- "accept failed"
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.WriteString(conn, "220 local test relay\r\n")
		var body strings.Builder
		data := false
		buf := make([]byte, 1)
		line := ""
		for {
			if _, err = conn.Read(buf); err != nil {
				return
			}
			line += string(buf)
			if !strings.HasSuffix(line, "\r\n") {
				continue
			}
			command := strings.TrimSuffix(line, "\r\n")
			line = ""
			if data {
				if command == "." {
					data = false
					_, _ = io.WriteString(conn, "250 stored\r\n")
				} else {
					body.WriteString(command + "\n")
				}
				continue
			}
			switch {
			case strings.HasPrefix(command, "EHLO"), strings.HasPrefix(command, "HELO"), strings.HasPrefix(command, "MAIL FROM:"), strings.HasPrefix(command, "RCPT TO:"):
				_, _ = io.WriteString(conn, "250 OK\r\n")
			case command == "DATA":
				data = true
				_, _ = io.WriteString(conn, "354 send body\r\n")
			case command == "QUIT":
				received <- body.String()
				_, _ = io.WriteString(conn, "221 bye\r\n")
				return
			default:
				_, _ = io.WriteString(conn, "500 unsupported\r\n")
			}
		}
	}()
	sender, err := NewSMTP(SMTPConfig{Address: listener.Addr().String(), From: "Humagen <humagen@example.test>", TLS: "insecure_loopback"})
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.Send(context.Background(), EmailMessage{ID: "opaque-test", To: "person@example.test", Subject: "Humagen update", Body: "Open Humagen to review your update."}); err != nil {
		t.Fatal(err)
	}
	message := <-received
	parsed, err := mail.ReadMessage(strings.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	addresses, err := parsed.Header.AddressList("To")
	if err != nil || len(addresses) != 1 || addresses[0].Address != "person@example.test" {
		t.Fatalf("recipient: %v %v", addresses, err)
	}
	for _, text := range []string{"Message-ID: <opaque-test@humagen>", "Subject: Humagen update", "Open Humagen"} {
		if !strings.Contains(message, text) {
			t.Fatalf("missing %q in delivered message", text)
		}
	}
}
