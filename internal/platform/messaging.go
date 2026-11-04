package platform

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/urmzd/orchagent/flow/connectors"
	"github.com/urmzd/orchagent/flow/connectors/imessage"
	"github.com/urmzd/orchagent/flow/connectors/whatsapp"
	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/signature"

	"github.com/urmzd/humagen/pkg/clients"
)

// WhatsApp and iMessage put a person's phone in the workspace. Each client is
// one business number (WhatsApp, over Meta's Cloud API) or one Apple ID's
// Messages relayed by a BlueBubbles server on the operator's Mac (iMessage).
// Neither surface can edit a sent message, so neither streams: a run sends one
// message when it waits on a person and one with its answer.
//
// The rest is a client like any other. A request is offered to the clients
// whose identity it states (the phone number id in a WhatsApp payload, the
// ?server= of an iMessage webhook), and the first whose own secret verifies
// it takes it: Meta's app secret over the body, or the secret the
// BlueBubbles webhook URL carries. The tenant, the agents and the person the
// run acts for all come from that client's record.

// prepareMessage runs on every WhatsApp and iMessage message a client's
// connector takes, after its files are in and before the event is
// published. It is the one place the channel-agnostic inbound work goes, so
// a transcriber added later (a voice note is an attachment of kind audio,
// attachment.OfKind(wire.AttachmentsOf(*e), attachment.KindAudio), with
// empty text) plugs in here, through Config.PrepareMessage, and every
// messaging surface gets it at once. An error refuses the message.
func (p *Platform) prepareMessage(ctx context.Context, e *event.Envelope) error {
	if p.cfg.PrepareMessage == nil {
		return nil
	}
	return p.cfg.PrepareMessage(ctx, e)
}

// dialWhatsApp builds a WhatsApp client's connector: the token is the Cloud
// API access token (and what media is downloaded with), the signing secret
// the Meta app secret, and the verify token what the subscription handshake
// echoes.
func (p *Platform) dialWhatsApp(c clients.Client, secrets resolved) (*seat, error) {
	var api whatsapp.API = whatsapp.NewHTTPAPI(secrets.token, http.DefaultClient)
	if p.cfg.WhatsAppAPI != nil {
		api = p.cfg.WhatsAppAPI(secrets.token)
	}
	verifier := signature.NewMeta(secrets.signing)
	peer, err := whatsapp.New(whatsapp.Config{
		API: api, Verifier: verifier, VerifyToken: secrets.verify,
		PhoneNumberID: c.Identity.PhoneNumberID,
		Agents:        clientAgents{store: p.Agents, client: c},
		Prepare:       p.prepareMessage,
		MaxBody:       maxWebhookBody,
		Tenant:        c.Tenant,
		Files:         connectors.Files{Store: p.files, Log: p.log},
		MediaToken:    secrets.token,
	})
	if err != nil {
		return nil, err
	}
	return &seat{client: c, peer: peer, verifier: verifier, verifyToken: secrets.verify}, nil
}

// dialIMessage builds an iMessage client's connector: the token is the
// BlueBubbles server password and the signing secret the one its webhook
// URL carries as ?secret=.
func (p *Platform) dialIMessage(c clients.Client, secrets resolved) (*seat, error) {
	o := c.IMessage
	if o == nil {
		return nil, errors.New("an imessage client gives its server's url")
	}
	var api imessage.API = imessage.NewHTTPAPI(o.URL, secrets.token, http.DefaultClient)
	if p.cfg.IMessageAPI != nil {
		api = p.cfg.IMessageAPI(o.URL, secrets.token)
	}
	peer, err := imessage.New(imessage.Config{
		API: api, Server: c.Identity.Server, Secret: secrets.signing, Method: o.Method,
		Agents:  clientAgents{store: p.Agents, client: c},
		Prepare: p.prepareMessage,
		MaxBody: maxWebhookBody,
		Tenant:  c.Tenant,
		Files:   connectors.Files{Store: p.files, Log: p.log},
	})
	if err != nil {
		return nil, err
	}
	return &seat{client: c, peer: peer, verifier: urlSecret(secrets.signing)}, nil
}

// urlSecret verifies a request by the secret its URL carries, as a
// BlueBubbles webhook is: it signs nothing and takes only a URL. It is
// compared in constant time, and an empty secret matches nothing.
type urlSecret string

// Verify refuses every request: a URL secret is never in the headers. The
// surface calls VerifyRequest.
func (urlSecret) Verify(http.Header, []byte) error { return errUnverified }

// VerifyRequest checks the one ?secret= the request carries.
func (s urlSecret) VerifyRequest(r *http.Request, _ []byte) error {
	got := r.URL.Query()[imessage.SecretParam]
	if s == "" || len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(s)) != 1 {
		return errUnverified
	}
	return nil
}

var errUnverified = errors.New("platform: the request is not verified")

// requestVerifier is a verifier that reads more of the request than its
// headers, such as urlSecret.
type requestVerifier interface {
	VerifyRequest(r *http.Request, body []byte) error
}

// verified reports whether a seat's own secret verifies a request.
func (st *seat) verified(r *http.Request, body []byte) bool {
	if v, ok := st.verifier.(requestVerifier); ok {
		return v.VerifyRequest(r, body) == nil
	}
	return st.verifier.Verify(r.Header, body) == nil
}

// whatsAppClaim reads the business number a WhatsApp notification is for.
// A notification that names more than one says nothing, so every client is
// offered it and the first whose app secret verifies it takes it.
func whatsAppClaim(body []byte) clients.Claim {
	var n struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Metadata struct {
						PhoneNumberID string `json:"phone_number_id"`
					} `json:"metadata"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if json.Unmarshal(body, &n) != nil {
		return clients.Claim{}
	}
	number := ""
	for _, e := range n.Entry {
		for _, ch := range e.Changes {
			switch id := ch.Value.Metadata.PhoneNumberID; {
			case id == "":
			case number == "":
				number = id
			case number != id:
				return clients.Claim{}
			}
		}
	}
	return clients.Claim{PhoneNumberID: number}
}

// serveHandshake answers Meta's subscription check on GET. No payload names
// the number, so it goes to the first WhatsApp client whose verify token it
// carries, compared in constant time, and that client's connector answers
// it. One that matches none is refused.
func (s *surface) serveHandshake(w http.ResponseWriter, r *http.Request, refresh bool) {
	token := r.URL.Query().Get("hub.verify_token")
	for _, st := range s.all() {
		if st.verifyToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(st.verifyToken)) != 1 {
			continue
		}
		if _, enabled := s.p.currentInstallation(st.client); !enabled {
			continue
		}
		st.peer.ServeHTTP(w, r)
		return
	}
	if refresh && s.p.refreshClients(true) {
		s.serveHandshake(w, r, false)
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}
