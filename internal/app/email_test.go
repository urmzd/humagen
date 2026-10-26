package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/spec"
)

func TestEmailConfigurationDisabledAndRecipientMapping(t *testing.T) {
	var system spec.System
	if got := emailConfiguration(system, "https://humagen.example.test", credref.Resolver{}); got != nil {
		t.Fatal("default config enabled email")
	}
	system.Spec.Notifications.Email = &spec.NotificationEmail{Address: "127.0.0.1:1025", From: "notify@example.test", TLS: "insecure_loopback", Recipients: map[string]map[string]string{"acme": {"ana": "ana@example.test"}}}
	cfg := emailConfiguration(system, "https://humagen.example.test/app", credref.Resolver{})
	if cfg == nil || cfg.Sender == nil || cfg.PublicURL != "https://humagen.example.test/app" || cfg.Recipients["acme"]["ana"] != "ana@example.test" {
		t.Fatal("native email config not composed into platform")
	}
}

func TestConfiguredEmailResolvesRotatedSecretOnEverySendWithoutLeakingIt(t *testing.T) {
	system := spec.System{}
	system.Spec.Notifications.Email = &spec.NotificationEmail{Address: "127.0.0.1:1", From: "notify@example.test", Username: "humagen", Password: &spec.Ref{Ref: "env:HUMAGEN_TEST_SMTP_PASSWORD"}, Recipients: map[string]map[string]string{"acme": {"ana": "ana@example.test"}}}
	calls := 0
	keys := credref.Resolver{LookupEnv: func(name string) (string, bool) {
		if name != "HUMAGEN_TEST_SMTP_PASSWORD" {
			t.Fatalf("wrong credential reference %q", name)
		}
		calls++
		if calls == 1 {
			return "FIRST-PRIVATE-SMTP-PASSWORD", true
		}
		return "ROTATED-PRIVATE-SMTP-PASSWORD", true
	}}
	cfg := emailConfiguration(system, "https://humagen.example.test", keys)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for i := 0; i < 2; i++ {
		err := cfg.Sender.Send(ctx, notifications.EmailMessage{ID: "isolated-test", To: "ana@example.test", Subject: "Local test", Body: "No external mail"})
		if err == nil {
			t.Fatal("cancelled send reported success")
		}
		if strings.Contains(err.Error(), "PRIVATE-SMTP-PASSWORD") {
			t.Fatal("resolved credential leaked in send error")
		}
	}
	if calls != 2 {
		t.Fatalf("resolved %d times, expected one fresh lookup per send", calls)
	}
}

func TestConfiguredEmailUnavailableSecretFailsBeforeTransport(t *testing.T) {
	system := spec.System{}
	system.Spec.Notifications.Email = &spec.NotificationEmail{Address: "127.0.0.1:1", From: "notify@example.test", Username: "humagen", Password: &spec.Ref{Ref: "op://Private/SMTP/password"}}
	calls := 0
	cfg := emailConfiguration(system, "https://humagen.example.test", credref.Resolver{Run: func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return []byte("DO-NOT-LEAK-PRIVATE-OUTPUT"), errors.New("DO-NOT-LEAK-PRIVATE-ERROR")
	}})
	err := cfg.Sender.Send(t.Context(), notifications.EmailMessage{ID: "isolated-test", To: "ana@example.test", Subject: "Test", Body: "No external mail"})
	if err == nil || calls != 1 {
		t.Fatal("missing secret did not fail closed before transport")
	}
	if strings.Contains(err.Error(), "DO-NOT-LEAK") || strings.Contains(err.Error(), "op://") {
		t.Fatal("credential resolution details exposed")
	}
}

func TestConfiguredEmailDefaultTLSRejectsPlaintextRelayBeforeAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	wire := make(chan string, 1)
	failures := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			failures <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err = fmt.Fprint(conn, "220 local.example.test ESMTP\r\n"); err != nil {
			failures <- err
			return
		}
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			failures <- err
			return
		}
		if !strings.HasPrefix(line, "EHLO ") {
			failures <- errors.New("native SMTP client sent no greeting")
			return
		}
		if _, err = fmt.Fprint(conn, "250 local.example.test\r\n"); err != nil {
			failures <- err
			return
		}
		remaining, err := io.ReadAll(reader)
		if err != nil {
			failures <- err
			return
		}
		wire <- line + string(remaining)
	}()
	system := spec.System{}
	system.Spec.Notifications.Email = &spec.NotificationEmail{Address: listener.Addr().String(), From: "notify@example.test", Username: "humagen", Password: &spec.Ref{Ref: "env:HUMAGEN_TEST_SMTP_PASSWORD"}}
	cfg := emailConfiguration(system, "https://humagen.example.test", credref.Resolver{LookupEnv: func(string) (string, bool) { return "PRIVATE-SMTP-SECRET", true }})
	err = cfg.Sender.Send(t.Context(), notifications.EmailMessage{ID: "isolated-tls-test", To: "ana@example.test", Subject: "Local test", Body: "No external mail"})
	if err == nil {
		t.Fatal("default SMTP mode accepted a relay without STARTTLS")
	}
	select {
	case received := <-wire:
		for _, forbidden := range []string{"AUTH ", "MAIL ", "RCPT ", "DATA", "PRIVATE-SMTP-SECRET"} {
			if strings.Contains(received, forbidden) {
				t.Fatal("credentials or mail reached a plaintext relay")
			}
		}
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(6 * time.Second):
		t.Fatal("local SMTP client did not close refused relay")
	}
}
