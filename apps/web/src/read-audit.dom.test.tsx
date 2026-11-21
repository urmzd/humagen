import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { Audit } from "./Audit.tsx";
import { stubFetch } from "./dom.ts";
import { ReadAudit } from "./ReadAudit.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});
test("recorded reads show the reader, place and route", async () => {
  net = stubFetch({
    "GET /api/read-audit": () => ({
      records: [
        {
          subject: "ana",
          project: "Account",
          thread: "private-1",
          route: "GET /api/threads/{id}",
          at: "2026-10-22T10:00:00Z",
        },
      ],
    }),
  });
  render(<ReadAudit />);
  await screen.findByText("ana");
  assert.ok(screen.getByText("Account / private-1"));
  assert.ok(screen.getByText("GET /api/threads/{id}"));
});
test("the audit names an empty window", async () => {
  net = stubFetch({ "GET /api/read-audit": () => ({ records: [] }) });
  render(<ReadAudit />);
  await screen.findByText(defaultCopy.readAudit.empty);
});

test("Audit loads break-glass records only when authorized", async () => {
  net = stubFetch({
    "GET /api/audit": () => ({ records: [] }),
    "GET /api/read-audit": () => ({ records: [] }),
  });
  render(<Audit />);
  await screen.findByText(defaultCopy.audit.empty);
  assert.ok(!net.seen.some((request) => request.path === "/api/read-audit"));
  assert.equal(screen.queryByRole("heading", { name: defaultCopy.readAudit.title }), null);

  cleanup();
  render(<Audit reads />);
  await screen.findByText(defaultCopy.readAudit.empty);
  // An empty section is headed by its name alone: a count is hidden at zero.
  assert.ok(screen.getByRole("heading", { name: defaultCopy.audit.changes }));
  assert.ok(screen.getByRole("heading", { name: defaultCopy.readAudit.title }));
  assert.equal(screen.queryByText("0"), null);
  assert.equal(screen.queryByText(defaultCopy.audit.limited), null);
});

test("a section as long as the server's cap says so and is not counted", async () => {
  const at = Date.parse("2026-10-22T10:00:00Z");
  net = stubFetch({
    "GET /api/audit": () => ({
      records: Array.from({ length: 100 }, (_, n) => ({
        id: `audit-${n}`,
        actor: "ana",
        at: new Date(at - n * 60_000).toISOString(),
        action: "memory.edit",
        resource: "memory",
        resource_id: `note-${n}`,
        scope: "personal",
        outcome: "succeeded",
      })),
    }),
    "GET /api/read-audit": () => ({
      records: Array.from({ length: 100 }, (_, n) => ({
        subject: "governor",
        project: "Secret",
        route: "GET /api/projects/{name}",
        at: new Date(at - n * 60_000).toISOString(),
      })),
    }),
  });
  render(<Audit reads />);
  await waitFor(() => assert.equal(screen.getAllByText(defaultCopy.audit.limited).length, 2));
  assert.ok(screen.getByRole("heading", { name: defaultCopy.audit.changes }));
  assert.ok(screen.getByRole("heading", { name: defaultCopy.readAudit.title }));
  assert.equal(screen.queryByText("100"), null);
});

const governorRead = {
  subject: "governor",
  project: "Secret",
  route: "GET /api/projects/{name}",
  at: "2026-10-22T10:00:00Z",
};
const changesWithTheRead = [
  {
    id: "read-1",
    actor: "governor",
    at: "2026-10-22T10:00:00Z",
    action: "project.break_glass_read",
    resource: "project",
    resource_id: "Secret",
    scope: "project",
    project: "Secret",
    route: "GET /api/projects/{name}",
    rule: "projects:govern",
    outcome: "succeeded",
  },
  {
    id: "audit-1",
    actor: "ana",
    at: "2026-10-22T09:00:00Z",
    action: "project.created",
    resource: "project",
    resource_id: "Secret",
    scope: "project",
    project: "Secret",
    outcome: "succeeded",
  },
];

test("a break-glass read is listed once when both sections show", async () => {
  net = stubFetch({
    "GET /api/audit": () => ({ records: changesWithTheRead }),
    "GET /api/read-audit": () => ({ records: [governorRead] }),
  });
  render(<Audit reads />);
  await screen.findByText("Project created");
  await waitFor(() => assert.equal(screen.queryByText("Project break glass read"), null));
  // The reader is named by the reads section alone, and the changes hold the one change.
  assert.equal(screen.getAllByText("governor").length, 1);
  assert.equal(screen.getAllByText("GET /api/projects/{name}").length, 1);
  // Each heading carries its own count, read after a pause: "Workspace changes, 1".
  assert.ok(screen.getByRole("heading", { name: `${defaultCopy.readAudit.title} , 1` }));
  assert.ok(screen.getByRole("heading", { name: `${defaultCopy.audit.changes} , 1` }));
  const changes = screen.getByRole("region", { name: `${defaultCopy.audit.changes} , 1` });
  assert.equal(within(changes).getAllByRole("listitem").length, 1);
});

test("a break-glass read the reads section does not list stays among the changes", async () => {
  net = stubFetch({
    "GET /api/audit": () => ({ records: changesWithTheRead }),
    "GET /api/read-audit": () => ({ records: [] }),
  });
  render(<Audit reads />);
  await screen.findByText(defaultCopy.readAudit.empty);
  await screen.findByText("Project break glass read");
  assert.ok(screen.getByText("Project created"));

  // Without the reads section nothing is taken out.
  cleanup();
  render(<Audit />);
  await screen.findByText("Project break glass read");
});

test("when the server's 100 records are all reads listed above, the page does not say there are no changes", async () => {
  const at = Date.parse("2026-10-22T10:00:00Z");
  const reads = Array.from({ length: 100 }, (_, n) => ({
    subject: "governor",
    project: "Secret",
    route: "GET /api/projects/{name}",
    at: new Date(at - n * 60_000).toISOString(),
  }));
  net = stubFetch({
    "GET /api/audit": () => ({
      records: reads.map((read, n) => ({
        id: `read-${n}`,
        actor: read.subject,
        at: read.at,
        action: "project.break_glass_read",
        resource: "project",
        resource_id: read.project,
        scope: "project",
        project: read.project,
        route: read.route,
        rule: "projects:govern",
        outcome: "succeeded",
      })),
    }),
    "GET /api/read-audit": () => ({ records: reads }),
  });
  render(<Audit reads />);
  await screen.findByText(defaultCopy.audit.hiddenByReads);
  assert.equal(screen.queryByText(defaultCopy.audit.empty), null);
  // The line stands alone: the hint that a list is capped is not repeated over no list.
  const changes = screen.getByRole("region", { name: defaultCopy.audit.changes });
  assert.equal(within(changes).queryByText(defaultCopy.audit.limited), null);
  assert.equal(within(changes).queryByText(defaultCopy.audit.limitedByReads), null);
});

test("a capped list shortened by reads says why it is short", async () => {
  const at = Date.parse("2026-10-22T10:00:00Z");
  const reads = Array.from({ length: 97 }, (_, n) => ({
    subject: "governor",
    project: "Secret",
    route: "GET /api/projects/{name}",
    at: new Date(at - n * 60_000).toISOString(),
  }));
  net = stubFetch({
    "GET /api/audit": () => ({
      records: [
        ...reads.map((read, n) => ({
          id: `read-${n}`,
          actor: read.subject,
          at: read.at,
          action: "project.break_glass_read",
          resource: "project",
          resource_id: read.project,
          scope: "project",
          project: read.project,
          route: read.route,
          rule: "projects:govern",
          outcome: "succeeded",
        })),
        ...Array.from({ length: 3 }, (_, n) => ({
          id: `audit-${n}`,
          actor: "ana",
          at: new Date(at - (200 + n) * 60_000).toISOString(),
          action: "memory.edit",
          resource: "memory",
          resource_id: `note-${n}`,
          scope: "personal",
          outcome: "succeeded",
        })),
      ],
    }),
    "GET /api/read-audit": () => ({ records: reads }),
  });
  render(<Audit reads />);
  const changes = await screen.findByRole("region", { name: defaultCopy.audit.changes });
  await within(changes).findByText(defaultCopy.audit.limitedByReads);
  assert.equal(within(changes).getAllByRole("listitem").length, 3);
  assert.equal(within(changes).queryByText(defaultCopy.audit.limited), null);
});

test("with Projects chosen the capped lines do not send a person to choose a domain", async () => {
  const at = Date.parse("2026-10-22T10:00:00Z");
  const reads = Array.from({ length: 100 }, (_, n) => ({
    subject: "governor",
    project: "Secret",
    route: "GET /api/projects/{name}",
    at: new Date(at - n * 60_000).toISOString(),
  }));
  const asChanges = reads.map((read, n) => ({
    id: `read-${n}`,
    actor: read.subject,
    at: read.at,
    action: "project.break_glass_read",
    resource: "project",
    resource_id: read.project,
    scope: "project",
    project: read.project,
    route: read.route,
    rule: "projects:govern",
    outcome: "succeeded",
  }));
  const created = {
    id: "audit-1",
    actor: "ana",
    at: new Date(at - 200 * 60_000).toISOString(),
    action: "project.created",
    resource: "project",
    resource_id: "Secret",
    scope: "project",
    project: "Secret",
    outcome: "succeeded",
  };
  let records = [...asChanges.slice(0, 99), created];
  net = stubFetch({
    "GET /api/audit": () => ({ records }),
    "GET /api/read-audit": () => ({ records: reads }),
  });
  render(<Audit reads />);
  const copy = defaultCopy.audit;
  // With every domain on view, another domain is a way to more changes.
  await screen.findByText(copy.limitedByReads);
  fireEvent.change(screen.getByLabelText(copy.domain), { target: { value: "project" } });
  fireEvent.click(screen.getByRole("button", { name: copy.apply }));
  // Reads are project records: with Projects chosen only a shorter window helps.
  await screen.findByText(copy.limitedByProjectReads);
  assert.equal(screen.queryByText(copy.limitedByReads), null);
  assert.ok(!copy.limitedByProjectReads.includes("domain"));

  records = asChanges;
  fireEvent.click(screen.getByRole("button", { name: copy.refresh }));
  await screen.findByText(copy.hiddenByProjectReads);
  assert.equal(screen.queryByText(copy.hiddenByReads), null);
  assert.ok(!copy.hiddenByProjectReads.includes("domain"));
});

test("Filter unfolds the same fields, says how many were applied and takes focus back", async () => {
  const queries: URLSearchParams[] = [];
  net = stubFetch({
    "GET /api/audit": (_body, query) => {
      queries.push(query);
      return { records: [] };
    },
  });
  render(<Audit />);
  const copy = defaultCopy.audit;
  await screen.findByText(copy.empty);
  const filter = screen.getByRole("button", { name: copy.filter });
  const fields = screen.getByLabelText(copy.actor).closest("form");
  assert.ok(fields);
  assert.equal(filter.getAttribute("aria-controls"), fields.id);
  // Folded in a column as narrow as a phone's; the stylesheet shows the row in a wider one.
  assert.equal(filter.getAttribute("aria-expanded"), "false");
  assert.ok(fields.classList.contains("hidden"));
  assert.ok(fields.classList.contains("@md:grid"));
  assert.ok(filter.parentElement?.classList.contains("@md:hidden"));

  fireEvent.click(filter);
  assert.equal(filter.getAttribute("aria-expanded"), "true");
  assert.ok(!fields.classList.contains("hidden"));
  fireEvent.change(screen.getByLabelText(copy.actor), { target: { value: "ana" } });
  fireEvent.change(screen.getByLabelText(copy.domain), { target: { value: "memory" } });
  fireEvent.click(screen.getByRole("button", { name: copy.apply }));
  await waitFor(() => assert.equal(queries.length, 2));
  assert.equal(queries[1]?.get("actor"), "ana");
  // Applied, the fields fold again, focus is back on Filter and it counts the two set.
  assert.equal(filter.getAttribute("aria-expanded"), "false");
  assert.ok(fields.classList.contains("hidden"));
  assert.ok(document.activeElement === filter);
  assert.match(filter.textContent ?? "", /^Filter\s*,\s*2$/);

  // A window that is not valid keeps the fields open, where the reason shows.
  fireEvent.click(filter);
  fireEvent.click(screen.getByRole("button", { name: copy.dateRange }));
  fireEvent.change(await screen.findByLabelText(copy.from), { target: { value: "2026-10-20" } });
  fireEvent.change(screen.getByLabelText(copy.to), { target: { value: "2026-10-19" } });
  fireEvent.click(screen.getByRole("button", { name: copy.apply }));
  await screen.findByText(copy.invalidRange);
  assert.equal(filter.getAttribute("aria-expanded"), "true");
});

test("folded, Refresh and Export CSV are under More", async () => {
  net = stubFetch({ "GET /api/audit": () => ({ records: [] }) });
  render(<Audit />);
  const copy = defaultCopy.audit;
  await screen.findByText(copy.empty);
  const more = screen.getByRole("button", { name: defaultCopy.more });
  // The folded header holds two controls: Filter and More.
  assert.equal(more.parentElement?.parentElement?.querySelectorAll(":scope > button").length, 1);
  fireEvent.click(more);
  const menu = screen.getByRole("menu", { name: defaultCopy.more });
  assert.deepEqual(
    within(menu)
      .getAllByRole("menuitem")
      .map((item) => item.textContent),
    [copy.refresh, copy.export],
  );
  // Nothing to export yet, as on the wide header.
  const exportRow = within(menu).getByRole("menuitem", { name: copy.export }) as HTMLButtonElement;
  assert.equal(exportRow.disabled, true);
  fireEvent.click(within(menu).getByRole("menuitem", { name: copy.refresh }));
  await waitFor(() =>
    assert.equal(net?.seen.filter((request) => request.path.startsWith("/api/audit")).length, 2),
  );
  assert.equal(screen.queryByRole("menu"), null);
});

test("Refresh reads the break-glass reads again with the changes", async () => {
  net = stubFetch({
    "GET /api/audit": () => ({ records: changesWithTheRead }),
    "GET /api/read-audit": () => ({ records: [governorRead] }),
  });
  render(<Audit reads />);
  await screen.findByText("Project created");
  const count = (path: string) => net?.seen.filter((request) => request.path === path).length;
  assert.equal(count("/api/read-audit"), 1);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.refresh }));
  await waitFor(() => assert.equal(count("/api/read-audit"), 2));
  await waitFor(() => assert.equal(count("/api/audit"), 2));
});

test("a failed refresh of the reads says so and stops leaving them out of the changes", async () => {
  let refuse = false;
  net = stubFetch({
    "GET /api/audit": () => ({ records: changesWithTheRead }),
    "GET /api/read-audit": () =>
      refuse ? new Response(null, { status: 500 }) : { records: [governorRead] },
  });
  render(<Audit reads />);
  await screen.findByText("Project created");
  await waitFor(() => assert.equal(screen.queryByText("Project break glass read"), null));
  refuse = true;
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.refresh }));
  await screen.findByText(defaultCopy.readAudit.failed);
  // The list read earlier is not shown as if current: its heading counts nothing.
  assert.ok(screen.getByRole("heading", { name: defaultCopy.readAudit.title }));
  // The read is among the changes again, so it is still on the page.
  await screen.findByText("Project break glass read");
  assert.ok(screen.getByText("Project created"));
});

test("a failed read says the audit could not load", async () => {
  net = stubFetch({});
  render(<ReadAudit />);
  await screen.findByText(defaultCopy.readAudit.failed);
});

test("Audit applies local calendar times as ISO filters only on submission", async () => {
  const queries: URLSearchParams[] = [];
  net = stubFetch({
    "GET /api/audit": (_body, query) => {
      queries.push(query);
      return { records: [] };
    },
  });
  render(<Audit />);
  await screen.findByText(defaultCopy.audit.empty);
  fireEvent.change(screen.getByLabelText(defaultCopy.audit.actor), { target: { value: " ana " } });
  fireEvent.change(screen.getByLabelText(defaultCopy.audit.domain), {
    target: { value: "memory" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.dateRange }));
  fireEvent.change(await screen.findByLabelText(defaultCopy.audit.from), {
    target: { value: "2026-10-20" },
  });
  fireEvent.change(screen.getByLabelText(defaultCopy.audit.to), {
    target: { value: "2026-10-21" },
  });
  fireEvent.change(screen.getByLabelText(`${defaultCopy.audit.from} ${defaultCopy.audit.time}`), {
    target: { value: "09:30" },
  });
  fireEvent.change(screen.getByLabelText(`${defaultCopy.audit.to} ${defaultCopy.audit.time}`), {
    target: { value: "17:00" },
  });
  assert.equal(queries.length, 1);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.apply }));
  await waitFor(() => assert.equal(queries.length, 2));
  const applied = queries[1];
  assert.ok(applied);
  assert.equal(applied.get("actor"), "ana");
  assert.equal(applied.get("resource"), "memory");
  assert.equal(applied.get("from"), new Date(2026, 9, 20, 9, 30).toISOString());
  assert.equal(applied.get("to"), new Date(2026, 9, 21, 17).toISOString());
});

test("Audit rejects reversed or overlong windows without fetching", async () => {
  net = stubFetch({ "GET /api/audit": () => ({ records: [] }) });
  render(<Audit />);
  await screen.findByText(defaultCopy.audit.empty);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.dateRange }));
  fireEvent.change(await screen.findByLabelText(defaultCopy.audit.from), {
    target: { value: "2026-10-20" },
  });
  fireEvent.change(screen.getByLabelText(defaultCopy.audit.to), {
    target: { value: "2026-10-19" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.apply }));
  await screen.findByText(defaultCopy.audit.invalidRange);
  assert.equal(net.seen.length, 1);
  fireEvent.change(screen.getByLabelText(defaultCopy.audit.to), {
    target: { value: "2026-12-19" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.apply }));
  await screen.findByText(defaultCopy.audit.invalidRange);
  assert.equal(net.seen.length, 1);
});

test("Audit retains event evidence and disables export after a refused refresh", async () => {
  let refuse = false;
  net = stubFetch({
    "GET /api/audit": () =>
      refuse
        ? new Response(null, { status: 403 })
        : {
            records: [
              {
                id: "audit-1",
                actor: "ana",
                at: "2026-10-22T10:00:00Z",
                action: "memory.edit",
                resource: "memory",
                resource_id: "note-1",
                scope: "personal",
                outcome: "succeeded",
                before: { text: "Old" },
                after: { text: "Revised" },
              },
            ],
          },
  });
  render(<Audit />);
  await screen.findByText("Memory edit");
  assert.ok(screen.getByText(defaultCopy.audit.outcomes.succeeded));
  fireEvent.click(screen.getByText(defaultCopy.audit.details));
  fireEvent.click(screen.getByText(defaultCopy.audit.before));
  assert.ok(screen.getByText("Old"));
  fireEvent.click(screen.getByText(defaultCopy.audit.after));
  assert.ok(screen.getByText("Revised"));
  assert.equal(
    (screen.getByRole("button", { name: defaultCopy.audit.export }) as HTMLButtonElement).disabled,
    false,
  );
  refuse = true;
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.audit.refresh }));
  await screen.findByText(defaultCopy.audit.failed);
  assert.equal(
    (screen.getByRole("button", { name: defaultCopy.audit.export }) as HTMLButtonElement).disabled,
    true,
  );
});
