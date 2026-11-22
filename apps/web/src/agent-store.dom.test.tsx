import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { AgentStore } from "./AgentStore.tsx";
import type { StoreAgent, StorePackage } from "./api.ts";
import { stubFetch } from "./dom.ts";

const words = defaultCopy.agentStore;

function agent(name: string, over: Partial<StoreAgent> = {}): StoreAgent {
  return {
    name,
    description: `${name} does its part.`,
    installed: false,
    customized: false,
    update_available: false,
    taken: false,
    provider: "anthropic",
    model: "claude-sonnet-5-5",
    fallbacks: [],
    tools: [],
    access: {},
    connectors: [],
    tool_servers: [],
    ...over,
  };
}

function core(over: Partial<StorePackage> = {}): StorePackage {
  return {
    name: "humagen/core",
    title: "Core",
    description: "A writer and an analyst.",
    version: "sha256:3f2a9c1e0000",
    installed: false,
    agents: [agent("humagen-writer"), agent("humagen-analyst")],
    missing: ["humagen-writer", "humagen-analyst"],
    ...over,
  };
}

function installedCore(over: Partial<StoreAgent> = {}): StorePackage {
  return core({
    installed: true,
    missing: [],
    agents: [
      agent("humagen-writer", { installed: true, ...over }),
      agent("humagen-analyst", { installed: true }),
    ],
  });
}

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

/** The store as the app mounts it: the open package is the address's, kept here. */
function Store({
  canWrite = true,
  at,
  onChanged,
}: {
  canWrite?: boolean;
  at?: string;
  onChanged?: () => void;
}) {
  const [selected, setSelected] = useState(at);
  return (
    <AgentStore
      canWrite={canWrite}
      selected={selected}
      onOpen={setSelected}
      onChanged={onChanged}
    />
  );
}

const review = (): StorePackage => ({
  name: "humagen/review",
  title: "Review",
  description: "Pull request reviews.",
  version: "sha256:abcdef1234",
  installed: false,
  agents: [
    agent("code-reviewer", {
      fallbacks: [{ provider: "openai", model: "gpt-5.1" }],
      tools: ["file_read", "mcp:docs"],
      access: { spawn: ["qa-reviewer"] },
      connectors: ["github"],
      tool_servers: ["docs"],
    }),
    agent("qa-reviewer", {
      fallbacks: [{ provider: "openai", model: "gpt-5.1" }],
      tools: ["file_read", "mcp:docs"],
      connectors: ["github"],
      tool_servers: ["docs"],
    }),
  ],
  missing: ["code-reviewer", "qa-reviewer"],
});

test("the catalog lists each package by what it does, and Install adds its agents", async () => {
  let changed = 0;
  net = stubFetch({
    "GET /api/store/packages": () => ({ packages: [core(), review()] }),
    "POST /api/store/packages/humagen%2Fcore/install": () => ({
      package: installedCore(),
      added: ["humagen-writer", "humagen-analyst"],
    }),
  });
  const { container } = render(<Store onChanged={() => changed++} />);
  const card = (await screen.findByRole("link", { name: "Core" })) as HTMLAnchorElement;
  // The card is the link to the package's page.
  assert.equal(card.getAttribute("href"), "/agent-store/humagen/core");
  assert.ok(screen.getByText("A writer and an analyst."));
  // A row names the agents it adds, by their shown names.
  assert.ok(screen.getByText("Humagen Writer, Humagen Analyst"));
  // What a package needs is on its card; what each agent is granted is not.
  assert.ok(screen.getByText(words.connector("github")));
  assert.equal(container.querySelector("[data-agent]"), null);
  assert.equal(screen.queryByText(words.installed), null);
  fireEvent.click(screen.getByRole("button", { name: `${words.install} Core` }));
  assert.ok(await screen.findByText(words.installed));
  assert.equal(changed, 1);
  assert.equal(screen.queryByRole("button", { name: `${words.install} Core` }), null);
});

test("opening a card shows the package's page, and Store leads back", async () => {
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [core(), review()] }) });
  const { container } = render(<Store />);
  fireEvent.click(await screen.findByRole("link", { name: "Review" }));
  assert.ok(container.querySelector('[data-agent="code-reviewer"]'));
  assert.equal(container.querySelector('[data-agent="humagen-writer"]'), null);
  const back = screen.getByRole("link", { name: words.title }) as HTMLAnchorElement;
  assert.equal(back.getAttribute("href"), "/agent-store");
  fireEvent.click(back);
  assert.ok(screen.getByRole("link", { name: "Core" }));
  assert.equal(container.querySelector("[data-agent]"), null);
});

test("a package's page says once what every agent shares, and nests the agents one calls", async () => {
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [review()] }) });
  const { container } = render(<Store at="humagen/review" />);
  await screen.findByRole("button", { name: `${words.install} Review` });
  const facts = Object.fromEntries(
    [...container.querySelectorAll("dl > dt")].map((dt) => [
      dt.textContent,
      dt.nextElementSibling?.textContent,
    ]),
  );
  assert.equal(facts[words.needs], `${words.connector("github")}${words.toolServer("docs")}`);
  assert.equal(words.connector("github"), "GitHub connection");
  assert.equal(facts[words.runsOn], "anthropic / claude-sonnet-5-5, then openai / gpt-5.1");
  assert.equal(facts[words.everyAgentTools], "file_read, mcp:docs");
  assert.equal(facts.Version, "abcdef1");
  // Said once above, so no agent repeats its model or tools.
  const lead = container.querySelector('[data-agent="code-reviewer"]') as HTMLElement;
  assert.ok(!lead.textContent?.includes("claude-sonnet-5-5"));
  assert.ok(!lead.textContent?.includes("file_read"));
  assert.ok(lead.textContent?.includes("code-reviewer does its part."));
  // The agent it calls is listed under it, and once.
  assert.ok(lead.querySelector('[data-agent="qa-reviewer"]'));
  assert.equal(container.querySelectorAll('[data-agent="qa-reviewer"]').length, 1);
  assert.ok(screen.getByRole("list", { name: words.callsNamed(["qa-reviewer"]) }));
});

test("agents that differ each say their own model and tools", async () => {
  const mixed = core({
    agents: [
      agent("humagen-writer", { tools: ["file_write"] }),
      agent("humagen-analyst", { provider: "openai", model: "gpt-5.1", tools: ["runs_summary"] }),
    ],
  });
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [mixed] }) });
  const { container } = render(<Store at="humagen/core" />);
  await screen.findByRole("button", { name: `${words.install} Core` });
  const terms = [...container.querySelectorAll("dl > dt")].map((dt) => dt.textContent);
  assert.deepEqual(terms, ["Version"]);
  const writer = container.querySelector('[data-agent="humagen-writer"]')?.textContent ?? "";
  assert.ok(writer.includes("anthropic / claude-sonnet-5-5"));
  assert.ok(writer.includes("file_write"));
  const analyst = container.querySelector('[data-agent="humagen-analyst"]')?.textContent ?? "";
  assert.ok(analyst.includes("openai / gpt-5.1"));
  assert.ok(analyst.includes("runs_summary"));
});

test("an installed agent's name opens its page", async () => {
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [installedCore()] }) });
  const { container } = render(<Store at="humagen/core" />);
  await screen.findByRole("button", { name: `${words.remove} Core` });
  const link = container.querySelector('[data-agent="humagen-writer"] a') as HTMLAnchorElement;
  assert.equal(link.textContent, "@humagen-writer");
  assert.match(link.getAttribute("href") ?? "", /humagen-writer/);
});

test("Remove asks first, saying the agents are retired and their history stays", async () => {
  let changed = 0;
  net = stubFetch({
    "GET /api/store/packages": () => ({ packages: [installedCore()] }),
    "DELETE /api/store/packages/humagen%2Fcore": () => ({
      package: core(),
      retired: ["humagen-writer", "humagen-analyst"],
      customized: [],
    }),
  });
  render(<Store at="humagen/core" onChanged={() => changed++} />);
  fireEvent.click(await screen.findByRole("button", { name: `${words.remove} Core` }));
  const question = words.removeQuestion("Core", []);
  assert.match(question, /agents are retired; their history stays/);
  assert.ok(screen.getByText(question));
  // Keep It keeps the package.
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.confirm.keep }));
  assert.equal(screen.queryByText(question), null);
  assert.equal(
    net.seen.some((r) => r.method === "DELETE"),
    false,
  );
  fireEvent.click(screen.getByRole("button", { name: `${words.remove} Core` }));
  fireEvent.click(screen.getByRole("button", { name: words.remove }));
  await waitFor(() => assert.equal(changed, 1));
  assert.ok(
    net.seen.some((r) => r.method === "DELETE" && r.path === "/api/store/packages/humagen%2Fcore"),
  );
  assert.ok(await screen.findByRole("button", { name: `${words.install} Core` }));
  assert.ok(screen.getByText(words.removedNote(["humagen-writer", "humagen-analyst"])));
});

test("removing a package with a customized agent names it and retires it with force", async () => {
  net = stubFetch({
    "GET /api/store/packages": () => ({ packages: [installedCore({ customized: true })] }),
    "DELETE /api/store/packages/humagen%2Fcore": (_body, query) => {
      assert.equal(query.get("force"), "true");
      return {
        package: core(),
        retired: ["humagen-writer", "humagen-analyst"],
        customized: ["humagen-writer"],
      };
    },
  });
  render(<Store at="humagen/core" />);
  assert.ok(await screen.findByText(words.customized(["humagen-writer"])));
  assert.ok(screen.getByText(words.changed));
  fireEvent.click(screen.getByRole("button", { name: `${words.remove} Core` }));
  const question = words.removeQuestion("Core", ["humagen-writer"]);
  assert.match(question, /Changes to humagen-writer stay in its history too/);
  assert.ok(screen.getByText(question));
  fireEvent.click(screen.getByRole("button", { name: words.remove }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "DELETE")));
});

test("a partly removed package offers Restore Missing, and a taken name blocks Install", async () => {
  const partly = core({
    installed: true,
    missing: ["humagen-analyst"],
    agents: [agent("humagen-writer", { installed: true }), agent("humagen-analyst")],
  });
  const taken: StorePackage = {
    ...review(),
    agents: [agent("code-reviewer", { taken: true }), agent("qa-reviewer")],
    missing: ["qa-reviewer"],
  };
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [partly, taken] }) });
  render(<Store />);
  assert.ok(await screen.findByRole("button", { name: `${words.restore} Core` }));
  assert.ok(screen.getByText(words.incomplete));
  assert.ok(screen.getByText(words.nameTaken));
  const blocked = screen.getByRole("button", {
    name: `${words.install} Review`,
  }) as HTMLButtonElement;
  assert.equal(blocked.disabled, true);
  // Each package's page says which agents, and what to do.
  fireEvent.click(screen.getByRole("link", { name: "Review" }));
  assert.ok(screen.getByText(words.taken(["code-reviewer"])));
  fireEvent.click(screen.getByRole("link", { name: words.title }));
  fireEvent.click(screen.getByRole("link", { name: "Core" }));
  assert.ok(screen.getByText(words.missing(["humagen-analyst"])));
});

test("someone who only reads agents sees the store and no Install or Remove", async () => {
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [core(), installedCore()] }) });
  render(<Store canWrite={false} />);
  const cards = await screen.findAllByRole("link", { name: "Core" });
  assert.equal(screen.queryByRole("button", { name: `${words.install} Core` }), null);
  fireEvent.click(cards[0] as HTMLElement);
  assert.equal(screen.queryByRole("button", { name: `${words.install} Core` }), null);
  assert.equal(screen.queryByRole("button", { name: `${words.remove} Core` }), null);
});

test("a refused install says why", async () => {
  net = stubFetch({
    "GET /api/store/packages": () => ({ packages: [core()] }),
    "POST /api/store/packages/humagen%2Fcore/install": () =>
      new Response(
        JSON.stringify({ error: "@humagen-writer is one of this workspace's own agents" }),
        {
          status: 409,
        },
      ),
  });
  render(<Store />);
  fireEvent.click(await screen.findByRole("button", { name: `${words.install} Core` }));
  assert.ok(await screen.findByText(/one of this workspace's own agents/));
});

test("an address for a package the store lacks says so above the catalog", async () => {
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [core()] }) });
  render(<Store at="humagen/gone" />);
  assert.ok(await screen.findByText(words.notFound("humagen/gone")));
  assert.ok(screen.getByRole("link", { name: "Core" }));
});

test("the store's first read is a skeleton, and a failed read says so", async () => {
  net = stubFetch({
    "GET /api/store/packages": () => new Response("{}", { status: 500 }),
  });
  const { container } = render(<Store />);
  assert.ok(container.querySelector('[data-testid="skeleton"]'));
  assert.ok(await screen.findByText(words.failed));
  assert.equal(container.querySelector('[data-testid="skeleton"]'), null);
});
