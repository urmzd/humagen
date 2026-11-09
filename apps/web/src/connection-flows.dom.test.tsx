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
        name: "Other MCP",
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
  const sheet = await screen.findByRole("dialog", { name: "Add MCP" });
  fireEvent.click(within(sheet).getByRole("button", { name: "Sign In" }));
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

/** Reviews Notion's tools from its card's one action; returns that button. */
async function openToolReview() {
  const notion = await screen.findByRole("listitem", { name: "Notion" });
  const review = within(notion).getByRole("button", { name: "Review Tools" });
  review.focus();
  fireEvent.click(review);
  return review;
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
        name: "Other MCP",
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

/** The open sheet: the page under it is hidden while it is open. */
const sheet = (name: string) => screen.findByRole("dialog", { name });

/**
 * Waits until the stub has answered a request, on plain timers. A waitFor
 * started while a request is in flight can stall happy-dom here, so the
 * page is observed only after the answer is in.
 */
async function sent(method: string, path: string) {
  for (let i = 0; i < 200; i++) {
    if (net?.seen.some((r) => r.method === method && r.path === path)) break;
    await new Promise((done) => setTimeout(done, 5));
  }
  await new Promise((done) => setTimeout(done, 30));
}

test("key MCPs: a value, a replaced reference and a removal, each from its card", async () => {
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
  const calendarCard = await screen.findByRole("listitem", { name: "calendar" });
  assert.ok(within(screen.getByRole("listitem", { name: "issues" })).getByText("Connected"));
  fireEvent.click(within(calendarCard).getByRole("button", { name: "Connect" }));
  const calendar = within(await sheet("calendar")).getByRole("region", { name: "calendar" });
  fireEvent.click(within(calendar).getByRole("button", { name: "Value" }));
  fireEvent.change(within(calendar).getByLabelText(/^Value/), {
    target: { value: "private-value" },
  });
  fireEvent.click(within(calendar).getByRole("button", { name: "Connect" }));
  await sent("PUT", "/api/me/credentials/calendar");
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));
  await within(screen.getByRole("listitem", { name: "calendar" })).findByText("Connected");
  assert.equal(screen.queryByDisplayValue("private-value"), null);

  fireEvent.click(
    within(screen.getByRole("listitem", { name: "issues" })).getByRole("button", {
      name: "Replace",
    }),
  );
  const issues = within(await sheet("issues")).getByRole("region", { name: "issues" });
  fireEvent.change(within(issues).getByLabelText(/^Reference/), {
    target: { value: "op://People/alice/replacement" },
  });
  fireEvent.click(within(issues).getByRole("button", { name: "Replace" }));
  await sent("PUT", "/api/me/credentials/issues");
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));

  const more = screen.getByRole("button", { name: "More for issues" });
  more.focus();
  fireEvent.keyDown(more, { key: "ArrowDown" });
  fireEvent.click(await screen.findByRole("menuitem", { name: "Remove" }));
  await sent("DELETE", "/api/me/credentials/issues");
  await waitFor(() =>
    assert.ok(
      within(screen.getByRole("listitem", { name: "issues" })).getByRole("button", {
        name: "Connect",
      }),
    ),
  );
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

test("an address naming a key MCP opens its sheet, and follows the route", async () => {
  net = stubFetch(canonicalConnectionRoutes(configuredCredentials()));
  const view = render(<Connections provider="calendar" canConnect />);
  assert.ok(within(await sheet("calendar")).getByLabelText(/^Reference/));
  view.rerender(<Connections provider="issues" canConnect />);
  assert.ok(within(await sheet("issues")).getByLabelText(/^Reference/));
  view.rerender(<Connections canConnect />);
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));
  assert.deepEqual(
    net.seen.filter((r) => r.method !== "GET"),
    [],
  );
});

test("a connection's card opens its sheet; its menu holds only what else it can do", async () => {
  net = stubFetch(canonicalConnectionRoutes(configuredCredentials()));
  render(<Connections canConnect />);
  const card = await screen.findByRole("listitem", { name: "issues" });
  const more = within(card).getByRole("button", { name: "More for issues" });
  fireEvent.keyDown(more, { key: "ArrowDown" });
  const rows = (await screen.findAllByRole("menuitem")).map((r) => r.textContent);
  assert.equal(rows.includes("Details"), false, "the card itself is Details");
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  fireEvent.click(within(card).getByRole("button", { name: "issues" }));
  assert.ok(within(await sheet("issues")).getByLabelText(/^Reference/));
});

test("a refused key keeps what was typed, with the reason in its sheet", async () => {
  net = stubFetch({
    ...canonicalConnectionRoutes(configuredCredentials()),
    "PUT /api/me/credentials/calendar": () =>
      new Response(JSON.stringify({ error: "This credential cannot be connected" }), {
        status: 403,
      }),
  });
  render(<Connections provider="calendar" canConnect />);
  const dialog = await sheet("calendar");
  const calendar = within(dialog).getByRole("region", { name: "calendar" });
  fireEvent.click(within(calendar).getByRole("button", { name: "Value" }));
  const value = within(calendar).getByLabelText(/^Value/) as HTMLInputElement;
  fireEvent.change(value, { target: { value: "keep-private-value" } });
  fireEvent.click(within(calendar).getByRole("button", { name: "Connect" }));
  await sent("PUT", "/api/me/credentials/calendar");
  await within(dialog).findByText("This credential cannot be connected");
  assert.equal(value.value, "keep-private-value");
  assert.ok(within(calendar).getByRole("button", { name: "Connect" }));
});

test("without the right to connect, cards and sheets show status and nothing to press", async () => {
  net = stubFetch(canonicalConnectionRoutes(configuredCredentials()));
  const view = render(<Connections provider="calendar" canConnect={false} />);
  const dialog = await sheet("calendar");
  await within(dialog).findByText(/Connecting a credential takes permission to run agents/);
  assert.equal(within(dialog).queryByRole("textbox"), null);
  view.rerender(<Connections provider="issues" canConnect={false} />);
  assert.ok(within(await sheet("issues")).getByText(/reads op:\/\/People\/alice\/issues/));
  view.rerender(<Connections canConnect={false} />);
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));
  for (const name of ["Connect", "Replace", "Add MCP", "Review Tools", "Enable"]) {
    assert.equal(screen.queryByRole("button", { name }), null, name);
  }
  assert.equal(
    net.seen.some((r) => r.method !== "GET"),
    false,
  );
});

test("Add MCP adds one by URL, reviews its tools, and shows it as a card", async () => {
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
  render(<Connections canConnect />);
  const mcps = await screen.findByRole("region", { name: "MCPs" });
  fireEvent.click(await within(mcps).findByRole("button", { name: "Add MCP" }));
  const dialog = await sheet("Add MCP");
  assert.ok(within(dialog).getByText("Allowed hosts: tools.example.com."));
  const form = within(dialog).getByRole("form", { name: "New MCP" });
  fireEvent.change(within(form).getByLabelText(/^Name/), { target: { value: "my-tools" } });
  fireEvent.change(within(form).getByLabelText(/^URL/), {
    target: { value: "https://tools.example.com/mcp" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Review Tools" }));
  await sent("POST", "/api/me/tool-servers/review");
  await within(form).findByText("find_docs");
  assert.equal(
    net.seen.some((r) => r.method === "PUT"),
    false,
    "review does not grant unapproved tools",
  );
  fireEvent.click(within(form).getByRole("button", { name: "Approve Tool List" }));
  await sent("PUT", "/api/me/context");
  await waitFor(() => assert.equal(screen.queryByRole("dialog"), null));
  const added = await screen.findByRole("listitem", { name: "my-tools" });
  assert.ok(within(added).getByText("Added"));
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    run: {},
    version: 1,
    tool_servers: [reviewed],
  });
});

/** Presets the server offers, with nothing connected and nothing installed. */
function offered(over: Record<string, (body: unknown) => unknown> = {}) {
  const preset = (id: string, name: string, kind: string, scope = "person") => ({
    id,
    name,
    kind,
    scope,
    available: true,
    connected: false,
  });
  return {
    "GET /api/connections/presets": () => [
      preset("notion", "Notion", "oauth"),
      preset("other", "Other MCP", "tools"),
      preset("drive", "Google Drive", "source_oauth"),
      preset("bigquery", "BigQuery", "catalog_oauth"),
      preset("slack", "Slack", "client_oauth", "workspace"),
      preset("github", "GitHub", "github_install", "workspace"),
    ],
    "GET /api/me/credentials": () => ({ providers: [], references: [], values: false }),
    "GET /api/me/context": () => ({ run: {}, version: 1, tool_servers: [] }),
    "GET /api/clients": () => ({ clients: [], caps: { max: 10, max_per_surface: 10 } }),
    ...over,
  };
}

test("Connections is Apps and MCPs, each item once, with what is connected on its own card", async () => {
  net = stubFetch(
    offered({
      "GET /api/clients": () => ({
        clients: [
          {
            id: "acme-slack",
            name: "Acme Slack",
            surface: "slack",
            enabled: true,
            identity: {},
            credentials: {},
          },
        ],
        caps: { max: 10, max_per_surface: 10 },
      }),
      "GET /api/me/context": () => ({
        run: {},
        version: 1,
        tool_servers: [{ name: "my-tools", url: "https://tools.example.com/mcp", tools: [] }],
      }),
    }),
  );
  render(<Connections canConnect canWrite />);
  const apps = await screen.findByRole("list", { name: "Apps" });
  const mcps = screen.getByRole("list", { name: "MCPs" });
  const slack = await within(apps).findByRole("listitem", { name: "Slack" });
  await within(slack).findByText("Installed");
  assert.ok(within(slack).getByRole("button", { name: "Manage" }));
  assert.equal(screen.getAllByRole("listitem", { name: "Slack" }).length, 1, "Slack appears once");
  await within(mcps).findByRole("listitem", { name: "my-tools" });
  assert.ok(within(mcps).getByRole("listitem", { name: "Notion" }));
  // Data sources are added inside a corpus, not here; Add MCP replaces a card for any server.
  for (const name of ["Google Drive", "BigQuery", "Other MCP"]) {
    assert.equal(screen.queryByRole("listitem", { name }), null, name);
  }
  assert.equal(screen.queryByRole("list", { name: "Data sources" }), null);
  for (const name of ["Installed apps", "Your connections", "Your tool servers"]) {
    assert.equal(screen.queryByRole("region", { name }), null, name);
  }
  for (const words of [/tool server/i, /act(s)? as you/i, /agent tools/i]) {
    assert.equal(screen.queryByText(words), null, String(words));
  }
  assert.equal(screen.queryByRole("alert"), null);
  assert.equal(screen.queryByRole("tab"), null);
});

test("an item that is off says so on its card; an administrator sees Enable and how", async () => {
  const off = {
    "GET /api/connections/presets": () => [
      {
        id: "notion",
        name: "Notion",
        kind: "oauth",
        scope: "person",
        endpoint: "https://mcp.notion.com/mcp",
        available: false,
        connected: false,
        reason: "An administrator must enable secure account connections for this workspace.",
      },
    ],
  };
  net = stubFetch(offered(off));
  const view = render(<Connections canConnect canWrite={false} />);
  const notion = await screen.findByRole("listitem", { name: "Notion" });
  assert.ok(within(notion).getByText("Not enabled"));
  assert.equal(within(notion).queryByRole("button", { name: /Connect|Enable/ }), null);
  assert.equal(screen.queryByRole("alert"), null);
  view.unmount();

  net.restore();
  net = stubFetch(offered(off));
  render(<Connections canConnect canWrite />);
  const card = await screen.findByRole("listitem", { name: "Notion" });
  fireEvent.click(within(card).getByRole("button", { name: "Enable" }));
  const dialog = await sheet("Notion");
  assert.ok(within(dialog).getByText(/service connection authorization/));
});

// The reason is the server's own words (toolHostRefusal); the card matches "MCP hosts" in it.
test("an MCP on a host the workspace does not allow says so, and an administrator is sent to Limits", async () => {
  net = stubFetch(
    offered({
      "GET /api/connections/presets": () => [
        {
          id: "notion",
          name: "Notion",
          kind: "oauth",
          scope: "person",
          endpoint: "https://mcp.notion.com/mcp",
          available: false,
          connected: false,
          reason:
            "No MCP hosts are allowed yet. Ask a workspace administrator to allow this service in Limits.",
        },
      ],
    }),
  );
  render(<Connections canConnect canWrite />);
  const card = await screen.findByRole("listitem", { name: "Notion" });
  assert.ok(within(card).getByText("Host not allowed"));
  fireEvent.click(within(card).getByRole("button", { name: "Enable" }));
  const dialog = await sheet("Notion");
  assert.ok(within(dialog).getByText(/Its host is not allowed yet/));
  assert.ok(within(dialog).getByRole("link", { name: "Allow it in Limits" }));
});

test("an empty MCP group is its title and Add MCP; without the right to connect it is gone", async () => {
  const appsOnly = {
    "GET /api/connections/presets": () => [
      {
        id: "slack",
        name: "Slack",
        kind: "client_oauth",
        scope: "workspace",
        available: true,
        connected: false,
      },
    ],
  };
  net = stubFetch(offered(appsOnly));
  const view = render(<Connections canConnect canWrite />);
  const mcps = await screen.findByRole("region", { name: "MCPs" });
  assert.ok(await within(mcps).findByRole("button", { name: "Add MCP" }));
  assert.equal(within(mcps).queryByRole("list"), null);
  assert.equal(within(mcps).queryByRole("paragraph"), null);
  view.unmount();

  net.restore();
  net = stubFetch(offered(appsOnly));
  render(<Connections canConnect={false} />);
  await screen.findByRole("region", { name: "Apps" });
  assert.equal(screen.queryByRole("region", { name: "MCPs" }), null);
});

/** A GitHub app installed for acme, with one rule. */
function githubApp() {
  const client = {
    id: "acme-github",
    name: "Acme GitHub",
    surface: "github" as const,
    enabled: true,
    managed: "installation",
    version: 3,
    identity: { owners: ["acme"] },
    credentials: { token: {}, signing_secret: {} },
    default_agent: "code-reviewer",
    agents: ["code-reviewer", "triage"],
    github: {
      automations: [
        {
          name: "review-prs",
          repository: "acme/api",
          event: "pull_request" as const,
          action: "opened",
          agent: "code-reviewer",
          instructions: "Review the change.",
          paused: false,
        },
      ],
    },
  };
  return client;
}

test("the GitHub app's sheet holds the rules that start a session, saved on the app", async () => {
  const client = githubApp();
  net = stubFetch(
    offered({
      "GET /api/connections/presets": () => [
        {
          id: "github",
          name: "GitHub",
          kind: "github_install",
          scope: "workspace",
          available: true,
          connected: true,
        },
      ],
      "GET /api/clients": () => ({ clients: [client], caps: { max: 10, max_per_surface: 10 } }),
      "GET /api/agents": () => ({
        agents: [{ spec: { name: "code-reviewer" } }, { spec: { name: "triage" } }],
      }),
      "GET /api/projects": () => ({ projects: [] }),
      "PUT /api/clients/acme-github": (body) => ({ ...client, ...(body as object), version: 4 }),
      "GET /api/connections/github": () => ({
        connection: { id: "github", name: "GitHub" },
        mappings: [],
        note: "",
        truncated: false,
      }),
    }),
  );
  render(<Connections canConnect canWrite />);
  const card = await screen.findByRole("listitem", { name: "GitHub" });
  fireEvent.click(within(card).getByRole("button", { name: "Manage" }));
  const dialog = await sheet("GitHub");
  const rules = await within(dialog).findByRole("region", { name: "Start a session when" });
  const rule = within(rules).getByRole("listitem", { name: "review-prs" });
  assert.ok(within(rule).getByText("Pull request opened"));
  assert.ok(within(rule).getByText("acme/api → @code-reviewer"));

  fireEvent.click(within(rules).getByRole("button", { name: "Add Rule" }));
  const form = within(rules).getByRole("form", { name: "New rule" });
  fireEvent.change(within(form).getByLabelText(/^Name/), { target: { value: "triage-issues" } });
  fireEvent.change(within(form).getByLabelText(/^Repository/), { target: { value: "acme/api" } });
  fireEvent.change(within(form).getByLabelText(/^Event/), { target: { value: "issues" } });
  await waitFor(() =>
    assert.ok(within(form).getByRole("option", { name: "@triage" }), "routable agents load"),
  );
  fireEvent.change(within(form).getByLabelText(/^Agent/), { target: { value: "triage" } });
  fireEvent.change(within(form).getByLabelText(/^Instructions/), {
    target: { value: "Label and route the issue." },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Save Rule" }));
  await sent("PUT", "/api/clients/acme-github");
  const body = net.seen.find((r) => r.method === "PUT")?.body as {
    github: { automations: { name: string; event: string; action: string; agent: string }[] };
  };
  assert.deepEqual(
    body.github.automations.map((a) => [a.name, a.event, a.action, a.agent]),
    [
      ["review-prs", "pull_request", "opened", "code-reviewer"],
      ["triage-issues", "issues", "opened", "triage"],
    ],
  );
});

test("a rule the form cannot save says why before anything is sent", async () => {
  const client = githubApp();
  net = stubFetch(
    offered({
      "GET /api/connections/presets": () => [
        {
          id: "github",
          name: "GitHub",
          kind: "github_install",
          scope: "workspace",
          available: true,
          connected: true,
        },
      ],
      "GET /api/clients": () => ({ clients: [client], caps: { max: 10, max_per_surface: 10 } }),
      "GET /api/agents": () => ({ agents: [{ spec: { name: "code-reviewer" } }] }),
      "GET /api/projects": () => ({ projects: [] }),
    }),
  );
  render(<Connections provider="github" canConnect canWrite />);
  const dialog = await sheet("GitHub");
  const rules = await within(dialog).findByRole("region", { name: "Start a session when" });
  fireEvent.click(within(rules).getByRole("button", { name: "Add Rule" }));
  const form = within(rules).getByRole("form", { name: "New rule" });
  fireEvent.change(within(form).getByLabelText(/^Name/), { target: { value: "review-prs" } });
  fireEvent.click(within(form).getByRole("button", { name: "Save Rule" }));
  await within(form).findByText("A rule is already named review-prs.");
  assert.equal(
    net.seen.some((r) => r.method !== "GET"),
    false,
  );
});
