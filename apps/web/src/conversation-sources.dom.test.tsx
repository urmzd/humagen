import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConversationInfo } from "./ConversationShell.tsx";
import type { Thread } from "./api.ts";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => { if (!net) return; cleanup(); net.restore(); net = undefined; });
const at = "2026-10-26T10:00:00Z";
const thread: Thread = { id: "context-thread", project: "Launch", channel: "general", visibility: "private", participants: [], version: 1, created_by: "ana", created_at: at, updated_at: at };

test("conversation source cards show actual mode, health, scope, owner and retained-copy time", async () => {
  net = stubFetch({
    "GET /api/projects/Launch/knowledge": () => ({ version: 1, knowledge: ["hybrid", "live"], given: [{ name: "hybrid", from: "Launch" }, { name: "live", from: "Launch" }, { name: "hidden-source", from: "Launch" }] }),
    "GET /api/knowledge": () => ({ sources: [
      { name: "hybrid", documents: 2, passages: 3, excluded: 0, variants: [], updated_at: at, lifecycle: { mode: "hybrid", status: "needs_attention", cause: "auth_expired", provider: "notion", owner: "Ana", project: "Launch", origin: "page-visible", last_good_at: at, connected_at: at, stop_after_seconds: 86400, attempts: [] } },
      { name: "live", documents: 0, passages: 0, excluded: 0, variants: [], updated_at: at, lifecycle: { mode: "live", status: "connected", provider: "notion", owner: "Ana", project: "Launch", connected_at: at, stop_after_seconds: 86400, attempts: [] } },
    ] }),
    "GET /api/knowledge/hybrid/documents": () => ({ documents: [] }),
  });
  render(<ConversationInfo panel="context" thread={thread} project="Launch" onClose={() => {}} />);
  assert.ok(await screen.findByText(defaultCopy.sourceLifecycle.modes.hybrid));
  assert.ok(screen.getByText(defaultCopy.sourceLifecycle.modes.live));
  assert.ok(screen.getByText(defaultCopy.sourceLifecycle.status.needs_attention));
  assert.ok(screen.getByText(defaultCopy.sourceLifecycle.cause.auth_expired));
  assert.ok(screen.getAllByText("Ana").length > 0);
  assert.ok(screen.getAllByText("Launch").length > 0);
  assert.ok(document.querySelector(`time[datetime="${at}"]`));
  assert.equal(screen.queryByText("hidden-source"), null);
  assert.equal(net.seen.some((request) => request.path.includes("hidden-source")), false);
  const details = [...document.querySelectorAll("details")].find((node) => node.textContent?.includes("hybrid"));
  assert.ok(details);
  details.open = true;
  fireEvent(details, new Event("toggle", { bubbles: true }));
  await waitFor(() => assert.ok(net?.seen.some((request) => request.path === "/api/knowledge/hybrid/documents")));
  assert.equal(net.seen.some((request) => request.path.includes("hidden-source")), false);
});

test("conversation Instructions uses the real read-confined composer without requesting source lists", async () => {
  let query: URLSearchParams | undefined;
  net = stubFetch({
    "GET /api/projects/Launch/context": (_body, params) => {
      query = params;
      return { layers: [
        { kind: "instructions", from: "Launch", text: "Use the saved launch instructions.", budget: 8000, used: 35, omitted: 0, sent: true, unit: "characters" },
        { kind: "ancestor_notes", from: "Company", text: "One note omitted.", budget: 6000, used: 0, omitted: 1, sent: false, unit: "characters" },
      ] };
    },
  });
  render(<ConversationInfo panel="context" contextTab="instructions" thread={{...thread,channel:"planning",participants:[{kind:"agent",name:"lead",at}]}} project="Launch" onClose={() => {}} />);
  fireEvent.click(await screen.findByRole("button", {name:defaultCopy.projects.context.preview.submit}));
  assert.ok(await screen.findByText("Use the saved launch instructions."));
  assert.equal(query?.get("agent"),"lead");
  assert.equal(query?.get("channel"),"planning");
  assert.ok(screen.getByText("One note omitted."));
  assert.equal(net.seen.some(request=>request.path==="/api/knowledge"),false);
});
