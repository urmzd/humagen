import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { type Client, type Source } from "./api.ts";
import { ConnectionDetail } from "./ConnectionDetail.tsx";
import { ConnectionHealth } from "./ConnectionHealth.tsx";
import { Connections } from "./Connections.tsx";
import { stubFetch } from "./dom.ts";
import { InstalledClientSetup } from "./InstalledClientSetup.tsx";
import { SourceLifecycle } from "./SourceLifecycle.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  sessionStorage.clear();
});
const at = "2026-11-07T12:00:00Z";

test("custom OAuth is the person's own and keeps its endpoint after refusal", async () => {
  net = stubFetch({
    "GET /api/connections/presets": () => [
      {
        id: "other",
        name: "Other tool server",
        kind: "tools",
        available: true,
        connected: false,
        scope: "person",
      },
    ],
    "GET /api/me/credentials": () => ({
      providers: [],
      references: [],
      values: false,
      allowed_hosts: ["tools.example.com"],
      oauth_available: true,
    }),
    "GET /api/me/context": () => ({ run: {}, version: 0, tool_servers: [] }),
    "GET /api/clients": () => ({ clients: [], caps: { max: 10, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "POST /api/connections/other/authorize": () =>
      new Response(JSON.stringify({ error: "Administrator registration required" }), {
        status: 503,
      }),
  });
  render(<Connections provider="other" canWrite />);
  const endpoint = await screen.findByPlaceholderText("https://tools.example.com/mcp");
  fireEvent.change(endpoint, { target: { value: "https://tools.example.com/mcp" } });
  fireEvent.click(screen.getByLabelText("Use a registered application"));
  fireEvent.change(screen.getByLabelText("Authorization issuer"), {
    target: { value: "https://auth.example.com" },
  });
  fireEvent.change(screen.getByLabelText("Client ID"), { target: { value: "registered-client" } });
  fireEvent.change(screen.getByLabelText("Client secret (if required)"), {
    target: { value: "private-client-secret" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Connect with OAuth" }));
  await screen.findByText("Administrator registration required");
  assert.equal((endpoint as HTMLInputElement).value, "https://tools.example.com/mcp");
  assert.deepEqual(net.seen.find((r) => r.method === "POST")?.body, {
    endpoint: "https://tools.example.com/mcp",
    scope: "person",
    registration: {
      issuer: "https://auth.example.com",
      client_id: "registered-client",
      client_secret: "private-client-secret",
    },
  });
  assert.equal(
    (screen.getByLabelText("Client secret (if required)") as HTMLInputElement).value,
    "private-client-secret",
  );
});

test("credential health starts unverified and reports only the returned check", async () => {
  net = stubFetch({
    "POST /api/clients/slack-one/check": () => ({
      healthy: false,
      checked_at: at,
      message: "Credentials were refused.",
    }),
  });
  render(<ConnectionHealth client="slack-one" canCheck />);
  assert.ok(screen.getByText(/Access has not been checked/));
  fireEvent.click(screen.getByRole("button", { name: "Test access" }));
  await screen.findByText(/Access not verified.*Credentials were refused/);
  assert.equal(screen.queryByText(/^Access verified/), null);
});

test("Drive account connection requires a separate explicit source binding", async () => {
  const source: Source = {
    name: "docs",
    documents: 0,
    passages: 0,
    excluded: 0,
    variants: [],
    updated_at: at,
    lifecycle: {
      mode: "live",
      provider: "drive",
      origin: "folder",
      project: "Launch",
      owner: "alice",
      status: "connected",
      connected_at: at,
      stop_after_seconds: 86400,
      attempts: [],
    },
  };
  net = stubFetch({
    "GET /api/connections/presets": () => [
      {
        id: "drive",
        name: "Google Drive",
        kind: "source_oauth",
        available: true,
        connected: true,
        scope: "person",
      },
    ],
    "GET /api/me/credentials": () => ({ providers: [], references: [], values: false }),
    "GET /api/knowledge/docs/dependencies": () => ({ agents: [], projects: [] }),
    "PUT /api/knowledge/docs/credential": () => source,
  });
  render(<SourceLifecycle source={source} canWrite onChanged={() => {}} />);
  const use = await screen.findByRole("button", { name: "Use my Google Drive account" });
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
  fireEvent.click(use);
  await screen.findByText(/Preview its resources to verify access/);
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, { oauth: "drive" });
});

test("native installation setup changes routes without sending credential material", async () => {
  const client = {
    id: "slack-one",
    name: "Slack",
    surface: "slack",
    identity: { team: "T12345" },
    credentials: { token: { set: true }, signing_secret: { set: true } },
    enabled: false,
    managed: "installation",
    version: 1,
    created_at: at,
    updated_at: at,
    created_by: "alice",
    agents: [],
  } as Client;
  net = stubFetch({
    "PUT /api/clients/slack-one": () => client,
    "GET /api/agents": () => ({ agents: [{ spec: { name: "helper" } }] }),
    "GET /api/projects": () => ({ projects: [] }),
  });
  render(<InstalledClientSetup client={client} onChanged={() => {}} />);
  await screen.findByRole("option", { name: "helper" });
  fireEvent.change(screen.getByLabelText("Default agent"), { target: { value: "helper" } });
  fireEvent.click(screen.getByRole("button", { name: "Save installation" }));
  await waitFor(() => assert.equal(net?.seen.filter((r) => r.method === "PUT").length, 1));
  const saved = net.seen.find((r) => r.method === "PUT")?.body as Record<string, unknown>;
  assert.equal(saved.default_agent, "helper");
  assert.equal(saved.enabled, false);
  assert.equal("credentials" in saved, false);
});

test("saved health and actual scope mapping load without claiming a fresh check", async () => {
  const detail = {
    connection: {
      id: "drive",
      name: "Google Drive",
      kind: "source_oauth",
      available: true,
      connected: true,
      scope: "person",
      granted_scopes: ["drive.readonly"],
      health: { healthy: true, checked_at: at, message: "Saved provider observation." },
    },
    mappings: [
      {
        kind: "source",
        id: "docs",
        name: "Docs",
        agents: ["researcher"],
        projects: ["Launch"],
        tools: [],
        places: ["GitHub acme/docs"],
      },
    ],
    note: "Configured uses visible to you.",
    truncated: false,
  };
  net = stubFetch({ "GET /api/connections/drive": () => detail });
  render(
    <>
      <ConnectionHealth provider="drive" canCheck />
      <ConnectionDetail provider="drive" />
    </>,
  );
  await screen.findByText(/Access verified.*Saved provider observation/);
  await screen.findByText(/Granted permissions: drive.readonly/);
  assert.ok(screen.getByText("Agents: researcher"));
  assert.ok(screen.getByText("Projects: Launch"));
  assert.ok(screen.getByText("Reply destinations: GitHub acme/docs"));
  assert.equal(
    net.seen.some((r) => r.method === "POST"),
    false,
  );
});
