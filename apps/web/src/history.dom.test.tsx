import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { History, keeps, pullConversation, sourceOf } from "./History.tsx";
import { href, parse } from "./route.ts";

const now = new Date().toISOString();
const thread = (id: string, origin: string, extra: Record<string, unknown> = {}) => ({
  id,
  title: `Conversation ${id}`,
  named: true,
  origin,
  channel: "general",
  agents: ["code-reviewer"],
  people: [],
  visibility: "shared",
  messages: 1,
  turns: 1,
  branches: 0,
  active: 0,
  failed: 0,
  created_at: now,
  updated_at: now,
  ...extra,
});
const pull = {
  run: "review",
  thread: "pr",
  repo: "acme/api",
  number: 42,
  title: "Review API change",
  agent: "code-reviewer",
  status: "waiting" as const,
  created_at: now,
  errors: 1,
  warnings: 2,
  infos: 0,
  ci: "success" as const,
};
let net: ReturnType<typeof stubFetch>;
afterEach(() => {
  cleanup();
  net?.restore();
});

test("history lists every source, and a review opens on its pull request tab", async () => {
  net = stubFetch({
    "GET /api/threads": () => ({ threads: [thread("chat", "api"), thread("pr", "github")] }),
    "GET /api/pull-requests": () => ({ pulls: [pull] }),
  });
  const routes: unknown[] = [];
  render(<History route={{ page: "history" }} go={(r) => routes.push(r)} />);
  await screen.findByRole("link", { name: /Conversation chat/ });
  const review = await screen.findByRole("link", { name: /acme\/api #42/ });
  fireEvent.click(review);
  assert.deepEqual(routes, [pullConversation(pull)]);
  fireEvent.click(screen.getByRole("button", { name: "GitHub" }));
  assert.deepEqual(routes[1], { page: "history", source: "github" });
});

test("one origin is filtered by the server", async () => {
  net = stubFetch({
    "GET /api/threads": () => ({ threads: [] }),
    "GET /api/pull-requests": () => ({ pulls: [] }),
  });
  render(<History route={{ page: "history", source: "slack" }} go={() => {}} />);
  await screen.findByText("Nothing matches these filters.");
  assert.ok(net.seen.some((s) => s.path.includes("origin=slack")));
});

test("filters the server cannot apply are applied in the page", () => {
  const waiting = thread("w", "cron", { waiting: 1, active: 1 }) as never;
  assert.equal(keeps(waiting, { source: "scheduled", status: "needs" }), true);
  assert.equal(keeps(waiting, { source: "chat" }), false);
  assert.equal(keeps(waiting, { q: "nothing like it" }), false);
  assert.equal(sourceOf("manual"), "scheduled");
  assert.equal(sourceOf("something-new"), "chat");
});

test("history filters roundtrip in the route, and the old pull requests address lands on GitHub", () => {
  const route = {
    page: "history" as const,
    q: "API",
    source: "github" as const,
    status: "needs" as const,
  };
  const address = new URL(href(route), "http://localhost");
  assert.deepEqual(parse(address.pathname, address.search), route);
  assert.deepEqual(
    JSON.parse(JSON.stringify(parse("/pull-requests", ""))),
    JSON.parse(JSON.stringify({ page: "history", source: "github" })),
  );
});
