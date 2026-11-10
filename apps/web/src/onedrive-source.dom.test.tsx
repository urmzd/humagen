import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Source } from "./api.ts";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { stubFetch } from "./dom.ts";
import { ConnectSource } from "./Integrations.tsx";
import { SourceCatalog, type SourceChoice } from "./SourceCatalog.tsx";
import { SourceLifecycle } from "./SourceLifecycle.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});
const at = "2026-11-07T12:00:00Z";
const source: Source = {
  name: "finance",
  documents: 0,
  passages: 0,
  excluded: 0,
  variants: [],
  updated_at: at,
  lifecycle: {
    mode: "indexed",
    provider: "onedrive",
    origin: "D4648F06C91D9D3D!54927",
    project: "Finance",
    owner: "alice",
    status: "connected",
    connected_at: at,
    stop_after_seconds: 86400,
    attempts: [],
  },
};
const presets = (connected: boolean) => [
  {
    id: "onedrive",
    name: "OneDrive",
    kind: "source_oauth",
    available: true,
    connected,
    scope: "person",
  },
];

test("New Source offers OneDrive and creates a OneDrive folder source", async () => {
  const picked: SourceChoice[] = [];
  render(<SourceCatalog onSelect={(choice) => picked.push(choice)} />);
  fireEvent.click(
    screen.getByRole("button", {
      name: new RegExp(defaultCopy.sourceCatalog.onedriveHint.slice(0, 20)),
    }),
  );
  assert.deepEqual(picked, ["onedrive"]);
  cleanup();

  let body: unknown;
  const created: string[] = [];
  net = stubFetch({
    "POST /api/knowledge": (input) => {
      body = input;
      return { ...source, name: "finance" };
    },
  });
  render(
    <ConnectSource
      initialProvider="onedrive"
      onCreated={(name) => created.push(name)}
      onCancel={() => {}}
    />,
  );
  const form = screen.getByRole("form", { name: "Connect Source" });
  assert.equal(
    (within(form).getByLabelText(defaultCopy.sourceOperations.provider) as HTMLSelectElement).value,
    "onedrive",
  );
  fireEvent.change(within(form).getByLabelText("Source Name"), { target: { value: "finance" } });
  fireEvent.change(within(form).getByLabelText(defaultCopy.sourceCatalog.onedriveOrigin), {
    target: { value: "D4648F06C91D9D3D!54927" },
  });
  fireEvent.click(within(form).getByRole("button", { name: "Create Source" }));
  await waitFor(() => assert.deepEqual(created, ["finance"]));
  assert.deepEqual(body, {
    name: "finance",
    description: "",
    configuration: {
      provider: "onedrive",
      origin: "D4648F06C91D9D3D!54927",
      project: "~",
      mode: "live",
      sync_every_seconds: 0,
    },
  });
});

test("a OneDrive source is read with the person's own OneDrive account", async () => {
  net = stubFetch({
    "GET /api/connections/presets": () => presets(true),
    "GET /api/me/credentials": () => ({ providers: [], references: [], values: false }),
    "GET /api/knowledge/finance/dependencies": () => ({ agents: [], projects: [] }),
    "PUT /api/knowledge/finance/credential": () => source,
  });
  render(<SourceLifecycle source={source} canWrite onChanged={() => {}} />);
  const use = await screen.findByRole("button", { name: "Use my OneDrive account" });
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
  fireEvent.click(use);
  await screen.findByText(/Your OneDrive account is connected to this source/);
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, { oauth: "onedrive" });
});

test("an unconnected OneDrive account links to Connections", async () => {
  net = stubFetch({
    "GET /api/connections/presets": () => presets(false),
    "GET /api/me/credentials": () => ({ providers: [], references: [], values: false }),
    "GET /api/knowledge/finance/dependencies": () => ({ agents: [], projects: [] }),
  });
  render(<SourceLifecycle source={source} canWrite onChanged={() => {}} />);
  const link = await screen.findByRole("link", { name: "Connect OneDrive" });
  assert.equal(link.getAttribute("href"), "/connections?provider=onedrive");
  assert.equal(screen.queryByRole("button", { name: "Use my OneDrive account" }), null);
});

test("OneDrive has a mark of its own on Connections", () => {
  const { container } = render(<ConnectionLogo id="onedrive" name="OneDrive" />);
  assert.ok(container.querySelector("svg path"), "OneDrive fell back to its initial");
});
