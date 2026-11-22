# Upgrading

Steps an operator runs once, after a release that needs them.

## Filing past work into sessions

A mention on a GitHub pull request or issue opens or continues that place's
session ([GitHub sessions](../reference/github.md#sessions)). A run from
before that release is a conversation of its own, and Recent and Sessions do
not list it. `humagen migrate sessions` files that work into sessions once.

```
humagen migrate sessions [-state postgres] [-tenant NAME] [-apply]
```

| Flag | Default | Meaning |
|------|---------|---------|
| `-state` | `postgres` when `$HUMAGEN_POSTGRES_URL` is set, else `memory` | The stores to read and write. The command writes through the stores, never with SQL |
| `-tenant` | every tenant the run index holds | One tenant only |
| `-apply` | off | Write the changes. Without it the command writes nothing and prints what it would do |

### What it changes

| Past work | What the command does |
|-----------|-----------------------|
| A GitHub run that a mention on a pull request or issue started | Files it in that place's session, the one a mention there opens now: the same key, `github://<owner>/<repo>/pull/<n>` (or `issues/<n>`) and the agent that answered first. It opens the session when there is none, oldest run first |
| The runs in that run's conversation | Moves them into the session's conversation, so the session lists them as its turns and the old conversation is no longer listed |
| A run of the session's owner | Binds it to the session's record, in the order the runs were asked. A run the runtime bound when it ran stays bound once |
| The person who asked | Adds the person the GitHub client maps the login to now (`github.people`). A login mapped to nobody adds nobody. The session is private to these people, as a new one is, and gets no Slack audience record |

### What it leaves

| Past work | Why |
|-----------|-----|
| A conversation from before sessions (a `th-` thread) | A session's id is made from its key (`s_...`). The conversation's messages, the runs filed in it, its surface's ids and every link to it are kept under its own id. It cannot become a session without a new id, which would break them |
| A Slack thread or direct message from before sessions | The same reason. Replies on Slack keep reaching the conversation |
| A GitHub run that used its asker's private context | Filed with other people who asked there, they would read it |
| A GitHub run that no mention on a pull request or issue started (an automation, the automatic review) | New runs of these kinds are not filed in sessions either |
| A GitHub run filed in another project than its place's session | A run is not moved between projects |

The command lists each of these with its reason.

### Running it

Run it from a machine or pod that reaches the database, with the release
that has the command.

1. Back up the database.
2. Run the dry run. It writes nothing:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate sessions -state postgres
   ```

3. Read the output. Each line is one change, or one thing left and why. The
   counts at the end of each tenant say how many of each kind. The last line
   says how many changes an apply would write.
4. Apply it:

   ```sh
   HUMAGEN_POSTGRES_URL=... humagen migrate sessions -state postgres -apply
   ```

5. Run the dry run again. It must end with `would write 0 changes`.
6. Open the console as a person who asked on one of the pull requests. Find
   the session in Recent and in Sessions.

The servers can keep running. The command writes each record at the version
it read, as a server does, and a second run finds what the first one did.
