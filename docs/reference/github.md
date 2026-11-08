# GitHub agent delivery

The GitHub connector accepts signed mentions on issues, pull requests, reviews,
and review comments. It also accepts review requests for configured agent logins.
Responses use one Markdown comment per run, edited as events arrive.

## Configuration

Each GitHub installation is a [client](clients.md): the accounts it serves, its
installation and app ids, its token and webhook secret, the agents it may reach,
and the options on this page. A workspace may have several, and every one
delivers to `/connectors/github`. A delivery goes to the client that serves its
account and whose webhook secret verifies it.

For a single installation, the `HUMAGEN_GITHUB_*` variables on this page
create the default client, `default-github`:

| Variable | Client field |
|----------|--------------|
| `HUMAGEN_GITHUB_TOKEN`, `HUMAGEN_GITHUB_WEBHOOK_SECRET` | `credentials.token`, `credentials.signing_secret` |
| `HUMAGEN_GITHUB_OWNERS` | `identity.owners` |
| `HUMAGEN_GITHUB_APP`, `HUMAGEN_GITHUB_INSTALLATION` | `identity.app_id`, `identity.installation`. Optional |
| `HUMAGEN_GITHUB_BOTS` | `bots` |
| `HUMAGEN_GITHUB_AUTO_LABELS` | `github.auto_labels` |
| `HUMAGEN_GITHUB_CHECKS`, `HUMAGEN_GITHUB_GUIDES`, `HUMAGEN_GITHUB_MUTATIONS` | `github.checks`, `github.guides`, `github.mutations` |
| `HUMAGEN_GITHUB_READ_REPOSITORIES` | `github.read_repositories` |
| `HUMAGEN_GITHUB_LOGIN` | `github.login` |
| `HUMAGEN_GITHUB_DEFAULT_AGENT` | `default_agent` |

The default client may route to any agent. One created through the API routes
to its default agent and its `agents` list, and an auto-label or a bot that
names any other agent is refused when the client is saved.

## Who a comment asks

The rule is Slack's: an `@agent` in a comment, or an `auto_labels` label, is a
tag; otherwise the client's default; otherwise nothing. Slack delivers a
message only when it mentions the app, so on GitHub a comment that names no
agent is addressed to the client only when it mentions the app's own login.
Set it as `github.login`: the login the app comments as, `humagen[bot]`, which
people mention as `@humagen`. With it set, and the app's login not in `bots`:

- `@docs-bot summarize this` tags docs-bot.
- `@humagen @docs-bot summarize this` tags docs-bot. The app's mention is taken
  out of the text.
- `@humagen what changed?` names no agent, so it is answered as an untagged Slack
  message outside a bound channel is: by the client's `default_agent`, then by
  the default of the client's home channel and its chain. The client's
  `agents` list holds: a default it may not route to is refused, never swapped
  for one it may. With no default anywhere, nothing runs and nothing is posted.
- A comment that names no agent and does not mention the app starts nothing,
  so a busy repository wakes no agent on every comment. A bot's comment starts
  nothing, whatever it mentions.

A login in the client's `bots` names its agent, as a Slack bot user in `bots`
does, today as well. So when `bots` maps the app's own login to an agent, as
`HUMAGEN_GITHUB_BOTS='humagen[bot]=ops'` does and the [PR guide's
setup](#pr-guide-and-review-threads) does for its agent, `@humagen` is a tag for
that agent: `@humagen what changed?` asks ops, and so does `@humagen @docs-bot
summarize this`, since the first agent a comment names answers it. The
client's `default_agent` is not asked.

A review, a review comment and a new issue or pull request are read the same
way. Every run is filed in the client's [home](clients.md#the-record), and
acts for nobody, as every GitHub run does: no client maps GitHub logins to
people. A comment addressed to the app on a pull request posts no guide, since
the guide names the agent that answers; the next one that names an agent does.

## Automatic labels

Set repository-specific label mappings alongside the GitHub token and webhook
secret:

```sh
export HUMAGEN_GITHUB_AUTO_LABELS='{"acme/handbook":{"agent:auto":"docs-bot"}}'
```

Register `docs-bot` with Humagen and subscribe the GitHub webhook to `issues`
and `pull_request` events. Applying `agent:auto` starts a run using the subject's
title and body. The run answers on that subject unless delivery routes override
it. Repository matching is case-insensitive; label names match exactly.

Applying the label starts the first run. Subsequent PR `synchronize` events
start a review of the new head while the configured label remains. Removing the
label stops automatic commit reviews; unrelated label changes do not start runs.
Draft and closed PRs do not trigger commit reviews. Bot comments are ignored;
bot-authored commits on activated PRs can trigger reviews of requested fixes. `HUMAGEN_GITHUB_OWNERS` also restricts inbound triggers.
A GitHub delivery ID is required for automatic labels and commit events: retries retain their run
identity, while removing and reapplying a label produces a new delivery.
Automatic commit reviews also deduplicate by repository, PR, agent and head SHA;
explicit user mentions start independent requests.

An empty mapping disables automatic runs. Malformed mappings fail startup.
The configured agent must exist in the registry when the trigger is accepted.

## Reading context and structured reviews

`github_read_context` returns the subject and a page of comments. For PRs it
also returns the head SHA, reviews, inline comments and changed-file patches.
Follow `next_page` until absent. A missing patch is unavailable evidence.
Reads default to the source repository. To allow GitHub context from Slack,
Jira, or links in another repository, grant exact repository names. A run from
another surface reads through the oldest GitHub client in its workspace that may
route to its agent:

```sh
export HUMAGEN_GITHUB_READ_REPOSITORIES='acme/handbook,acme/service'
```

The read tools accept an `address` containing a `github://` issue/PR address or
an HTTPS GitHub issue/PR URL. References are parsed into API coordinates, never
fetched as arbitrary URLs. These grants permit reads only; mutation targets stay
bound to the original signed PR request. Native reaction counts (`+1`, `-1`)
are included in context so the next answer can account for feedback. Reactions
do not start runs or grant permission to change code. Reads require the token's Issues and Pull
requests permissions. GitHub's 3,000-file PR API limit still applies.

The [agent template](../../examples/github-review/agent.json) grants this read
tool and specifies the structured review contract. Replace `YOUR_MODEL_ID`
with a model available to your configured Anthropic provider, then register it:

```sh
humagen serve -agent-specs examples/github-review/agent.json
```

Ordinary questions receive Markdown answers. Reviews return a JSON report with
`format: "humagen.github.review.v1"`, which the sink converts to Markdown:
severity counts, summary, comments, an aligned findings table, related concepts,
and native reaction/mention instructions. Counts are derived from findings.
Every finding requires a suggestion, observed evidence with a source, and a
follow-up prompt. The formatter checks completeness, not the truth of claims.
Reports with missing evidence are visibly rejected rather than shown as clean.

Structured output is held while the run streams, then rendered in its existing
comment. Lifecycle-only routes also render the final report. Ordinary Markdown
answers retain their streaming behavior.

### Reviews in the console and Slack

The console and the Slack app read the same report and draw it as a review
card: the pull request and the reviewed commit, the summary, severity counts
computed by the client, findings grouped by severity with their evidence, notes
and related links. The same checks apply: a report the formatter would reject
is shown as the text it was. While a review streams, the card shows its summary
once it has arrived; Slack holds the JSON back and posts the card when the run
ends. In the console, **Ask Follow-up** puts `@<agent> <prompt>` in the message
box without sending it, and **Copy as Markdown** copies the client-side
equivalent of the comment the GitHub sink posts. In Slack, each follow-up is
shown as words to ask with.

Evidence that is a permalink to lines at a full commit SHA
(`https://github.com/<owner>/<repo>/blob/<sha>/<path>#L<a>-L<b>`) reads as
`path:a-b`. In the console it opens the cited lines in place, with three lines
of context, line numbers and a link to GitHub. The lines come from
`GET /api/github/source` (`runs:read`), which reads the file at that commit
through a GitHub client of the caller's workspace, so the client's token never
reaches the browser. A client reads repositories its owners include or its
`read_repositories` name; any other repository, another workspace's, or a file
the commit lacks answers 404. One request shows at most 200 lines and 20 lines
of context, from a file of at most 256,000 bytes of UTF-8 text, and files read
at a commit are kept, 64 at most, since they never change.

## Check progress

Set `HUMAGEN_GITHUB_CHECKS=true` with a GitHub App token that has Checks write
permission ([GitHub check-run API](https://docs.github.com/en/rest/checks/runs)).
Each PR run gets a `fiber/<agent>` check on its pinned head SHA.
The check shows queued/running state, tools and the final review. Failed runs or
review errors yield failure; invalid structured reports require action. Other
completed turns are neutral: completion alone does not prove tests passed.
Issues continue using comments without checks.

PR comment webhooks omit the head, so enabling checks resolves it through the
API before accepting the tag. Context reads reject a changed head rather than
silently reviewing newer code under an older check. Check recovery searches by
run ID and commit before creating, including after a lost create response.
GitHub provides no atomic create-if-absent check operation; multiple processes
must serialize delivery of the same run. Comment handles still live in memory,
so comment recovery across process restarts remains a limitation.

## PR guide and review threads

Enable the guide and identify the authenticated bot login:

```sh
export HUMAGEN_GITHUB_GUIDES=true
export HUMAGEN_GITHUB_BOTS='github-agent[bot]=github-agent'
```

The first accepted PR trigger posts one guide with intent, the current SHA,
changed-file navigation, review instructions and one bot mention. The same
comment updates on subsequent commits; each agent run still owns its separate
review. Guides are discovered by a stable marker and configured author, so
retries and connector restarts do not create another activation comment.
A copied marker in a human comment is ignored. Configure a bot mapping for every
agent that receives PR triggers while guides are enabled.

Guide updates compare the current PR head immediately before writing. Within a
connector, guide writes are serialized. GitHub does not offer compare-and-swap
comment updates: a commit arriving between the final head read and comment edit
can leave the guide temporarily stale until its new webhook is processed.
Multiple server instances need shared serialization for guide creation.

Tags in inline PR review comments answer inside the same native review thread,
including when the tag is itself a reply. The answer is edited as it streams.
Issue and PR conversation tags continue answering in the main conversation.
Explicit delivery overrides retain their configured destination.

## Requested fixes

Enable mutations and register the optional
[fixer spec](../../examples/github-review/fixer.json) after configuring its model:

```sh
export HUMAGEN_GITHUB_MUTATIONS=true
humagen serve -agent-specs examples/github-review/fixer.json
```

A repository writer can request a fix in a PR conversation or review thread:

```text
@github-fixer /fix handle the retry timeout and add a regression test
```

`github_read_file` reads complete UTF-8 source at an immutable SHA before editing.
`github_apply_changes` creates one atomic commit with up to 20 file additions,
replacements or deletions, bounded to 500 KB total replacement content. The
source PR must be open and its branch must be in the same repository. Fork PRs
are read-only for this tool. Branch protections still apply.

Fixes require an explicit `/fix` mention from a person with effective GitHub
write permission. Permission is checked at acceptance and again before writing.
The connector signs a grant bound to the run ID, actor, repository, PR and head.
The run ID is the runtime's, derived from the client's workspace and the
delivery ID, so a connector that may commit is built with its workspace.
The runtime supplies the actual run identity separately from model arguments,
so copying a source URI into another invocation does not authorize a write.
Each client signs with its own webhook secret under a distinct protocol
prefix, so a grant from one client is worth nothing to another; mutations
require at least 16 bytes. Rotating it invalidates old grants.
Automatic reviews and ordinary questions never receive a mutation grant.
Explicit `/check` requests receive a separate workflow-only grant.

Commits use GitHub's atomic `expectedHeadOid` comparison. If another commit wins,
the tool refuses to overwrite it. Retrying an identical request recovers its
commit when that commit is still the branch head, including after a lost API
response. A later branch update requires a fresh request. Results report the
actual commit SHA and URL, and explicitly state that this action did not run
tests. The GitHub token needs Contents write permission for commits and Metadata
read permission to check the requester. Workflow-file changes may need additional
GitHub permission.

## Running and observing checks

With mutations enabled, a repository writer can request:

```text
@github-fixer /check 123456789
```

The number is an existing GitHub Actions run ID. The connector binds the grant
to that workflow, its observed attempt, and the PR head. `github_run_checks`
reruns that exact run, so it retains the original commit and ref instead of
using a moving branch. `github_workflow_status` lists runs for a commit or polls
one run, including its attempt, state, conclusion and GitHub URL. The token needs
Actions read permission for status and Actions write permission for reruns.

Requested execution is reported as pending until GitHub returns a result. A
newer or currently running attempt is observed rather than restarted. GitHub
has no atomic compare-and-rerun endpoint: concurrent requests and the interval
before an accepted attempt becomes visible remain governed by GitHub's API.
No local shell commands or arbitrary workflows are dispatched. The repository
must already have a workflow run; commits can trigger its configured CI normally.

The UI is GitHub Markdown, as clarified in the request; no separate browser UI
or vitrum component integration is needed.

## Local preview

Run the formatter without a GitHub token or model:

```sh
go run ./examples/github-review
```

The [rendered preview](../../examples/github-review/preview.md) uses synthetic
findings to demonstrate severity counts, comments, the aligned evidence table,
related concepts, and native feedback instructions. It is not a repository review.

## Event automations

An event automation starts a session when GitHub sends a matching event. It
is configured on the installation with `GET /api/clients` and
`PUT /api/clients/{id}` (`github.automations`); it is not a schedule, so the
console's Scheduled page does not list it. The same client read/write
permissions, tenant boundary and agent visibility rules apply. A rule is:

```json
{
  "name": "failed-check",
  "repository": "acme/app",
  "event": "check_run",
  "action": "completed",
  "conclusion": "failure",
  "agent": "reviewer",
  "instructions": "Investigate this check failure and report the smallest fix.",
  "paused": false
}
```

Supported events are `check_run`, `issues`, `pull_request` and `push`. For a
push, `action` is empty. Issue and pull-request rules may additionally match
`label`; a blank conclusion or label means no condition on that field.
Subscribe the GitHub App or repository webhook to the desired events and
permit those event IDs in the installation's capabilities. All events enter
through the existing signed GitHub webhook endpoint.

A client holds at most 50 rules. Names are unique within it; target agents
and repository owners must already be allowed by that client. Runs have no
person's credentials and use the client's existing home and runtime policy.
The prompt receives the rule's instructions and bounded repository, issue/PR
number and head SHA identifiers, never arbitrary text from the webhook.

Every run is a session, kept and readable in the console. It answers on the
pull request or issue that triggered it (`github://owner/repo/pull/7` or
`.../issues/7`), through the GitHub output policy every answer there passes.
A push, or a check run with no pull request, has no thread, and its answer
stays in its session. To send work somewhere else on a timetable, use a
schedule: its `deliver` names a Slack channel or a GitHub issue.

Own bot events and bot-authored commits do not trigger automation. Push and
check-run payloads must include commit author evidence (`head_commit.author`
or `check_run.check_suite.head_commit.author`); missing evidence is ignored.
A GitHub delivery ID, installation and rule name identify the run, so a
webhook redelivery does not start a second run. The existing `auto_labels`
map remains supported and is listed alongside event rules.

## Native reviews and feedback

Managed installations submit validated `humagen.review.v2` results as native
reviews when `github.login` is configured. The reviewed head is pinned; a moved
head refuses publication. Each applicable finding gets a line comment and an
optional committable suggestion. Missing diff locations become review-body text.
Approval additionally requires `approve_reviews: true` on the destination
repository's currently linked project, or the installation's configured home
when no link exists. Ambiguous or unavailable placement denies approval, even
when a cross-repository action grant permits posting. The default emits comments
and change requests. Review recovery uses
an app-owned marker and the durable delivery store to avoid duplicate reviews.

The host verifies evidence references against recorded tool results and session
reads before rendering a native review, Slack output, or console history. A
continued reviewer can cite an earlier helper turn after reading that session;
sharing a session still requires a current read grant. Evidence added after the
review completed cannot retroactively substantiate it. Unknown references show
as unverified, and denied source content requires an authorized summary.
Publication retries while the completed review is not yet in the journal.

`github.checks: true` already creates an in-progress check on that pinned commit
and edits its summary through completion. It needs Checks write permission.
Automatic reviews remain explicit per repository, for example:

```json
{"github":{"checks":true,"auto_labels":{"acme/service":{"humagen:auto":"reviewer"}}}}
```

Applying the configured label triggers a review; later pushes with the label
trigger a new review. Unlabeled pull requests do not start automatically. Existing
check conclusion behavior is preserved; no fixed push limit or label precedence
is introduced.

Thumb reactions on delivered answers are polled in bounded pages, at most one
page per installation every 15 seconds, for answers delivered in the last 30
days. `/feedback up|down [comment]` records feedback on the known session, or on
an inline finding when replying to its comment. Signed review-thread resolution
and reopening record and remove the corresponding finding assessment. Native
event IDs deduplicate retries across replicas. `/stop` uses the existing
installation-scoped stop policy. Subscribe to `pull_request_review_thread` for
resolution feedback and grant the `reaction` capability for polling.

Native actions may target another surface only when the exact repository is in
`github.action_repositories`; `read_repositories` never grants writes. All runtime
and installation action policies still apply. Reactions use GitHub's supported
emoji: eyes, rocket, -1 and confused. Slack's stop-hand emoji is not a GitHub
reaction, so GitHub uses `/stop`.

Thumb removals require two identical complete polling snapshots. Partial,
failed or over-limit scans withdraw nothing. The original reaction ID owns its
assessment, so removing an old thumb cannot clear a newer same-rating or console
assessment, and replaying its addition cannot restore a removed rating. Poll
progress and removals survive replica changes through the delivery store. GitHub
pagination is not a transactional snapshot; changing collections delay the
stable-snapshot check. At most 10,000 reactions are tracked per delivered answer.

Delivered native suggestions retain their exact path, line range, replacement
and reviewed SHA. Polling records `suggestion_committed` only when the current
PR head is a single-parent commit directly after that SHA, its diff includes the
target file, and the complete immutable file equals exactly that replacement.
The head is checked again after reading evidence. A reserved system actor links
the verified commit. Commit author and committer metadata cannot create a human
rating or evaluation candidate. This establishes the proposed edit was committed;
it does not claim a person used GitHub's Apply Suggestion button or approved the review.

Non-adjacent history, merges, additional edits in the same file,
omitted patches, unavailable/binary/oversized files and changed heads
remain unverified. A verified commit is historical evidence and is not erased
by a later revert. A moved head, resolved thread or outdated comment alone never
establishes a committed suggestion. See GitHub's [reaction API](https://docs.github.com/en/rest/reactions/reactions#list-reactions-for-a-pull-request-review-comment),
[commit API](https://docs.github.com/en/rest/commits/commits#get-a-commit), and
[review-thread fields](https://docs.github.com/en/graphql/reference/pulls#pullrequestreviewthread).

## Separate fix pull requests

Set `github.fix_pull_requests: true` to offer `github_fix_base` and
`github_create_fix_pr`. This preserves the existing `/fix` behavior. The first
action returns the repository's default branch and immutable SHA so the agent can
read complete files before preparing replacements. The second checks that base
and creates a new generated `humagen/fix-…` branch and a draft PR whose title ends
in `[humagen]`. Its body links the verified source issue or PR; issue links use
`Fixes`. It never updates the default or source branch and rejects changes under
`.github/workflows`.

The requester must have a verified GitHub identity with current repository write
permission. For console/Slack requests, `github.people` maps their workspace
subject to a GitHub login; ambiguous mappings deny. An explicit cross-surface
repository write grant is also required. Creating a fix PR asks for human approval
by default. Only explicit allow policies for both `write_files` and
`external_messages` skip that approval; a deny in either category wins.
Installation capability and agent tool grants continue to apply.

Retries recover the same branch, commit and PR using a request marker. A changed
fix branch is never overwritten. Opening the PR does not run tests, change
branch protection, merge, or deploy. Contents write and Pull requests write
permissions are required; workflow write permission is unnecessary.

Fix sessions started with a validated `source_alert` retain that alert in their
canonical session identity. Before creating a fix PR, the host reloads the run,
source alert and both sessions, checks the requester's current access and their
shared current project, and adds the source session and alert links. Links use
the existing `server.public_url`; model arguments cannot supply these fields.
Missing canonical URL omits console backlinks. An invalid or revoked pinned
source fails closed. No alert is guessed from other open alerts in a session.
