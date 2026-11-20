import type { Task, Turn } from "../types";

/** A finished call to a tool that writes a file, as the adapter records it. */
const wrote = (id: string, tool: string, args: unknown, at: number, output: string): Task => ({
  id,
  title: tool.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase()),
  tool,
  status: "complete",
  input: JSON.stringify(args, null, 2),
  output,
  at,
  durationMs: 600,
});

const changesText =
  "Empty refund reasons now default to `unspecified`, and migration `0042` is safe to run twice. The payments tests pass.";

const turn = {
  agent: { name: "Assistant" },
  text: changesText,
  citations: [],
  interrupts: [],
  notices: [],
  sessionUrl: "https://example.com/sessions/turn-changes",
} satisfies Partial<Turn>;

/**
 * A finished turn that changed three files: one edited twice, one written
 * whole, one created. Its summary names each file once, with its lines.
 */
export const changesTurn: Turn = {
  ...turn,
  id: "turn-changes",
  status: "done",
  durationMs: 52_000,
  tasks: [
    wrote(
      "ch1",
      "file_edit",
      {
        path: "services/payments-api/src/refund.ts",
        old_string: "  const reason = input.reason;\n",
        new_string:
          '  const reason = input.reason?.trim() || "unspecified";\n  audit.record("refund.reason", reason);\n',
      },
      0,
      "Edited services/payments-api/src/refund.ts.",
    ),
    wrote(
      "ch2",
      "file_edit",
      {
        path: "services/payments-api/src/refund.ts",
        old_string: "export function refund(input: RefundInput) {\n",
        new_string:
          "/** Refunds a charge. An empty reason is recorded as unspecified. */\nexport function refund(input: RefundInput) {\n",
      },
      0,
      "Edited services/payments-api/src/refund.ts.",
    ),
    wrote(
      "ch3",
      "file_write",
      {
        path: "services/payments-api/migrations/0042_refund_reason.sql",
        content:
          "ALTER TABLE refunds\n  ADD COLUMN IF NOT EXISTS reason text NOT NULL DEFAULT 'unspecified';\n",
      },
      0,
      "Wrote services/payments-api/migrations/0042_refund_reason.sql (86 bytes).",
    ),
    wrote(
      "ch4",
      "file_create",
      {
        path: "services/payments-api/src/refund.test.ts",
        content:
          'import { expect, it } from "vitest";\nimport { refund } from "./refund";\n\nit("defaults an empty reason", () => {\n  expect(refund({ charge: "ch_1", reason: " " }).reason).toBe("unspecified");\n});\n',
      },
      0,
      "Created services/payments-api/src/refund.test.ts.",
    ),
  ],
};

/** A finished turn that changed more files than its summary names. */
export const manyChangesTurn: Turn = {
  ...turn,
  id: "turn-many-changes",
  status: "done",
  durationMs: 96_000,
  text: "Every handler now reads its timeout from the shared config.",
  tasks: Array.from({ length: 11 }, (_, i) =>
    wrote(
      `mc${i}`,
      "file_edit",
      {
        path: `services/gateway/src/handlers/${["auth", "billing", "catalog", "checkout", "health", "inventory", "orders", "payments", "search", "shipping", "users"][i]}.ts`,
        old_string: "const timeoutMs = 5000;\n",
        new_string:
          i % 3 === 0
            ? 'import { config } from "../config";\n\nconst timeoutMs = config.timeoutMs;\n'
            : "const timeoutMs = config.timeoutMs;\n",
      },
      0,
      "Edited.",
    ),
  ),
};
