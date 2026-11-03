import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ApprovalModePicker } from "./ApprovalModePicker.tsx";
import { stubFetch } from "./dom.ts";

// DES-057: a conversation's approval mode, a text control under the composer.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

test("Approve for Me is chosen from a text control and saved for the conversation", async () => {
  net = stubFetch({
    "PUT /api/threads/th-1/approval": (body) =>
      (body as { mode: string }).mode === "auto"
        ? { id: "th-1", approval: { mode: "auto", by: "ada", at: "2026-11-03T00:00:00Z" } }
        : { id: "th-1" },
  });
  render(<ApprovalModePicker thread={{ id: "th-1" }} me="ada" />);
  const control = screen.getByRole("button", { name: "Approvals: Default" });
  fireEvent.click(control);
  const auto = await screen.findByRole("menuitemradio", { name: /^Approve for Me/ });
  assert.match(auto.textContent ?? "", /Budget requests/);
  fireEvent.click(auto);
  await screen.findByRole("button", { name: "Approvals: Approve for Me" });
  await waitFor(() =>
    assert.deepEqual(
      net?.seen.filter((r) => r.method === "PUT").map((r) => r.body),
      [{ mode: "auto" }],
    ),
  );
});

test("someone else's choice is named, and a refusal puts the mode back", async () => {
  net = stubFetch({
    "PUT /api/threads/th-1/approval": () =>
      new Response(JSON.stringify({ error: "nope" }), { status: 500 }),
  });
  render(
    <ApprovalModePicker
      thread={{ id: "th-1", approval: { mode: "auto", by: "mira", at: "2026-11-03T00:00:00Z" } }}
      me="ada"
      nameOf={() => "Mira"}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Approvals: Approved for Mira" }));
  fireEvent.click(await screen.findByRole("menuitemradio", { name: /^Default/ }));
  await screen.findByRole("alert");
  assert.ok(screen.getByRole("button", { name: "Approvals: Approved for Mira" }));
});
