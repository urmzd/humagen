# GitHub agent delivery

The GitHub connector accepts signed comments that ask `@humagen` on issues,
pull requests, reviews, and review comments. It also accepts review requests for configured agent logins.
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
| `HUMAGEN_GITHUB_CHECKS`, `HUMAGEN_GITHUB_GUIDES`, `HUMAGEN_GITHUB_MUTATIONS` | `github.checks`, `github.guides`, `github.mutations` |
| `HUMAGEN_GITHUB_READ_REPOSITORIES` | `github.read_repositories`. Optional: empty means the repositories the app is installed on (see [Repositories the app is installed on](#repositories-the-app-is-installed-on)) |
| `HUMAGEN_GITHUB_LOGIN` | `github.login` |
| `HUMAGEN_GITHUB_DEFAULT_AGENT` | `default_agent`: who answers `@humagen` when no agent is named, ahead of the entry agent, and who does automatic reviews. Optional |
| `HUMAGEN_GITHUB_ASKERS` | `github.askers`: the only logins whose mentions, reviews and pull requests are acted on, comma separated. Optional |
| `HUMAGEN_GITHUB_PEOPLE` | `github.people`: `login=subject` pairs, comma separated, such as `urmzd=tailscale:urmzd@github`. The subject is the person's console sign-in subject. A malformed or repeated entry stops the server. Optional |
| `HUMAGEN_GITHUB_AUTO_REVIEW` | `github.auto_review.enabled`: `true` switches automatic review on, `false` off. Unset, it is off. A repository's [settings](#repository-settings) override it |
| `HUMAGEN_GITHUB_AUTO_REVIEW_REPOSITORIES` | `github.auto_review.repositories`, as `owner/repo=on` or `owner/repo=off` pairs, comma separated |

`HUMAGEN_GITHUB_AUTO_LABELS` is retired: labels start nothing, and the server
refuses to start while it is set. Use [Automatic review](#automatic-review).

The default client may route to any agent. One created through the API routes
to its default agent and its `agents` list, and an event rule or a bot that
names any other agent is refused when the client is saved.

## Who may ask

On a public repository anyone can comment, so the client acts only on people
who may ask: those GitHub says can write to the repository (its owner, a
member of its organization or a collaborator, read from the payload's
`author_association`) and people the client maps (`github.people`). A
comment, a review or a pull request from anyone else starts nothing, its
event rules included, and the log says why. An issue from them still meets
the client's event rules, which are written to read issues, but asks no
agent. A review request counts as its requester's, since only people with
write access may request one.

`github.askers` (`HUMAGEN_GITHUB_ASKERS`, comma separated) narrows this to
the logins it lists:

```sh
export HUMAGEN_GITHUB_ASKERS=octocat,hubot
```

## Who a comment asks

`@humagen` is the one trigger. A comment, a review comment, a review body, or
the body of a new issue or pull request starts a run only when it holds
`@humagen`, in any case, as a whole word. The trigger counts anywhere in the
text, except where text is shown or quoted rather than said:

- code: an inline span (`` `@humagen help` ``, also across lines), a fenced
  block, a block indented four spaces, and `<code>` or `<pre>`;
- an HTML comment (`<!-- @humagen review -->`), so a pull request template
  can hold one without asking on every pull request;
- a quoted line (`> @humagen ...`) and the lines that run on from it.

A whole word means nothing joins the name to a word or a path: not
`@humagen.dev`, `ops@humagen`, `https://github.com/@humagen` or
`@humagen/reviewers`. A sentence may end on it (`thanks @humagen.`). The
app's own login without `[bot]` (`github.login`, such as `humagen-acme` for
`humagen-acme[bot]`) is a trigger too.

The agent named right after the trigger answers. Write the name so it can
only be a name: after `ask`, with `:` or `,` after it, or as `@name`. A bare
first word is part of the question, even when an agent has that name, so
`@humagen docs are stale` asks the default about stale docs. Otherwise the
default answers:

| Comment | Who answers | What the agent reads |
|---|---|---|
| `@humagen what changed?` | The default (below) | `what changed?` |
| `@humagen ask research-bot why is the cache cold?` | research-bot | `why is the cache cold?` |
| `@humagen research-bot: why is the cache cold?` | research-bot | `why is the cache cold?` |
| `@humagen research-bot why is the cache cold?` | The default | `research-bot why is the cache cold?` |
| `Thanks. Could @humagen summarize this?` | The default | The whole comment, as written |
| `@research-bot why is the cache cold?` | Nobody | Nothing starts |
| `> @humagen what changed?` and a reply under it | Nobody | Nothing starts |

`@humagen @research-bot ...` names research-bot too, but on GitHub it also
notifies any account called `research-bot`. Prefer `ask research-bot`.

A trigger that opens the comment is its address, and is taken out with the
agent's name after it. Anywhere else the whole comment is sent as written.

An agent's name alone asks nobody. On GitHub `@name` is an account, and the
account may be a stranger's: `@github-reviewer` notifies the GitHub user of
that name. So a mention of an agent's name, or of a login in `bots`, starts
nothing.

When no agent is named, the default answers, nearest first:

1. The repository's agent, when its [settings](#repository-settings) name one.
2. The client's `default_agent`.
3. The workspace's default.
4. The workspace's entry agent, the agent that answers a Slack direct message.

The client's `agents` list holds: a default it may not route to is refused,
never swapped for one it may. A plain question asks the default even where
another agent answered before, so `@humagen what changed?` gets a plain
answer on a pull request that was reviewed.

A review requested from the `humagen` account (or another trigger's) is the
default's, as `@humagen review` is: the repository's agent reviews, else the
next default.

A reply without the trigger continues a conversation an agent already has
there: the agent that answered there before reads it, whatever the reply
mentions. A comment that holds no trigger anywhere else starts nothing, so a
busy repository wakes no agent on every comment. A bot's comment starts
nothing, whatever it holds.

Add the `humagen` GitHub account as a collaborator on each repository, with
read access. GitHub then offers `@humagen` as people type it. The account
exists so that the name is Humagen's: GitHub cannot autocomplete or notify a
GitHub App's login (`humagen[bot]`), and an agent's name may be anyone's.

`bots` still maps a login to its agent for a review request: requesting a
review from a mapped login asks that agent. It no longer makes a mention a
tag, the app's own login included.

**Upgrading.** A deployment that sets `HUMAGEN_GITHUB_DEFAULT_AGENT` to a
reviewer, such as `github-reviewer`, still sends every plain question to that
reviewer. Unset it, or set it to the entry agent, so a plain question gets a
plain answer. To keep automatic review, name the reviewer in the repository's
[settings](#repository-settings). People ask the reviewer by name:
`@humagen ask github-reviewer to review this`. A `HUMAGEN_GITHUB_BOTS` entry for the
app's own login (`humagen[bot]=github-reviewer`) no longer changes who a
mention asks, and can go.

A review, a review comment and a new issue or pull request are read the same
way. Every run is filed where the client files it: the project its repository
is linked to, else the client's [home](clients.md#the-record). It acts for the
person the client maps the GitHub login to (`github.people`), and for nobody
when the client maps the login to nobody. A comment addressed to the app on a
pull request posts no guide, since the guide names the agent that answers; the
next one that names an agent does.

## Sessions

A pull request or an issue is one session, as a Slack thread is. The first
mention there opens it, and every later mention there continues it, whatever
the pull request's head commit is then:

| Field | Value |
|-------|-------|
| Key | `github://<owner>/<repo>/pull/<n>` or `github://<owner>/<repo>/issues/<n>`, and the agent that answered first |
| Owner | The agent that answered first. Another agent asked there answers in the same session, with a conversation of its own |
| Filed in | The project and channel the run is filed in: the repository's linked project, else the client's home |
| Title | The pull request's or issue's title, else `<owner>/<repo>#<n>` |
| People | Each person the client maps a login that asked there to, added when they ask |
| Visibility | Private to those people |

The key is the one the runtime already keeps the agent's conversation under,
so the session the console lists is the conversation the agent continues.
The console lists it in Sessions and in Recent for each person in it, with
Where set to GitHub, linked to the pull request or issue.

**Who reads it.** Only the people who asked there read the session in the
console. A person is found by `github.people` (for the default
client, `HUMAGEN_GITHUB_PEOPLE`), so map each login whose sessions should
show in the console. GitHub showed each of them the repository, so the session shows
them nothing they could not read. Humagen cannot check that anyone else can
read the repository, so nobody else is added, and a login the client maps to
nobody adds nobody. A workspace administrator reads it by exception, as any
private session.

**What an answer may use.** The session is on the client's surface, so a
turn there reads the corpora a GitHub thread may show: public corpora the
whole workspace reads, never the asker's private sources
([RFC 0010](../rfcs/0010-corpora.md#7-sensitivity-private-sessions-and-personal-context)). What is posted to GitHub, and how,
is what it was before sessions: the same signed delivery, the same review
checks and the same actions.

**Memory.** A turn in a GitHub session is remembered as GitHub turns were
before sessions: it may teach project memory, and it never becomes anyone's
personal memory proposal, since anyone may have written on the pull request.

**Agents it calls.** An agent the mention's run calls belongs to the same
session and acts on GitHub as the mention's run does. A reply a person posts
in the session from the console is a private turn: it does not act on GitHub.

**Two clients on one repository.** The client that answered first holds the
pull request's session. The other client's runs there stay their own
conversations, as before sessions.

A run an automation or the automatic review starts is not filed in the
session: no person asked it.

## Automatic review

Mentions start work. Automatic review is off unless something turns it on: the
client's switch below, or a repository's [settings](#repository-settings) in
the console.

When it is on, a pull request is reviewed automatically when it opens or
reopens ready for review, leaves draft, or gets a push. The client's default
agent, or the repository's own agent, reviews it, on the pull request; with
neither, nothing is reviewed. A default agent from the agent store that the workspace has not
installed, such as `code-reviewer` before the Review package is installed,
reviews nothing either: the delivery is taken, and the server logs
`automatic review skipped` saying to install the Review package from the
Store. It is off unless switched on, and labels start nothing:

```json
{"github":{"auto_review":{"enabled":true,"repositories":{"acme/handbook":false}}}}
```

A client configured by environment variables takes these from
`HUMAGEN_GITHUB_AUTO_REVIEW` and `HUMAGEN_GITHUB_AUTO_REVIEW_REPOSITORIES`;
for example, off everywhere but one test repository:

```sh
export HUMAGEN_GITHUB_AUTO_REVIEW=false
export HUMAGEN_GITHUB_AUTO_REVIEW_REPOSITORIES=acme/review-sandbox=on
```

`enabled` is the connection's switch, off when left out. `repositories` turns it
on or off for exact `owner/repo` names whatever `enabled` says, for at most 64
repositories the client serves. Both are defaults: a repository's settings in
the console win over them for that repository. Subscribe the webhook to
`pull_request`.

Drafts and closed pull requests wait. One commit is one review: its run is
named for the client, repository, pull request and head SHA, so a redelivery,
or a pull request that opens and is pushed at the same commit, joins it. The
run is asked with identifiers only, never the pull request's title or body;
the reviewer reads the pull request with its tools. An `@humagen` comment still
starts its own request.

Opening, reopening or leaving draft reviews the whole pull request. Each push
after that reviews only the commits after the last one reviewed. Five pushes
are reviewed; the sixth pauses automatic review on that pull request, and
later pushes wait. A person who asks `@humagen` on the pull request, such
as `@humagen review`, resumes it: the count starts again, and the next push is reviewed from the last
reviewed commit. Opening, reopening or leaving draft also starts the count
again. The count and the pause are kept per pull request in the shared store
(`pull_reviews`), so every replica sees them.

## Reading context and structured reviews

`github_read_context` returns the subject and a page of comments. For PRs it
also returns the head SHA, reviews, inline comments and changed-file patches.
Follow `next_page` until absent. A missing patch is unavailable evidence.

Its `address` takes an issue or pull request as `github://owner/repo/pull/12`,
`github://owner/repo/issues/12`, `owner/repo#12` or the `https://github.com`
URL of one. It also takes a repository as `owner/repo`, `github://owner/repo`
or `https://github.com/owner/repo` (a trailing `.git` is dropped), which
returns one bounded page: the
description, the default branch, the first 4000 bytes of the README, and the
10 most recently updated open pull requests and the 10 open issues, each with
the address that reads it. A repository with a `number` reads that issue or
pull request. A repository read passes the same grants as its issues below.
Every read leads with a `notice` field marking the GitHub text in it as
untrusted data. The grants read `address` in any letter case, as the tool
does, and refuse a call that names it twice or passes an argument the tool
does not declare.
Reads default to the source repository. A run from Slack, Jira, or a link in
another repository may also read any repository the client's GitHub App
installation covers (see
[Repositories the app is installed on](#repositories-the-app-is-installed-on)).
A run from another surface reads through the oldest GitHub client in its
workspace that may route to its agent.

`read_repositories` adds exact repository names to that. It is optional. Use
it for a client that signs in with a personal token, which has no installation
to list:

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

The [agent template](https://github.com/urmzd/humagen/blob/main/examples/github-review/agent.json) grants this read
tool and specifies the structured review contract. Replace `YOUR_MODEL_ID`
with a model available to your configured Anthropic provider, then register it:

```sh
humagen serve -agent-specs examples/github-review/agent.json
```

### Reply or review

The host decides what a run on GitHub starts as. It chooses when the run
starts and saves the choice on the run's tag as `answer_shape`. The model
reads it in its trigger context.

| Shape | When | What the agent returns |
| :--- | :--- | :--- |
| `review` | On a pull request, and only on a signal that cannot be misread: an [automatic review](#automatic-review), a review requested from the app, an [event automation](#event-automations) whose shape is `review`, the bare command `@humagen review`, or `/review` | The fields of a `humagen.review.v2` report, as one JSON object. The host renders and posts it. |
| `reply` | Anything else: a question, a follow-up, a request for a review in other words, or anything on an issue | Plain Markdown prose: short paragraphs, code in fences, files linked as `path:line` at the head. |

The host reads the command and nothing else. It does not guess what a
sentence means.

| Message on a pull request | Starts as |
| :--- | :--- |
| `@humagen review`, `@humagen re-review`, `@Humagen Review.`, `review` in a conversation the agent already has | `review` |
| `/review`, `/review focus on auth` | `review` |
| `@humagen review this function's naming?`, `@humagen review the retry loop` | `reply` |
| `@humagen can you review this?`, `please review this PR` | `reply` |
| `hey @humagen review`, `@octocat review` | `reply` |
| `this review is wrong`, `don't review yet` | `reply` |
| Any of these on an issue | `reply` |

The command is the whole message: `review`, `re-review` or `rereview`, in
any case, with space around it and `.`, `!` or `?` after it. The trigger may
address it when the trigger opens the message (`@humagen review`,
`@humagen: review`). `/review` is a command when the message starts with it;
what follows it is what to look at. Like any message, it reaches Humagen
with the trigger (`@humagen /review`) or in a conversation the agent already
has. Quoted lines do not count. Any other
word, a trigger inside a sentence, or a mention of anyone else makes the
message a reply.

#### A reply that becomes a review

A person often asks for a review in their own words: `have a look before I
merge`, `can you review this?`, `check this for security`. Such a run starts
as a reply, and the agent reads what was meant. The built-in `code-reviewer`
has one tool for this, `start_review`, which turns its run into a review.
After it the agent returns the report, and the host checks, renders and posts
it as it does for any review.

`start_review` takes no arguments. The host allows it only when all of these
hold, and refuses it with a reason otherwise:

- The run is on a pull request. On an issue there is nothing to review.
- A person started the run. A run an agent started for another (a handoff, a
  subagent or a delegation) cannot start a review, and neither can a run a
  rule started: a rule's `shape` is the rule's to set.
- The run is not a review yet. A run becomes a review once; a second call
  changes nothing.

The switch is an action on the run's record, not a change to the saved
`answer_shape`. A retry, the format check and the native review all read
the record, so the run stays a review. Any agent may be given the tool in
its `tools`; an agent without it answers a reply run in prose.

A run from before the host chose has no shape. The built-in `code-reviewer`
returns a report there only when it was asked to review a change.

The model never writes the reviewed commit. The host scopes the report to the
head it pinned when the run started (the event's head, else its channel's
`head_sha`, else the head the run read with `github_read_context`), and
replaces any commit the model wrote. A review with no recorded head is shown
as needing correction.

Only a review gets the online format check: a run saved as `review`, or one
that called `start_review`. Its answer must be a complete review report, else
the session is marked for review with the reason. A reply is never
format-checked, whatever its agent's instructions say, and it posts no native
review, even if its agent answered with a report.

A report is strict in structure and free in its words. `validateV2` requires a
title, a verdict, unique finding ids, evidence and a prompt for each finding,
and bounds the free text: a summary of at most 1,500 characters, a finding
title of 150, `why` and `suggestion` of 600 each, at most 10 comments and 8
pieces of evidence for each finding. A location is a repository path (no
leading `/`, no `..`, no backtick) and a line range in it. Model text is
escaped when it is rendered: HTML, links and emphasis show as typed, and only
inline `code` stays code. An invisible word joiner (U+2060) follows each `@`
and `#`, and sits inside `GH-`, so GitHub turns no `@name`, `@org/team`,
`#12` or `owner/repo#12` into a mention or a reference; an HTML entity such
as `&#64;` does not stop GitHub. A bare address is shown as code, and a
source or related link is a link only into the reviewed repository, so a
review links nowhere else. An em dash in model text reads as a comma.

### How a review reads

The host renders every review the same way, from the report:

1. The verdict in words, in bold, such as **Needs changes: one problem to fix
   before merging.** or **Looks good to merge.**
2. The summary, in one to three sentences.
3. Each finding, most urgent first: a bold numbered title, a small label with
   its severity (Must fix, Should fix or Note), its location linked at the
   reviewed head and the agent that found it, a blank line, then why it
   matters and what to do. A finding with no line in the diff shows its
   change here.
4. With eight findings or more, a compact table of them, collapsed.
5. The evidence, collapsed: each item numbered for its finding, without the
   finding's title again. Evidence the host could not verify says so.
6. A hidden marker, `<!-- humagen.review.v2 verdict=… errors=… warnings=… info=… -->`,
   for programs, then the session link.

```markdown
**Needs changes: one problem to fix before merging.**

This change adds retries with backoff to webhook delivery. The loop now loses the final error, so a failed delivery looks like a success.

**1. Retry loop drops the final error**
<sub>Must fix · [`internal/retry/retry.go:45`](https://github.com/acme/webhooks/blob/3f9c…/internal/retry/retry.go#L45) · found by code-reviewer</sub>

After the last failed attempt `Do` returns `nil`, so callers treat a failed delivery as a success. Return `lastErr` when the loop ends.
```

Each line comment is the finding as a colleague leaves it: the title, why
and what to do, a small severity label, then the committable suggestion.
No ids, tool names or JSON are visible. The golden files in
`internal/presentation/github/testdata/golden` hold a reply and four reviews,
and their test checks the voice: no em dash, no filler opener, no internal
id.

Structured output is held while the run streams, then rendered in its existing
comment. Lifecycle-only routes also render the final report. Ordinary Markdown
answers retain their streaming behavior. The first format,
`humagen.github.review.v1`, still renders the same way, as a comment.

### The answer comment

An ordinary answer's comment holds the answer alone. While the run works, it
shows one status line, such as `**humagen** _(working)_`. The tools the agent
called, the helpers it started and the helpers it waits on are the session's
trace: the comment does not show them. A small footer links the session in the
console, where the trace is:

```markdown
The pull request links GitHub and Slack accounts through OAuth.

<sub>[View the session in Humagen ↗](https://console.example/sessions/s_...)</sub>
```

The footer needs the console's canonical address (`server.public_url`); without one
there is no footer. A failure, a stop and a question a person must answer are
still said in the comment. A structured review renders as described above.

### get_context on an issue or pull request

`get_context` in a session that started on an issue or pull request reads that
place. It gives the title and state, the description, the five latest comments
by people, and for a pull request its head, changed files and patches. GitHub
lists comments oldest first, so when the first page is full the latest are
read from the last page its `Link` header names, and the page before it when
the last holds too few comments by people: at most three requests. When they
cannot be read, the answer says the latest may be missing. Comments by
apps (`[bot]`) and by any login a client of the workspace answers as are left
out. Each part is cut to a bound, and the whole stays under the tool's 12,000
characters.

A run reads through the GitHub client that raised it when the session's root
is that client's thread on that issue or pull request, and only when that
client serves the owner, names the repository in `read_repositories`, or
covers it by its App installation. Any other run, and a person in the console,
reads as a cross-surface read: under the workspace's grants and the
repository's `cross_surface_reads`. Anything else gets a note that the place
may not be read, and no text.

What is read is untrusted. It is wrapped in one `github-data` block whose
markers carry a nonce made for that read, as the reads made before a run are.

A brief that a run on an issue or pull request gives another agent ends with
the place, for example `Asked on GitHub pull request acme/site#7
(https://github.com/acme/site/pull/7).`, so the helper never has to ask which
repository.

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
reaches the browser. A client reads repositories its owners include, its
installation covers, or its `read_repositories` name; any other repository, another workspace's, or a file
the commit lacks answers 404. One request shows at most 200 lines and 20 lines
of context, from a file of at most 256,000 bytes of UTF-8 text, and files read
at a commit are kept, 64 at most, since they never change.

## Repositories the app is installed on

A client that signs in as a GitHub App reads the repositories its
installation covers from `GET /installation/repositories`, with the
installation token it already uses. The installation is the boundary: its
token reaches nothing else, so Humagen grants nothing else.

- **Session picker.** A new session offers the repositories every enabled
  GitHub client of the workspace covers, plus any its `read_repositories` or
  `action_repositories` name, deduplicated and sorted. The picker shows the
  first 100. A session may choose any of them.
- **Reads.** `github_read_context`, `github_read_file` and
  `github_workflow_status` from another surface, and the console's source,
  issue and pull request reads, reach any repository the installation covers,
  unless its [settings](#repository-settings) keep it to its own runs.
  The read tools check the client's `owners` first, so they refuse an
  installed repository of an account `owners` does not list. The console's
  reads do not: they reach any repository the installation covers. Keep
  `owners` to the installation's account so the two agree.
- **Writes.** Unchanged. A write stays bound to the original signed request,
  or to `action_repositories`.

The list is read when first needed and kept for 5 minutes. It holds at most
1,000 repositories, 100 to a page, and one read gives up after 10 seconds.
Callers that find the list due share one read. Each one waits only until its
own request ends, and then uses the list as it stands. A caller that gives up
does not stop the read for the others. The picker reads every client's list
at the same time.
When a read fails, Humagen keeps the list it had, or uses only the named
repositories when it had none, logs the failure, and tries again after a
minute. A repository added to the installation appears within 5 minutes; one
removed is refused by GitHub at once, since the token no longer reaches it.

A client with a personal token has no installation to list. It offers and
reads only what its owners and `read_repositories` name.

## Repository settings

Each repository a GitHub client covers can have settings of its own, kept in
the workspace's store (`github_repo_settings`), one versioned record per
client and repository, at most 1,000 per client. Set them in the console under
**Settings > Apps > GitHub > Repositories**
(`/settings/apps/github/<client>/repositories`), or with the API:

| Route | Permission |
|---|---|
| `GET /api/clients/{id}/repositories` | `clients:read` |
| `GET /api/clients/{id}/repositories/{owner}/{repo}` | `clients:read` |
| `PUT /api/clients/{id}/repositories/{owner}/{repo}` | `clients:write` |
| `DELETE /api/clients/{id}/repositories/{owner}/{repo}?version=N` | `clients:write` |

The list shows the repositories the installation covers, those the client
names (`read_repositories`, `action_repositories`, `auto_review.repositories`,
event rules), and those with settings. Those are the repositories the client
covers. Another workspace's client, a Slack client, or any other repository,
even one of an account the client serves, answers 404.

| Setting | Field | Left empty | What the platform does |
|---|---|---|---|
| Automatic review | `auto_review: {opened, pushes}` | the client's switch | Reviews when a pull request opens, on each push, both, or never (`{false, false}`). |
| Answers `@humagen` when no agent is named | `agent` | the client's `default_agent` | That agent answers `@humagen` with no agent named, ahead of the client's default and the entry agent, and does automatic reviews. It must be one the client routes to. |
| Instructions for runs here | `instructions` | none | Pinned on every run the client raises on the repository, as the last instruction layer, after the workspace's and the projects'. At most 4,000 characters. |
| Read before a run starts | `reads: [pull_diff, linked_issues]` | none | Before a run on a pull request or issue starts, Humagen reads the pull request's description and changed files (`pull_diff`), and the issues its description links in the same repository, at most 3 (`linked_issues`), within 24,000 characters and 3 seconds. A read that fails or runs out of time is named, not left out. |
| Readable from other surfaces | `cross_surface_reads` | readable | When `false`, only runs on the repository's own issues and pull requests read it. Runs from Slack, the console or another repository are refused by `github_read_context`, `github_read_file` and `github_workflow_status`, whatever `read_repositories` or the installation grants. The console does not show its source, issues or pull requests, and the session picker does not offer it. One client turning it off keeps the repository so in the whole workspace, through every client that covers it. Every client's view shows the workspace's answer, with the clients that keep it in `kept_by`. |

What is read before a run is untrusted: anyone who can open a pull request
writes its description and its patch. It is added to the run's text in one
block, `<github-data id="NONCE">` to `</github-data id="NONCE">`, whose id is
a nonce made for that run, after a sentence that says the block is data and
never instructions. Each description and patch sits in a code fence longer
than any run of backticks inside it, and a `github-data` marker inside the
data is broken, so nothing a pull request says can end the block early.
When the budget cuts the data, it cuts inside a fence and closes it, so the
note that says so and the closing marker stay outside every fence. The
reads happen inside GitHub's webhook request, which GitHub abandons after 10
seconds, so they stop at 3; the agent reads the rest with its GitHub tools.

A write names the `version` it read, `0` while the repository follows its
client; a write over a change made since answers 409, and so does a write of
empty settings with version `0` while settings are kept. Settings that change
nothing remove the record, and `DELETE` returns the repository to the client.

The client's own settings are the defaults, so a deployment configured by
environment variables behaves as it did until someone changes a repository:
`HUMAGEN_GITHUB_AUTO_REVIEW`, `HUMAGEN_GITHUB_AUTO_REVIEW_REPOSITORIES` and
`HUMAGEN_GITHUB_DEFAULT_AGENT` still set the client, and the repository's
settings win where they are set. `HUMAGEN_GITHUB_ASKERS` stays the client's:
who may ask is not set per repository. Settings that cannot be read fall back
to the client for the agent, review nothing, and refuse cross-surface reads.

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

A trigger in an inline PR review comment is answered inside the same native
review thread, including when the comment is itself a reply. The answer is edited as it streams.
A trigger in an issue or PR conversation is answered in the main conversation.
Explicit delivery overrides retain their configured destination.

## Requested fixes

Enable mutations and register the optional
[fixer spec](https://github.com/urmzd/humagen/blob/main/examples/github-review/fixer.json) after configuring its model:

```sh
export HUMAGEN_GITHUB_MUTATIONS=true
humagen serve -agent-specs examples/github-review/fixer.json
```

A repository writer can request a fix in a PR conversation or review thread:

```text
@humagen ask github-fixer /fix handle the retry timeout and add a regression test
```

`github_read_file` reads complete UTF-8 source at an immutable SHA before editing.
`github_apply_changes` creates one atomic commit with up to 20 file additions,
replacements or deletions, bounded to 500 KB total replacement content. The
source PR must be open and its branch must be in the same repository. Fork PRs
are read-only for this tool. Branch protections still apply.

Fixes require an explicit `/fix` mention from a person with effective GitHub
write permission. The command is the first word after the address, the
trigger and the agent's name: `@humagen ask github-fixer /fix the typo` asks for a
fix, and a comment that puts words or a mention before the command, as
`/check @humagen ask github-fixer 123456789` does before the run id, grants
nothing. Permission is checked at acceptance and again before writing.
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
@humagen ask github-fixer /check 123456789
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

The [rendered preview](https://github.com/urmzd/humagen/blob/main/examples/github-review/preview.md) uses synthetic
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
push, `action` is empty. A blank conclusion means no condition on it. Labels
start nothing: a rule cannot fire on `labeled` or `unlabeled`, and one stored
that way reads paused for good, with a `note`. A rule stored with a `label`
reads with a `note` saying the label is ignored, and saving drops both.
Subscribe the GitHub App or repository webhook to the desired events and
permit those event IDs in the installation's capabilities. All events enter
through the existing signed GitHub webhook endpoint.

A client holds at most 50 rules. Names are unique within it; target agents
and repository owners must already be allowed by that client. Runs have no
person's credentials and use the client's existing home and runtime policy.
The prompt receives the rule's instructions and bounded repository, issue/PR
number and head SHA identifiers, never arbitrary text from the webhook.

A rule's `shape` says what its runs answer with (see
[Reply or review](#reply-or-review)): `review` posts a review, `reply`
answers in prose. Left out, a rule on `pull_request` reviews and any other
replies, whatever its instructions say. A rule's run keeps the rule's shape:
its agent cannot turn it into a review with `start_review`. The console's
rule editor does not set it yet; set it in the client's configuration or
through the API.

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
webhook redelivery does not start a second run. Automatic review is not a
rule: it is the connection's switch (see [Automatic review](#automatic-review)).

## Native reviews and feedback

Managed installations submit validated `humagen.review.v2` results as native
reviews when `github.login` is configured, for runs saved as `review` (see
[Reply or review](#reply-or-review)). The host pins the reviewed head; a moved
head refuses publication. Each finding on a line of the diff gets a line
comment and an optional committable suggestion. A finding with no location,
or one off the diff, stays in the review body with its change. Each finding
may name the agent that found it (`found_by`, such as `security-reviewer` or
`security-reviewer#a`); the review shows the agent's name without the label.
Agents never approve: an `approve` verdict is posted as a comment,
`request_changes` as a change request, and a person approves and merges.
Review recovery uses an app-owned marker and the durable delivery store to
avoid duplicate reviews. Before it posts, the host records in the delivery
store that the review is in the current layout. A review with no record was
posted by the code from before reviews read as prose, and recovery
reproduces it in that table layout, so it is still receipted and its
findings stay mapped.

The host verifies evidence references against recorded tool results and session
reads before rendering a native review, Slack output, or console history. A
continued reviewer can cite an earlier helper turn after reading that session;
sharing a session still requires a current read grant. Evidence added after the
review completed cannot retroactively substantiate it. Unknown references show
as unverified, and denied source content requires an authorized summary.
Publication retries while the completed review is not yet in the journal.

`github.checks: true` already creates an in-progress check on that pinned commit
and edits its summary through completion. It needs Checks write permission.
[Automatic review](#automatic-review) runs show their progress there too.

Thumb reactions on delivered answers are polled in bounded pages, at most one
page per installation every 15 seconds, for answers delivered in the last 30
days. `/feedback up|down [comment]` records feedback on the known session, or on
an inline finding when replying to its comment. Signed review-thread resolution
and reopening record and remove the corresponding finding assessment. Native
event IDs deduplicate retries across replicas. `/stop` uses the existing
installation-scoped stop policy. Subscribe to `pull_request_review_thread` for
resolution feedback and grant the `reaction` capability for polling.

Native actions may target another surface only when the exact repository is in
`github.action_repositories`; neither `read_repositories` nor the
installation's repositories grant writes. All runtime
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
