import type { Task, Turn } from "@humagen/core";

/**
 * Turns a conversation transcript draws, written as the adapter folds them
 * from a run's events: a repository check that failed in two steps and in
 * the agent it delegated to, an answer that names pull requests, a group of
 * steps with a command and a connector call, and a command still running.
 * The transcript gallery (`transcript-gallery.html`) and its DOM tests read
 * them; nothing here calls the platform.
 */

const agent = { name: "humagen" };
const json = (v: unknown) => JSON.stringify(v, null, 2);

/** Two reads that worked and a source check that failed twice. */
const checks: Task[] = [
  {
    id: "read-1",
    title: "Read file",
    tool: "file_read",
    status: "complete",
    input: json({ path: "workspace/repos.md" }),
    output: "urmzd/humagen\nurmzd/saige\n",
    durationMs: 180,
    at: 0,
  },
  {
    id: "read-2",
    title: "Read file",
    tool: "file_read",
    status: "complete",
    input: json({ path: "workspace/notes/status.md" }),
    output: "Last checked: November 18.",
    durationMs: 140,
    at: 0,
  },
  {
    id: "source-1",
    title: "Check source",
    tool: "github_workflow_status",
    status: "error",
    input: json({ owner: "urmzd", repo: "humagen", ref: "main" }),
    output:
      "github_workflow_status: no GitHub connection can read urmzd/humagen\nThe workspace has no GitHub App installation for the owner urmzd, so the request was refused before it was sent.\nRequest: GET /repos/urmzd/humagen/actions/runs?branch=main",
    durationMs: 2_300,
    at: 0,
  },
  {
    id: "source-2",
    title: "Check source",
    tool: "github_workflow_status",
    status: "error",
    input: json({ owner: "urmzd", repo: "saige", ref: "main" }),
    output:
      "github_workflow_status: no GitHub connection can read urmzd/saige\nThe workspace has no GitHub App installation for the owner urmzd.",
    durationMs: 1_900,
    at: 0,
  },
];

const intro = "I'm bringing in @github-reviewer to check the current status of your repositories.";

/** The delegated run that failed, with what it was asked and why it stopped. */
const reviewer: Task = {
  id: "call-reviewer",
  title: "github-reviewer",
  tool: "call_agent",
  status: "error",
  input: json({
    agent: "github-reviewer",
    title: "Check repository status",
    brief: "Check the CI status and open pull requests of urmzd/humagen and urmzd/saige.",
  }),
  output:
    "github_read_context failed: GitHub is not connected for the person this run acts for\nThe run tried github_read_context three times and every attempt was refused, so it stopped.",
  durationMs: 201_000,
  subagent: {
    name: "github-reviewer",
    runId: "r-reviewer-1",
    durationMs: 201_000,
    hue: 48,
  },
  at: intro.length,
};

const after: Task[] = [
  {
    id: "read-3",
    title: "Read file",
    tool: "file_read",
    status: "complete",
    input: json({ path: "workspace/repos.md" }),
    output: "urmzd/humagen\nurmzd/saige\n",
    durationMs: 120,
    at: intro.length,
  },
  {
    id: "cmd-1",
    title: "Run command",
    tool: "bash",
    status: "complete",
    input: json({ command: "gh auth status 2>&1 | head -3" }),
    output: "You are not logged into any GitHub hosts. To log in, run: gh auth login",
    durationMs: 820,
    at: intro.length,
  },
];

const ending =
  "\n\nWhich repositories would you like me to check? If you haven't already, please ensure your GitHub account is connected so I can access the current status and workflow details.";

/** Image 2 of the review: two failed checks, a failed delegated run, then the answer. */
export function failedTurn(): Turn {
  return {
    id: "turn-failed",
    agent,
    status: "done",
    text: `${intro}${ending}`,
    tasks: [...checks, reviewer, ...after],
    citations: [],
    interrupts: [],
    notices: [],
    durationMs: 220_000,
  };
}

/** The same turn, when the platform recorded which connection the run was missing. */
export function connectTurn(): Turn {
  return {
    ...failedTurn(),
    id: "turn-connect",
    needsConnection: [{ provider: "github", name: "GitHub" }],
  };
}

/** An answer that names pull requests, an issue and a commit, by reference and by address. */
export function referencesTurn(): Turn {
  const text = [
    'CI failures on urmzd/humagen#461: Lint and "API contract and client". Pulling the logs.',
    "",
    "Two failures on https://github.com/urmzd/humagen/pull/461 block every PR stacked above it: a lint complaint in `store.go` and stale generated copy that needs `pnpm welcome-copy`. The flake is tracked in https://github.com/urmzd/humagen/issues/402, and the fix landed in https://github.com/urmzd/humagen/commit/9f2c4e1a7b3d5c6e8f0a1b2c3d4e5f6a7b8c9d0e.",
    "",
    "I'll fix both on urmzd/humagen#461's branch, then merge up the stack.",
  ].join("\n");
  return {
    id: "turn-refs",
    agent,
    status: "done",
    text,
    tasks: [],
    citations: [],
    interrupts: [],
    notices: [],
    durationMs: 41_000,
  };
}

const toolsIntro = "Pulling the failing job's logs before I touch the branch.";

/** A group of steps: tools loaded, a command run, a pull request read. */
export function toolsTurn(): Turn {
  return {
    id: "turn-tools",
    agent,
    status: "done",
    text: `${toolsIntro}\n\nThe regenerated copy keys are the only drift. Committing and pushing the fix next.`,
    tasks: [
      {
        id: "load-1",
        title: "Loaded tools",
        tool: "tool_search",
        status: "complete",
        input: json({
          query:
            "select:mcp__github__pull_request_read,mcp__github__actions_list,mcp__github__get_job_logs",
          max_results: 3,
        }),
        output: "Loaded 3 tools.",
        durationMs: 640,
        at: toolsIntro.length,
      },
      {
        id: "cmd-2",
        title: "Run command",
        tool: "bash",
        status: "complete",
        input: json({
          command:
            "cd /home/user/humagen && git status --short && git log --oneline -3 origin/feat/agent-bot",
        }),
        output:
          " M packages/components/src/copy.ts\n3b21ba2 feat(runtime): journal a request's files",
        durationMs: 1_240,
        at: toolsIntro.length,
      },
      {
        id: "gh-1",
        title: "Get details for a single pull request",
        tool: "github_pull_request_read",
        status: "complete",
        input: json({ owner: "urmzd", repo: "humagen", pullNumber: 461, method: "get" }),
        output: json({
          number: 461,
          state: "open",
          title: "feat(agents): choose how each agent answers",
        }),
        durationMs: 910,
        at: toolsIntro.length,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
    durationMs: 18_000,
  };
}

const runningIntro =
  "Before pushing, I'll run the same linter CI uses (golangci-lint v2.15.0) on the top branch, since it contains every PR's code.";

/** A command still running, started `elapsedMs` before `now`. */
export function runningTurn(now = Date.now(), elapsedMs = 112_000): Turn {
  return {
    id: "turn-running",
    agent,
    status: "working",
    text: runningIntro,
    tasks: [
      {
        id: "cmd-3",
        title: "Installing the CI linter version",
        tool: "bash",
        status: "in_progress",
        input: json({
          command:
            "cd /home/user/humagen; GOBIN=$HOME/.local/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 2>&1 | tail -2; $HOME/.local/bin/golangci-lint version 2>&1 | head -1",
        }),
        startedAt: new Date(now - elapsedMs).toISOString(),
        at: runningIntro.length,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
  };
}

const retriedIntro = "Reading the pull request before I review it.";

/**
 * A source read that was refused and then read at its full address: the
 * first attempt failed, the second worked, so the line says it was retried,
 * not that it failed. The production case behind it: `urmzd/humagen` was
 * refused, `github://urmzd/humagen/pull/541` read in 2.2 s.
 */
export function retriedTurn(): Turn {
  return {
    id: "turn-retried",
    agent: { name: "code-reviewer" },
    status: "done",
    text: `${retriedIntro}\n\nThe change is small and the tests cover it. One suggestion below.`,
    tasks: [
      {
        id: "context-1",
        title: "Read GitHub context",
        tool: "github_read_context",
        status: "error",
        input: json({ address: "urmzd/humagen" }),
        output: "connector: undeliverable: expected a github:// address or github.com issue/PR URL",
        durationMs: 40,
        at: retriedIntro.length,
      },
      {
        id: "context-2",
        title: "Read GitHub context",
        tool: "github_read_context",
        status: "complete",
        input: json({ address: "github://urmzd/humagen/pull/541" }),
        output: json({ subject: { number: 541, title: "Show a recovered failure quietly" } }),
        durationMs: 2_200,
        at: retriedIntro.length,
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
    durationMs: 14_000,
  };
}

export const transcriptScenes = {
  failed: { ask: "please check my repos; whats their status right now?", turn: failedTurn },
  connect: { ask: "please check my repos; whats their status right now?", turn: connectTurn },
  refs: { ask: "Why is the stack blocked?", turn: referencesTurn },
  tools: { ask: "Fix CI on the first PR.", turn: toolsTurn },
  running: { ask: "Lint the top branch before pushing.", turn: runningTurn },
  retried: { ask: "Review urmzd/humagen#541.", turn: retriedTurn },
} as const;

export type TranscriptScene = keyof typeof transcriptScenes;
