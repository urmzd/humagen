# Email notifications

Email is an optional outbound notification transport. It sends a count of the
updates the recipient can currently read and a link to Humagen. It does not copy
message bodies, project names, source names or provider credentials into mail.
Reading incoming email is a separate connector and is not implemented.

Configure a relay and exact account mappings in the operator-owned System file:

```yaml
api_version: humagen/v1
kind: System
metadata:
  name: company
spec:
  server:
    public_url: https://humagen.example.test
  notifications:
    email:
      address: smtp.example.test:587
      from: Humagen <notifications@example.test>
      username: notifications
      password:
        ref: env:HUMAGEN_SMTP_PASSWORD
      tls: starttls
      recipients:
        company:
          "issuer|person-subject": person@example.test
```

`starttls` is the default and requires verified TLS before authentication or
mail. `implicit` uses verified TLS from connection establishment. The explicit
`insecure_loopback` mode is for a literal loopback IP and accepts no credentials;
it can be used with a disposable local test relay. Credentials are resolved from
the configured reference for each send, so credential rotation is not copied
into preference or delivery records.

The recipient key is the exact authenticated subject, not a display name or
email claim. The tenant must match. Background delivery uses the current
server policy's standing subject grants and current project membership. It
never borrows remembered directory roles or prior sign-in group claims. Add the
appropriate subject grant to the existing authorization policy when needed.
No recipient address is returned by the console API.

The person then enables Email under You, Notifications, chooses Immediate or
Digest for the desired event kinds, and saves. Email and event kinds default to
off. The existing timezone and quiet hours apply to email. An unavailable
account cannot opt in through a direct API call. Email consent has a separate
read version: stale changes return 409, and the console refreshes the current
email choice while preserving the person's edited briefing.

Delivery runs without an open browser. Each poll considers at most five mapped
accounts and sends at most five messages. Failed relay submissions remain
pending across restart. Before retry, the worker checks current visibility,
opt-in, event modes and quiet hours again. Switching an event kind off cancels
its pending submission; switching to digest defers it to the next digest window.

Email acknowledgments are separate from console notification acknowledgments,
and neither changes the authoritative inbox. Delivery records contain opaque
inbox references and stable message identities, follow inbox retention, and
are bounded. SMTP acceptance means the configured relay accepted the message.
A crash or lost relay response can cause a retry; the stable Message-ID permits
relay deduplication but does not promise exactly-once inbox delivery.

Email opt-in and opt-out changes are recorded before mutation in the person's
private audit history. Recipient mappings remain in the operator-owned file;
changing notification preferences never grants access to a project or message.
