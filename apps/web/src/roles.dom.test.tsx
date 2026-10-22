import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { ProjectInbox, ProjectPeople } from "./ProjectTabs.tsx";
import { projectView } from "./projects.fixtures.ts";

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

test("hiding the only announcement keeps the inbox and its switch, and Show for Me brings it back", async () => {
  let hidden = true;
  const mine = () => (hidden ? { ...news, state: "dismissed", hidden: true, unread: false } : { ...news, unread: false });
  net = stubFetch({
    // Open holds nothing for the reader while their copy is hidden; All holds it.
    "GET /api/projects/Field/inbox": (_b, q) => ({ messages: q.get("state") === "open" && hidden ? [] : [mine()], next: null }),
    "POST /api/inbox/m-1/unhide": () => {
      hidden = false;
      return mine();
    },
  });
  let opened: string | null = null;
  const member = projectView({ name: "Field", access: { role: "member", from: "Field", via: "*" }, actions: ["propose"] });
  const { rerender } = render(<ProjectInbox project={member} me="cy" signal={0} onMessage={(id) => (opened = id)} onOpen={() => {}} onChanged={() => {}} />);
  assert.ok(await screen.findByText("New announcements land here. What you hid is under Resolved."));
  assert.ok(!screen.queryByText(/Nothing has been announced/));
  assert.ok(screen.getByText("Everything"), "the kind chips stay");
  fireEvent.click(screen.getByText("Resolved"));
  fireEvent.click(await screen.findByText("Field offsite"));
  assert.ok(screen.getByText("Hidden"), "its row says it is hidden, not dismissed");
  assert.equal(opened, "m-1");
  rerender(<ProjectInbox project={member} me="cy" signal={0} message="m-1" onMessage={(id) => (opened = id)} onOpen={() => {}} onChanged={() => {}} />);
  assert.ok(await screen.findByText("Hidden for you. Others still see it."));
  assert.ok(!screen.queryByText("Withdraw"), "a member who did not post it withdraws nothing");
  fireEvent.click(screen.getByText("Show for Me"));
  await waitFor(() => assert.ok(net?.seen.some((s) => s.method === "POST" && s.path === "/api/inbox/m-1/unhide")));
  await waitFor(() => assert.ok(!screen.queryByText("Hidden for you. Others still see it.")));
  assert.ok(screen.getAllByText("Hide for Me").length > 0);
});

test("an open announcement says to hide it, as its button does, and Withdraw sits on the message", async () => {
  net = stubFetch({ "GET /api/projects/Field/inbox": () => ({ messages: [news], next: null }) });
  const owner = projectView({ name: "Field", access: { role: "owner", from: "Field", via: "ana" }, actions: ["announce", "edit"] });
  const { rerender } = render(<ProjectInbox project={owner} me="ana" signal={0} onMessage={() => {}} onOpen={() => {}} onChanged={() => {}} />);
  await screen.findAllByText("Field offsite");
  assert.ok(!screen.queryByText("Withdraw"), "nothing to withdraw until an announcement is open");
  rerender(<ProjectInbox project={owner} me="ana" signal={0} message="m-1" onMessage={() => {}} onOpen={() => {}} onChanged={() => {}} />);
  const pane = await screen.findByRole("region", { name: "Message" });
  assert.ok(within(pane).getByText("Withdraw"));
  assert.ok(within(pane).getByText("Hide for Me"));
  assert.ok(within(pane).getByText("Nothing to decide. Hide it when you're done. Others still see it."));
  assert.ok(!screen.queryByText(/Dismiss it/));
});

test("a link to an announcement the reader hid opens it in the project's inbox, with Show for Me", async () => {
  const hidden = { ...news, state: "dismissed", hidden: true, unread: false };
  net = stubFetch({
    "GET /api/projects/Field/inbox": () => ({ messages: [], next: null }),
    "GET /api/inbox/m-1": () => ({ message: hidden, history: [] }),
    "POST /api/inbox/m-1/unhide": () => ({ ...news, unread: false }),
  });
  const member = projectView({ name: "Field", access: { role: "member", from: "Field", via: "*" }, actions: ["propose"] });
  render(<ProjectInbox project={member} me="cy" signal={0} message="m-1" onMessage={() => {}} onOpen={() => {}} onChanged={() => {}} />);
  assert.ok(await screen.findByText("Hidden for you. Others still see it."));
  fireEvent.click(screen.getByText("Show for Me"));
  // Back for the reader, it is in their Open list again.
  await waitFor(() => assert.ok(screen.getAllByText("Hide for Me").length > 0));
  assert.ok(within(screen.getByRole("region", { name: "Messages" })).getByText("Field offsite"));
});
