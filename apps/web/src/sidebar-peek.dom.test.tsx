import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Me, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs } from "./preferences.ts";
import { HOLD_MS, PEEK_MS } from "./RowPreview.tsx";
import { Sidebar } from "./Sidebar.tsx";

// DES-055: a sidebar row previews itself on hover, or on a long press.

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

test("resting the pointer on a row previews its title, project, update and last line by whom", async () => {
  net = stubFetch({
    "GET /api/marks": () => ({ archived: [], read: {}, tips: [] }),
    "GET /api/threads": () => ({ threads: [row], next: null }),
  });
  render(<Bar />);
  const button = await screen.findByRole("button", { name: /^Vendor contract/ });
  fireEvent.pointerEnter(button, { pointerType: "mouse" });
  assert.equal(screen.queryByRole("tooltip"), null, "nothing opens at once");
  await wait(PEEK_MS + 50);
  const card = await screen.findByRole("tooltip");
  assert.match(card.textContent ?? "", /Vendor contract/);
  assert.match(card.textContent ?? "", /Spring launch · Updated Oct 21/);
  assert.match(card.textContent ?? "", /Clause 4 caps liability/);
  assert.equal(card.querySelector(".aui-initials")?.textContent, "LE");
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
  const button = await screen.findByRole("button", { name: /^Vendor contract/ });
  fireEvent.touchStart(button, { touches: [{ clientX: 10, clientY: 10 }] });
  await wait(HOLD_MS + 50);
  await screen.findByRole("tooltip");
  fireEvent.touchEnd(button);
  fireEvent.click(button);
  assert.equal(opened, 0, "the click after a long press is swallowed");
  fireEvent.click(button);
  assert.equal(opened, 1, "a tap after that opens it");
});
