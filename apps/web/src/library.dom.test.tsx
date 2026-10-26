import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { defaultCopy } from "@humagen/components";
import { stubFetch } from "./dom.ts";
import { MemoryLibrary } from "./MemoryLibrary.tsx";
import { SkillsLibrary } from "./SkillsLibrary.tsx";
import { PersonalPreferences } from "./PersonalPreferences.tsx";
import { NotificationPanel } from "./Notifications.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});
const me = {
  principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
  permissions: ["projects:read", "projects:write", "runs:write"],
  personal_project: "~personal",
};
const personal = {
  briefing: "",
  notifications: {},
  quiet_start: "",
  quiet_end: "",
  timezone: "UTC",
  remember: false,
  use_personal_memory_in_projects: false,
};

test("editing a skill creates an exact new draft without resubmitting revision metadata", async () => {
  const first = {
    id: "sk-one",
    scope: { kind: "personal", subject: "ada" },
    name: "brief",
    description: "Briefing",
    instructions: "Before",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  let current = first;
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [current] }),
    "GET /api/library/skills/sk-one": () => ({
      revisions: current.version === 1 ? [first] : [first, current],
    }),
    "POST /api/library/skills": (body) => {
      const value = body as { scope: object; definition: typeof first; version: number };
      assert.equal(value.version, 1);
      assert.equal(value.definition.instructions, "After");
      assert.equal("scope" in value.definition, false);
      assert.equal("version" in value.definition, false);
      assert.equal("tests" in value.definition, false);
      current = { ...first, instructions: "After", version: 2 };
      return current;
    },
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /brief.*Version 1/s }));
  fireEvent.change(await screen.findByLabelText("Instructions"), { target: { value: "After" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));
  await waitFor(() =>
    assert.equal(net?.seen.filter((request) => request.method === "POST").length, 1),
  );
});

test("personal briefing saves only writable profile fields", async () => {
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
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/preferences": () => profile,
    "PUT /api/preferences": (body) => {
      assert.deepEqual(Object.keys(body as object).sort(), [
        "accent",
        "display_name",
        "personal",
        "theme",
      ]);
      return { ...profile, ...(body as object) };
    },
  });
  let savedName = "";
  render(
    <PersonalPreferences
      section="profile"
      onSaved={(value) => {
        savedName = value.display_name;
      }}
    />,
  );
  fireEvent.change(await screen.findByLabelText(defaultCopy.personalPreferences.displayName), {
    target: { value: "Nadia" },
  });
  fireEvent.change(await screen.findByLabelText(defaultCopy.personalPreferences.briefing), {
    target: { value: "Start with the conclusion" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalPreferences.save }));
  await screen.findByRole("status");
  assert.equal(savedName, "Nadia");
  assert.equal(
    (
      net.seen.find((request) => request.method === "PUT")?.body as {
        personal: { briefing: string };
      }
    ).personal.briefing,
    "Start with the conclusion",
  );
});

test("dismissing a delivered notification acknowledges only its projection", async () => {
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/me/notifications": () => ({
      notifications: [
        {
          id: "ntf-one",
          mode: "digest",
          at: "2026-10-26T12:00:00Z",
          items: [
            {
              inbox: "in-one",
              kind: "mention",
              subject: "Review the release",
              at: "2026-10-26T11:00:00Z",
            },
          ],
        },
      ],
    }),
    "POST /api/me/notifications/ntf-one/ack": () => ({}),
  });
  render(<NotificationPanel />);
  const summary = await screen.findByText("Notifications");
  fireEvent.click(summary);
  await screen.findByText("Review the release");
  fireEvent.click(screen.getByRole("button", { name: "Dismiss Notification" }));
  await waitFor(() => assert.equal(!!screen.queryByText("Review the release"), false));
  assert.deepEqual(
    net.seen.filter((request) => request.method === "POST").map((request) => request.path),
    ["/api/me/notifications/ntf-one/ack"],
  );
});

test("memory correction retains scope and never resubmits source provenance", async () => {
  const original = {
    id: "m-one",
    text: "Ship on Tuesday",
    source: "artifact",
    status: "approved",
    kind: "decision",
    at: "2026-10-26T12:00:00Z",
    source_name: "Release notes",
    source_url: "/conversations/th-one/deliverables/art-one/provenance",
  };
  let current = original;
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/me/memory": () => ({ notes: [current] }),
    "PUT /api/me/memory/m-one": (body) => {
      const input = body as { text: string; kind: string; expires_at: null };
      assert.deepEqual(Object.keys(input).sort(), ["expires_at", "kind", "text"]);
      assert.equal(input.kind, "decision");
      current = { ...original, text: input.text };
      return current;
    },
  });
  render(<MemoryLibrary />);
  const edit = await screen.findByRole("button", { name: "Edit" });
  fireEvent.click(edit);
  assert.equal(
    (screen.getByRole("link", { name: "Open Source" }) as HTMLAnchorElement).getAttribute("href"),
    original.source_url,
  );
  fireEvent.change(screen.getByLabelText(defaultCopy.memoryLibrary.text), {
    target: { value: "Ship on Wednesday" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Correction" }));
  await screen.findByText("Ship on Wednesday");
  assert.equal(net.seen.filter((request) => request.method === "PUT").length, 1);
});
