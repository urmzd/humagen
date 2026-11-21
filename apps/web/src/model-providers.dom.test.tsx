import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ModelProvider, ModelRef } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { forgetConfiguredProviders, ModelChain } from "./ModelChain.tsx";
import { ModelProviderSettings } from "./ModelProviders.tsx";

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
  const list = await screen.findByRole("list", { name: "Available providers" });
  assert.deepEqual(
    within(list)
      .getAllByRole("listitem")
      .map((li) => li.querySelector("span")?.textContent),
    ["Anthropic", "OpenAI", "Google"],
  );
  assert.equal(within(list).getAllByText("Not set up").length, 3);
  fireEvent.click(within(list).getByRole("button", { name: "Set Up Anthropic" }));
  const form = screen.getByRole("form", { name: "Model provider" });
  assert.equal(within(form).queryByLabelText(/^Name/), null, "a kind is enabled by its own name");
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: "sk-secret" } });
  fireEvent.click(within(form).getByRole("button", { name: "Set Up" }));
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
  const available = await screen.findByRole("list", { name: "Available providers" });
  const add = screen.getByRole("button", { name: "Add OpenAI-Compatible Server" });
  assert.ok(add.compareDocumentPosition(available) & Node.DOCUMENT_POSITION_FOLLOWING);
  fireEvent.click(add);
  const dialog = screen.getByRole("dialog", { name: "Add OpenAI-Compatible Server" });
  const form = within(dialog).getByRole("form", { name: "Model provider" });
  fireEvent.change(within(form).getByLabelText(/^Name/), { target: { value: "groq" } });
  fireEvent.change(within(form).getByLabelText(/^Base URL/), {
    target: { value: "https://api.groq.com/openai/v1" },
  });
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: "gsk" } });
  fireEvent.click(within(form).getByRole("button", { name: "Add Server" }));
  await screen.findByText(/api\.groq\.com · key kept/);
  await waitFor(() => assert.equal(document.activeElement, add));
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
    "PATCH /api/model-providers/openai": () =>
      new Response(
        JSON.stringify({ error: 'model provider in use: "openai" is used by @writer' }),
        {
          status: 409,
        },
      ),
  });
  render(<ModelProviderSettings canEdit />);
  const list = await screen.findByRole("list", { name: "Configured providers" });
  const anthropic = within(list).getAllByRole("listitem")[0] as HTMLElement;
  assert.ok(within(anthropic).getByText("Deployment"));
  assert.equal(within(anthropic).queryByRole("button"), null);
  assert.equal(within(anthropic).queryByRole("switch"), null);
  const toggle = within(list).getByRole("switch", { name: "Enable OpenAI" });
  fireEvent.click(toggle);
  await screen.findByText(/@writer/);
  assert.equal(toggle.getAttribute("aria-checked"), "true");
  assert.deepEqual(net.seen.find((r) => r.method === "PATCH")?.body, { enabled: false });
});

test("turning a configured provider off and on keeps its row and its key", async () => {
  let provider: ModelProvider = {
    name: "openai",
    kind: "openai",
    key_stored: true,
  };
  net = stubFetch({
    "GET /api/model-providers": () => setup([provider], ["anthropic"]),
    "PATCH /api/model-providers/openai": (body) => {
      provider = { ...provider, disabled: !(body as { enabled: boolean }).enabled };
      return provider;
    },
  });
  render(<ModelProviderSettings canEdit />);
  const toggle = await screen.findByRole("switch", { name: "Enable OpenAI" });
  assert.equal(toggle.getAttribute("aria-checked"), "true");
  fireEvent.click(toggle);
  await waitFor(() => assert.equal(toggle.getAttribute("aria-checked"), "false"));
  assert.ok(screen.getByText("OpenAI · key kept"));
  assert.equal(screen.queryByRole("button", { name: "Set Up OpenAI" }), null);

  fireEvent.click(screen.getByRole("button", { name: "Manage OpenAI" }));
  const menu = screen.getByRole("menu", { name: "Manage OpenAI" });
  assert.equal(within(menu).queryByRole("menuitem", { name: "Check" }), null);
  fireEvent.click(within(menu).getByRole("menuitem", { name: "Remove" }));
  // Removing asks again in place on the row, with no dialog, and rests on Keep It.
  const remove = screen.getByRole("group", {
    name: "Remove OpenAI? This deletes the provider configuration and its stored key.",
  });
  assert.equal(screen.queryByRole("dialog"), null);
  assert.ok(toggle.closest("li")?.contains(remove), "the question sits on the provider's own row");
  const keep = within(remove).getByRole("button", { name: "Keep It" });
  assert.equal(document.activeElement, keep);
  fireEvent.click(keep);
  assert.equal(screen.queryByRole("group", { name: /^Remove OpenAI/ }), null);

  fireEvent.click(toggle);
  await waitFor(() => assert.equal(toggle.getAttribute("aria-checked"), "true"));
  assert.deepEqual(
    net.seen.filter((r) => r.method !== "GET").map((r) => [r.method, r.body]),
    [
      ["PATCH", { enabled: false }],
      ["PATCH", { enabled: true }],
    ],
  );
});

test("removing a provider confirms in place, and a refusal is said on its row", async () => {
  let refuse = true;
  net = stubFetch({
    "GET /api/model-providers": () => setup([{ name: "openai", kind: "openai", key_stored: true }]),
    "DELETE /api/model-providers/openai": () =>
      refuse
        ? new Response(
            JSON.stringify({ error: 'model provider in use: "openai" is used by @writer' }),
            { status: 409 },
          )
        : undefined,
  });
  render(<ModelProviderSettings canEdit />);
  const list = await screen.findByRole("list", { name: "Configured providers" });
  const ask = () => {
    fireEvent.click(screen.getByRole("button", { name: "Manage OpenAI" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Remove" }));
    return screen.getByRole("group", { name: /^Remove OpenAI\? This deletes/ });
  };
  fireEvent.click(within(ask()).getByRole("button", { name: "Remove" }));
  assert.ok(await within(list).findByText(/is used by @writer/));
  assert.ok(within(list).getByRole("switch", { name: "Enable OpenAI" }), "a refused row stays");
  assert.equal(screen.queryByRole("group", { name: /^Remove OpenAI/ }), null);

  refuse = false;
  fireEvent.click(within(ask()).getByRole("button", { name: "Remove" }));
  await screen.findByRole("button", { name: "Set Up OpenAI" });
  assert.equal(screen.queryByText(/is used by @writer/), null);
  assert.equal(net.seen.filter((r) => r.method === "DELETE").length, 2);
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
