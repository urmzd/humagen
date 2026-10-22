import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { ProjectInbox, ProjectPeople } from "./ProjectTabs.tsx";
import { globex, projectView } from "./projects.fixtures.ts";

// A hook at the top of a file runs after every test the loader holds, so it
// cleans up only after the tests here.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const news = {
  id: "m-1",
  kind: "announcement",
  subject: "Field offsite",
  from: { kind: "person", name: "ana" },
  at: "2026-10-21T09:00:00Z",
  updated_at: "2026-10-21T09:00:00Z",
  severity: "info",
  state: "open",
  unread: true,
  source: { project: "Field" },
  parts: [{ id: "p1", type: "text", state: "info", actions: [], text: "Lisbon, in May." }],
};

test("an owner changes who holds a role, and the whole list is sent with the version read", async () => {
  net = stubFetch({
    "GET /api/projects/Field/members": () => ({ version: 3, members: [{ who: "*", role: "member" }, { who: "ana", role: "owner" }], inherited: [] }),
    "PUT /api/projects/Field/members": (body) => ({ ...(body as object), inherited: [] }),
  });
  const owner = projectView({ name: "Field", access: { role: "owner", from: "Field", via: "ana" }, actions: ["edit"] });
  render(<ProjectPeople project={owner} onChanged={() => {}} />);
  await screen.findByText("ana");
  fireEvent.change(screen.getByLabelText(/subject, or role:/), { target: { value: "role:field-eng" } });
  fireEvent.click(screen.getByText("Add Person"));
  fireEvent.click(await screen.findByText("Save Members"));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "PUT")));
  const put = net?.seen.find((s) => s.method === "PUT");
  assert.deepEqual(put?.body, {
    version: 3,
    members: [
      { who: "*", role: "member" },
      { who: "ana", role: "owner" },
      { who: "role:field-eng", role: "member" },
    ],
  });
});

test("a viewer reads who holds a role, and who owns it from above, with nothing to change", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/members": () => ({ version: 1, members: [{ who: "*", role: "viewer" }], inherited: [{ who: "ana", from: "Field" }] }),
  });
  render(<ProjectPeople project={projectView({ name: "Globex", access: { role: "viewer", from: "Globex", via: "*" } })} onChanged={() => {}} />);
  await screen.findByText("Everyone in the workspace");
  assert.ok(screen.getByText("Owner from Field"));
  assert.ok(!screen.queryByText("Add Person"));
  assert.ok(!screen.queryByText("Remove"));
});

test("an owner of an account everyone sees is asked to make it members only, and choosing it drops everyone", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/members": () => ({ version: 2, members: [{ who: "*", role: "viewer" }, { who: "ana", role: "owner" }], inherited: [] }),
    "PUT /api/projects/Globex/members": (body) => ({ ...(body as object), inherited: [] }),
  });
  const owner = projectView({ name: "Globex", kind: "account", access: { role: "owner", from: "Globex", via: "ana" }, actions: ["edit"] });
  render(<ProjectPeople project={owner} onChanged={() => {}} />);
  assert.ok(await screen.findByText(/is visible to everyone\. Make it members only\?/));
  fireEvent.click(screen.getByText("Make Members Only"));
  assert.ok(await screen.findByText(/^Members only\./));
  fireEvent.click(screen.getByText("Save Members"));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "PUT")));
  assert.deepEqual(net?.seen.find((s) => s.method === "PUT")?.body, { version: 2, members: [{ who: "ana", role: "owner" }] });
});

test("an account under a project everyone owns is not offered members only, and says where to name owners", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/members": () => ({
      version: 1,
      members: [{ who: "*", role: "viewer" }, { who: "ana", role: "owner" }],
      inherited: [{ who: "*", from: "Field" }],
    }),
  });
  const owner = projectView({ name: "Globex", kind: "account", access: { role: "owner", from: "Globex", via: "ana" }, actions: ["edit"] });
  render(<ProjectPeople project={owner} onChanged={() => {}} />);
  assert.ok(await screen.findByText(/^Everyone owns Field, so everyone sees this project too\./));
  assert.ok(!screen.queryByText("Make Members Only"));
  assert.ok(!screen.queryByText(/is visible to everyone/));
  assert.equal(screen.getAllByText("Remove").length, 1, "only ana can be removed, never everyone");
});

test("a company-wide project keeps everyone, so everyone cannot be removed there", async () => {
  net = stubFetch({
    "GET /api/projects/News/members": () => ({ version: 1, members: [{ who: "*", role: "member" }, { who: "ana", role: "owner" }], inherited: [] }),
  });
  const owner = projectView({ name: "News", kind: "company", access: { role: "owner", from: "News", via: "ana" }, actions: ["edit"] });
  render(<ProjectPeople project={owner} onChanged={() => {}} />);
  await screen.findByText("Everyone in the workspace");
  assert.equal(screen.getAllByText("Remove").length, 1);
  assert.ok(screen.getByText("Everyone is at least a member of a company-wide project."));
  assert.ok(!screen.queryByText("Make Members Only"));
});

test("a project's inbox lists its announcements, and someone allowed posts one", async () => {
  net = stubFetch({
    "GET /api/projects/Field/inbox": () => ({ messages: [news], next: null }),
    "POST /api/projects/Field/announcements": () => news,
  });
  const owner = projectView({ name: "Field", access: { role: "owner", from: "Field", via: "ana" }, actions: ["announce", "edit"] });
  render(<ProjectInbox project={owner} me="ana" signal={0} onMessage={() => {}} onOpen={() => {}} onChanged={() => {}} />);
  await screen.findAllByText("Field offsite");
  fireEvent.click(screen.getByText("Post Announcement"));
  fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Holiday" } });
  fireEvent.change(screen.getByLabelText("What to say"), { target: { value: "Friday off." } });
  fireEvent.click(screen.getAllByText("Post Announcement").at(-1) as HTMLElement);
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST")));
  assert.deepEqual(net?.seen.find((s) => s.method === "POST")?.body, { subject: "Holiday", text: "Friday off.", severity: "info" });
});

test("an announcement is hidden for the reader alone, and its author withdraws it for everyone", async () => {
  net = stubFetch({
    "GET /api/projects/Field/inbox": () => ({ messages: [news], next: null }),
    "DELETE /api/projects/Field/announcements/m-1": () => undefined,
  });
  let cleared = false;
  const member = projectView({ name: "Field", access: { role: "member", from: "Field", via: "*" }, actions: ["propose"] });
  render(<ProjectInbox project={member} me="ana" signal={0} message="m-1" onMessage={() => (cleared = true)} onOpen={() => {}} onChanged={() => {}} />);
  await screen.findAllByText("Field offsite");
  assert.ok(screen.getAllByText("Hide for Me").length > 0);
  assert.ok(!screen.queryByText("Post Announcement"));
  fireEvent.click(screen.getByText("Withdraw"));
  await waitFor(() => assert.ok(cleared));
  assert.ok(net?.seen.some((s) => s.method === "DELETE" && s.path === "/api/projects/Field/announcements/m-1"));
});

test("the People tab calls each person by name, never by subject, and the owners above by their project's title", async () => {
  net = stubFetch({
    "GET /api/projects/Globex/members": () => ({
      version: 4,
      members: [
        { who: "*", role: "viewer" },
        { who: "maya.chen", role: "owner", name: "Maya Chen" },
        { who: "tailscale:pat@example.com", role: "member", name: "pat" },
      ],
      inherited: [
        { who: "maya.chen", from: "Field", name: "Maya Chen" },
        { who: "role:field-leads", from: "" },
      ],
    }),
    "PUT /api/projects/Globex/members": (body) => ({ ...(body as object), inherited: [] }),
  });
  const owner = projectView({ name: "Globex", kind: "account", path: globex.path, access: { role: "owner", from: "Globex", via: "maya.chen" }, actions: ["edit"] });
  render(<ProjectPeople project={owner} onChanged={() => {}} />);
  assert.equal((await screen.findAllByText("Maya Chen")).length, 2, "a member, and an owner from above");
  assert.ok(screen.getByText("pat"));
  assert.ok(!screen.queryByText("maya.chen"), "a subject is never shown");
  assert.ok(!screen.queryByText(/example\.com/), "nor one that holds an address");
  assert.ok(screen.getByText("Owner from Field engineering"), "the project above by its title");
  assert.ok(screen.getByText("Owner from the workspace"));
  assert.ok(screen.getByText("Everyone who holds field-leads"));
  assert.ok(screen.getByLabelText("Role: Maya Chen: Owner"), "a role is changed for a person by name");
  // A change names who holds a role by subject alone.
  fireEvent.click(screen.getAllByText("Remove").at(-1) as HTMLElement);
  fireEvent.click(await screen.findByText("Save Members"));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "PUT")));
  assert.deepEqual(net?.seen.find((s) => s.method === "PUT")?.body, {
    version: 4,
    members: [
      { who: "*", role: "viewer" },
      { who: "maya.chen", role: "owner" },
    ],
  });
});

test("an announcement shows its author by name, and the author, known by subject, still withdraws it", async () => {
  const named = { ...news, from: { kind: "person", name: "priya.nair", display: "Priya Nair" } };
  net = stubFetch({
    "GET /api/projects/Field/inbox": () => ({ messages: [named], next: null }),
    "DELETE /api/projects/Field/announcements/m-1": () => undefined,
  });
  const member = projectView({ name: "Field", access: { role: "member", from: "Field", via: "*" }, actions: ["propose"] });
  render(<ProjectInbox project={member} me="priya.nair" signal={0} message="m-1" onMessage={() => {}} onOpen={() => {}} onChanged={() => {}} />);
  await screen.findAllByText("Field offsite");
  assert.ok(screen.getAllByText("Priya Nair").length > 0);
  assert.ok(!screen.queryByText("priya.nair"), "the subject is never shown");
  assert.ok(screen.getByText("Withdraw"), "its author withdraws it");
});
