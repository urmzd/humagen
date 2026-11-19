# Run an agent on Claude Code or Codex

An agent can run its turns on a coding agent already installed on the host:
Claude Code (`claude`) or Codex (`codex`). The program is the model and the
tools at once. Humagen starts it in a directory you name, it reads, edits and
runs what it decides to there, and its answer comes back as the agent's, with
each tool it used shown as the turn's tool activity.

Use it to work in a repository from Humagen (edit, build, test, commit, open a
pull request) with the account the program is already signed in to, a Claude
or ChatGPT subscription included.

**The program runs on the host, as the server's user, outside the run's
[sandbox](../reference/spec.md#sandbox).** It can reach what that user can
reach: files, the network, signed-in command-line tools. That is what makes it
useful and why it is for your own machine, not a server other people share.
Nothing turns it on but a model of this kind in the spec.

The host is macOS or Linux. These kinds stop a program and what it started,
and hold their session state between processes, in ways only those systems
have; Humagen does not build them for Windows.

## Set it up

Sign the program in once, the way it documents (`claude` then `/login`, or
`codex login`). Then declare a model of its kind, and an agent on it:

```yaml
api_version: humagen/v1
kind: Platform
metadata:
  name: default
spec:
  models:
    - name: claude-code
      kind: claude-code
      claude-code:
        dir: /Users/me/github/my-repo
        allow: ["Bash(go test:*)", "Bash(git status:*)", "Bash(git diff:*)"]
    - name: codex
      kind: codex
      codex:
        dir: /Users/me/github/my-repo
  agents:
    - spec: {name: coder, provider: claude-code, model: default, system_prompt: You work in this repository. Run its tests before you answer.}
    - spec: {name: codexer, provider: codex, model: default, system_prompt: You work in this repository.}
```

`humagen start -f humagen.yaml`, then message `@coder`. The agent's `model` is
passed to the program as its model (`opus`, `sonnet`, a full model name);
`default` leaves the program's own choice. The agent's `effort` is passed
too, as `--effort` to Claude Code and as `model_reasoning_effort` to Codex.

| Field | Behavior |
| --- | --- |
| `dir` | Required. The absolute directory the program works in. One model works in one directory: declare another model for another repository |
| `permissions` | How much the program does without asking. Nobody is there to answer, so what it would ask for it does not do. `claude-code`: `acceptEdits` (the default: it edits files in `dir` and runs nothing), `default`, `plan` or `bypassPermissions`. `codex`: `workspace-write` (the default: it edits and runs commands inside its own sandbox, in `dir`), `read-only` or `danger-full-access` |
| `allow` | `claude-code` only. Tools it may use without asking, as its permission rules write them: `Bash(go test:*)`, `Bash(git:*)`, `WebFetch`. With `acceptEdits`, these are the commands it may run |
| `api_key` | `true` lets the program use an API key in the server's environment. By default `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN` (or `OPENAI_API_KEY` and `CODEX_API_KEY`) are withheld from it, because a key in the environment takes the place of the subscription sign-in |
| `command` | The program, when it is not `claude` or `codex` on `PATH` |
| `args` | Further arguments on every run |
| `timeout` | Ends a run that takes longer, as `"30m"`. Left out: two hours |
| `state_dir` | Where the session of each conversation is remembered. Left out: a directory under the user's cache directory |

## What carries over, and what does not

- **The program's own configuration applies.** It runs as you, in `dir`, so it
  reads what it reads in a terminal: `CLAUDE.md` and `AGENTS.md` files, nearest
  first, its skills and sub-agents, its MCP servers, its hooks and its
  permission settings. A rule its settings deny stays denied whatever `allow`
  says. What [agentspec](https://github.com/urmzd/agentspec) links into the
  program is there too.
- **Which settings file applies.** Claude Code's project settings (hooks,
  `deny` rules) are read from `dir` itself: `dir/.claude/settings.json`.
  A settings file in a directory above `dir` was not applied in our checks,
  though the `CLAUDE.md` files above `dir` were read: with `dir` set to a
  folder inside a project, a `deny` rule and a hook in the project's own
  `.claude/settings.json` did nothing. To make a settings file apply
  wherever `dir` is, name it: `args: ["--settings",
  "/Users/me/github/my-repo/.claude/settings.json"]`. Do not rely on a hook
  or a `deny` rule you have not seen act in a run.

- **One session a conversation.** The next message in a conversation continues
  the program's own session (`claude --resume`, `codex exec resume`), so it
  still knows the files it read and the commands it ran. A conversation it has
  no session for starts one and is told what was said so far.
- **A branch has its own session.** A message that follows an earlier turn,
  one that already has a reply, starts a branch, and a branch sees its own
  turns only. A session is continued only by a turn whose conversation so far
  begins with everything that session was sent and answered; the two programs
  can copy a whole session but not one up to an earlier turn, so a branch
  starts a new session, told what was said in that branch, and each branch
  then continues its own. The check reads the conversation so far that
  Humagen sends with each turn, so a session is also left behind, and a new
  one started and told what was said, when: its turn failed or was stopped;
  the workspace replays no history; the message itself holds lines that read
  like Humagen's "Conversation so far"; or two sessions fit equally. A
  conversation too long to be replayed whole (the workspace's history limit)
  continues no session either: what is left out cannot be compared, and two
  branches can end alike, so each turn from then on starts a session that is
  told the turns that were replayed, and the program no longer remembers the
  files it read in earlier turns. A session recorded by a release before this one has no history
  on record, so the next message of its conversation starts a new session
  once. A session's own `/compact` does not change which branch it belongs
  to: it goes on being continued. Processes that share a `state_dir` (a
  server and its workers on one host) hold it one at a time, so two branches
  asked at once never continue the same session; a session that cannot be
  taken out of the state is not continued.
- **The program's own commands.** In a conversation that is continuing, a
  message that is one line starting with `/` goes to Claude Code as typed, so
  `/compact`, `/context` and `/cost` act on that conversation's session and
  their output is the answer. A conversation's first message is always asked
  as a question.
- **Images and PDFs.** An image sent with a message is shown to the program:
  to Claude Code on its input, to Codex as a file kept only for the run.
  Claude Code also reads a PDF. Anything else (audio, video, another kind of
  document) is refused with an error that names it, and the program is not
  run. A session that is starting is shown the images of earlier messages
  too.
- **Carrying on from a session you already have.** Start a conversation with
  `/resume ID`, where ID is a session of the program's own (as
  `humagen import sessions` shows it after the colon in a document's `uri`,
  or as the program lists it). The conversation carries on from a copy of
  that session, so the original is left as it was. Say what to ask after the
  id (`/resume ID what was left to do?`); with nothing more, it is asked
  where it stands. Claude Code finds only sessions that began in `dir`. A
  Codex session that began outside `dir` is refused, and its copy runs under
  this model's `permissions`, not the ones it began with.
- **Tool activity.** Each tool the program runs is published as the run's tool
  call and its outcome, with the command or file it worked on and the end of
  its output.
- **Notices are not failures.** Codex reports some things as errors and then
  goes on to answer: that it left skill descriptions out of what the model
  sees, or that it is reconnecting. The run fails only when the program says
  the turn failed or ends without finishing it, so such a notice does not make
  Humagen run the turn again.
- **Usage.** Tokens are counted on the run. A subscription has no price per
  token, so the run is counted as unpriced.
- **Stopping.** Stopping the run stops the program and what it started.
- **A job that outlasts the run.** A command the program runs in the
  foreground ends with its run. For a job that should keep going, ask the
  program to start it detached and to leave its handle in `dir`:
  `nohup ./job.sh > job.log 2>&1 & echo $! > job.pid`. The run ends and the
  job goes on. A later message, in that conversation or a new one, reads the
  end of `job.log` and stops the job with the pid in `job.pid`, so the job is
  not started twice. Humagen keeps no record of such a job: it is not listed
  anywhere, its log is not shown in the console as it grows, and it ends with
  the host. It needs `Bash` in `allow`.
- **Library skills and their supporting files.** A message that names a
  published [library skill](../architecture/managed-library.md) (`$name`)
  gives the program the skill's instructions, as it gives any agent. The
  program is offered no Humagen tool, so it cannot read the skill's
  supporting files with `file_read`. For a turn on these kinds Humagen writes
  the supporting files of the revision the run is pinned to into a directory
  of that turn's own, under `humagen-skill-files-<uid>` in the host's
  temporary directory, outside `dir`, and what the program is asked names
  each file by its absolute path there. Claude Code is given that directory
  with `--add-dir`; Codex reads outside its workspace in every sandbox and
  needs nothing. Which directory Claude Code is given is never read from the
  message: it is the one placed for the turn's own run, in its own workspace,
  so a path typed into a message, another run's included, adds no directory.
  An agent whose models are a native one and a coding agent, either as the
  fallback of the other, has the files placed too, and is told both ways:
  the native model reads them with `file_read`, the program at the paths.
  The files are the pinned revision's exact bytes, binary
  files included, read only and without an execute bit: a later draft does
  not change them. Only the skills the run was admitted with are written,
  within the library's limits for each (64 files, 1 MiB a supporting file,
  2 MiB in all), so a large schema or source file is there whole, though a
  native model's `file_read` refuses one over 128 KiB. The directory is
  removed when the turn returns, whether it answered, failed or was stopped,
  so each turn is told a new path; one left by a process that was killed is
  removed after seven days. A turn whose files cannot be written fails and
  says so, and is not run without them. Claude Code's `--add-dir` also lets
  it write there under `acceptEdits`: the files are a copy for one turn, so
  nothing it does there reaches the library. As everywhere on these kinds,
  a program that runs commands (`allow` with `Bash`, `bypassPermissions`, or
  Codex) can read whatever the server's user can.
- **The agent's own tools do not reach the program.** Tools granted to the
  agent in Humagen (file tools, `mcp:` servers, calling another agent) are not
  offered to it. It uses the tools and MCP servers of its own configuration.
  Approvals in Humagen do not gate its tools: `permissions` and `allow` do.
- **A structured answer is not enforced.** An agent with an output schema
  cannot run on these kinds.
- **A conversation's first follow-up.** Humagen does not tell a model provider
  which conversation a request is in, so this kind reads the thread from the
  turn's context. A conversation started with `POST /api/runs` names no thread
  on its first turn; its second turn is matched to the first by what was asked
  and answered, in full. If two conversations in the same directory began with
  the same words and the same answer, the second turn starts a new session,
  told what was said, instead of guessing.
