import type { Task, Turn } from "../types";

/**
 * Turns that did real work with no plan: shell commands, reads, searches and
 * file writes, as the platform's own tools report them. A `shell` result is
 * the text `exit <code>` and then the end of the command's output
 * (`internal/platform/isolation.go`), whether the command passed or not.
 */

const step = (
  id: string,
  tool: string,
  args: unknown,
  at: number,
  over: Partial<Task> = {},
): Task => ({
  id,
  title: tool.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase()),
  tool,
  status: "complete",
  input: JSON.stringify(args, null, 2),
  at,
  durationMs: 800,
  ...over,
});

const words = [
  "I'll find where refunds are rounded and run the tests.",
  "\n\nThe rounding is in `refund.ts`. One test fails before any change, so I fixed the rounding and the migration it depends on.",
  "\n\nRefunds now round half up, and the payments tests pass. The lint step still fails on an import that was unused before this change.",
].join("");

const cut = (n: number) => words.split("\n\n").slice(0, n).join("\n\n").length;

const TEST_FAIL = [
  "exit 1",
  "> payments-api@2.4.0 test",
  "> vitest run src/refund.test.ts",
  "",
  " FAIL  src/refund.test.ts > refund > rounds half up",
  "AssertionError: expected 10.12 to be 10.13",
  "  at src/refund.test.ts:42:27",
  "",
  " Test Files  1 failed (1)",
  "      Tests  1 failed | 6 passed (7)",
].join("\n");

const LINT_FAIL = [
  "exit 2",
  "src/ledger.ts:3:10 lint/correctness/noUnusedImports",
  "  This import is unused.",
  "Checked 48 files in 212ms. Found 1 error.",
].join("\n");

const workTasks: Task[] = [
  step("w1", "web_search", { query: "banker's rounding vs half up refunds" }, cut(1)),
  step("w2", "file_list", { path: "services/payments-api/src" }, cut(1)),
  step("w3", "file_read", { path: "services/payments-api/src/refund.ts" }, cut(1)),
  step("w4", "file_read", { path: "services/payments-api/src/refund.test.ts" }, cut(1)),
  step("w5", "shell", { command: "pnpm test src/refund.test.ts" }, cut(1), {
    output: TEST_FAIL,
    durationMs: 4200,
  }),
  step(
    "w6",
    "file_write",
    {
      path: "services/payments-api/src/refund.ts",
      content:
        "export const round = (n: number) =>\n  Math.round((n + Number.EPSILON) * 100) / 100;\n",
    },
    cut(2),
  ),
  step(
    "w7",
    "file_write",
    {
      path: "services/payments-api/migrations/0042_refund_reason.sql",
      content: "ALTER TABLE refunds\n  ADD COLUMN IF NOT EXISTS reason text;\n",
    },
    cut(2),
  ),
  step("w8", "shell", { command: "pnpm test src/refund.test.ts" }, cut(2), {
    output: "exit 0\n Test Files  1 passed (1)\n      Tests  7 passed (7)",
    durationMs: 3900,
  }),
  step("w9", "web_search", { query: "biome noUnusedImports autofix" }, cut(2)),
  step("w10", "shell", { command: "pnpm exec biome check src" }, cut(2), {
    output: LINT_FAIL,
    durationMs: 600,
  }),
];

const base: Turn = {
  id: "turn-work",
  agent: { name: "Assistant" },
  status: "done",
  text: words,
  tasks: workTasks,
  citations: [],
  interrupts: [],
  notices: [],
  durationMs: 52_000,
  sessionUrl: "https://example.com/sessions/turn-work",
};

/** A finished turn: ten calls of four kinds, two commands that exited non-zero. */
export const workTurn: Turn = base;

/** The same turn part way through: the first command is still running. */
export const workingTurn: Turn = {
  ...base,
  id: "turn-working",
  status: "working",
  durationMs: undefined,
  text: words.slice(0, cut(1)),
  tasks: workTasks
    .slice(0, 5)
    .map((t) =>
      t.id === "w5" ? { ...t, status: "in_progress", output: undefined, durationMs: undefined } : t,
    ),
};
