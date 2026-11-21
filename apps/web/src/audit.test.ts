import assert from "node:assert/strict";
import { test } from "node:test";
import { auditCSV, changesBeside } from "./audit.ts";

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

test("a break-glass read the reads section lists is left out of the changes, and no other row is", () => {
  const read = {
    id: "read-1",
    at: "2026-10-22T10:00:00.123456789Z",
    actor: "governor",
    action: "project.break_glass_read",
    resource: "project",
    resource_id: "Secret",
    scope: "project",
    outcome: "succeeded",
    project: "Secret",
    thread: "private-1",
    route: "GET /api/threads/{id}",
  } as const;
  const earlier = { ...read, id: "read-0", at: "2026-10-20T10:00:00Z" };
  const change = { ...read, id: "audit-1", action: "project.members" };
  const listed = [
    {
      subject: "governor",
      project: "Secret",
      thread: "private-1",
      route: "GET /api/threads/{id}",
      at: "2026-10-22T10:00:00.123456789Z",
    },
  ];
  assert.deepEqual(
    changesBeside([read, earlier, change], listed).map((row) => row.id),
    ["read-0", "audit-1"],
  );
  assert.deepEqual(
    changesBeside([read, change], []).map((row) => row.id),
    ["read-1", "audit-1"],
  );
});
