import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Me, Preferences } from "./api.ts";
import { stubFetch } from "./dom.ts";
import type { PrefsRead } from "./preferences.ts";
import type { SettingsSection } from "./route.ts";
import { Settings } from "./Settings.tsx";

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
const defaults: Preferences = { display_name: "", theme: "system", accent: "purple", has_photo: false, photo_version: 0, pinned: [], muted: [] };
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

test("the audit address refuses a reader without governance", async () => {
 net = stubFetch({});
 render(page(saved,"read","read-audit"));
 await screen.findByText("Only workspace administrators may read this audit.");
 assert.ok(!net.seen.some(r=>r.path==="/api/read-audit"));
});
