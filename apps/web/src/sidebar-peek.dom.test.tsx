import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Me, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import { HOLD_MS, PEEK_MS } from "./RowPreview.tsx";
import { Sidebar } from "./Sidebar.tsx";

// DES-055: a sidebar row previews itself on hover, on a long press, or under
// keyboard focus, and the preview says the row's status in words (DES-054).

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const me: Me = {
  principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
  permissions: ["runs:read", "runs:write"],
};

const row: ThreadSummary = {
  id: "t1",
  title: "Vendor contract",
  named: true,
  origin: "api",
  project: "Launch",
  channel: "general",
  agents: ["lead"],
  people: ["ada"],
  visibility: "shared",
  messages: 1,
  turns: 1,
  branches: 0,
  active: 0,
  waiting: 0,
  failed: 0,
  last: "Clause 4 caps liability at the fee.",
  last_by: "lead",
  last_agent: true,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-21T09:05:00Z",
};

function Bar({ go = () => {} }: { go?: () => void }) {
  const noop = () => {};
  return (
    <Sidebar
      open
      onToggle={noop}
      route={{ page: "new" }}
      go={go}
      me={me}
      prefs={{ ...unreadPrefs, pinned: [] }}
      can={(p) => me.permissions.includes(p)}
      threads={[]}
      projects={[{ name: "Launch", title: "Spring launch" } as never]}
      pinned={[]}
      onPin={async () => {}}
      inboxCount={0}
      live
      onSignOut={noop}
    />
  );
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

test("resting the pointer on a row previews its whole title and how long ago it moved", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [row], next: null }),
  });
  render(<Bar />);
  const button = await screen.findByRole("link", { name: /^Vendor contract/ });
  fireEvent.pointerEnter(button, { pointerType: "mouse" });
  assert.equal(screen.queryByRole("tooltip"), null, "nothing opens at once");
  await wait(PEEK_MS + 50);
  const card = await screen.findByRole("tooltip");
  assert.match(card.textContent ?? "", /Vendor contract/);
  assert.match(card.textContent ?? "", /\d+(m|h|d|w|mo|y) ago$/);
  assert.doesNotMatch(card.textContent ?? "", /Spring launch|Clause 4/, "no project or last line");
  assert.equal(button.getAttribute("aria-describedby"), card.id);
  fireEvent.pointerLeave(button, { pointerType: "mouse" });
  await waitFor(() => assert.equal(screen.queryByRole("tooltip"), null));
});

test("a long press previews a row and does not open it", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [row], next: null }),
  });
  let opened = 0;
  render(<Bar go={() => opened++} />);
  const button = await screen.findByRole("link", { name: /^Vendor contract/ });
  fireEvent.touchStart(button, { touches: [{ clientX: 10, clientY: 10 }] });
  await wait(HOLD_MS + 50);
  await screen.findByRole("tooltip");
  fireEvent.touchEnd(button);
  fireEvent.click(button);
  assert.equal(opened, 0, "the click after a long press is swallowed");
  fireEvent.click(button);
  assert.equal(opened, 1, "a tap after that opens it");
});

// The badge on the row is a glyph alone, and its tooltip needs a mouse.
const broke: ThreadSummary = {
  ...row,
  id: "t2",
  title: "Check my repos",
  failed: 1,
  failure: "provider: rate limited (429)",
};
const reason = "Failed: provider: rate limited (429)";

test("a long press says in words why a row failed", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [broke], next: null }),
  });
  render(<Bar />);
  const link = await screen.findByRole("link", { name: /^Check my repos/ });
  assert.ok(!link.textContent?.includes(reason), "the row itself shows the badge only");
  fireEvent.touchStart(link, { touches: [{ clientX: 10, clientY: 10 }] });
  await wait(HOLD_MS + 50);
  const card = await screen.findByRole("tooltip");
  const status = card.querySelector("[data-peek-status]");
  assert.equal(status?.textContent, reason);
  assert.equal(
    status?.getAttribute("aria-hidden"),
    "true",
    "the row's own description already says it, so a screen reader hears it once",
  );
  assert.match(card.textContent ?? "", /^Check my repos.*\d+(m|h|d|w|mo|y) ago$/);
});

test("keyboard focus previews a row with its status, and leaving puts it away", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [broke, row], next: null }),
  });
  let opened = 0;
  render(<Bar go={() => opened++} />);
  const link = await screen.findByRole("link", { name: /^Check my repos/ });
  const other = await screen.findByRole("link", { name: /^Vendor contract/ });
  link.focus();
  assert.equal(screen.queryAllByRole("tooltip").length, 0, "nothing opens at once");
  await wait(PEEK_MS + 50);
  const card = await screen.findByRole("tooltip");
  assert.equal(card.querySelector("[data-peek-status]")?.textContent, reason);
  assert.ok(link.getAttribute("aria-describedby")?.split(" ").includes(card.id));

  // One preview at a time: the pointer resting on another row replaces it.
  fireEvent.pointerEnter(other, { pointerType: "mouse" });
  await wait(PEEK_MS + 50);
  const cards = screen.getAllByRole("tooltip");
  assert.equal(cards.length, 1);
  assert.ok(!cards[0]?.querySelector("[data-peek-status]"), "a row with no status has none");
  fireEvent.pointerLeave(other, { pointerType: "mouse" });
  await waitFor(() => assert.equal(screen.queryAllByRole("tooltip").length, 0));

  link.blur();
  link.focus();
  await wait(PEEK_MS + 50);
  await screen.findByRole("tooltip");
  link.blur();
  await waitFor(() => assert.equal(screen.queryAllByRole("tooltip").length, 0));

  link.focus();
  await wait(PEEK_MS + 50);
  await screen.findByRole("tooltip");
  fireEvent.click(link);
  assert.equal(opened, 1, "Enter opens the conversation");
  await waitFor(() => assert.equal(screen.queryAllByRole("tooltip").length, 0));
});
