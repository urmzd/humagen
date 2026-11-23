# Upgrading

Steps an operator runs once, after a release that needs them.

## Filing past work into sessions

A mention on a GitHub pull request or issue opens or continues that place's
session ([GitHub sessions](../reference/github.md#sessions)). A run from
before that release is a conversation of its own, and Recent and Sessions do
not list it. `humagen migrate sessions` files that work into sessions once.

```
humagen migrate sessions -state postgres [-tenant NAME] [-apply]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-state` | `postgres` when `$HUMAGEN_POSTGRES_URL` is set, else `memory` | The stores to read and write. The command refuses `memory`, and a database that holds no runs: both have nothing to migrate. It writes through the stores, never with SQL |
| `-tenant` | every tenant the run index holds | One tenant only |
| `-apply` | off | Write the changes. Without it the command writes nothing and prints what it would do |
| `-allow-empty-store` | off | For tests only: run against the in-memory store or an empty database |

The output starts with the store and the tenants it read, then how many runs
of each tenant it read. Check them before you go on.

### What it changes

| Past work | What the command does |
|-----------|-----------------------|
| A GitHub run that a mention on a pull request or issue started | Files it in that place's session, the one a mention there opens now: the same key, `github://<owner>/<repo>/pull/<n>` (or `issues/<n>`) and the agent that answered first. It opens the session when there is none, oldest run first |
| The runs in that run's conversation | Moves them into the session's conversation, so the session lists them as its turns and the old conversation is no longer listed |
| A run of the session's owner | Binds it to the session's record, in the order the runs were asked and ahead of any newer run bound already, so the session's latest run stays its newest. A run the runtime bound when it ran stays bound once |
| The conversation's title in the run index | The pull request's or issue's title, from the run's trigger |
| The person who asked | Adds the person the GitHub client maps the login to now (`github.people`). A login mapped to nobody adds nobody. The session is private to these people, as a new one is, and gets no Slack audience record |

### What it leaves

| Past work | Why |
|-----------|-----|
| A conversation from before sessions (a `th-` thread) | A session's id is made from its key (`s_...`). The conversation's messages, the runs filed in it, its surface's ids and every link to it are kept under its own id. It cannot become a session without a new id, which would break them |
| A Slack thread or direct message from before sessions | The same reason. Replies on Slack keep reaching the conversation |
| A GitHub run that used its asker's private context | Filed with other people who asked there, they would read it |
| A GitHub run that no mention on a pull request or issue started (an automation, the automatic review) | New runs of these kinds are not filed in sessions either |
| A GitHub run filed in another project than its place's session | A run is not moved between projects |
| A GitHub run of a second GitHub client on the same repository, when the first client's session of that place has the same owner | The first client holds that session. A mention there now is left its own conversation too |

The command lists each of these with its reason.

### Running it

There is no undo. Restoring the backup also loses everything the servers
wrote after it: messages, runs and sessions.

The people map comes from the GitHub client's record in the database. A
server writes it there when it starts with `HUMAGEN_GITHUB_PEOPLE` (or
`github.people` in the spec). Without the map, the sessions get no people,
and past runs that every member read until now are read by administrators
alone. Do the steps in this order:

1. Deploy the release that has the command, with `HUMAGEN_GITHUB_PEOPLE`
   set to each GitHub login and the person it is, as
   `login=subject,login=subject`.
2. Confirm that the GitHub client's record has the map: read it with
   `GET /api/clients/<id>` and check that `github.people` lists each login.
3. Back up the database.
4. Run the dry run. It writes nothing:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate sessions -state postgres
   ```

5. Read the output.
   - `store:` and `tenants:` must name the production database and
     workspace, and each tenant must read more than 0 runs.
   - There must be no `WARNING` line. A warning names a GitHub client with
     no people map, or one that is gone or turned off. Fix it and go back to
     step 2.
   - Each line is one change, or one thing left and why. The counts at the
     end of each tenant say how many of each kind, and the last line says how
     many changes an apply would write.
6. Apply it:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate sessions -state postgres -apply
   ```

7. Run the dry run again. It must end with `would write 0 changes`.
8. Open the console as a person who asked on one of the pull requests. Find
   the session in Recent and in Sessions.

The servers can keep running. The command writes each record at the version
it read, as a server does, and a second run finds what the first one did.
