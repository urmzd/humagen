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

The [agent template](../examples/github-review/agent.json) grants this read
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
[fixer spec](../examples/github-review/fixer.json) after configuring its model:

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

The [rendered preview](../examples/github-review/preview.md) uses synthetic
findings to demonstrate severity counts, comments, the aligned evidence table,
related concepts, and native feedback instructions. It is not a repository review.
