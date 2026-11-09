import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

test("the store lists each package with its version and agents, and Install adds them", async () => {
  let changed = 0;
  net = stubFetch({
    "GET /api/store/packages": () => ({ packages: [core()] }),
    "POST /api/store/packages/humagen%2Fcore/install": () => ({
      package: installedCore(),
      added: ["humagen-writer", "humagen-analyst"],
    }),
  });
  render(<AgentStore canWrite onChanged={() => changed++} />);
  assert.ok(await screen.findByText("Core"));
  assert.ok(screen.getByText("A writer and an analyst."));
  assert.ok(screen.getByText(words.version("sha256:3f2a9c1e0000")));
  assert.equal(words.version("sha256:3f2a9c1e0000"), "Version 3f2a9c1");
  assert.ok(screen.getByText("@humagen-writer"));
  assert.ok(screen.getByText("@humagen-analyst"));
  assert.equal(screen.queryByText(words.installed), null);
  fireEvent.click(screen.getByRole("button", { name: `${words.install} Core` }));
  assert.ok(await screen.findByText(words.installed));
  assert.ok(screen.getByText(words.installedNote(["humagen-writer", "humagen-analyst"])));
  assert.equal(changed, 1);
  assert.ok(screen.getByRole("button", { name: `${words.remove} Core` }));
  assert.equal(screen.queryByRole("button", { name: `${words.install} Core` }), null);
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
  render(<AgentStore canWrite onChanged={() => changed++} />);
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
  render(<AgentStore canWrite />);
  assert.ok(await screen.findByText(words.customized(["humagen-writer"])));
  fireEvent.click(screen.getByRole("button", { name: `${words.remove} Core` }));
  const question = words.removeQuestion("Core", ["humagen-writer"]);
  assert.match(question, /Changes to humagen-writer stay in its history too/);
  assert.ok(screen.getByText(question));
  fireEvent.click(screen.getByRole("button", { name: words.remove }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "DELETE")));
});

test("a partly removed package offers Restore Missing, and a taken name blocks Install", async () => {
  const review: StorePackage = {
    name: "humagen/review",
    title: "Review",
    description: "Pull request reviews.",
    version: "sha256:abcdef1234",
    installed: false,
    agents: [agent("code-reviewer", { taken: true }), agent("qa-reviewer")],
    missing: ["qa-reviewer"],
  };
  net = stubFetch({
    "GET /api/store/packages": () => ({
      packages: [
        core({
          installed: true,
          missing: ["humagen-analyst"],
          agents: [agent("humagen-writer", { installed: true }), agent("humagen-analyst")],
        }),
        review,
      ],
    }),
  });
  render(<AgentStore canWrite />);
  assert.ok(await screen.findByText(words.missing(["humagen-analyst"])));
  assert.ok(screen.getByRole("button", { name: `${words.restore} Core` }));
  assert.ok(screen.getByText(words.taken(["code-reviewer"])));
  const blocked = screen.getByRole("button", {
    name: `${words.install} Review`,
  }) as HTMLButtonElement;
  assert.equal(blocked.disabled, true);
});

test("someone who only reads agents sees the store and no Install or Remove", async () => {
  net = stubFetch({ "GET /api/store/packages": () => ({ packages: [core(), installedCore()] }) });
  render(<AgentStore canWrite={false} />);
  assert.ok((await screen.findAllByText("Core")).length > 0);
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
  render(<AgentStore canWrite />);
  fireEvent.click(await screen.findByRole("button", { name: `${words.install} Core` }));
  assert.ok(await screen.findByText(/one of this workspace's own agents/));
});
