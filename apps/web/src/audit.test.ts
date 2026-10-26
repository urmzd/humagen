import assert from "node:assert/strict";
import { test } from "node:test";
import { auditCSV } from "./audit.ts";
test("audit CSV preserves escaped snapshots and makes spreadsheet formulas inert", () => {
  const csv = auditCSV(
    [
      {
        id: "opaque",
        at: "2026-10-26T12:00:00Z",
        actor: '  =IMPORTDATA("private")',
        action: "memory.forget",
        resource: "memory",
        resource_id: "id",
        scope: "personal",
        outcome: "succeeded",
        before: { name: "A, B" },
      },
    ],
    ["ID"],
  );
  assert.ok(csv.includes('"\'  =IMPORTDATA(""private"")"'));
  assert.ok(csv.includes('"{""name"":""A, B""}"'));
  assert.ok(csv.endsWith("\r\n"));
});
