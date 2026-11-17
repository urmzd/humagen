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

- **One session a conversation.** The next message in a conversation continues
  the program's own session (`claude --resume`, `codex exec resume`), so it
  still knows the files it read and the commands it ran. A conversation it has
  no session for starts one and is told what was said so far.
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
- **Tool activity.** Each tool the program runs is published as the run's tool
  call and its outcome, with the command or file it worked on and the end of
  its output.
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
  and answered. If two conversations in the same directory began with the same
  words and the same answer, the second turn starts a new session, told what
  was said, instead of guessing.
