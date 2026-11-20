import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ApprovalToggle, forgetApprover } from "./ApprovalToggle.tsx";
import { stubFetch } from "./dom.ts";
import { forgetConfiguredProviders } from "./ModelChain.tsx";

// A session's approval mode: one control under the composer that names the
// mode and opens a menu of the three, each with one line on what the approval
// agent does in it.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  forgetApprover();
  forgetConfiguredProviders();
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const approver = (provider: string) => ({
  agents: [{ spec: { name: "approver", provider, model: "m" }, version: 1 }],
});
const providers = { providers: [{ name: "anthropic", kind: "anthropic", declared: true }] };

const words = defaultCopy.approvals;

/** Open the menu from the control named by the current mode, and press a mode's row. */
async function pick(current: string, mode: "ask" | "auto" | "decide") {
  fireEvent.click(screen.getByRole("button", { name: current }));
  await screen.findByRole("menu");
  const row = document.querySelector<HTMLElement>(`[data-approval-option="${mode}"]`);
  assert.ok(row, `no row for ${mode}`);
  fireEvent.click(row);
}

test("the control opens a menu of the three modes, each with a short description", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("anthropic"),
    "GET /api/model-providers": () => providers,
  });
  render(<ApprovalToggle thread={{ id: "th-1" }} me="ada" />);
  // One control: nothing sits beside it, and nothing is said in a tooltip.
  assert.equal(screen.getAllByRole("button").length, 1);
  const control = screen.getByRole("button", { name: "Approvals: Ask Me" });
  assert.equal(control.getAttribute("title"), null);
  assert.equal(control.getAttribute("aria-haspopup"), "menu");
  fireEvent.click(control);
  const menu = await screen.findByRole("menu", { name: "Approvals" });
  const rows = [...menu.querySelectorAll('[role="menuitemradio"]')];
  assert.deepEqual(
    rows.map((r) => r.textContent),
    [
      `${words.ask}${words.askHint}`,
      `${words.auto}${words.autoHint}`,
      `${words.decide}${words.decideHint}`,
    ],
  );
  assert.deepEqual(
    rows.map((r) => r.getAttribute("aria-checked")),
    ["true", "false", "false"],
  );
  for (const hint of [words.askHint, words.autoHint, words.decideHint])
    assert.ok(hint.length <= 80, `"${hint}" is not short`);
  // Arrow keys move through the rows.
  (rows[0] as HTMLElement).focus();
  const focused = () => document.activeElement?.getAttribute("data-approval-option");
  fireEvent.keyDown(menu, { key: "ArrowDown" });
  assert.equal(focused(), "auto");
  fireEvent.keyDown(menu, { key: "End" });
  assert.equal(focused(), "decide");
  fireEvent.keyDown(menu, { key: "ArrowDown" });
  assert.equal(focused(), "ask");
});

test("a mode picked from the menu is saved for the session, and the menu closes", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("anthropic"),
    "GET /api/model-providers": () => providers,
    "PUT /api/threads/th-1/approval": (body) => {
      const mode = (body as { mode: string }).mode;
      return mode === "ask"
        ? { id: "th-1" }
        : { id: "th-1", approval: { mode, by: "ada", at: "2026-11-03T00:00:00Z" } };
    },
  });
  render(<ApprovalToggle thread={{ id: "th-1" }} me="ada" />);
  await pick("Approvals: Ask Me", "decide");
  await screen.findByRole("button", { name: "Approvals: Run and Answer" });
  await waitFor(() =>
    assert.equal(
      screen
        .getByRole("button", { name: "Approvals: Run and Answer" })
        .getAttribute("aria-expanded"),
      "false",
    ),
  );
  await pick("Approvals: Run and Answer", "auto");
  await screen.findByRole("button", { name: "Approvals: Run" });
  // Picking the mode already set changes nothing.
  await pick("Approvals: Run", "auto");
  await pick("Approvals: Run", "ask");
  await screen.findByRole("button", { name: "Approvals: Ask Me" });
  await waitFor(() =>
    assert.deepEqual(
      net?.seen.filter((r) => r.method === "PUT").map((r) => r.body),
      [{ mode: "decide" }, { mode: "auto" }, { mode: "ask" }],
    ),
  );
});

test("someone else's choice is named, and a refusal puts the mode back", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("anthropic"),
    "GET /api/model-providers": () => providers,
    "PUT /api/threads/th-1/approval": () =>
      new Response(JSON.stringify({ error: "nope" }), { status: 500 }),
  });
  render(
    <ApprovalToggle
      thread={{ id: "th-1", approval: { mode: "decide", by: "mira", at: "2026-11-03T00:00:00Z" } }}
      me="ada"
      nameOf={() => "Mira"}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Approvals: Run and Answer, chosen by Mira" }),
  );
  const menu = await screen.findByRole("menu");
  assert.ok(menu.textContent?.includes(words.chosenBy("Mira")));
  fireEvent.click(screen.getByRole("menuitemradio", { name: /^Ask Me/ }));
  await screen.findByRole("alert");
  assert.ok(screen.getByRole("button", { name: "Approvals: Run and Answer, chosen by Mira" }));
});

test("with no session yet the choice is held, and an approver with no model says every action waits", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("gemini"),
    "GET /api/model-providers": () => providers,
  });
  const chosen: string[] = [];
  render(<ApprovalToggle me="ada" onPending={(m) => chosen.push(m)} />);
  fireEvent.click(screen.getByRole("button", { name: "Approvals: Ask Me" }));
  const menu = await screen.findByRole("menu");
  await waitFor(() => assert.ok(menu.textContent?.includes(words.waiting)));
  fireEvent.click(screen.getByRole("menuitemradio", { name: /^Run and Answer/ }));
  assert.deepEqual(chosen, ["decide"]);
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
});
