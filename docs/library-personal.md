# Library and personal settings

This layer supplies dedicated Skills, Memory, Audit, Profile, Notifications,
Privacy and Personal Agents components and APIs. The upper Outcomes layer mounts
the canonical routes and navigation. Connected Accounts uses the existing real
provider connections page.

Skills have immutable scoped revisions. A draft leaves the published revision
in use. Test Skill invokes the configured agent and model through the real
runtime with every permitted action requiring approval; ReadContext Ask or
Never withholds automatic notes. Publication requires a
successful completed test of the exact revision. Promotion creates a draft in
the destination scope, where it must be tested before publication. Private
instructions retain a durable publication classification before model admission.

Memory supports personal, agent, project and workspace scopes, search, kinds,
expiry, corrections, explicit review and immediate audited forgetting. Expired
and pending notes do not enter a turn. Corrections preserve original provenance
and record their editor. Visible artifact and conversation sources have links;
hidden sources never expose names or IDs. New IDs are opaque. Personal notes
stay with their exact human owner, including against governance access.

Propose to Project selects an approved, unexpired note. Only its text, kind and
expiry cross the reviewed audience boundary. Its new attribution names the
copier, while original private provenance stays private. The destination receives
a pending suggestion for its owners to review, and the source stays in place.

Profile names are consumed by new messages and real turn actors without changing
the authenticated subject. Briefing preferences enter real turns. Personal
memory enters a shared project only after explicit opt-in. Notification choices
feed the bounded durable native console delivery worker, with hourly digests,
quiet hours and Off behavior. Dismissal changes the delivery projection alone.
Email remains explicitly unavailable. See [native notifications](native-notifications.md).

Personal Agents lists actual personal-project homes, pause state and reviewed
moves into owned projects. A file-managed project can receive an agent when the
caller owns it; editing the project specification is a separate permission.

Audit shows bounded retained records, truthful historical adapters and current
call-policy snapshots. It filters by date, actor and resource domain, expands
redacted before/after fields and exports formula-safe CSV. Personal records have
exact-owner visibility. A filtered limit never reveals hidden record counts.

## Reproduce the visual checks

`scripts/screens/library.ts` supplies thirteen states, each checked at phone and
desktop widths once the upper host mounts its routes and activates this helper.
They use the isolated scripts/screens server and real API seed.
The expanded digest uses a retained delivery fixture; real timing, replay and
confinement are covered by deterministic worker journey tests.

```sh
SCREENS_ONLY=library-skills,library-skills-draft,library-memory-personal,library-memory-correction,library-memory-review,library-memory-promotion,audit-expanded,you-profile,you-notifications,you-privacy,you-agents,you-agent-review,you-notification-digest make screens
```

The report and side-by-side image index are written to `.screens/`.
