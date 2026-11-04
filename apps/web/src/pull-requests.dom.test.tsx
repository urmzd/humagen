import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { PullRequests, pullConversation } from "./PullRequests.tsx";
import { href, parse } from "./route.ts";

const item = {
  run: "review",
  thread: "conversation",
  repo: "acme/api",
  number: 42,
  title: "Review API change",
  agent: "writer",
  status: "waiting" as const,
  created_at: "2026-11-03T12:00:00Z",
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
test("review cards open a conversation with its pull tab", async () => {
  net = stubFetch({ "GET /api/pull-requests": () => ({ pulls: [item] }) });
  const routes: unknown[] = [];
  render(<PullRequests route={{ page: "pull-requests" }} go={(r) => routes.push(r)} />);
  const link = await screen.findByRole("link", { name: /Review API change/ });
  assert.ok(link.getAttribute("href")?.includes("pull%3Aacme"));
  fireEvent.click(link);
  assert.deepEqual(routes, [pullConversation(item)]);
  fireEvent.click(screen.getByRole("button", { name: "List" }));
  assert.deepEqual(routes[1], { page: "pull-requests", display: "list" });
});
test("pull request filters roundtrip in the route", () => {
  const route = {
    page: "pull-requests" as const,
    q: "API change",
    repo: "acme/api",
    agent: "writer",
    status: "waiting" as const,
    display: "list" as const,
  };
  const address = new URL(href(route), "http://localhost");
  assert.deepEqual(parse(address.pathname, address.search), route);
});
test("empty and failed responses are explicit", async () => {
  net = stubFetch({ "GET /api/pull-requests": () => ({ pulls: [] }) });
  render(<PullRequests route={{ page: "pull-requests" }} go={() => {}} />);
  await screen.findByText("No pull requests match these filters.");
});
