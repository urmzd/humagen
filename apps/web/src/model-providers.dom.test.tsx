import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ModelProvider, ModelRef } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { forgetConfiguredProviders, ModelChain } from "./ModelChain.tsx";
import { MessageChoicesControl, ModelProviderSettings } from "./ModelProviders.tsx";

// There is no built-in model: an administrator enables Anthropic, OpenAI or Google by
// name, which agents' models name, or adds an OpenAI-compatible server.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  forgetConfiguredProviders();
});

const setup = (workspace: ModelProvider[], deployment: string[] = []) => ({
  providers: deployment.map((name) => ({
    name,
    declared: true,
    kind: name as "anthropic",
    source: "deployment" as const,
  })),
  kinds: ["anthropic", "google", "openai"],
  workspace,
});

test("the three kinds are listed, and enabling one sends its key under its own name", async () => {
  let workspace: ModelProvider[] = [];
  net = stubFetch({
    "GET /api/model-providers": () => setup(workspace),
    "PUT /api/model-providers/anthropic": () => {
      workspace = [{ name: "anthropic", kind: "anthropic", key_stored: true }];
      return workspace[0];
    },
    "POST /api/model-providers/anthropic/check": () => ({
      id: "provider:anthropic",
      name: "anthropic",
      status: "operational",
      detail: "Reachable, key accepted",
      latency_ms: 40,
    }),
  });
  render(<ModelProviderSettings canEdit />);
  const list = await screen.findByRole("list", { name: "Model providers" });
  assert.deepEqual(
    within(list)
      .getAllByRole("listitem")
      .map((li) => li.querySelector("span")?.textContent),
    ["Anthropic", "OpenAI", "Google"],
  );
  assert.equal(within(list).getAllByText("Not enabled").length, 3);
  fireEvent.click(within(list).getAllByRole("button", { name: "Enable" })[0] as HTMLElement);
  const form = screen.getByRole("form", { name: "Model provider" });
  assert.equal(within(form).queryByLabelText(/^Name/), null, "a kind is enabled by its own name");
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: "sk-secret" } });
  fireEvent.click(within(form).getByRole("button", { name: "Enable" }));
  await screen.findByText("Answering");
  const put = net.seen.find((r) => r.method === "PUT");
  assert.equal(put?.path, "/api/model-providers/anthropic");
  assert.deepEqual(put?.body, { kind: "anthropic", key: "sk-secret" });
  assert.equal(screen.queryByText(/sk-secret/), null, "the key is never shown");
});

test("an OpenAI-compatible server is added by name and address", async () => {
  let workspace: ModelProvider[] = [];
  net = stubFetch({
    "GET /api/model-providers": () => setup(workspace),
    "PUT /api/model-providers/groq": (body) => {
      workspace = [
        {
          name: "groq",
          kind: "openai",
          base_url: "https://api.groq.com/openai/v1",
          key_stored: true,
        },
      ];
      return { ...workspace[0], ...(body as object) };
    },
    "POST /api/model-providers/groq/check": () => ({
      id: "provider:groq",
      name: "groq",
      status: "operational",
      latency_ms: 1,
    }),
  });
  render(<ModelProviderSettings canEdit />);
  fireEvent.click(await screen.findByRole("button", { name: "Add OpenAI-Compatible Server" }));
  const form = screen.getByRole("form", { name: "Model provider" });
  fireEvent.change(within(form).getByLabelText(/^Name/), { target: { value: "groq" } });
  fireEvent.change(within(form).getByLabelText(/^Base URL/), {
    target: { value: "https://api.groq.com/openai/v1" },
  });
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: "gsk" } });
  fireEvent.click(within(form).getByRole("button", { name: "Add Server" }));
  await screen.findByText(/api\.groq\.com · key kept/);
  const put = net.seen.find((r) => r.method === "PUT");
  assert.deepEqual(put?.body, {
    kind: "openai",
    base_url: "https://api.groq.com/openai/v1",
    key: "gsk",
  });
});

test("a provider the deployment declares is read only, and turning one off names what uses it", async () => {
  net = stubFetch({
    "GET /api/model-providers": () =>
      setup([{ name: "openai", kind: "openai", key_stored: true }], ["anthropic"]),
    "DELETE /api/model-providers/openai": () =>
      new Response(
        JSON.stringify({ error: 'model provider in use: "openai" is used by @writer' }),
        {
          status: 409,
        },
      ),
  });
  render(<ModelProviderSettings canEdit />);
  const list = await screen.findByRole("list", { name: "Model providers" });
  const anthropic = within(list).getAllByRole("listitem")[0] as HTMLElement;
  assert.ok(within(anthropic).getByText("Deployment"));
  assert.equal(within(anthropic).queryByRole("button"), null);
  fireEvent.click(within(list).getByRole("button", { name: "Turn Off" }));
  fireEvent.click(screen.getByRole("button", { name: "Turn Off openai" }));
  await screen.findByText(/@writer/);
});

test("a chain no enabled provider answers says which it needs", async () => {
  net = stubFetch({
    "GET /api/model-providers": () => ({ providers: [], kinds: [] }),
  });
  const models: ModelRef[] = [
    { provider: "anthropic", model: "claude-sonnet-5-5" },
    { provider: "openai", model: "gpt-5.1" },
    { provider: "google", model: "gemini-3-flash-preview" },
  ];
  render(<ModelChain models={models} fallbackOn={[]} editable={false} onChange={() => {}} />);
  await waitFor(() =>
    assert.ok(screen.getByText(/Runs once an administrator enables anthropic, openai or google/)),
  );
});

test("message choices are picked from enabled providers' models, with efforts as toggles", async () => {
  net = stubFetch({
    "GET /api/model-providers": () => ({
      providers: [
        { name: "anthropic", declared: true, kind: "anthropic", source: "workspace" },
        { name: "groq", declared: true, kind: "openai", source: "workspace" },
      ],
      kinds: [],
    }),
  });
  let saved: unknown;
  render(
    <MessageChoicesControl
      saved={{ models: ["gone/model"] }}
      canEdit
      onSave={(next) => {
        saved = next;
      }}
    />,
  );
  const anthropic = await screen.findByRole("group", { name: "anthropic" });
  fireEvent.click(within(anthropic).getByRole("checkbox", { name: "Claude Haiku 5.5" }));
  // A model on a provider no longer enabled is shown, so it can be dropped.
  fireEvent.click(screen.getByRole("checkbox", { name: "gone/model" }));
  fireEvent.change(screen.getByLabelText("Provider of another model"), {
    target: { value: "groq" },
  });
  fireEvent.change(screen.getByLabelText("Model id"), { target: { value: "openai/gpt-oss-20b" } });
  fireEvent.click(screen.getByRole("button", { name: "Add" }));
  fireEvent.click(screen.getByRole("button", { name: "High" }));
  fireEvent.click(screen.getByRole("button", { name: "Save Choices" }));
  assert.deepEqual(saved, {
    models: ["anthropic/claude-haiku-5-5", "groq/openai/gpt-oss-20b"],
    efforts: ["high"],
  });
});
