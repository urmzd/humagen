# Upgrading

Steps an operator runs once, after a release that needs them.

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
| A Slack thread whose agent lives in a project | The Slack client's machine reads its threads with no role in a project, so it does not read that agent |
| A Slack thread whose client is not in the store, and a thread on any other surface | Its address cannot be worked out |
| A conversation nobody started, a read-only one, one that answers an announcement, and one that continues another conversation or an import | A session is none of these |
| A conversation from before threads, kept under its first run's id | A session's id is its own |
| A conversation that someone changed while the command read it | Who reads it may have changed. The command removes the session record it wrote for it, and the next run decides it again |
| An archived conversation | It is not listed and not changed |
| A GitHub run that used its asker's private context | Filed with other people who asked there, they would read it |
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
