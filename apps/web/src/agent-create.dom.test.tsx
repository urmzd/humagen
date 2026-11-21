import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentCreate, freeHue, handleOf, matching } from "./AgentCreate.tsx";
import { configureAgentHues, setAgentHues } from "./agentHues.ts";
import type { AgentTemplateCatalog, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  configureAgentHues(undefined);
});

const catalog: AgentTemplateCatalog = {
  model: { provider: "anthropic", model: "claude-sonnet-5-5" },
  templates: [
    {
      id: "blank",
      title: "Start from scratch",
      outcome: "A blank agent",
      description: "A new agent.",
      greeting: "I am new here.",
      shape: "circle",
      system_prompt: "Do what you are asked.",
      tools: ["record_outcome"],
      starters: [],
    },
    {
      id: "writer",
      title: "Document Writer",
      outcome: "Documents you can publish",
      description: "Drafts documents and publishes them.",
      greeting: "I turn notes into documents.",
      shape: "square",
      system_prompt: "Create clear deliverables.",
      tools: ["file_write", "publish_artifact"],
      starters: [{ title: "Draft a brief", prompt: "Draft a one-page brief." }],
    },
    {
      id: "watcher",
      title: "Watcher",
      outcome: "Tells you when something changes",
      description: "Watches a page.",
      greeting: "I keep an eye on things.",
      shape: "ghost",
      system_prompt: "Watch.",
      tools: ["web_fetch"],
      starters: [],
    },
  ],
};

test("a name becomes a handle people can tag, and colors others hold are skipped", () => {
  assert.equal(handleOf("Product Engineer"), "product-engineer");
  assert.equal(handleOf("  Café   Bot!! "), "cafe-bot");
  assert.equal(handleOf("!!!"), "");
  assert.equal(freeHue([0, 30]), 60);
  assert.equal(freeHue(Array.from({ length: 12 }, (_, i) => i * 30)), undefined);
  assert.deepEqual(
    matching(catalog.templates, "publish").map((t) => t.id),
    ["blank", "writer"],
  );
});

test("the goal step lists outcomes, searches them, says when none match and continues with the pick", () => {
  const picked: (string | undefined)[] = [];
  let described = 0;
  render(
    <AgentCreate
      agents={[]}
      builder="Humagen"
      seed={catalog}
      onPick={(t) => picked.push(t)}
      onBack={() => {}}
      onCreated={() => {}}
      onDescribe={() => described++}
    />,
  );
  assert.ok(screen.getByRole("heading", { name: "What's it for?" }));
  // Start from scratch is first and picked.
  const radios = screen.getAllByRole("radio") as HTMLInputElement[];
  assert.equal(radios[0]?.value, "blank");
  assert.equal(radios[0]?.checked, true);
  assert.ok(screen.getByText("Documents you can publish"));

  fireEvent.change(screen.getByRole("searchbox", { name: "Search templates" }), {
    target: { value: "zebra" },
  });
  assert.ok(screen.getByText('No template matches "zebra".'));
  assert.ok(screen.getByText("Start from scratch"));
  fireEvent.change(screen.getByRole("searchbox", { name: "Search templates" }), {
    target: { value: "documents" },
  });
  assert.equal(screen.queryByText("Watcher"), null);
  fireEvent.click(screen.getByRole("radio", { name: /Document Writer/ }));
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  assert.deepEqual(picked, ["writer"]);
  fireEvent.click(screen.getByRole("button", { name: "Describe It to Humagen" }));
  assert.equal(described, 1);
});

test("Customize previews the bot, then creates the agent from its template and keeps its look", async () => {
  setAgentHues({ lead: 0 });
  const created: unknown[] = [];
  const styled: { look: Record<string, unknown>; hue?: number }[] = [];
  net = stubFetch({
    "POST /api/agents": (body) => {
      created.push(body);
      return {
        spec: body,
        rev: 1,
        hash: "h",
        created_by: "ada",
        created_at: "2026-11-20T00:00:00Z",
      };
    },
    "PUT /api/agents/report-bot/look": (body) => {
      const b = body as { look: Record<string, unknown>; hue?: number };
      styled.push(b);
      return { agent: "report-bot", look: b.look, hue: b.hue ?? 30, taken: [], choices: {} };
    },
  });
  const made: Revision[] = [];
  render(
    <AgentCreate
      template="writer"
      agents={[{ spec: { name: "lead" } } as Revision]}
      seed={catalog}
      onPick={() => {}}
      onBack={() => {}}
      onCreated={(r) => made.push(r)}
    />,
  );
  assert.ok(screen.getByRole("heading", { name: "Make it yours" }));
  const name = screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement;
  assert.equal(name.value, "Document Writer");
  fireEvent.change(name, { target: { value: "Report Bot" } });
  assert.ok(screen.getByText("People tag it as @report-bot."));
  // Another agent's color cannot be picked.
  assert.equal((screen.getByRole("button", { name: "Hue 0" }) as HTMLButtonElement).disabled, true);
  fireEvent.click(screen.getByRole("button", { name: "ghost" }));
  fireEvent.click(screen.getByRole("button", { name: "Hue 90" }));
  assert.ok(screen.getByText("It starts on anthropic / claude-sonnet-5-5."));
  fireEvent.click(screen.getByRole("button", { name: "Create Agent" }));
  await waitFor(() => assert.equal(made.length, 1));
  assert.deepEqual(created, [
    {
      name: "report-bot",
      description: "Drafts documents and publishes them.",
      provider: "anthropic",
      model: "claude-sonnet-5-5",
      system_prompt: "Create clear deliverables.",
      tools: ["file_write", "publish_artifact"],
      access: { spawn: [] },
    },
  ]);
  assert.deepEqual(styled, [
    {
      look: {
        shape: "ghost",
        display_name: "Report Bot",
        greeting: "I turn notes into documents.",
        starters: [{ title: "Draft a brief", prompt: "Draft a one-page brief." }],
      },
      hue: 90,
    },
  ]);
});

test("a taken name is said plainly, and the agent is not opened", async () => {
  net = stubFetch({
    "POST /api/agents": () => new Response(JSON.stringify({ error: "taken" }), { status: 409 }),
  });
  const made: Revision[] = [];
  render(
    <AgentCreate
      template="writer"
      agents={[]}
      seed={catalog}
      onPick={() => {}}
      onBack={() => {}}
      onCreated={(r) => made.push(r)}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Create Agent" }));
  assert.ok(await screen.findByText("An agent or a person already has that name. Try another."));
  assert.equal(made.length, 0);
});

test("when the templates cannot be read, the page says so and Retry reads them again", async () => {
  let reads = 0;
  net = stubFetch({
    "GET /api/agent-templates": () => {
      reads++;
      return reads === 1 ? new Response("{}", { status: 500 }) : catalog;
    },
  });
  render(<AgentCreate agents={[]} onPick={() => {}} onBack={() => {}} onCreated={() => {}} />);
  assert.ok(await screen.findByText("The templates could not be read."));
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  assert.ok(await screen.findByRole("heading", { name: "What's it for?" }));
  assert.equal(reads, 2);
});

test("an address naming a template no longer offered falls back to the goal step", () => {
  render(
    <AgentCreate
      template="gone"
      agents={[]}
      seed={catalog}
      onPick={() => {}}
      onBack={() => {}}
      onCreated={() => {}}
    />,
  );
  assert.ok(screen.getByText("That template is not offered any more. Pick another."));
});
