import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Me, Preferences } from "./api.ts";
import { stubFetch } from "./dom.ts";
import type { PrefsRead } from "./preferences.ts";
import type { SettingsSection } from "./route.ts";
import { Settings } from "./Settings.tsx";
import { ClientSettings } from "./Clients.tsx";

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const me: Me = { principal: { subject: "ada", tenant: "acme", kind: "user", name: "Developer", roles: ["admin"] }, permissions: ["runs:read", "runs:write"] };
// What the console draws with before the saved preferences are read.
const defaults: Preferences = { display_name: "", theme: "system", accent: "red", has_photo: false, photo_version: 0, pinned: [], muted: [] };
const saved: Preferences = { ...defaults, display_name: "Ada", theme: "dark", accent: "green" };

function page(prefs: Preferences, prefsRead: PrefsRead, section: SettingsSection = "profile", onPrefs: (p: Preferences) => void = () => {}) {
  const noop = () => {};
  return (
    <Settings
      me={me}
      prefs={prefs}
      prefsRead={prefsRead}
      workspace={null}
      agents={[]}
      projects={[]}
      canAdmin={false}
      section={section}
      onSection={noop}
      explain="runs:read"
      onExplain={noop}
      onPrefs={onPrefs}
      onWorkspace={noop}
      onOpenProjects={noop}
      onOpenWorkspace={noop}
    />
  );
}

const nameField = () => screen.getByRole("textbox") as HTMLInputElement;
const saveName = () => screen.getByRole("button", { name: "Save Name" }) as HTMLButtonElement;

test("opened before the saved name is read, the profile shows it once it is, and Save Name waits for a change", async () => {
  net = stubFetch({});
  const { rerender } = render(page(defaults, "reading"));
  // Nothing that could save is offered over a name not read yet.
  assert.ok(!screen.queryByRole("textbox"));
  assert.ok(!screen.queryByRole("button", { name: "Save Name" }));

  rerender(page(saved, "read"));
  assert.equal(nameField().value, "Ada");
  assert.ok(saveName().disabled, "Save Name with nothing changed");
  assert.ok(!net.seen.some((r) => r.method === "PUT"));
});

test("a name typed is saved, and the field then follows what was saved", async () => {
  let shown = saved;
  net = stubFetch({ "PUT /api/preferences": (body) => ({ ...shown, ...(body as Partial<Preferences>) }) });
  const { rerender } = render(page(shown, "read", "profile", (p) => (shown = p)));
  fireEvent.change(nameField(), { target: { value: "Ada L." } });
  assert.ok(!saveName().disabled);
  fireEvent.click(saveName());
  await waitFor(() => assert.equal(shown.display_name, "Ada L."));
  const put = net.seen.find((r) => r.method === "PUT" && r.path === "/api/preferences");
  assert.deepEqual(put?.body, { display_name: "Ada L.", theme: "dark", accent: "green" });
  rerender(page(shown, "read", "profile", (p) => (shown = p)));
  assert.equal(nameField().value, "Ada L.");
  assert.ok(saveName().disabled);
});

test("the theme and accent are not offered, so not saved, before the saved ones are read", () => {
  net = stubFetch({});
  render(page(defaults, "reading", "appearance"));
  assert.ok(!screen.queryByRole("button", { name: "Dark" }));
  assert.ok(!net.seen.some((r) => r.method === "PUT"));
  cleanup();
  render(page(defaults, "failed", "appearance"));
  assert.ok(screen.getByText("Your preferences could not be read. Reload to try again."));
  assert.ok(!screen.queryByRole("button", { name: "Dark" }));
});

test("appearance saves red and green accents without rewriting a saved legacy choice on open", async () => {
  let shown: Preferences = { ...saved, accent: "purple" };
  net = stubFetch({ "PUT /api/preferences": (body) => ({ ...shown, ...(body as Partial<Preferences>) }) });
  const { rerender } = render(page(shown, "read", "appearance", (next) => { shown = next; }));
  assert.equal(net.seen.some((request) => request.method === "PUT"), false);
  assert.equal(screen.getByRole("button", { name: "Purple" }).getAttribute("aria-pressed"), "true");
  for (const [label, accent] of [["Red", "red"], ["Green", "green"]] as const) {
    fireEvent.click(screen.getByRole("button", { name: label }));
    await waitFor(() => assert.equal(shown.accent, accent));
    const request = net.seen.filter((request) => request.method === "PUT").at(-1);
    assert.equal((request?.body as { accent: string }).accent, accent);
    rerender(page(shown, "read", "appearance", (next) => { shown = next; }));
    assert.equal(screen.getByRole("button", { name: label }).getAttribute("aria-pressed"), "true");
  }
});

test("the audit address refuses a reader without governance", async () => {
 net = stubFetch({});
 render(page(saved,"read","read-audit"));
 await screen.findByText("Only workspace administrators may read this audit.");
 assert.ok(!net.seen.some(r=>r.path==="/api/read-audit"));
});

const clientsCaps = { max: 10, max_per_surface: 5 };
const connectField = (name: string) => (screen.queryByLabelText(name, { exact: true }) ?? screen.getByLabelText((text) => text.startsWith(name))) as HTMLInputElement;
const changeConnection = (name: string, value: string) => fireEvent.change(connectField(name), { target: { value } });
async function openConnect() {
  render(<ClientSettings canWrite />);
  fireEvent.click(await screen.findByRole("button", { name: "Connect a System" }));
  changeConnection("Connection ID", "trial-client");
  changeConnection("Name", "Trial connection");
}
const submitConnection = () => fireEvent.submit(screen.getByRole("form", { name: "Connect a system" }));

test("connects a Slack client through the real client endpoint with default-deny agent reach", async () => {
  let created: unknown;
  let clients: unknown[] = [];
  net = stubFetch({
    "GET /api/clients": () => ({ clients, caps: clientsCaps }),
    "POST /api/clients": (input) => {
      created = input;
      clients = [{ ...(input as object), id: "trial-client", version: 1, credentials: { token: { set: true }, signing_secret: { set: true } } }];
      return clients[0];
    },
  });
  await openConnect();
  changeConnection("Slack team ID", "T0123ABC");
  changeConnection("Token", "slack-test-token");
  changeConnection("Signing secret", "test-signing");
  submitConnection();
  await waitFor(() => assert.ok(!screen.queryByRole("form", { name: "Connect a system" })));
  assert.deepEqual(created, {
    id: "trial-client", name: "Trial connection", surface: "slack", identity: { team: "T0123ABC" },
    default_agent: "", agents: [], enabled: true,
    credentials: { token: { value: "slack-test-token" }, signing_secret: { value: "test-signing" } },
  });
  await screen.findByRole("heading", { name: "Trial connection Slack" });
  assert.ok(screen.getByText("No agent"));
  assert.ok(!document.body.textContent?.includes("slack-test-token"));
});

test("connects GitHub with explicit agent reach and environment references", async () => {
  let created: unknown;
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: clientsCaps }),
    "POST /api/clients": (input) => { created = input; return { ...(input as object), version: 1 }; },
  });
  await openConnect();
  changeConnection("System", "github");
  changeConnection("GitHub accounts", " acme, octo ");
  changeConnection("Installation ID (optional)", "1234");
  changeConnection("App ID (optional)", "45");
  changeConnection("GitHub bot login", "review-helper[bot]");
  changeConnection("Default agent (optional)", "reviewer");
  changeConnection("Other allowed agents (optional)", "helper, helper");
  changeConnection("Token source", "reference");
  changeConnection("Signing secret source", "reference");
  changeConnection("Token reference", "env:HUMAGEN_GITHUB_TOKEN");
  changeConnection("Signing secret reference", "env:HUMAGEN_GITHUB_WEBHOOK_SECRET");
  submitConnection();
  await waitFor(() => assert.ok(!screen.queryByRole("form", { name: "Connect a system" })));
  assert.deepEqual(created, {
    id: "trial-client", name: "Trial connection", surface: "github",
    identity: { owners: ["acme", "octo"], app_id: "45", installation: "1234" },
    default_agent: "reviewer", agents: ["helper"], enabled: true,
    github: { login: "review-helper[bot]" },
    credentials: { token: { ref: "env:HUMAGEN_GITHUB_TOKEN" }, signing_secret: { ref: "env:HUMAGEN_GITHUB_WEBHOOK_SECRET" } },
  });
});

test("a refused connection preserves non-secret details and clears credential values", async () => {
  net = stubFetch({
    "GET /api/clients": () => ({ clients: [], caps: clientsCaps }),
    "POST /api/clients": () => new Response(JSON.stringify({ error: "Connection limit reached." }), { status: 409 }),
  });
  await openConnect();
  changeConnection("Slack team ID", "T0123ABC");
  changeConnection("Token", "discard-token");
  changeConnection("Signing secret", "discard-signing");
  submitConnection();
  await screen.findByText("Connection limit reached.");
  assert.equal(connectField("Token").value, "");
  assert.equal(connectField("Signing secret").value, "");
  assert.equal(connectField("Name").value, "Trial connection");
  assert.equal(connectField("Slack team ID").value, "T0123ABC");
});

test("connection creation is absent without permission, and wildcard reach is refused", async () => {
  net = stubFetch({ "GET /api/clients": () => ({ clients: [], caps: clientsCaps }) });
  render(<ClientSettings canWrite={false} />);
  await screen.findByText("No clients yet.");
  assert.ok(!screen.queryByRole("button", { name: "Connect a System" }));
  cleanup();
  await openConnect();
  changeConnection("Other allowed agents (optional)", "*");
  submitConnection();
  await screen.findByText("List individual agent names, separated by commas. This form does not grant access to every agent.");
  assert.ok(!net.seen.some((r) => r.method === "POST"));
});

test("changing the connection surface clears entered secrets", async () => {
  net = stubFetch({ "GET /api/clients": () => ({ clients: [], caps: clientsCaps }) });
  await openConnect();
  changeConnection("Token", "slack-test-token");
  changeConnection("Signing secret", "slack-test-signing");
  changeConnection("System", "github");
  assert.equal(connectField("Token").value, "");
  assert.equal(connectField("Signing secret").value, "");
});

test("GitHub default routing requires the actual app login and rejects invalid default agent names", async () => {
  net = stubFetch({ "GET /api/clients": () => ({ clients: [], caps: clientsCaps }) });
  await openConnect();
  changeConnection("System", "github");
  changeConnection("Default agent (optional)", "*");
  submitConnection();
  await screen.findByText("The default must name one agent. Use letters, digits, underscores or dashes, starting with a letter or digit.");
  changeConnection("Default agent (optional)", "reviewer");
  submitConnection();
  await screen.findByText("Give the GitHub App's own login, such as your-app[bot], so mentioning it can reach the default agent.");
  assert.ok(!net.seen.some((r) => r.method === "POST"));
});

test("connection input patterns compile using browser Unicode sets and reject malformed names", async () => {
  net = stubFetch({ "GET /api/clients": () => ({ clients: [], caps: clientsCaps }) });
  await openConnect();
  for (const input of document.querySelectorAll<HTMLInputElement>("input[pattern]")) {
    assert.doesNotThrow(() => new RegExp(input.pattern, "v"));
  }
  const idPattern = new RegExp(`^(?:${connectField("Connection ID").pattern})$`, "v");
  assert.ok(idPattern.test("slack-client"));
  assert.ok(!idPattern.test("not valid"));
  const agentPattern = new RegExp(`^(?:${connectField("Default agent (optional)").pattern})$`, "v");
  assert.ok(agentPattern.test("review-agent_1"));
  assert.ok(!agentPattern.test("*"));
});
