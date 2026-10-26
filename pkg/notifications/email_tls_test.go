package notifications

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSMTPRequiresSTARTTLSBeforeAuthenticationOrMail(t *testing.T) {
	for _, mode := range []string{"", "starttls"} {
		name := mode
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var commands []string
			var relayError error
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					relayError = err
					return
				}
				defer func() { _ = conn.Close() }()
				if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					relayError = err
					return
				}
				if _, err := fmt.Fprint(conn, "220 local test relay\r\n"); err != nil {
					relayError = err
					return
				}
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					command := scanner.Text()
					mu.Lock()
					commands = append(commands, command)
					mu.Unlock()
					switch {
					case strings.HasPrefix(command, "EHLO "):
						// Advertise AUTH deliberately. The missing STARTTLS extension
						// must stop the client before credentials or an envelope leave.
						_, err = fmt.Fprint(conn, "250-local test relay\r\n250 AUTH PLAIN\r\n")
					case strings.HasPrefix(command, "HELO "):
						_, err = fmt.Fprint(conn, "250 local test relay\r\n")
					case command == "QUIT":
						_, err = fmt.Fprint(conn, "221 closing\r\n")
					default:
						_, err = fmt.Fprint(conn, "550 command must not be attempted without TLS\r\n")
					}
					if err != nil {
						relayError = err
						return
					}
				}
				relayError = scanner.Err()
			}()
			t.Cleanup(func() {
				_ = listener.Close()
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					t.Error("SMTP test relay did not stop")
				}
			})
			sender, err := NewSMTP(SMTPConfig{Address: listener.Addr().String(), From: "humagen@example.test", Username: "dummy-user", Password: "dummy-password", TLS: mode})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			err = sender.Send(ctx, EmailMessage{ID: "tls-required-test", To: "recipient@example.test", Subject: "Local protocol validation", Body: "No message should be sent."})
			if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
				t.Fatalf("missing STARTTLS was not refused: %v", err)
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("SMTP client did not close after refusing plaintext relay")
			}
			if relayError != nil {
				t.Fatalf("SMTP relay failed: %v", relayError)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(commands) == 0 || !strings.HasPrefix(commands[0], "EHLO ") {
				t.Fatalf("client never inspected relay capabilities: %q", commands)
			}
			for _, command := range commands {
				upper := strings.ToUpper(command)
				if strings.HasPrefix(upper, "AUTH") || strings.HasPrefix(upper, "MAIL") || strings.HasPrefix(upper, "RCPT") || strings.HasPrefix(upper, "DATA") || strings.Contains(command, "dummy-user") || strings.Contains(command, "dummy-password") {
					t.Fatalf("SMTP sent credentials or mail before TLS: %q", commands)
				}
			}
		})
	}
}
