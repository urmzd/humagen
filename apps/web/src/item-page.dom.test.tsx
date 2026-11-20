import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, render, screen } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { ItemPage } from "./ItemPage.tsx";
import { SessionsPage } from "./SessionCenter.tsx";
import type { AgentSessionSummary } from "./sessionCenter.ts";

let restore: (() => void) | undefined;
afterEach(() => {
  cleanup();
  restore?.();
  restore = undefined;
});

const now = "2026-11-07T12:00:00Z";
const session = (id: string, parent: string, origin = "api"): AgentSessionSummary => ({
  id,
  parent,
  agent: "humagen",
  project: "",
  title: `Work ${id}`,
  status: "done",
  updated_at: now,
  conversation: {
    id,
    title: `Work ${id}`,
    named: true,
    origin,
    project: "",
    channel: "",
    agents: ["humagen"],
    people: ["ana"],
    visibility: "shared",
    messages: 1,
    turns: 1,
    branches: 0,
    active: 0,
    waiting: 0,
    failed: 0,
    created_at: now,
    updated_at: now,
  },
});

const address = "github://urmzd/humagen/pull/5";
const path = `/api/items/${encodeURIComponent(address)}`;

test("an item names what it is, links to it, and lists its sessions and backlinks", async () => {
  const net = stubFetch({
    [`GET ${path}`]: () => ({
      address,
      kind: "github",
      title: "Fix the nightly deploy",
      url: "https://github.com/urmzd/humagen/pull/5",
      sessions: [
        session("s_review", address, "github"),
        session("s_slack", "slack://T1/C1/1.2", "slack"),
      ],
      backlinks: [
        { place: "slack://T1/C2/9.9", at: now, session: "s_other" },
        { place: "https://example.com/notes", at: now, url: "https://example.com/notes" },
      ],
    }),
  });
  restore = net.restore;
  render(<ItemPage address={address} go={() => {}} />);
  assert.ok(await screen.findByRole("heading", { name: "Fix the nightly deploy" }));
  assert.equal(
    screen
      .getByRole("link", { name: defaultCopy.trace.place.openIn("GitHub") })
      .getAttribute("href"),
    "https://github.com/urmzd/humagen/pull/5",
  );
  assert.equal(
    screen.getByRole("link", { name: /Work s_review/ }).getAttribute("href"),
    "/messages/s_review",
  );
  assert.ok(screen.getByRole("link", { name: /Work s_slack/ }));
  assert.ok(screen.getByRole("heading", { name: defaultCopy.items.backlinks }));
  assert.equal(
    screen.getByRole("link", { name: "Slack thread" }).getAttribute("href"),
    "/messages/s_other",
  );
  assert.equal(
    screen.getByRole("link", { name: "example.com/notes" }).getAttribute("target"),
    "_blank",
  );
});

test("until the platform serves items, the sessions rooted at the address stand for it", async () => {
  const net = stubFetch({
    "GET /api/sessions": (_b, q) => ({
      sessions: q.get("parent") === address ? [session("s_review", address, "github")] : [],
      next: "",
    }),
  });
  restore = net.restore;
  render(<ItemPage address={address} go={() => {}} />);
  assert.ok(await screen.findByRole("heading", { name: "urmzd/humagen PR #5" }));
  assert.ok(await screen.findByRole("link", { name: /Work s_review/ }));
  assert.equal(screen.queryByRole("heading", { name: defaultCopy.items.backlinks }), null);
});

test("Sessions keeps one place, and an item's sessions are those rooted at it", async () => {
  const net = stubFetch({
    "GET /api/sessions": () => ({
      sessions: [
        session("s_slack", "slack://T1/C1/1.2", "slack"),
        session("s_pr", address, "github"),
        session("s_web", "console:p"),
      ],
      next: "",
    }),
  });
  restore = net.restore;
  render(<SessionsPage route={{ page: "sessions", place: "slack" }} go={() => {}} tick={0} />);
  assert.ok(await screen.findByRole("link", { name: /Work s_slack/ }));
  assert.equal(screen.queryByRole("link", { name: /Work s_pr/ }), null);
  assert.equal(screen.queryByRole("link", { name: /Work s_web/ }), null);
  cleanup();
  render(<SessionsPage route={{ page: "sessions", item: address }} go={() => {}} tick={0} />);
  assert.ok(
    await screen.findByRole("link", { name: defaultCopy.places.item("urmzd/humagen PR #5") }),
  );
  assert.ok(
    net.seen.some(
      (r) =>
        r.path.startsWith("/api/sessions?") &&
        new URLSearchParams(r.path.split("?")[1]).get("parent") === address,
    ),
  );
});
