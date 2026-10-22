import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import type { HomeNotification, HomeUpdate } from "@humagen/core";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { HomeBoxes } from "./HomePage.tsx";

// The loader runs every DOM file in one process, so this cleans up only after its own tests.
let rendered = false;
afterEach(() => {
  if (!rendered) return;
  cleanup();
  rendered = false;
});

const failed: HomeNotification = { kind: "inbox", id: "m-5", from: "any-run-fails", summary: "triage failed", at: "2026-10-21T11:00:00Z" };
const posted: HomeUpdate = {
  id: "m-4",
  project: "Launch",
  projectTitle: "Launch",
  subject: "Launch moves to November 4",
  from: { kind: "person", name: "ana" },
  at: "2026-10-21T10:00:00Z",
  unread: true,
};

test("Home's rows open what they name, and each box's text control goes to the rest in the inbox", () => {
  const did: string[] = [];
  rendered = true;
  render(
    <HomeBoxes
      notifications={{ state: "ready", value: { items: [failed], inbox: { total: 3 } } }}
      updates={{ state: "ready", value: [posted] }}
      onOpen={(n) => did.push(`open ${n.id}`)}
      onInbox={() => did.push("inbox")}
      onUpdate={(u) => did.push(`update ${u.id}`)}
      onUpdates={() => did.push("updates")}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: /triage failed/ }));
  fireEvent.click(screen.getByRole("button", { name: /Launch moves to November 4/ }));
  fireEvent.click(screen.getByRole("button", { name: "Open Inbox" }));
  fireEvent.click(screen.getByRole("button", { name: "See All" }));
  assert.deepEqual(did, ["open m-5", "update m-4", "inbox", "updates"]);
  // Each box is a region named by its heading.
  assert.ok(screen.getByRole("region", { name: /Needs you/ }));
  assert.ok(screen.getByRole("region", { name: "What's new" }));
});
