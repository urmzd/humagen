import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ProjectAgentsView, Revision } from "./api.ts";
import { Chat } from "./Chat.tsx";
import { stubFetch } from "./dom.ts";

const at = "2026-10-01T09:00:00Z";
const agent = (name: string, description = ""): Revision => ({
  rev: 1,
  spec: { name, description, provider: "offline", model: "offline", system_prompt: `${name}.` },
  created_at: at,
  created_by: "ada",
  hash: name,
});
// globex-helper lives in Globex, so it is not available at the workspace.
const agents = [agent("lead", "Coordinates work."), agent("helper"), agent("globex-helper", "Knows the Globex account.")];
const homes = { "globex-helper": "Globex" };

const workspace = (defaultResolved?: string): ProjectAgentsView => ({
  version: 2,
  enable: ["*"],
  ...(defaultResolved ? { default: defaultResolved } : {}),
  available: { all: true, names: [] },
  offers: [{ agent: "*", from: "~" }],
  homes: [],
  default_resolved: defaultResolved ? { agent: defaultResolved, from: "project:~" } : null,
});

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

function openChat(view: ProjectAgentsView) {
  net = stubFetch({
    "GET /api/projects/~/agents": () => view,
    "GET /api/pick": () => ({ agent: "lead", reason: "matched work" }),
    "POST /api/runs": (body) => ({
      id: "r1",
      tag_id: "api-r-1",
      tenant: "acme",
      agent: (body as { agent?: string }).agent ?? "lead",
      rev: 1,
      origin: "api",
      text: (body as { text: string }).text,
      root: "r1",
      depth: 0,
      thread: "r1",
      status: "accepted",
      calls: 0,
      created_at: at,
      updated_at: at,
    }),
  });
  const noop = () => {};
  render(
    <Chat
      threadId={null}
      summary={undefined}
      tick={0}
      agents={agents}
      homes={homes}
      projects={[]}
      canRun
      canCreateProject={false}
      greeting="Good morning"
      onThread={noop}
      onChanged={noop}
      onProjectCreated={noop}
    />,
  );
  return net;
}

/** The rows of the composer's menu, by what each says first. */
async function menuRows(): Promise<string[]> {
  fireEvent.click(screen.getByRole("button", { name: /^Choose who answers/ }));
  const list = await screen.findByRole("list", { name: "Agents" });
  return within(list)
    .getAllByRole("button")
    .map((b) => b.textContent ?? "");
}

async function send(text: string) {
  fireEvent.change(screen.getByLabelText("Message", { selector: "textarea" }), { target: { value: text } });
  fireEvent.submit(screen.getByRole("form", { name: "Message" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "POST" && r.path === "/api/runs")));
  return net?.seen.find((r) => r.method === "POST" && r.path === "/api/runs")?.body as Record<string, unknown>;
}

test("the composer's menu offers only the agents available where the message goes, with @auto first when it is the default there", async () => {
  openChat(workspace("auto"));
  // Under the composer, in words, who answers a message that tags nobody.
  await screen.findByText("@auto picks who answers");
  const rows = await menuRows();
  assert.equal(rows.length, 3);
  assert.match(rows[0] ?? "", /^@autoDefault/);
  assert.match(rows[1] ?? "", /^lead/);
  assert.match(rows[2] ?? "", /^helper/);
  assert.ok(!rows.some((r) => r.includes("globex-helper")));
});

test("with no default where the message goes, the composer says nobody answers and @auto comes last", async () => {
  openChat(workspace());
  await screen.findByText("Nobody answers; tag an agent");
  const rows = await menuRows();
  assert.match(rows[0] ?? "", /^lead/);
  assert.match(rows[rows.length - 1] ?? "", /^@auto/);
  assert.ok(!rows.some((r) => r.includes("Default")));
});

test("a message the person leaves untagged is sent with no agent, so the default where it goes answers", async () => {
  openChat(workspace("lead"));
  await screen.findByText("@lead answers");
  const body = await send("What changed this week?");
  assert.ok(!("agent" in body), `the untagged message named an agent: ${JSON.stringify(body)}`);
  assert.equal(body.text, "What changed this week?");
});

test("an agent the person chooses is sent by name, and the composer says it answers", async () => {
  openChat(workspace("lead"));
  await screen.findByText("@lead answers");
  await menuRows();
  fireEvent.click(screen.getByRole("button", { name: /^helper/ }));
  await screen.findByText("@helper answers");
  const body = await send("Help with the launch.");
  assert.equal(body.agent, "helper");
});

test("@auto chosen in the menu is sent as auto, not left to the default", async () => {
  openChat(workspace("lead"));
  await screen.findByText("@lead answers");
  await menuRows();
  fireEvent.click(screen.getByRole("button", { name: /^@auto/ }));
  await screen.findByText("@auto picks who answers");
  const body = await send("Who knows billing?");
  assert.equal(body.agent, "auto");
});
