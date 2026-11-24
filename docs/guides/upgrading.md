# Upgrading

Steps an operator runs once, after a release that needs them.

## Mapping people for the Slack webhook connector

The Slack webhook connector (`/connectors/slack`) starts and continues runs
only for a person the client's team maps
([who can start and stop a run](../reference/slack.md#who-can-start-and-stop-a-run-on-the-webhook-connector)).
After this release a Slack client whose team maps nobody starts no runs, and
the server says so at start, once for each such client:

```
humagen: Slack client "default-slack" in workspace "default" maps no people, so its webhook connector starts no runs until people are mapped for its team ...
```

Map the people who should ask, and grant each one `runs:write`:

1. Map each Slack user id to the person it is, in `slack.people` of an
   enabled Slack client of the same team: on the client itself through
   `PUT /api/clients/{id}` or Settings, Connections, or on the Slack app's
   record with `surfaces.slack.people` in the Platform document
   ([clients](../reference/clients.md#people-a-slack-app-answers-for)). A
   client that maps people names a `slack.machine`.
2. Grant each mapped subject `runs:write` in the workspace's policy by
   subject (`grant operator to subject "..."`): a Slack message carries no
   sign-in, so a role held only through a group does not count.
3. Make each of them a member of any project a bound Slack channel files
   into.

The default client the `HUMAGEN_SLACK_*` variables make has no people of its
own and is not changed through the API. Its team's people come from another
Slack client record of the same team, such as the one
`surfaces.slack.people` keeps; without `HUMAGEN_SLACK_APP` no second client
can be made for its team through the API. A connector an embedding program
passes as a `Connection` has no client record and starts no runs on Slack:
make a client for it instead.

A run is stopped from Slack by a mapped person or by the Slack user who
asked for it. Ratings by reaction are unchanged.

## Filing past work into sessions

A mention on a GitHub pull request or issue opens or continues that place's
session ([GitHub sessions](../reference/github.md#sessions)). A run from
before that release is a conversation of its own, and Recent and Sessions do
not list it. Neither do they list a conversation from before sessions (a
`th-` thread, a Slack thread or direct message among them). `humagen migrate
sessions` files that work into sessions once.

```
humagen migrate sessions -state postgres [-tenant NAME] [-server-tenant NAME] [-apply]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-state` | `postgres` when `$HUMAGEN_POSTGRES_URL` is set, else `memory` | The stores to read and write. The command refuses `memory`, and a database that holds no runs: both have nothing to migrate. It writes through the stores, never with SQL |
| `-tenant` | every tenant the run index holds | One tenant only |
| `-apply` | off | Write the changes. Without it the command writes nothing and prints what it would do |
| `-server-tenant` | `default` | The tenant the server's `-tenant` names. The clients the server's environment configures (`HUMAGEN_GITHUB_*`, `HUMAGEN_SLACK_*`) are never stored, so the command builds them in this tenant from its own environment, as `serve` does |
| `-allow-empty-store` | off | For tests only: run against the in-memory store or an empty database |

The output starts with the store, the environment's clients it built, and
the tenants it read, then how many runs of each tenant it read. Check them before you go on.

### What it changes

| Past work | What the command does |
|-----------|-----------------------|
| A GitHub run that a mention on a pull request or issue started | Files it in that place's session, the one a mention there opens now: the same key, `github://<owner>/<repo>/pull/<n>` (or `issues/<n>`) and the agent that answered first. It opens the session when there is none, oldest run first |
| The runs in that run's conversation | Moves them into the session's conversation, so the session lists them as its turns and the old conversation is no longer listed |
| A run of the session's owner | Binds it to the session's record, in the order the runs were asked and ahead of any newer run bound already, so the session's latest run stays its newest. A run the runtime bound when it ran stays bound once |
| The conversation's title in the run index | The pull request's or issue's title, from the run's trigger |
| The person who asked | Adds the person the GitHub client maps the login to now (`github.people`). A login mapped to nobody adds nobody. The session is private to these people, as a new one is, and gets no Slack audience record |
| The session's times | Dates the session from its work: started when its first run was asked, last active when its latest run or message was. Recent and Sessions then list it where that work falls, not at the time the command ran |
| A session the command finds | Dates it from its runs, and changes nothing else, when its record was created after the first run the command files in it had ended. That is a session an earlier run of the command opened, which that release dated at the time it ran, so it sat at the top of Recent. It is also a session a mention opened on a pull request or issue that has older runs: the command files those runs in it, and the session then starts when the oldest of them was asked. An archived session that is not deleted is dated the same way, and stays archived. A session whose first run is the mention that opened it is left as it is, while that run is queued or running and after it ends. A session whose first run is another run that has not ended, such as one that stopped before it reached an ending, is left as it is too, with a `github session not dated` line that names the run. The servers keep running during the command, so a mention may open a session while it reads |
| A session's last activity | The time of its latest run or message. It is never earlier than the latest message in the session when the command writes. A later change that is neither, such as a person added, does not count as activity |

### Conversations from before sessions

| Past work | What the command does |
|-----------|-----------------------|
| A conversation from before sessions (a `th-` thread) that names one agent | Adopts it as that agent's session under the id it has. Its messages, the runs filed in it and every link to it stay under that id, so nothing moves and no link breaks. Its record gains the session and its one agent; the session store gains a record under that id |
| A Slack thread or direct message from before sessions | The same, at the Slack thread's address. Replies on Slack keep reaching it, now as a session's |

The agent a conversation becomes the session of is the one it names as its
default, else the one agent it selects, else the first agent that answered
in it, else, on Slack, the client's default agent.

When it is adopted, a conversation is read by the people who read it
before, and by nobody else:

- It stays in its project and channel, shared or private as it was, with
  the same participants. A shared conversation is read by everyone who reads
  its project. A private one is read by its participants. A Slack
  client's machine reads the threads on its surface.
- A session is also read only by those who read its agent. The command
  adopts a conversation only when everyone who reads it reads that agent
  too. Any other conversation is left, and its line says why.
- Nothing is read across tenants. The command reads and writes one tenant
  at a time.
- A Slack reply in an adopted thread does not change who takes part. A
  session that Slack starts now follows the members of its Slack
  conversation; an adopted one keeps the people it had.

Nobody gains a conversation by its adoption. Afterwards, someone can stop
reading one they read before, in these cases:

- A session is read only by those who read its agent. If the agent later
  moves to a project some readers do not read, such as one kept to its
  members, those readers stop reading the adopted conversation. As a
  conversation, it did not depend on the agent.
- The session has one agent, and that does not change. The conversation's
  default and selected agents are set to it, so a person can no longer
  select another agent there. A message can still tag another agent.
- A private Slack thread that is adopted keeps the people it had. People
  who join its Slack conversation later are not added, and they do not
  read it in the console. This holds for a reply posted by the Slack app
  and for a message sent with a `slack` ref that names the thread.

A person's untagged message in an adopted conversation is answered by its
agent, as in any session. Before, the console ran only the agents a message
tagged.

### What it leaves

| Past work | Why |
|-----------|-----|
| A conversation that names no agent, and that no agent answered | A session belongs to one agent |
| A conversation that selects several agents, or whose default and selected agent differ | A session has one agent |
| A conversation whose agent is gone, or may not answer in its project | Not everyone who reads the conversation reads that agent, or nobody does |
| A Slack thread whose agent lives in a project the Slack client's machine does not read | The machine reads the threads on its surface, and a session is read only by those who read its agent. The machine reads an agent of the workspace, and an agent of a project open to everyone or to the machine itself |
| A Slack thread whose client is not in the store, and a thread on any other surface | Its address cannot be worked out |
| A conversation nobody started, a read-only one, one that answers an announcement, and one that continues another conversation or an import | A session is none of these |
| A conversation from before threads, kept under its first run's id | A session's id is its own |
| A conversation that someone changed, archived or deleted while the command read it | Who reads it may have changed. The command removes the session record it wrote for it, and the next run decides it again. If a run was bound to that record in the meantime, the record is kept, the line says so, and the next run decides the conversation as the agent that record names |
| A second conversation of one Slack thread, one per Slack app of the team, for the same agent | One place is one session. The oldest is adopted, and the others keep reaching their app as conversations |
| An archived conversation | It is not listed and not changed. A session record that a stopped apply wrote for it is removed, and listed as `unfinished adoption removed` |
| A GitHub run that used its asker's private context | Filed with other people who asked there, they would read it |
| A GitHub run whose place's session was deleted | Nothing is filed in a deleted session, and it is not dated |
| A GitHub run that no mention on a pull request or issue started (an automation, the automatic review) | New runs of these kinds are not filed in sessions either |
| A GitHub run filed in another project than its place's session | A run is not moved between projects |
| A GitHub run of a second GitHub client on the same repository, when the first client's session of that place has the same owner | The first client holds that session. A mention there now is left its own conversation too |

The command lists each of these with its reason.

### Running it

There is no undo. Restoring the backup also loses everything the servers
wrote after it: messages, runs and sessions.

The people map comes from the GitHub client. A client the environment
configures (`default-github`, from `HUMAGEN_GITHUB_PEOPLE`) is never stored:
the command builds it from its own environment, so run it where the server's
environment is, such as inside the server's pod. A client made in the
console or by a spec is read from the database. Without the map, the
sessions get no people, and past runs that every member read until now are
read by administrators alone. Do the steps in this order:

1. Deploy the release that has the command, with `HUMAGEN_GITHUB_PEOPLE`
   set to each GitHub login and the person it is, as
   `login=subject,login=subject`.
2. Confirm that the GitHub client has the map: read it with
   `GET /api/clients/<id>` and check that `github.people` lists each login.
3. Back up the database.
4. Run the dry run in the server's environment. It writes nothing:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate sessions -state postgres
   ```

5. Read the output.
   - `store:` and `tenants:` must name the production database and
     workspace, and each tenant must read more than 0 runs.
   - `environment clients:` must name the server's GitHub client in the
     server's tenant, such as `default-github in tenant default`. Set
     `-server-tenant` when the server runs with another `-tenant`.
   - There must be no `WARNING` line. A warning names a GitHub client with
     no people map, or one that is gone or turned off. Fix it and go back to
     step 2.
   - Each line is one change, or one thing left and why. The counts at the
     end of each tenant say how many of each kind, and the last line says how
     many changes an apply would write.
   - Each `conversation adopted` and `slack conversation adopted` line says
     the agent, where the conversation is filed and who reads it. Check a
     few against the console before you apply.
6. Apply it:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate sessions -state postgres -apply
   ```

7. Run the dry run again. It must end with `would write 0 changes`. An
   adopted conversation is now listed as `conversation already a session`.
8. Open the console as a person who asked on one of the pull requests. Find
   the session in Recent and in Sessions. Open an adopted conversation by
   its old link, as one of its participants and as someone who is not.

It is safe to run again. A second apply writes nothing. An apply that
stopped part of the way is finished by the next one: a conversation whose
session record was written and whose own record was not is still a
conversation, and the next apply adopts it as the same agent's.

The servers can keep running. The command writes each record at the version
it read, as a server does, and a second run finds what the first one did.

### Dating sessions an earlier run opened

A release before this fix dated each session the command opened at the
time the command ran. Run the same command again to date those sessions
from their runs. There is no extra flag.

1. Run the dry run. Each session to date has a `github session dated from
   its runs` line with its new times and its old ones.
2. Apply it with `-apply`.
3. Run the dry run again. It must end with `would write 0 changes`.

A session whose runs carry no time has a `github session not dated` line
and is left with the times it has. So does a session whose first run never
reached an ending, such as one that stopped while it ran, unless that run
is the mention that opened the session: its line names the run.

## Journaling each request as it was asked

From this release, which runs saige v0.52.1, the thread journal records
each turn's request as it was asked: the trigger's context, then the
request's text, with its attachments. Each turn retrieves its own passages
from its knowledge sources for its own request, and they stay on that
turn's record. A turn from before
this release journaled its request together with the passages it retrieved,
and later turns in the same thread are shown that text as the conversation
so far. `humagen migrate journal` rewrites each journaled request to the
request as it was asked, once.

Apply it only once every server and worker runs this release. A process
on an earlier release still journals each request with its passages, and
an entry it journals after the command has read its thread stays as it was
written.

```
humagen migrate journal -state postgres [-tenant NAME] [-unmatched leave|replace] [-apply]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-state` | `postgres` when `$HUMAGEN_POSTGRES_URL` is set, else `memory` | The stores to read and write. The command refuses `memory`, and a database that holds no runs. It writes through the stores, never with SQL |
| `-tenant` | every tenant the run index holds | One tenant only |
| `-unmatched` | `leave` | What to do with a request that is not matched to its run: `leave` lists it and leaves it, `replace` sets it to `[the request is not kept]` |
| `-apply` | off | Write the changes. Without it the command writes nothing and prints what it would do |
| `-allow-empty-store` | off | For tests only: run against the in-memory store or an empty database |

### What it changes

| Journal entry | What the command does |
|---------------|-----------------------|
| A request journaled with the passages its turn retrieved | Sets it to the request as it was asked, read from the run's input in the run ledger. Every record of the entry (its place in the thread, its index by run, and its index by surface message when it has one) is written together, each at the version read |
| A request already as it was asked | Leaves it. When the ledger no longer holds its run, or cannot read the run's input, the request as it was asked is read from the trigger the run index holds for the run |
| A request whose run the ledger does not hold, or whose text is neither of these | Lists it as `request not matched` with the reason, and leaves it. With `-unmatched replace` it sets it to `[the request is not kept]`, which keeps the entry's place in the thread and its attachments, when its text may hold passages. A text with no blank line holds none: it is listed with `left: its text cannot hold passages` and left |
| An answer, and a message that no run journaled | Leaves it |
| Every entry in a thread that holds a run the ledger has not ended, such as one that is running or waiting on an answer | Leaves it, and lists the thread once as `thread left: a run has not ended` with that run. With `-apply` the command reads the runs that have not ended again just before it writes each entry, so a thread where a turn started while the command ran is left from that entry on: its line says `found when checked again before writing`, and entries of the thread written before it stay written. Run the command again after the run has ended |
| Every entry of a tenant when a run the ledger has not ended cannot be read, so its thread cannot be told | Leaves the tenant the run index holds that run in, or every tenant when it holds it in none, and lists it as `tenant left: a run cannot be read` with that run. Run the command again once the run has ended |
| An entry that changed while the command read it | Leaves it, and lists it as `entry changed while read`. The next run decides it again |

The output names the store, what is done with an unmatched request and the
tenants it read. For each tenant it says how many requests it read, then one
line for each entry it changes or leaves and each thread it leaves, by
record key and run id, then the counts of each kind. It never prints the
text of an entry. The last line says how many changes it wrote, or would
write.

### Running it

1. Upgrade every server and worker to this release.
2. Back up the database.
3. Run the dry run. It writes nothing:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate journal -state postgres
   ```

4. Read the output. `store:` and `tenants:` must name the production
   database and workspaces. Check the `request not matched` lines and decide
   whether to leave those requests or replace them.
5. Apply it:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate journal -state postgres -apply
   ```

   Add `-unmatched replace` to replace the requests that are not matched.
6. Run the dry run again. It must end with `would write 0 changes`, unless a
   thread or a tenant was left because a run had not ended: run the apply
   again once that run has ended.

It is safe to run again. A second apply writes nothing, and an apply that
stopped part of the way is finished by the next one. The servers can keep
running, once every one runs this release: the command reads one tenant at
a time, a page at a time, and writes each entry at the version it read. It
reads the runs that have not ended once for every tenant, and again just
before it writes each entry, and leaves the rest of a thread once it finds
a turn there. A turn that starts in a thread between a check and its write
can still be shown that thread partly rewritten; to rule it out, apply the
command while no turns start.
