import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { PersonalPreferences } from "./PersonalPreferences.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (net) {
    cleanup();
    net.restore();
    net = undefined;
  }
});
const personal = {
  briefing: "",
  notifications: {},
  quiet_start: "",
  quiet_end: "",
  timezone: "UTC",
  remember: false,
  use_personal_memory_in_projects: false,
};
const profile = {
  display_name: "Ada",
  theme: "system",
  accent: "purple",
  has_photo: false,
  photo_version: 0,
  pinned: [],
  muted: [],
  personal,
};
function mount(available: boolean) {
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
      permissions: [],
      first_admin: false,
      email_notifications_available: available,
    }),
    "GET /api/preferences": () => profile,
    "GET /api/me/notifications": () => ({ notifications: [] }),
    "PUT /api/preferences": (body) => ({ ...profile, ...(body as object) }),
  });
  render(<PersonalPreferences section="notifications" />);
}
test("email opt-in is saved only when this account has native delivery configured", async () => {
  mount(true);
  const checkbox = await screen.findByRole("switch", {
    name: defaultCopy.personalPreferences.email,
  });
  assert.equal((checkbox as HTMLInputElement).disabled, false);
  fireEvent.click(checkbox);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalPreferences.save }));
  await screen.findByRole("status");
  const body = net?.seen.find((r) => r.method === "PUT")?.body as { personal: { email: boolean } };
  assert.equal(body.personal.email, true);
});
test("an unconfigured account cannot enable email delivery", async () => {
  mount(false);
  const checkbox = await screen.findByRole("switch", {
    name: defaultCopy.personalPreferences.email,
  });
  assert.equal((checkbox as HTMLInputElement).disabled, true);
});

test("alert priorities stay off until explicitly saved with quiet hours and time zone", async () => {
  mount(true);
  const copy = defaultCopy.personalPreferences;
  for (const [kind, mode] of [
    ["urgent", "immediate"],
    ["normal", "digest"],
    ["low", "off"],
  ] as const) {
    const select = (await screen.findByRole("combobox", {
      name: copy.priorities[kind],
    })) as HTMLSelectElement;
    assert.equal(select.value, "off");
    fireEvent.change(select, {
      target: { value: mode },
    });
  }
  fireEvent.change(screen.getByLabelText(copy.start), { target: { value: "22:30" } });
  fireEvent.change(screen.getByLabelText(copy.end), { target: { value: "07:00" } });
  fireEvent.change(screen.getByLabelText(copy.timezone), { target: { value: "America/Toronto" } });
  fireEvent.click(screen.getByRole("button", { name: copy.save }));
  await screen.findByRole("status");
  const body = net?.seen.find((r) => r.method === "PUT")?.body as { personal: typeof personal };
  assert.deepEqual(body.personal.notifications, {
    urgent: "immediate",
    normal: "digest",
    low: "off",
  });
  assert.equal(body.personal.quiet_start, "22:30");
  assert.equal(body.personal.quiet_end, "07:00");
  assert.equal(body.personal.timezone, "America/Toronto");
});

test("saving a priority preserves existing legacy delivery preferences", async () => {
  const existing = {
    ...profile,
    personal: { ...personal, notifications: { approval: "digest", urgent: "immediate" } },
  };
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { kind: "user", subject: "ada", tenant: "acme", roles: [] },
      permissions: [],
      email_notifications_available: false,
    }),
    "GET /api/preferences": () => existing,
    "PUT /api/preferences": (body) => ({ ...existing, ...(body as object) }),
  });
  render(<PersonalPreferences section="notifications" />);
  const priority = (await screen.findByRole("combobox", {
    name: defaultCopy.personalPreferences.priorities.urgent,
  })) as HTMLSelectElement;
  assert.equal(priority.value, "immediate");
  fireEvent.change(priority, { target: { value: "off" } });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalPreferences.save }));
  await screen.findByRole("status");
  const body = net.seen.find((r) => r.method === "PUT")?.body as {
    personal: { notifications: Record<string, string> };
  };
  assert.deepEqual(body.personal.notifications, { approval: "digest", urgent: "off" });
});

test("a stale profile save refreshes email consent without losing the edited briefing", async () => {
  let reads = 0;
  let writes = 0;
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
      permissions: [],
      first_admin: false,
      email_notifications_available: true,
    }),
    "GET /api/preferences": () => ({
      ...profile,
      personal: { ...personal, email: reads++ === 0, email_version: reads === 1 ? 1 : 2 },
    }),
    "PUT /api/preferences": (body) =>
      ++writes === 1
        ? new Response(JSON.stringify({ error: "email preferences changed" }), { status: 409 })
        : { ...profile, ...(body as object) },
  });
  render(<PersonalPreferences section="profile" />);
  fireEvent.change(await screen.findByLabelText(defaultCopy.personalPreferences.briefing), {
    target: { value: "Keep this edited briefing" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalPreferences.save }));
  await screen.findByText(defaultCopy.personalPreferences.emailChanged);
  assert.equal(reads, 2);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalPreferences.save }));
  await screen.findByRole("status");
  const body = net.seen.filter((r) => r.method === "PUT")[1]?.body as {
    personal: { email: boolean; email_version: number; briefing: string };
  };
  assert.equal(body.personal.email, false);
  assert.equal(body.personal.email_version, 2);
  assert.equal(body.personal.briefing, "Keep this edited briefing");
});

test("quiet hours start from the browser's time zone until a window is saved", async () => {
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  mount(false);
  const field = (await screen.findByLabelText(
    defaultCopy.personalPreferences.timezone,
  )) as HTMLInputElement;
  assert.equal(field.value, zone);
});
