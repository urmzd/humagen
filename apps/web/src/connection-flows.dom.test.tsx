import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Client, Source, ToolCredentials } from "./api.ts";
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

function toolConnectionRoutes(review: (body: unknown) => unknown) {
  const presets = ["notion", "linear"].map((id) => ({
    id,
    name: id === "notion" ? "Notion" : "Linear",
    kind: "oauth",
    endpoint: `https://${id}.example.com/mcp`,
    available: true,
    connected: true,
    scope: "person",
  }));
  const detail = (id: string) => ({
    connection: presets.find((p) => p.id === id),
    mappings: [],
    note: "Configured uses visible to you.",
    truncated: false,
  });
  return {
    "GET /api/connections/presets": () => presets,
    "GET /api/connections/notion": () => detail("notion"),
    "GET /api/connections/linear": () => detail("linear"),
    "GET /api/me/context": () => ({ run: {}, version: 1, tool_servers: [] }),
    "GET /api/me/credentials": () => ({ providers: [], references: [], values: false }),
    "POST /api/me/tool-servers/review": review,
  };
}

const reviewedNotion = {
  name: "notion",
  url: "https://notion.example.com/mcp",
  oauth: "notion",
  tools: [
    {
      name: "search_docs",
      description: "Search my documents",
      class: "read",
      input_schema: { type: "object" },
    },
  ],
};

async function openToolReview() {
  const more = await screen.findByRole("button", { name: "More for Notion" });
  more.focus();
  fireEvent.keyDown(more, { key: "ArrowDown" });
  const review = await screen.findByRole("menuitem", { name: "Review tools" });
  review.focus();
  fireEvent.keyDown(review, { key: "Enter" });
  return more;
}

test("keyboard tool review opens the provider Sheet, keeps a refused approval visible, and restores focus", async () => {
  net = stubFetch({
    ...toolConnectionRoutes(() => reviewedNotion),
    "PUT /api/me/context": () =>
      new Response(JSON.stringify({ error: "Tool approval is not permitted" }), { status: 403 }),
  });
  render(<Connections />);
  const more = await openToolReview();
  const dialog = await screen.findByRole("dialog", { name: "Notion" });
  await within(dialog).findByText("search_docs");
  const approve = within(dialog).getByRole("button", { name: "Approve Tool List" });
  fireEvent.click(approve);
  await within(dialog).findByRole("alert");
  assert.ok(within(dialog).getByText("Tool approval is not permitted"));
  assert.ok(within(dialog).getByText("search_docs"));
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    run: {},
    version: 1,
    tool_servers: [reviewedNotion],
  });
  fireEvent.keyDown(dialog, { key: "Escape" });
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));
  await waitFor(() => assert.equal(document.activeElement === more, true));
});

test("a delayed tool review cannot appear under another provider or survive reopening", async () => {
  let resolve!: (value: typeof reviewedNotion) => void;
  net = stubFetch(
    toolConnectionRoutes(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    ),
  );
  const view = render(<Connections />);
  await openToolReview();
  await screen.findByRole("dialog", { name: "Notion" });
  await waitFor(() => assert.equal(typeof resolve, "function"));
  view.rerender(<Connections provider="linear" />);
  await screen.findByRole("dialog", { name: "Linear" });
  await act(async () => {
    resolve(reviewedNotion);
  });
  assert.equal(screen.queryByRole("region", { name: "Review connection tools" }), null);
  view.rerender(<Connections provider="notion" />);
  const dialog = await screen.findByRole("dialog", { name: "Notion" });
  assert.equal(within(dialog).queryByText("search_docs"), null);
  assert.equal(within(dialog).queryByRole("button", { name: "Approve Tool List" }), null);
});

test("a refused tool discovery is displayed inside the opened provider Sheet", async () => {
  net = stubFetch(
    toolConnectionRoutes(
      () =>
        new Response(JSON.stringify({ error: "Reconnect Notion before reviewing tools" }), {
          status: 409,
        }),
    ),
  );
  render(<Connections />);
  await openToolReview();
  const dialog = await screen.findByRole("dialog", { name: "Notion" });
  await within(dialog).findByText("Reconnect Notion before reviewing tools");
  assert.equal(within(dialog).queryByRole("button", { name: "Approve Tool List" }), null);
});

function configuredCredentials(): ToolCredentials {
  return {
    providers: [
      {
        provider: "calendar",
        transport: "http",
        endpoint: "https://calendar.example.com/mcp",
        tools: 1,
        connected: false,
      },
      {
        provider: "issues",
        transport: "http",
        endpoint: "https://issues.example.com/mcp",
        tools: 2,
        connected: true,
        kind: "reference",
        ref: "op://People/alice/issues",
        updated_at: at,
      },
    ],
    values: true,
    references: ["op://People/alice/"],
    allowed_hosts: ["tools.example.com"],
  };
}

function canonicalConnectionRoutes(credentials: ToolCredentials) {
  return {
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
    "GET /api/me/credentials": () => credentials,
    "GET /api/me/context": () => ({ run: {}, version: 1, tool_servers: [] }),
  };
}

test("canonical Connections exposes configured credential value, reference replacement and removal", async () => {
  const credentials = configuredCredentials();
  net = stubFetch({
    ...canonicalConnectionRoutes(credentials),
    "PUT /api/me/credentials/calendar": () => {
      const saved = { ...credentials.providers[0]!, connected: true, kind: "value" as const };
      credentials.providers[0] = saved;
      return saved;
    },
    "PUT /api/me/credentials/issues": () => {
      const saved = { ...credentials.providers[1]!, ref: "op://People/alice/replacement" };
      credentials.providers[1] = saved;
      return saved;
    },
    "DELETE /api/me/credentials/issues": () => {
      credentials.providers[1] = { ...credentials.providers[1]!, connected: false };
    },
  });
  render(<Connections canConnect />);
  const calendar = await screen.findByRole("region", { name: "calendar" });
  const issues = screen.getByRole("region", { name: "issues" });
  assert.ok(within(issues).getByRole("button", { name: "Replace" }));
  assert.ok(within(issues).getByRole("button", { name: "Remove" }));
  fireEvent.click(within(calendar).getByRole("button", { name: "Connect" }));
  fireEvent.click(within(calendar).getByRole("button", { name: "Value" }));
  fireEvent.change(within(calendar).getByLabelText(/^Value/), {
    target: { value: "private-value" },
  });
  fireEvent.click(within(calendar).getByRole("button", { name: "Connect" }));
  await within(calendar).findByRole("button", { name: "Replace" });
  assert.equal(screen.queryByDisplayValue("private-value"), null);
  assert.equal(screen.queryByRole("dialog"), null, "credential forms do not open the OAuth Sheet");
  fireEvent.click(within(issues).getByRole("button", { name: "Replace" }));
  fireEvent.change(within(issues).getByLabelText(/^Reference/), {
    target: { value: "op://People/alice/replacement" },
  });
  fireEvent.click(within(issues).getByRole("button", { name: "Replace" }));
  await within(issues).findByText(/reads op:\/\/People\/alice\/replacement/);
  fireEvent.click(within(issues).getByRole("button", { name: "Remove" }));
  await within(issues).findByRole("button", { name: "Connect" });
  assert.deepEqual(
    net.seen.filter((r) => r.method !== "GET"),
    [
      { method: "PUT", path: "/api/me/credentials/calendar", body: { value: "private-value" } },
      {
        method: "PUT",
        path: "/api/me/credentials/issues",
        body: { ref: "op://People/alice/replacement" },
      },
      { method: "DELETE", path: "/api/me/credentials/issues", body: undefined },
    ],
  );
});

test("configured credential selection follows route changes while local selection remains independent", async () => {
  net = stubFetch(canonicalConnectionRoutes(configuredCredentials()));
  const view = render(<Connections provider="calendar" canConnect />);
  const calendar = await screen.findByRole("region", { name: "calendar" });
  const issues = screen.getByRole("region", { name: "issues" });
  assert.ok(within(calendar).getByLabelText(/^Reference/));
  assert.equal(within(issues).queryByLabelText(/^Reference/), null);

  view.rerender(<Connections provider="issues" canConnect />);
  assert.ok(within(issues).getByLabelText(/^Reference/));
  assert.equal(within(calendar).queryByLabelText(/^Reference/), null);

  fireEvent.click(within(calendar).getByRole("button", { name: "Connect" }));
  view.rerender(<Connections provider="issues" canConnect />);
  assert.ok(within(calendar).getByLabelText(/^Reference/));
  assert.equal(within(issues).queryByLabelText(/^Reference/), null);

  view.rerender(<Connections canConnect />);
  assert.equal(within(calendar).queryByLabelText(/^Reference/), null);
  assert.equal(within(issues).queryByLabelText(/^Reference/), null);
  view.rerender(<Connections provider="calendar" canConnect />);
  assert.ok(within(calendar).getByLabelText(/^Reference/));
  assert.deepEqual(
    net.seen.filter((r) => r.method !== "GET"),
    [],
  );
});

test("canonical Connections retains a configured credential input after refusal", async () => {
  net = stubFetch({
    ...canonicalConnectionRoutes(configuredCredentials()),
    "PUT /api/me/credentials/calendar": () =>
      new Response(JSON.stringify({ error: "This credential cannot be connected" }), {
        status: 403,
      }),
  });
  render(<Connections provider="calendar" canConnect />);
  const calendar = await screen.findByRole("region", { name: "calendar" });
  fireEvent.click(within(calendar).getByRole("button", { name: "Value" }));
  const value = within(calendar).getByLabelText(/^Value/) as HTMLInputElement;
  fireEvent.change(value, { target: { value: "keep-private-value" } });
  fireEvent.click(within(calendar).getByRole("button", { name: "Connect" }));
  const ownConnections = screen.getByRole("region", { name: "Your connections" });
  await within(ownConnections).findByText("This credential cannot be connected");
  assert.equal(value.value, "keep-private-value");
  assert.ok(within(calendar).getByRole("button", { name: "Connect" }));
  assert.ok(screen.queryByRole("dialog") === null);
});

test("canonical Connections shows configured status without granting credential mutations", async () => {
  net = stubFetch(canonicalConnectionRoutes(configuredCredentials()));
  render(<Connections provider="calendar" canConnect={false} />);
  await screen.findByRole("region", { name: "calendar" });
  await screen.findByText(/Connecting a credential takes permission to run agents/);
  assert.ok(screen.getByText(/reads op:\/\/People\/alice\/issues/));
  for (const name of ["Connect", "Replace", "Remove", "Add Tool Server", "Connect with OAuth"]) {
    assert.equal(screen.queryByRole("button", { name }), null);
  }
  assert.equal(screen.queryByRole("textbox"), null);
  assert.equal(
    net.seen.some((r) => r.method !== "GET"),
    false,
  );
});

test("canonical Connections adds a custom tool server outside the OAuth Sheet with an accessible review", async () => {
  const credentials = configuredCredentials();
  const reviewed = {
    name: "my-tools",
    url: "https://tools.example.com/mcp",
    tools: [
      {
        name: "find_docs",
        description: "Search documents",
        class: "read",
        input_schema: { type: "object" },
      },
    ],
  };
  net = stubFetch({
    ...canonicalConnectionRoutes(credentials),
    "POST /api/me/tool-servers/review": () => reviewed,
    "PUT /api/me/context": (body) => ({ ...(body as object), version: 2 }),
  });
  render(
    <>
      <Connections canConnect />
      <div id="humagen-inspector-dock" />
    </>,
  );
  const oauth = await screen.findByRole("button", { name: "Connect with OAuth" });
  fireEvent.click(oauth);
  const sheet = await screen.findByRole("dialog", { name: "Other tool server" });
  assert.ok(within(sheet).getByRole("form", { name: "Connect tool server with OAuth" }));
  assert.equal(within(sheet).queryByRole("button", { name: "Add Tool Server" }), null);
  fireEvent.keyDown(sheet, { key: "Escape" });
  await waitFor(() => assert.ok(screen.queryByRole("dialog") === null));
  const add = await screen.findByRole("button", { name: "Add Tool Server" });
  add.focus();
  fireEvent.click(add);
  const inspector = await screen.findByRole("dialog", { name: "Add a tool server" });
  assert.equal(
    inspector.parentElement?.id,
    "humagen-inspector-dock",
    "the desktop Inspector remains portaled into its dock",
  );
  assert.ok(document.querySelector('[data-slot="sheet-overlay"]') === null);
  assert.ok(document.querySelector('[data-slot="sheet-content"]') === null);
  assert.ok(inspector.closest('[aria-hidden="true"], [inert]') === null);
  const form = within(inspector).getByRole("form", { name: "New tool server" });
  const name = within(form).getByLabelText(/^Name/);
  name.focus();
  assert.equal(document.activeElement === name, true);
  fireEvent.change(name, { target: { value: "my-tools" } });
  fireEvent.change(within(form).getByLabelText(/^Address/), {
    target: { value: "https://tools.example.com/mcp" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Review Tools" }));
  await within(form).findByText("find_docs");
  assert.equal(
    net.seen.some((r) => r.method === "PUT"),
    false,
    "review does not grant unapproved tools",
  );
  fireEvent.click(within(form).getByRole("button", { name: "Approve Tool List" }));
  await waitFor(() => assert.ok(screen.queryByRole("dialog") === null));
  assert.ok(screen.getByRole("listitem", { name: "my-tools" }));
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    run: {},
    version: 1,
    tool_servers: [reviewed],
  });
});
