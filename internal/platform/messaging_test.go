package platform_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/attachment"
	"github.com/urmzd/orchagent/flow/connectors/imessage"
	"github.com/urmzd/orchagent/flow/connectors/whatsapp"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/signature"
	saigetypes "github.com/urmzd/saige/agent/types"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
)

// phones hands each WhatsApp client the fake its access token names, and each
// iMessage client the fake its server password names.
type phones struct {
	mu       sync.Mutex
	whatsapp map[string]*whatsapp.Fake
	imessage map[string]*imessage.Fake
}

func (f *phones) wa(token string) *whatsapp.Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.whatsapp == nil {
		f.whatsapp = map[string]*whatsapp.Fake{}
	}
	if f.whatsapp[token] == nil {
		f.whatsapp[token] = whatsapp.NewFake()
	}
	return f.whatsapp[token]
}

func (f *phones) im(password string) *imessage.Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.imessage == nil {
		f.imessage = map[string]*imessage.Fake{}
	}
	if f.imessage[password] == nil {
		f.imessage[password] = imessage.NewFake()
	}
	return f.imessage[password]
}

func (f *phones) config() platform.Config {
	return platform.Config{
		WhatsAppAPI:    func(token string) whatsapp.API { return f.wa(token) },
		IMessageAPI:    func(_, password string) imessage.API { return f.im(password) },
		ClientInterval: -1,
	}
}

func whatsAppClient(id, number, agent string) platform.ClientInput {
	return platform.ClientInput{
		Client: clients.Client{
			ID: id, Name: id, Surface: clients.WhatsApp, Enabled: true,
			Identity: clients.Identity{PhoneNumberID: number}, DefaultAgent: agent,
		},
		Token:         clients.SecretInput{Value: "token-" + id},
		SigningSecret: clients.SecretInput{Value: "secret-" + id},
		VerifyToken:   clients.SecretInput{Value: "verify-" + id},
	}
}

func iMessageClient(id, server, agent string) platform.ClientInput {
	return platform.ClientInput{
		Client: clients.Client{
			ID: id, Name: id, Surface: clients.IMessage, Enabled: true,
			Identity: clients.Identity{Server: server}, DefaultAgent: agent,
			IMessage: &clients.IMessageOptions{URL: "https://bb.example.com"},
		},
		Token:         clients.SecretInput{Value: "password-" + id},
		SigningSecret: clients.SecretInput{Value: "secret-" + id},
	}
}

const (
	acmeNumber   = "106540352242922"
	globexNumber = "106540352242999"
	person       = "15551234567"
)

// waText is a WhatsApp notification of one text message to a number.
func waText(number, id, text string) string {
	return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"WABA1","changes":[{"field":"messages","value":{
"messaging_product":"whatsapp","metadata":{"display_phone_number":"+1 555-010-0000","phone_number_id":%q},
"contacts":[{"profile":{"name":"Ada"},"wa_id":%q}],
"messages":[{"from":%q,"id":%q,"timestamp":"1700000000","type":"text","text":{"body":%q}}]}}]}]}`, number, person, person, id, text)
}

// signedWhatsApp signs a body with an app secret the way Meta does and
// serves it.
func signedWhatsApp(h http.Handler, secret, body string) int {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/whatsapp", strings.NewReader(body))
	r.Header.Set(signature.HeaderMetaSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func waSent(f *whatsapp.Fake, text string) int {
	n := 0
	for _, m := range f.Messages() {
		if strings.Contains(m.Body, text) {
			n++
		}
	}
	return n
}

// A signed WhatsApp message raises a run of the client's default agent in its
// own tenant, and the answer goes back to the person once, as one message.
func TestAWhatsAppMessageIsAnsweredOnce(t *testing.T) {
	f := &phones{}
	c := &cast{says: map[string][][]saigetypes.Delta{"concierge": {say("Booked for 7pm.")}}}
	p := stage(t, c, f.config())
	create(t, p, tenant, whatsAppClient("acme-wa", acmeNumber, "concierge"))
	ingress := p.IngressHandler()

	body := waText(acmeNumber, "wamid.ONE", "Book a table for two")
	if code := signedWhatsApp(ingress, "secret-acme-wa", body); code != http.StatusOK {
		t.Fatalf("the message answered %d", code)
	}
	out := f.wa("token-acme-wa")
	eventually(t, "the answer was sent", func() bool { return waSent(out, "Booked for 7pm.") == 1 })
	// Meta retries a delivery with the same wamid: it joins its run.
	if code := signedWhatsApp(ingress, "secret-acme-wa", body); code != http.StatusOK {
		t.Fatalf("the retry answered %d", code)
	}
	time.Sleep(1500 * time.Millisecond) // past a flush tick
	if n := waSent(out, "Booked for 7pm."); n != 1 {
		t.Fatalf("the answer was sent %d times", n)
	}
	for _, m := range out.Messages() {
		if m.To != person || m.PhoneNumberID != acmeNumber {
			t.Fatalf("a message went to %s from %s", m.To, m.PhoneNumberID)
		}
	}
	r, ok := runOf(p, tenant, "concierge")
	if !ok || r.Client != "acme-wa" || r.Origin != "whatsapp" {
		t.Fatalf("run = %+v, want raised by client acme-wa", r)
	}
	if got := len(p.Runs.List(tenant, 50)); got != 1 {
		t.Fatalf("%d runs, want the retry to join the first", got)
	}
}

// Meta's GET handshake is answered with its challenge only for a client's own
// verify token, and an unsigned or wrongly signed POST is refused.
func TestTheWhatsAppHandshakeAndSignatureAreChecked(t *testing.T) {
	f := &phones{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"concierge": {say("x")}}}, f.config())
	create(t, p, tenant, whatsAppClient("acme-wa", acmeNumber, "concierge"))
	ingress := p.IngressHandler()

	for _, tc := range []struct {
		query string
		code  int
		body  string
	}{
		{"hub.mode=subscribe&hub.verify_token=verify-acme-wa&hub.challenge=1158201444", http.StatusOK, "1158201444"},
		{"hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=1", http.StatusForbidden, ""},
		{"hub.mode=subscribe&hub.challenge=1", http.StatusForbidden, ""},
		{"hub.mode=unsubscribe&hub.verify_token=verify-acme-wa&hub.challenge=1", http.StatusForbidden, ""},
	} {
		w := httptest.NewRecorder()
		ingress.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/connectors/whatsapp?"+tc.query, nil))
		if w.Code != tc.code || (tc.body != "" && w.Body.String() != tc.body) {
			t.Fatalf("%s: %d %q", tc.query, w.Code, w.Body.String())
		}
	}
	body := waText(acmeNumber, "wamid.X", "hi")
	if code := signedWhatsApp(ingress, "not-the-app-secret", body); code != http.StatusUnauthorized {
		t.Fatalf("a wrongly signed message answered %d", code)
	}
	w := httptest.NewRecorder()
	ingress.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/connectors/whatsapp", strings.NewReader(body)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("an unsigned message answered %d", w.Code)
	}
	if n := len(p.Runs.List(tenant, 50)); n != 0 {
		t.Fatalf("a refused message raised %d runs", n)
	}
}

// Two tenants with a WhatsApp number each: a message for one number, signed
// with its own app secret, never reaches the other tenant, and one signed with
// the other tenant's secret is refused.
func TestAWhatsAppClientOfAnotherTenantNeverClaimsTheMessage(t *testing.T) {
	f := &phones{}
	c := &cast{says: map[string][][]saigetypes.Delta{"concierge": {say("Acme answer.")}}}
	p := stage(t, c, f.config())
	agent(t, p, "globex", "helper")
	create(t, p, tenant, whatsAppClient("acme-wa", acmeNumber, "concierge"))
	create(t, p, "globex", whatsAppClient("globex-wa", globexNumber, "helper"))
	ingress := p.IngressHandler()

	// Globex's secret over a message for Acme's number is offered only to
	// Acme's client, whose secret it is not.
	if code := signedWhatsApp(ingress, "secret-globex-wa", waText(acmeNumber, "wamid.A", "hi")); code != http.StatusUnauthorized {
		t.Fatalf("another tenant's secret answered %d", code)
	}
	// A number no client serves is not found.
	if code := signedWhatsApp(ingress, "secret-acme-wa", waText("999", "wamid.B", "hi")); code != http.StatusNotFound {
		t.Fatalf("an unknown number answered %d", code)
	}
	if code := signedWhatsApp(ingress, "secret-acme-wa", waText(acmeNumber, "wamid.C", "hi")); code != http.StatusOK {
		t.Fatalf("acme's own message answered %d", code)
	}
	eventually(t, "acme answered", func() bool { return waSent(f.wa("token-acme-wa"), "Acme answer.") == 1 })
	if n := len(f.wa("token-globex-wa").Messages()); n != 0 {
		t.Fatalf("globex's number sent %d messages", n)
	}
	if n := len(p.Runs.List("globex", 50)); n != 0 {
		t.Fatalf("globex has %d runs", n)
	}
	// A turn in globex gets no WhatsApp action of acme's client.
	if got := len(p.Runs.List(tenant, 50)); got != 1 {
		t.Fatalf("acme has %d runs", got)
	}
}

// imessageHook is a BlueBubbles new-message webhook.
func imessageHook(guid, text, extra string) string {
	return fmt.Sprintf(`{"type":"new-message","data":{"guid":%q,"text":%q,"isFromMe":false,
"handle":{"address":"+15551234567","service":"iMessage"},
"chats":[{"guid":"iMessage;-;+15551234567","chatIdentifier":"x","displayName":""}],
"dateCreated":1700000000123,"itemType":0,"associatedMessageType":null%s}}`, guid, text, extra)
}

func postIMessage(h http.Handler, query, body string) int {
	r := httptest.NewRequest(http.MethodPost, "/connectors/imessage"+query, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// An iMessage webhook is believed only with its client's secret in the URL,
// and a voice note arrives on the run as an audio attachment, which the
// platform's message hook sees before the run is raised.
func TestAnIMessageNeedsItsSecretAndCarriesAVoiceNote(t *testing.T) {
	f := &phones{}
	c := &cast{says: map[string][][]saigetypes.Delta{"concierge": {say("Heard you.")}}}
	cfg := f.config()
	var mu sync.Mutex
	var prepared []event.Envelope
	cfg.PrepareMessage = func(_ context.Context, e *event.Envelope) error {
		mu.Lock()
		defer mu.Unlock()
		prepared = append(prepared, *e)
		return nil
	}
	p := stage(t, c, cfg)
	agent(t, p, "globex", "helper")
	create(t, p, tenant, iMessageClient("acme-im", "home", "concierge"))
	create(t, p, "globex", iMessageClient("globex-im", "office", "helper"))
	ingress := p.IngressHandler()

	const caf = "caff\x00\x01\x00\x00desc\x00\x00\x00\x00\x00\x00\x00\x20"
	f.im("password-acme-im").AddAttachment("ATT-1", []byte(caf))
	voice := imessageHook("G1", "￼", `, "isAudioMessage": true, "attachments": [{"guid": "ATT-1", "uti": "com.apple.coreaudio-format", "mimeType": null, "transferName": "Audio Message.caf", "totalBytes": 20}]`)

	for _, tc := range []struct {
		name, query string
		code        int
	}{
		{"no secret", "", http.StatusUnauthorized},
		{"wrong secret", "?secret=nope", http.StatusUnauthorized},
		{"two secrets", "?secret=secret-acme-im&secret=secret-acme-im", http.StatusUnauthorized},
		{"another tenant's secret for this server", "?server=home&secret=secret-globex-im", http.StatusUnauthorized},
		{"a server nobody has", "?server=nowhere&secret=secret-acme-im", http.StatusNotFound},
	} {
		if code := postIMessage(ingress, tc.query, voice); code != tc.code {
			t.Fatalf("%s: answered %d, want %d", tc.name, code, tc.code)
		}
	}
	if n := len(p.Runs.List(tenant, 50)) + len(p.Runs.List("globex", 50)); n != 0 {
		t.Fatalf("refused webhooks raised %d runs", n)
	}

	if code := postIMessage(ingress, "?secret=secret-acme-im", voice); code != http.StatusOK {
		t.Fatalf("the voice note answered %d", code)
	}
	out := f.im("password-acme-im")
	eventually(t, "the answer was sent", func() bool {
		for _, m := range out.Messages() {
			if m.Text == "Heard you." && m.Chat == "iMessage;-;+15551234567" {
				return true
			}
		}
		return false
	})
	mu.Lock()
	got := prepared
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("the message hook saw %d messages", len(got))
	}
	audio := attachment.OfKind(wire.AttachmentsOf(got[0]), attachment.KindAudio)
	if len(audio) != 1 || audio[0].MediaType != "audio/x-caf" || got[0].Tenant != tenant {
		t.Fatalf("the hook saw attachments %+v in tenant %q", wire.AttachmentsOf(got[0]), got[0].Tenant)
	}
	r, ok := runOf(p, tenant, "concierge")
	if !ok || r.Client != "acme-im" || r.Origin != "imessage" {
		t.Fatalf("run = %+v", r)
	}
	if !strings.Contains(strings.Join(c.requests("concierge"), "\n"), "Audio Message.caf") {
		t.Fatalf("the model was not given the voice note: %v", c.requests("concierge"))
	}
	if n := len(f.im("password-globex-im").Messages()); n != 0 {
		t.Fatalf("globex's server sent %d messages", n)
	}
}

// A WhatsApp or iMessage client's identity and options are its own surface's.
func TestMessagingClientsAreValidated(t *testing.T) {
	f := &phones{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"concierge": {say("x")}}}, f.config())
	bad := []platform.ClientInput{
		func() platform.ClientInput { in := whatsAppClient("a", "not-digits", "concierge"); return in }(),
		func() platform.ClientInput {
			in := whatsAppClient("b", acmeNumber, "concierge")
			in.VerifyToken = clients.SecretInput{}
			return in
		}(),
		func() platform.ClientInput {
			in := whatsAppClient("c", acmeNumber, "concierge")
			in.Identity.Team = "T01ABCDEF"
			return in
		}(),
		func() platform.ClientInput {
			in := iMessageClient("d", "home", "concierge")
			in.IMessage = nil
			return in
		}(),
		func() platform.ClientInput {
			in := iMessageClient("e", "home", "concierge")
			in.IMessage.URL = "https://user:pw@bb.example.com"
			return in
		}(),
		func() platform.ClientInput {
			in := iMessageClient("f", "home", "concierge")
			in.VerifyToken = clients.SecretInput{Value: "x"}
			return in
		}(),
		func() platform.ClientInput {
			in := whatsAppClient("g", acmeNumber, "concierge")
			in.WhatsApp = &clients.WhatsAppOptions{People: map[string]string{"+1555": "ana"}}
			return in
		}(),
	}
	for i, in := range bad {
		if _, err := p.CreateClient(context.Background(), tenant, "test", in); err == nil {
			t.Fatalf("bad client %d was created", i)
		}
	}
}

// A WhatsApp people map is phone numbers: a reader who may not change the
// client is not shown it.
func TestAWhatsAppPeopleMapIsShownOnlyToItsWriters(t *testing.T) {
	f := &phones{}
	p := stage(t, &cast{says: map[string][][]saigetypes.Delta{"concierge": {say("x")}}}, f.config())
	in := whatsAppClient("acme-wa", acmeNumber, "concierge")
	in.WhatsApp = &clients.WhatsAppOptions{People: map[string]string{person: "ana"}}
	c := create(t, p, tenant, in)
	reader := auth.Principal{Subject: "vic", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	admin := auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if got := p.Seer(reader).Client(c); got.WhatsApp == nil || len(got.WhatsApp.People) != 0 {
		t.Fatalf("a reader was shown %+v", got.WhatsApp)
	}
	if got := p.Seer(admin).Client(c); got.WhatsApp == nil || got.WhatsApp.People[person] != "ana" {
		t.Fatalf("an admin was shown %+v", got.WhatsApp)
	}
}
