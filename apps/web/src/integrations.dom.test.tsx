import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { defaultCopy } from "@humagen/components";
import type { Source } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { ContextHealth } from "./ContextHealth.tsx";
import { Integrations } from "./Integrations.tsx";
import {
  SourceResources,
  SourceSyncSettings,
  SourceAccessMode,
  SourceAttachments,
  sourceConfiguration,
} from "./SourceLifecycle.tsx";

const at = "2026-10-26T12:00:00Z";
const source: Source = {
  name: "runbooks",
  documents: 1,
  passages: 1,
  excluded: 0,
  variants: [],
  updated_at: at,
  lifecycle: {
    mode: "hybrid",
    provider: "drive",
    origin: "folder",
    project: "Launch",
    owner: "ana",
    status: "needs_attention",
    cause: "auth_expired",
    connected_at: at,
    last_good_at: at,
    stop_after_seconds: 86400,
    attempts: [],
  },
};
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

test("remote resource selection requires complete listing and blocks unsupported resources", async () => {
  net = stubFetch({
    "GET /api/knowledge/runbooks/resources": () => ({
      complete: false,
      resources: [
        {
          id: "a",
          name: "Allowed Page",
          kind: "page",
          uri: "https://example.com/a",
          supported: true,
        },
        {
          id: "b",
          name: "Unsupported Binary",
          kind: "file",
          uri: "",
          supported: false,
          unsupported_reason: "format",
        },
      ],
    }),
  });
  render(<SourceResources source={source} canWrite onChanged={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Load Remote Resources" }));
  await screen.findByText(defaultCopy.sourceOperations.incomplete);
  assert.equal(
    (screen.getByRole("checkbox", { name: /Allowed Page/ }) as HTMLInputElement).disabled,
    true,
  );
  assert.equal(
    (screen.getByRole("checkbox", { name: /Unsupported Binary/ }) as HTMLInputElement).disabled,
    true,
  );
  assert.equal(
    (screen.getByRole("button", { name: /Add Selected/ }) as HTMLButtonElement).disabled,
    true,
  );
  assert.equal(
    net.seen.some((r) => r.method !== "GET"),
    false,
  );
});

test("Add Selected pins exact provider IDs before actual import and preserves existing settings", async () => {
  let changed = 0;
  const configured = {
    ...source,
    lifecycle: {
      ...source.lifecycle!,
      resources: ["old"],
      snapshot: false,
      sync_every_seconds: 7200,
    },
  };
  net = stubFetch({
    "GET /api/knowledge/runbooks/resources": () => ({
      complete: true,
      resources: [{ id: "a", name: "Allowed Page", kind: "page", uri: "", supported: true }],
    }),
    "PUT /api/knowledge/runbooks/configuration": (body) => {
      assert.deepEqual(body, { ...sourceConfiguration(configured), resources: ["a"] });
      return configured;
    },
    "POST /api/knowledge/runbooks/sync": () => source,
    "PUT /api/knowledge/runbooks/attachment": (body) => {
      assert.deepEqual(body, { project: "Launch" });
      return { source: "runbooks", project: "Launch" };
    },
  });
  render(<SourceResources source={configured} canWrite onChanged={() => changed++} />);
  fireEvent.click(screen.getByRole("button", { name: "Load Remote Resources" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: /Allowed Page/ }));
  fireEvent.click(screen.getByRole("button", { name: /Add Selected/ }));
  await waitFor(() => assert.equal(changed, 1));
  assert.deepEqual(
    net.seen.map((r) => r.method),
    ["GET", "PUT", "POST", "PUT"],
  );
});

test("snapshot failure shows the backend refusal without claiming a frozen or live source", async () => {
  net = stubFetch({
    "POST /api/knowledge/runbooks/snapshot": () =>
      new Response(JSON.stringify({ error: "last good copy changed" }), { status: 409 }),
  });
  render(
    <SourceSyncSettings
      source={source}
      canWrite
      onChanged={() => assert.fail("failed snapshot reported changed")}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Keep as Snapshot" }));
  assert.ok(await screen.findByText("last good copy changed"));
  assert.equal(screen.queryByText("Saved"), null);
});

test("context health reads server-filtered sources and addresses selection", async () => {
  const picked: string[] = [];
  net = stubFetch({
    "GET /api/knowledge": () => ({ sources: [source] }),
    "GET /api/knowledge/runbooks/dependencies": () => ({ agents: [], projects: [] }),
  });
  render(
    <ContextHealth
      canWrite={false}
      onSelect={(name) => {
        if (name) picked.push(name);
      }}
    />,
  );
  fireEvent.click(await screen.findByRole("button", { name: /runbooks/ }));
  await waitFor(() => assert.deepEqual(picked, ["runbooks"]));
  assert.ok(screen.getByText(defaultCopy.sourceLifecycle.cause.auth_expired));
  assert.equal(screen.queryByRole("button", { name: "Refresh Copy" }), null);
});

test("integrations link rules are persisted with exact input and previews do not link anything", async () => {
  let posted: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [source] }),
    "GET /api/source-link-rules": () => [],
    "GET /api/source-link-suggestions": () => ({ suggestions: [] }),
    "POST /api/source-link-rules": (body) => {
      posted = body;
      return { id: "new", ...(body as object) };
    },
    "POST /api/source-link-rules/preview": () => ({
      matches: [{ project: "Launch", mode: "suggest", rules: ["rule-a"] }],
    }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="source:runbooks" tab="links" />);
  fireEvent.click(await screen.findByRole("button", { name: "New Rule" }));
  fireEvent.change(screen.getByLabelText("Match Pattern"), { target: { value: "launch-*" } });
  fireEvent.change(screen.getByLabelText("Destination Project"), { target: { value: "Launch" } });
  assert.equal((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled, true);
  fireEvent.change(screen.getByLabelText("Resource ID"), { target: { value: "remote-a" } });
  fireEvent.change(screen.getByLabelText("Attributes (JSON)"), {
    target: { value: '{"name":["launch-docs"]}' },
  });
  fireEvent.click(screen.getByRole("button", { name: "Preview Rules" }));
  assert.ok(await screen.findByText("rule-a"));
  const preview = net.seen.find((r) => r.path.endsWith("/preview"));
  assert.ok((preview?.body as { candidate?: unknown }).candidate);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    assert.deepEqual(posted, {
      provider: "drive",
      attribute: "name",
      pattern: "launch-*",
      project: "Launch",
      channel: "",
      mode: "suggest",
    }),
  );
  assert.equal(
    net.seen.some((r) => r.path.endsWith("/link")),
    false,
  );
});

test("external source creation includes its private project in one atomic request", async () => {
  let body: unknown;
  const picked: string[] = [];
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "POST /api/knowledge": (input) => {
      body = input;
      return { ...source, name: "private-source" };
    },
  });
  render(
    <Integrations
      canWrite={false}
      canKnowledgeWrite
      onSelect={(id) => {
        if (id) picked.push(id);
      }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Connect a System" }));
  const form = screen.getByRole("form", { name: "Connect Source" });
  fireEvent.change(within(form).getByLabelText("Source Name"), {
    target: { value: "private-source" },
  });
  fireEvent.change(within(form).getByLabelText("Folder or Page ID"), {
    target: { value: "folder-private" },
  });
  fireEvent.change(within(form).getByLabelText("Project"), {
    target: { value: "Private Project" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Create Source" }));
  await waitFor(() => assert.deepEqual(picked, ["source:private-source"]));
  assert.deepEqual(body, {
    name: "private-source",
    description: "",
    configuration: {
      provider: "drive",
      origin: "folder-private",
      project: "Private Project",
      mode: "indexed",
    },
  });
  assert.equal(
    net.seen.some((request) => request.method === "PUT"),
    false,
  );
});

test("Access changes live mode explicitly and clears indexed resource pins", async () => {
  let sent: unknown;
  const configured = {
    ...source,
    lifecycle: { ...source.lifecycle!, resources: ["private-pin"], sync_every_seconds: 3600 },
  };
  net = stubFetch({
    "PUT /api/knowledge/runbooks/configuration": (body) => {
      sent = body;
      return source;
    },
  });
  render(<SourceAccessMode source={configured} canWrite onChanged={() => {}} />);
  fireEvent.change(screen.getByLabelText("Access Mode"), { target: { value: "live" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Access Mode" }));
  await waitFor(() => assert.ok(sent));
  assert.equal((sent as { mode: string }).mode, "live");
  assert.equal(Object.hasOwn(sent as object, "resources"), false);
  assert.equal((sent as { project: string }).project, "Launch");
});

const client = {
  id: "local",
  name: "Local Slack",
  surface: "slack",
  identity: { team: "T123" },
  credentials: { token: {}, signing_secret: {} },
  enabled: true,
  version: 4,
} as const;
test("connection limits send exact versioned allow-lists and keep refused edits unsaved", async () => {
  let put: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [client], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "GET /api/clients/local/capabilities": () => ({
      version: 4,
      managed: false,
      legacy: true,
      events: [
        { id: "app_mention", enabled: true },
        { id: "message", enabled: false },
      ],
      actions: [
        {
          id: "slack_post",
          description: "Post a message",
          category: "external_message",
          enabled: true,
        },
      ],
      deliver_outcomes: true,
    }),
    "PUT /api/clients/local/capabilities": (body) => {
      put = body;
      return new Response(JSON.stringify({ error: "connection changed; reload" }), { status: 409 });
    },
    "GET /api/journal": () => ({ entries: [] }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="client:local" tab="events" />);
  const mention = await screen.findByRole("checkbox", { name: "App Mentions" });
  fireEvent.click(mention);
  fireEvent.click(screen.getByRole("checkbox", { name: "Deliver Outcomes" }));
  fireEvent.click(screen.getByRole("button", { name: "Save Connection Limits" }));
  await screen.findByText("connection changed; reload");
  assert.deepEqual(put, {
    version: 4,
    events: [],
    actions: ["slack_post"],
    deliver_outcomes: false,
  });
  assert.equal((mention as HTMLInputElement).checked, false);
  assert.ok(screen.getByText(defaultCopy.sourceOperations.workspaceEvents));
});

test("Slack direction uses latest version while preserving the channel and exact binding identity", async () => {
  let direction = "two_way";
  let put: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({
      clients: [{ ...client, managed: "environment" }],
      caps: { max: 20, max_per_surface: 10 },
    }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "GET /api/clients/local/bindings": () => ({
      bindings: [{ project: "Launch", channel: "general", slack_channel: "C123", direction }],
    }),
    "GET /api/projects/Launch/channels": () => ({
      channels: [
        {
          name: "general",
          version: 8,
          title: "Launch Chat",
          purpose: "Keep the launch on track",
          default: "helper",
          slack: { client: "local", channel: "C123", direction },
        },
      ],
    }),
    "PUT /api/projects/Launch/channels/general": (body) => {
      put = body;
      direction = (body as { slack: { direction: string } }).slack.direction;
      return body;
    },
  });
  render(<Integrations canWrite canKnowledgeWrite selected="client:local" tab="resources" />);
  fireEvent.change(await screen.findByRole("combobox", { name: "Bridge Direction: C123" }), {
    target: { value: "inbound" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Direction" }));
  await waitFor(() => assert.ok(put));
  assert.deepEqual(put, {
    version: 8,
    title: "Launch Chat",
    purpose: "Keep the launch on track",
    default: "helper",
    slack: { client: "local", channel: "C123", direction: "inbound" },
  });
});

test("a bridge whose direction changed since display is never overwritten with a fresh hidden version", async () => {
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [client], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "GET /api/clients/local/bindings": () => ({
      bindings: [
        { project: "Launch", channel: "general", slack_channel: "C123", direction: "two_way" },
      ],
    }),
    "GET /api/projects/Launch/channels": () => ({
      channels: [
        {
          name: "general",
          version: 9,
          slack: { client: "local", channel: "C123", direction: "outcomes" },
        },
      ],
    }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="client:local" tab="resources" />);
  fireEvent.change(await screen.findByRole("combobox", { name: "Bridge Direction: C123" }), {
    target: { value: "inbound" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Direction" }));
  await screen.findByText(defaultCopy.sourceOperations.bindingChanged);
  assert.equal(
    net.seen.some((request) => request.method === "PUT"),
    false,
  );
});


const suggestion = {
  id: "verified-1",
  client: "drive-connection",
  resource: { provider: "drive", id: "verified-page", attributes: { name: ["Launch Plan"] } },
  project: "Launch",
  channel: "general",
  knowledge: "runbooks",
  rules: ["rule-a"],
  state: "suggested",
  automatic: false,
  version: 3,
  created_at: at,
  updated_at: at,
};
test("Link acts only on the persisted verified suggestion and its displayed version", async () => {
  let decision: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [source] }),
    "GET /api/source-link-rules": () => [],
    "GET /api/source-link-suggestions": () => ({ suggestions: [suggestion] }),
    "POST /api/source-link-suggestions/verified-1/decision": (body) => {
      decision = body;
      return { ...suggestion, state: "linked", version: 4 };
    },
  });
  render(<Integrations canWrite canKnowledgeWrite selected="source:runbooks" tab="links" />);
  fireEvent.click(await screen.findByRole("button", { name: "Link" }));
  assert.ok(await screen.findByText("Linked"));
  assert.deepEqual(decision, { action: "link", version: 3 });
  assert.equal(screen.queryByRole("button", { name: "Link" }), null);
});

test("a refused Ignore keeps the verified suggestion pending and offers no invented success", async () => {
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [source] }),
    "GET /api/source-link-rules": () => [],
    "GET /api/source-link-suggestions": () => ({ suggestions: [suggestion] }),
    "POST /api/source-link-suggestions/verified-1/decision": () =>
      new Response(JSON.stringify({ error: "suggestion changed; reload" }), { status: 409 }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="source:runbooks" tab="links" />);
  fireEvent.click(await screen.findByRole("button", { name: "Ignore" }));
  await screen.findByText("suggestion changed; reload");
  assert.equal(screen.queryByText("Ignored"), null);
  assert.ok(screen.getByRole("button", { name: "Link" }));
  assert.ok(screen.getByRole("button", { name: "Refresh Suggestions" }));
});

test("Turn Into Rule opens a Suggest draft and never accepts or enables the resource", async () => {
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [source] }),
    "GET /api/source-link-rules": () => [],
    "GET /api/source-link-suggestions": () => ({ suggestions: [suggestion] }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="source:runbooks" tab="links" />);
  fireEvent.click(await screen.findByRole("button", { name: "Turn Into Rule" }));
  assert.equal((screen.getByLabelText("Match Pattern") as HTMLInputElement).value, "Launch Plan");
  assert.equal((screen.getByLabelText("Destination Project") as HTMLInputElement).value, "Launch");
  assert.equal((screen.getByLabelText("Link Mode") as HTMLSelectElement).value, "suggest");
  assert.equal((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled, true);
  assert.equal(
    net.seen.some((request) => request.method !== "GET"),
    false,
  );
});

test("failed provider import never attaches context or claims an available copy", async () => {
  net = stubFetch({
    "GET /api/knowledge/runbooks/resources": () => ({
      complete: true,
      resources: [{ id: "a", name: "Allowed Page", kind: "page", uri: "", supported: true }],
    }),
    "PUT /api/knowledge/runbooks/configuration": () => source,
    "POST /api/knowledge/runbooks/sync": () =>
      new Response(JSON.stringify({ error: "provider unavailable" }), { status: 503 }),
  });
  render(
    <SourceResources
      source={source}
      canWrite
      onChanged={() => assert.fail("failed import reported completion")}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Load Remote Resources" }));
  fireEvent.click(await screen.findByRole("checkbox", { name: /Allowed Page/ }));
  fireEvent.change(screen.getByLabelText("Target Project"), { target: { value: "" } });
  assert.equal(
    (screen.getByRole("button", { name: /Add Selected/ }) as HTMLButtonElement).disabled,
    true,
  );
  fireEvent.change(screen.getByLabelText("Target Project"), { target: { value: "Launch" } });
  fireEvent.click(screen.getByRole("button", { name: /Add Selected/ }));
  await screen.findByText(defaultCopy.sourceLifecycle.cause.unavailable);
  assert.equal(
    net.seen.some((request) => request.path.includes("/attachment")),
    false,
  );
});

test("live context attachment records channel placement without claiming provider verification", async () => {
  let attached = false;
  let sent: unknown;
  net = stubFetch({
    "GET /api/knowledge/runbooks/attachments": () => ({
      attachments: attached ? [{ source: "runbooks", project: "Launch", channel: "planning" }] : [],
    }),
    "PUT /api/knowledge/runbooks/attachment": (body) => {
      sent = body;
      attached = true;
      return { source: "runbooks", ...(body as object) };
    },
  });
  render(
    <SourceAttachments
      source={{ ...source, lifecycle: { ...source.lifecycle!, mode: "live", status: "connected" } }}
      canWrite
      onChanged={() => {}}
    />,
  );
  fireEvent.change(screen.getByLabelText("Attach to Channel (Optional)"), {
    target: { value: "planning" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Attach Context" }));
  const placed = await screen.findByRole("link", { name: "Launch / #planning" });
  assert.equal(placed.getAttribute("href"), "/projects/Launch/channels/planning");
  assert.deepEqual(sent, { project: "Launch", channel: "planning" });
  assert.ok(screen.getByText(defaultCopy.sourceOperations.liveAttachment));
  assert.equal(
    net.seen.some((request) => request.path.endsWith("/sync")),
    false,
  );
  assert.equal(screen.queryByText("In sync"), null);
});

test("detaching exact channel context preserves the source and its retained documents", async () => {
  let attached = true;
  net = stubFetch({
    "GET /api/knowledge/runbooks/attachments": () => ({
      attachments: attached ? [{ source: "runbooks", project: "Launch", channel: "planning" }] : [],
    }),
    "DELETE /api/knowledge/runbooks/attachment": (_body, query) => {
      assert.equal(query.get("project"), "Launch");
      assert.equal(query.get("channel"), "planning");
      attached = false;
    },
  });
  render(<SourceAttachments source={source} canWrite onChanged={() => {}} />);
  fireEvent.click(await screen.findByRole("button", { name: "Detach Context" }));
  await screen.findByText(defaultCopy.sourceOperations.noAttachments);
  assert.equal(
    net.seen.some(
      (request) => request.method === "DELETE" && request.path === "/api/knowledge/runbooks",
    ),
    false,
  );
  assert.equal(source.documents, 1);
});

test("automatic link history shows inactive relationships without implying current attachment", async () => {
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [source] }),
    "GET /api/source-link-rules": () => [],
    "GET /api/source-link-suggestions": (_body, query) => {
      assert.equal(query.get("knowledge"), "runbooks");
      assert.equal(query.get("limit"), "200");
      return {
        suggestions:
          query.get("state") === "linked"
            ? [{ ...suggestion, state: "linked", automatic: true, active: false }]
            : [],
      };
    },
  });
  render(<Integrations canWrite canKnowledgeWrite selected="source:runbooks" tab="links" />);
  fireEvent.change(await screen.findByLabelText("Link History"), { target: { value: "linked" } });
  await screen.findByText("Inactive");
  assert.equal(screen.queryByRole("button", { name: "Link" }), null);
});

const client = {
  id: "local",
  name: "Local Slack",
  surface: "slack",
  identity: { team: "T123" },
  credentials: { token: {}, signing_secret: {} },
  enabled: true,
  version: 4,
} as const;
test("connection limits send exact versioned allow-lists and keep refused edits unsaved", async () => {
  let put: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [client], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "GET /api/clients/local/capabilities": () => ({
      version: 4,
      managed: false,
      legacy: true,
      events: [
        { id: "app_mention", enabled: true },
        { id: "message", enabled: false },
      ],
      actions: [
        {
          id: "slack_post",
          description: "Post a message",
          category: "external_message",
          enabled: true,
        },
      ],
      deliver_outcomes: true,
    }),
    "PUT /api/clients/local/capabilities": (body) => {
      put = body;
      return new Response(JSON.stringify({ error: "connection changed; reload" }), { status: 409 });
    },
    "GET /api/journal": () => ({ entries: [] }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="client:local" tab="events" />);
  const mention = await screen.findByRole("checkbox", { name: "App Mentions" });
  fireEvent.click(mention);
  fireEvent.click(screen.getByRole("checkbox", { name: "Deliver Outcomes" }));
  fireEvent.click(screen.getByRole("button", { name: "Save Connection Limits" }));
  await screen.findByText("connection changed; reload");
  assert.deepEqual(put, {
    version: 4,
    events: [],
    actions: ["slack_post"],
    deliver_outcomes: false,
  });
  assert.equal((mention as HTMLInputElement).checked, false);
  assert.ok(screen.getByText(defaultCopy.sourceOperations.workspaceEvents));
});

test("Slack direction uses latest version while preserving the channel and exact binding identity", async () => {
  let direction = "two_way";
  let put: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({
      clients: [{ ...client, managed: "environment" }],
      caps: { max: 20, max_per_surface: 10 },
    }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "GET /api/clients/local/bindings": () => ({
      bindings: [{ project: "Launch", channel: "general", slack_channel: "C123", direction }],
    }),
    "GET /api/projects/Launch/channels": () => ({
      channels: [
        {
          name: "general",
          version: 8,
          title: "Launch Chat",
          purpose: "Keep the launch on track",
          default: "helper",
          slack: { client: "local", channel: "C123", direction },
        },
      ],
    }),
    "PUT /api/projects/Launch/channels/general": (body) => {
      put = body;
      direction = (body as { slack: { direction: string } }).slack.direction;
      return body;
    },
  });
  render(<Integrations canWrite canKnowledgeWrite selected="client:local" tab="resources" />);
  fireEvent.change(await screen.findByRole("combobox", { name: "Bridge Direction: C123" }), {
    target: { value: "inbound" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Direction" }));
  await waitFor(() => assert.ok(put));
  assert.deepEqual(put, {
    version: 8,
    title: "Launch Chat",
    purpose: "Keep the launch on track",
    default: "helper",
    slack: { client: "local", channel: "C123", direction: "inbound" },
  });
});

test("a bridge whose direction changed since display is never overwritten with a fresh hidden version", async () => {
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [client], caps: { max: 20, max_per_surface: 10 } }),
    "GET /api/knowledge": () => ({ sources: [] }),
    "GET /api/clients/local/bindings": () => ({
      bindings: [
        { project: "Launch", channel: "general", slack_channel: "C123", direction: "two_way" },
      ],
    }),
    "GET /api/projects/Launch/channels": () => ({
      channels: [
        {
          name: "general",
          version: 9,
          slack: { client: "local", channel: "C123", direction: "outcomes" },
        },
      ],
    }),
  });
  render(<Integrations canWrite canKnowledgeWrite selected="client:local" tab="resources" />);
  fireEvent.change(await screen.findByRole("combobox", { name: "Bridge Direction: C123" }), {
    target: { value: "inbound" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Direction" }));
  await screen.findByText(defaultCopy.sourceOperations.bindingChanged);
  assert.equal(
    net.seen.some((request) => request.method === "PUT"),
    false,
  );
});
