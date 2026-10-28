import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
