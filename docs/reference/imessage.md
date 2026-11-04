# iMessage

An iMessage client is one Apple ID's Messages, relayed by a
[BlueBubbles](https://bluebubbles.app) server on the operator's Mac. A person
texts that Apple ID from Messages on their iPhone, an agent answers, and the
answer comes back in the same chat. The connector is orchagent's
`flow/connectors/imessage`; its own notes are in
`flow/docs/architecture/messaging.md` in that module.

## What a person sees

Messages cannot be edited once sent, so nothing streams. A run sends nothing
while it works, one message when it waits on a person (a question or an
approval, as plain text), and one with its answer when it ends. A model can
send a progress note with `imessage_send_message`, which reaches only the
chat the run is answering.

A one-to-one chat is a direct message: a message that names no agent is
answered by the client's `default_agent`, then by the default chain of the
client's home. In a group chat an agent is named with `@name`.

An audio message, an image or a document is downloaded from the BlueBubbles
server, stored under the workspace and carried on the run as an attachment.
An audio message is an attachment of kind `audio` (Core Audio Format,
`audio/x-caf`, unless the server converts it) with empty text. Humagen does
not transcribe it yet; the place a transcriber plugs in is
`Platform.prepareMessage` (`internal/platform/messaging.go`), through
`platform.Config.PrepareMessage`, shared with WhatsApp.

## Setup

1. On a Mac signed in to Messages, install and run the BlueBubbles server.
   Make it reachable from Humagen's server over TLS: its Cloudflare or ngrok
   tunnel, or a proxy of your own. Note its **URL** and **server password**.
2. Choose a **name** for the server (`home`, `office-mac`: lowercase letters,
   digits and dashes) and a **webhook secret**, a long random string.
3. Create the client: in the console under Settings, Clients, Connect a
   System, choose iMessage; or through `POST /api/clients`:

   ```json
   {
     "name": "Concierge",
     "surface": "imessage",
     "identity": {"server": "home"},
     "default_agent": "concierge",
     "imessage": {"url": "https://bb.example.com", "people": {"+15551234567": "ana"}},
     "credentials": {
       "token": {"ref": "env:HUMAGEN_IMESSAGE_PASSWORD"},
       "signing_secret": {"ref": "env:HUMAGEN_IMESSAGE_WEBHOOK_SECRET"}
     }
   }
   ```

4. In BlueBubbles, under API and webhooks, add a webhook for new messages at
   `https://<your host>/connectors/imessage?secret=<webhook secret>`. With
   more than one iMessage client, add `&server=<name>` so the request is
   offered to that client alone.

Sends use AppleScript. Set `imessage.method` to `private-api` when the server
runs the Private API helper, which also sends audio as an audio message.

## How a request is believed

BlueBubbles signs nothing and its webhook takes only a URL, so the secret is
in the URL. `POST /connectors/imessage` is offered to the iMessage clients
whose server the URL's `server` names (every one when it names none), in
every workspace, and the first whose webhook secret equals the URL's single
`secret`, compared in constant time, takes it. No secret, a wrong one, or two
is a 401; a server no client serves is a 404. The tenant, the agents and the
person the run acts for come from that client's record. Messages the Mac's
own account sent, tapbacks, reactions and group events are dropped, which is
what keeps an answer from waking the agent again.

Keep the URL as secret as a password: anyone who has it can post as the
server. Rotate it by updating the client's signing secret and the webhook in
BlueBubbles together.

## People

`imessage.people` maps a sender's handle, a `+number` in international form
or an email address, to the person it is in this workspace, by subject. A
mapped person's run needs their `runs:write`; a handle the map does not name
is answered for nobody. Only those who may change clients are shown the map.
Runs are filed in the client's home (see
[clients.md](clients.md)), so give an iMessage client a home only the right
people read.

## Environment

For a single server, these variables create the default client,
`default-imessage`, which may route to any agent:

| Variable | Client field |
|----------|--------------|
| `HUMAGEN_IMESSAGE_URL` | `imessage.url` |
| `HUMAGEN_IMESSAGE_PASSWORD` | `credentials.token`: the server password |
| `HUMAGEN_IMESSAGE_WEBHOOK_SECRET` | `credentials.signing_secret`: the webhook URL's `secret` |
| `HUMAGEN_IMESSAGE_SERVER` | `identity.server`. Optional, `home` when unset |
| `HUMAGEN_IMESSAGE_METHOD` | `imessage.method`. Optional |
| `HUMAGEN_IMESSAGE_DEFAULT_AGENT` | `default_agent`. Optional |

The first three go together. A Platform document declares the same client as
`surfaces.imessage` ([spec.md](spec.md#whatsapp-and-imessage)).

## What is not built

- Answering a question or an approval by replying in the chat; it is
  answered in the console.
- Replying with audio. The connector has `SendAudio`; nothing calls it yet.
- Starting a chat with someone new.
- Which moments of a run were sent is kept in memory, per process, as on
  WhatsApp.
