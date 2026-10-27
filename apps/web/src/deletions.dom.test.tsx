import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { Deletion } from "./api.ts";
import { DeletionQueue } from "./DeletionQueue.tsx";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch>;
afterEach(() => { cleanup(); net.restore(); });

const row = (id: string, due: boolean): Deletion => ({
  id, title: `Draft ${id}`, project: "Launch", archived_by: "Ada",
  archived_at: "2026-10-01T12:00:00Z", delete_after: "2026-10-31T12:00:00Z",
  due, requested: due, messages: 2,
});

test("the deletion queue waits for explicit approval and cannot approve an item before it is due", async () => {
  net = stubFetch({ "GET /api/deletions": () => ({ deletions: [row("due", true), row("waiting", false)] }) });
  render(<DeletionQueue />);
  const waiting = (await screen.findByText("Draft waiting")).closest("li");
  assert.ok(waiting);
  assert.equal((within(waiting).getByRole("button", { name: "Approve Deletion" }) as HTMLButtonElement).disabled, true);
  assert.ok(within(waiting).getByRole("button", { name: "Restore" }));
  assert.equal(net.seen.filter(request => request.method === "POST").length, 0);
  assert.ok(!waiting.textContent?.includes("·"));
});

test("approval refills the review batch and restoration cancels the next deletion", async () => {
  let current = [row("first", true)];
  net = stubFetch({
    "GET /api/deletions": () => ({ deletions: current }),
    "POST /api/deletions/first/approve": () => { current = [row("next", false)]; },
    "POST /api/deletions/next/reject": () => { current = []; },
  });
  render(<DeletionQueue />);
  const first = (await screen.findByText("Draft first")).closest("li");
  assert.ok(first);
  fireEvent.click(within(first).getByRole("button", { name: "Approve Deletion" }));
  const next = (await screen.findByText("Draft next")).closest("li");
  assert.ok(next);
  assert.ok(!screen.queryByText("Draft first"));
  fireEvent.click(within(next).getByRole("button", { name: "Restore" }));
  await screen.findByText("Nothing is archived.");
  assert.deepEqual(net.seen.filter(request => request.method === "POST").map(request => request.path), ["/api/deletions/first/approve", "/api/deletions/next/reject"]);
});

test("a refused deletion keeps its row and reports the failure", async () => {
  net = stubFetch({ "GET /api/deletions": () => ({ deletions: [row("first", true)] }) });
  render(<DeletionQueue />);
  await screen.findByText("Draft first");
  fireEvent.click(screen.getByRole("button", { name: "Approve Deletion" }));
  await screen.findByText("Could not delete. Try again.");
  assert.ok(screen.getByText("Draft first"));
});
