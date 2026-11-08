import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ModelProvider } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { forgetConfiguredProviders } from "./ModelChain.tsx";
import { AgentPackages, ModelProviderSettings } from "./ModelProviders.tsx";

// There is no built-in model: an administrator sets up each provider, and the
// platform's agents are installed on a model they pick.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  forgetConfiguredProviders();
});

const groq: ModelProvider = {
  name: "groq",
  kind: "openai",
  base_url: "https://api.groq.com/openai/v1",
  key_stored: true,
};

test("with no provider, the page says agents can't answer and offers to add one", async () => {
  net = stubFetch({
    "GET /api/model-providers": () => ({
      providers: [],
      kinds: ["anthropic", "google", "openai"],
      workspace: [],
    }),
    "GET /api/agent-packages": () => ({ packages: [] }),
  });
  render(<ModelProviderSettings canEdit />);
  await screen.findByText(/No model provider yet/);
  fireEvent.click(screen.getByRole("button", { name: "Add Provider" }));
  assert.ok(screen.getByRole("form", { name: "Model provider" }));
});

test("a provider is added with its key as a value, never shown again, then checked", async () => {
  let workspace: ModelProvider[] = [];
  net = stubFetch({
    "GET /api/model-providers": () => ({
      providers: [],
      kinds: ["anthropic", "google", "openai"],
      workspace,
    }),
    "GET /api/agent-packages": () => ({ packages: [] }),
    "PUT /api/model-providers/groq": (body) => {
      workspace = [groq];
      return { ...groq, ...(body as object), key: undefined };
    },
    "POST /api/model-providers/groq/check": () => ({
      id: "provider:groq",
      name: "groq",
      status: "operational",
      detail: "Reachable, key accepted",
      latency_ms: 40,
    }),
  });
  render(<ModelProviderSettings canEdit />);
  await screen.findByText(/No model provider yet/);
  fireEvent.click(screen.getByRole("button", { name: "Add Provider" }));
  const form = screen.getByRole("form", { name: "Model provider" });
  fireEvent.click(within(form).getByRole("button", { name: /OpenAI/ }));
  fireEvent.change(within(form).getByLabelText(/^Name/), { target: { value: "groq" } });
  fireEvent.change(within(form).getByLabelText(/^Base URL/), {
    target: { value: "https://api.groq.com/openai/v1" },
  });
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: "gsk-secret" } });
  fireEvent.click(within(form).getByRole("button", { name: "Add Provider" }));
  await screen.findByText("groq");
  const put = net.seen.find((r) => r.method === "PUT");
  assert.deepEqual(put?.body, {
    kind: "openai",
    base_url: "https://api.groq.com/openai/v1",
    key: "gsk-secret",
  });
  await screen.findByText("Answering");
  assert.equal(screen.queryByText(/gsk-secret/), null, "the key is never shown");
  assert.ok(screen.getByText(/api\.groq\.com · key kept/));
});

test("removing a provider asks once, and a refusal names what still runs on it", async () => {
  net = stubFetch({
    "GET /api/model-providers": () => ({ providers: [], kinds: [], workspace: [groq] }),
    "GET /api/agent-packages": () => ({ packages: [] }),
    "DELETE /api/model-providers/groq": () =>
      new Response(JSON.stringify({ error: 'model provider in use: "groq" is used by @writer' }), {
        status: 409,
      }),
  });
  render(<ModelProviderSettings canEdit />);
  fireEvent.click(await screen.findByRole("button", { name: "Remove" }));
  fireEvent.click(screen.getByRole("button", { name: "Remove groq" }));
  await screen.findByText(/@writer/);
});

test("the built-in agents install on the model picked, from the providers set up", async () => {
  let installed = 0;
  net = stubFetch({
    "GET /api/model-providers": () => ({
      providers: [{ name: "groq", declared: true, kind: "openai", source: "workspace" }],
      kinds: [],
    }),
    "GET /api/agent-packages": () => ({
      packages: [
        {
          name: "core",
          title: "Core",
          description: "A writer, an analyst and a builder",
          installed,
          agents: [{ name: "humagen-writer", description: "Writes", installed: installed > 0 }],
        },
      ],
    }),
    "POST /api/agent-packages/core/install": () => {
      installed = 1;
      return {};
    },
  });
  let done = 0;
  render(<AgentPackages canInstall onInstalled={() => done++} />);
  const model = await screen.findByLabelText("Install on model");
  await waitFor(() => assert.equal((model as HTMLInputElement).value, "gpt-5.1"));
  fireEvent.change(model, { target: { value: "openai/gpt-oss-20b" } });
  fireEvent.click(screen.getByRole("button", { name: "Install Core" }));
  await waitFor(() => assert.equal(done, 1));
  const post = net.seen.find((r) => r.method === "POST");
  assert.deepEqual(post?.body, { provider: "groq", model: "openai/gpt-oss-20b" });
  await waitFor(() => assert.equal(screen.queryByRole("button", { name: "Install Core" }), null));
});

test("with no provider set up, the install card points to setting one up", async () => {
  net = stubFetch({
    "GET /api/model-providers": () => ({ providers: [], kinds: [] }),
    "GET /api/agent-packages": () => ({
      packages: [
        {
          name: "core",
          title: "Core",
          description: "",
          installed: 0,
          agents: [{ name: "w", description: "", installed: false }],
        },
      ],
    }),
  });
  let opened = 0;
  render(<AgentPackages canInstall onProviders={() => opened++} />);
  fireEvent.click(await screen.findByRole("button", { name: "Set Up a Provider" }));
  assert.equal(opened, 1);
  assert.equal(screen.queryByRole("button", { name: /^Install/ }), null);
});
