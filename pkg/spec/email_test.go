package spec

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNotificationEmailNativeSpecRoundTrip(t *testing.T) {
	cfg := Config{}
	cfg.System.Defaults()
	cfg.System.Spec.Notifications.Email = &NotificationEmail{Address: "smtp.example.test:587", From: "Humagen <notifications@example.test>", Username: "humagen", Password: &Ref{Ref: "env:HUMAGEN_TEST_SMTP_PASSWORD"}, Recipients: map[string]map[string]string{"acme": {"ana": "ana@example.test"}}}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "password_ref:") {
		t.Fatal("SMTP secret bypassed the native reference schema")
	}
	path := filepath.Join(t.TempDir(), "humagen.yaml")
	writeFile(t, path, string(data))
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.System.Spec.Notifications, cfg.System.Spec.Notifications) {
		t.Fatalf("email configuration did not round trip: %+v", got.System.Spec.Notifications)
	}
	if err := got.Check(Kinds{}); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationEmailSpecFailsClosed(t *testing.T) {
	base := NotificationEmail{Address: "smtp.example.test:587", From: "notify@example.test", Recipients: map[string]map[string]string{"acme": {"ana": "ana@example.test"}}}
	for _, tc := range []struct {
		name   string
		change func(*NotificationEmail)
	}{
		{"relay without port", func(e *NotificationEmail) { e.Address = "smtp.example.test" }},
		{"invalid sender", func(e *NotificationEmail) { e.From = "invalid" }},
		{"sender injection", func(e *NotificationEmail) { e.From = "notify@example.test\r\nBcc: other@example.test" }},
		{"unknown security mode", func(e *NotificationEmail) { e.TLS = "off" }},
		{"plaintext remote", func(e *NotificationEmail) { e.TLS = "insecure_loopback" }},
		{"plaintext hostname", func(e *NotificationEmail) { e.Address = "localhost:1025"; e.TLS = "insecure_loopback" }},
		{"plaintext authenticated", func(e *NotificationEmail) {
			e.Address = "127.0.0.1:1025"
			e.TLS = "insecure_loopback"
			e.Username = "humagen"
			e.Password = &Ref{Ref: "env:HUMAGEN_TEST_SMTP_PASSWORD"}
		}},
		{"username without reference", func(e *NotificationEmail) { e.Username = "humagen" }},
		{"reference without username", func(e *NotificationEmail) { e.Password = &Ref{Ref: "env:HUMAGEN_TEST_SMTP_PASSWORD"} }},
		{"empty tenant", func(e *NotificationEmail) {
			e.Recipients = map[string]map[string]string{"": {"ana": "ana@example.test"}}
		}},
		{"empty subject", func(e *NotificationEmail) {
			e.Recipients = map[string]map[string]string{"acme": {"": "ana@example.test"}}
		}},
		{"subject injection", func(e *NotificationEmail) {
			e.Recipients = map[string]map[string]string{"acme": {"ana\nother": "ana@example.test"}}
		}},
		{"invalid mailbox", func(e *NotificationEmail) { e.Recipients = map[string]map[string]string{"acme": {"ana": "invalid"}} }},
		{"mailbox injection", func(e *NotificationEmail) {
			e.Recipients = map[string]map[string]string{"acme": {"ana": "ana@example.test\r\nBcc: other@example.test"}}
		}},
		{"bounded recipients", func(e *NotificationEmail) {
			e.Recipients = map[string]map[string]string{"acme": {}}
			for i := 0; i < 1001; i++ {
				e.Recipients["acme"][fmt.Sprintf("person-%d", i)] = "ana@example.test"
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.change(&e)
			if err := (NotificationSettings{Email: &e}).check(); err == nil {
				t.Fatal("unsafe email configuration accepted")
			}
		})
	}
	for _, raw := range []string{`{"address":"smtp.example.test:587","from":"notify@example.test","username":"humagen","password":"RAW-PRIVATE-PASSWORD","recipients":{}}`, `{"address":"smtp.example.test:587","from":"notify@example.test","username":"humagen","password":{"ref":"file:/tmp/password"},"recipients":{}}`} {
		path := filepath.Join(t.TempDir(), "humagen.yaml")
		var email map[string]any
		if err := json.Unmarshal([]byte(raw), &email); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(map[string]any{"api_version": "humagen/v1", "kind": "System", "spec": map[string]any{"notifications": map[string]any{"email": email}}})
		writeFile(t, path, string(data))
		if _, err := Load(path); err == nil {
			t.Fatal("raw or unsupported SMTP credential accepted")
		}
	}
	for _, address := range []string{"127.0.0.1:1025", "[::1]:1025"} {
		e := base
		e.Address = address
		e.TLS = "insecure_loopback"
		if err := (NotificationSettings{Email: &e}).check(); err != nil {
			t.Fatalf("isolated no-auth loopback rejected: %v", err)
		}
	}
	if err := (NotificationSettings{}).check(); err != nil {
		t.Fatal("disabled email rejected")
	}
}
