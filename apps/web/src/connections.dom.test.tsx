import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ToolCredentials } from "./api.ts";
import { YourConnections } from "./Connections.tsx";
import { cannotConnect } from "./connections.ts";
import { stubFetch } from "./dom.ts";

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const at = "2026-10-21T09:00:00Z";
const mine: ToolCredentials = {
  providers: [
    { provider: "calendar", transport: "http", endpoint: "https://calendar.acme.example/mcp", tools: 1, connected: false },
    { provider: "issues", transport: "http", endpoint: "https://issues.acme.example/mcp", tools: 2, connected: true, kind: "reference", ref: "op://People/lee/issues", updated_at: at },
  ],
  values: true,
  references: ["op://People/lee/"],
};

test("someone who may not connect is told so in words, with no Connect, form or Remove to press", async () => {
  net = stubFetch({ "GET /api/me/credentials": () => mine });
  let why = 0;
  // Even an address that opens a tool's form opens none.
  render(<YourConnections open="calendar" onOpen={() => {}} canConnect={false} onWhy={() => why++} />);
  assert.ok(await screen.findByText(/Connecting a credential takes permission to run agents/));
  assert.ok(screen.getByText(cannotConnect, { exact: false }));
  // Where each tool stands is still shown.
  assert.ok(screen.getAllByText("Not connected").length > 0);
  assert.ok(screen.getByText(/reads op:\/\/People\/lee\/issues/));
  for (const name of ["Connect", "Replace", "Remove", "Cancel"]) assert.ok(!screen.queryByRole("button", { name }), `${name} is offered`);
  assert.ok(!screen.queryByRole("textbox"));
  const link = screen.getByRole("link", { name: "Why can't I?" });
  assert.equal(link.getAttribute("href"), "/settings/access?permission=runs%3Awrite");
  fireEvent.click(link);
  assert.equal(why, 1);
  assert.ok(!net?.seen.some((r) => r.method !== "GET"));
});

test("someone who may connect is given the form an address opens, and Replace and Remove on what they connected", async () => {
  net = stubFetch({ "GET /api/me/credentials": () => mine });
  render(<YourConnections open="calendar" onOpen={() => {}} canConnect />);
  assert.ok(await screen.findByRole("button", { name: "Cancel" }));
  assert.ok(screen.getByRole("button", { name: "Replace" }));
  assert.ok(screen.getByRole("button", { name: "Remove" }));
  assert.ok(!screen.queryByText(cannotConnect, { exact: false }));
});
