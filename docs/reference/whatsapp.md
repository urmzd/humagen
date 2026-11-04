# WhatsApp

A WhatsApp client is one business number on Meta's WhatsApp Cloud API. A
person writes to the number from the WhatsApp they already use, an agent
answers, and the answer comes back in the same chat. The connector is
orchagent's `flow/connectors/whatsapp`; its own notes are in
`flow/docs/architecture/messaging.md` in that module.

## What a person sees

WhatsApp cannot edit a sent message, so nothing streams. A run sends nothing
while it works, one message when it waits on a person (a question or an
approval, as plain text), and one message with its answer when it ends. A
model can send a progress note on a long task with `whatsapp_send_message`,
which reaches only the person the run is answering.

Every WhatsApp chat is one person and the business, so every message is a
direct message: it names no agent and is answered by the client's
`default_agent`, then by the default chain of the client's home. A message
may still address an agent the client routes to with `@name`.

A voice note, an image, a video or a document is downloaded with the access
token, stored under the workspace, and carried on the run as an attachment.
A voice note is an attachment of kind `audio` with empty text. Humagen does
not transcribe it yet; the place a transcriber plugs in is
`Platform.prepareMessage` (`internal/platform/messaging.go`), through
`platform.Config.PrepareMessage`, which runs on every WhatsApp and iMessage
message after its files are in and before the run is raised.

Meta accepts a free-form reply only within 24 hours of the person's last
message. A run that ends later is refused by the API, and the send is
retried until the run is evicted.

## Setup

1. Create a Meta app with the WhatsApp product. Note the business number's
   **phone number id** (not the number itself, it is shown beside the number
   under WhatsApp, API Setup), the **app secret** (App settings, Basic) and a
   system user **access token** with `whatsapp_business_messaging`.
2. Choose a **verify token**, any string you keep secret.
3. Create the client: in the console under Settings, Clients, Connect a
   System, choose WhatsApp; or through `POST /api/clients`:

   ```json
   {
     "name": "Front desk",
     "surface": "whatsapp",
     "identity": {"phone_number_id": "106540352242922"},
     "default_agent": "concierge",
     "whatsapp": {"people": {"15551234567": "ana"}},
     "credentials": {
       "token": {"ref": "env:HUMAGEN_WHATSAPP_TOKEN"},
       "signing_secret": {"ref": "env:HUMAGEN_WHATSAPP_APP_SECRET"},
       "verify_token": {"ref": "env:HUMAGEN_WHATSAPP_VERIFY_TOKEN"}
     }
   }
   ```

4. In the app's WhatsApp configuration, set the callback URL to
   `https://<your host>/connectors/whatsapp`, the verify token to the one you
   chose, and subscribe the webhook to `messages`. Meta checks the URL with a
   `GET` first: the server answers it for the client whose verify token it
   carries.

The webhook listener (`humagen serve -webhook-addr`), like the main one, serves
`/connectors/whatsapp` for both methods, so only it needs to be public.

## How a request is believed

`POST /connectors/whatsapp` is signed with the app secret
(`X-Hub-Signature-256`). The request is offered to the clients whose
`phone_number_id` the payload names, in every workspace, and the first whose
own app secret verifies the exact bytes takes it; nothing is parsed before
that. A number no client serves is a 404, and one whose clients' secrets do
not verify it a 401. The tenant, the agents and the person the run acts for
come from that client's record. Status callbacks, reactions and messages from
the business number itself are dropped. A retried delivery keeps its message
id (`wamid...`), which is the event id, so it joins the run it already
started.

`GET /connectors/whatsapp` answers `hub.challenge` only when `hub.mode` is
`subscribe` and `hub.verify_token` is a client's verify token, compared in
constant time. Anything else is a 403.

## People

`whatsapp.people` maps a WhatsApp id, the person's number in international
form with digits only, to the person it is in this workspace, by subject. A
mapped person's run is held to what they may do: it needs their `runs:write`.
A number the map does not name is answered for nobody, as an unmapped Slack
user is. The map is phone numbers, so only those who may change clients are
shown it. Runs are filed in the client's home (see [clients.md](clients.md)),
so give a WhatsApp client a home only the right people read.

## Environment

For a single number, these variables create the default client,
`default-whatsapp`, which may route to any agent:

| Variable | Client field |
|----------|--------------|
| `HUMAGEN_WHATSAPP_TOKEN` | `credentials.token`: the access token, also used to download media |
| `HUMAGEN_WHATSAPP_APP_SECRET` | `credentials.signing_secret`: the app secret |
| `HUMAGEN_WHATSAPP_VERIFY_TOKEN` | `credentials.verify_token` |
| `HUMAGEN_WHATSAPP_PHONE_NUMBER_ID` | `identity.phone_number_id` |
| `HUMAGEN_WHATSAPP_DEFAULT_AGENT` | `default_agent`. Optional |

The first four go together: some set without the others stops the server
from starting. A Platform document declares the same client as
`surfaces.whatsapp` ([spec.md](spec.md#whatsapp-and-imessage)).

## What is not built

- Answering a question or an approval by replying in the chat. The question
  is sent as text; it is answered in the console.
- Replying with a voice note. The connector has `SendAudio`; nothing calls it
  yet.
- Messaging someone who has not written first. That needs an approved
  template, and from a model it is a spam risk.
- Which moments of a run were sent is kept in memory, per process. A restart
  forgets it, and a redelivery after that sends the moment again.
