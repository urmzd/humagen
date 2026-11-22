import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ApprovalToggle, forgetApprover } from "./ApprovalToggle.tsx";
import { stubFetch } from "./dom.ts";
import { forgetConfiguredProviders } from "./ModelChain.tsx";

// A session's approval mode: one pill under the composer, the mode's icon
// and word, named "Approval mode: Manual" or "Approval mode: Auto", that
// opens a menu of the two under a one-line question, each with its icon and
// one line on what happens in it.

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
async function pick(current: string, mode: "ask" | "auto") {
  fireEvent.click(screen.getByRole("button", { name: current }));
  await screen.findByRole("menu");
  const row = document.querySelector<HTMLElement>(`[data-approval-option="${mode}"]`);
  assert.ok(row, `no row for ${mode}`);
  fireEvent.click(row);
}

test("the control opens a menu of the two modes under a heading, each with a short description", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("anthropic"),
    "GET /api/model-providers": () => providers,
  });
  render(<ApprovalToggle thread={{ id: "th-1" }} me="ada" />);
  // One control, found by its name: the mode's icon and its word.
  assert.equal(screen.getAllByRole("button").length, 1);
  const control = screen.getByRole("button", { name: "Approval mode: Manual" });
  assert.equal(control.textContent, "Manual");
  assert.ok(control.querySelector("svg"), "the mode's icon is beside its word");
  assert.equal(control.getAttribute("aria-haspopup"), "menu");
  fireEvent.click(control);
  const menu = await screen.findByRole("menu", { name: "Approval mode" });
  // A one-line question heads the menu; then exactly two rows, each with its icon.
  assert.equal(menu.firstElementChild?.textContent, words.question);
  const rows = [...menu.querySelectorAll('[role="menuitemradio"]')];
  assert.deepEqual(
    rows.map((r) => r.textContent),
    [`Manual${words.askHint}`, `Auto${words.autoHint}`],
  );
  assert.ok(rows.every((r) => r.querySelector("svg")));
  // Auto acts without a person: its icon carries the warning color, and no row is filled,
  // so only the check says which mode is chosen.
  assert.match(rows[1]?.querySelector("svg")?.getAttribute("class") ?? "", /text-aui-warn/);
  assert.doesNotMatch(rows[0]?.querySelector("svg")?.getAttribute("class") ?? "", /text-aui-warn/);
  for (const r of rows) assert.doesNotMatch(r.className, /bg-aui-warn/);
  assert.deepEqual(
    rows.map((r) => r.getAttribute("aria-checked")),
    ["true", "false"],
  );
  for (const hint of [words.askHint, words.autoHint])
    assert.ok(hint.length <= 80, `"${hint}" is not short`);
  // Arrow keys move through the rows.
  (rows[0] as HTMLElement).focus();
  const focused = () => document.activeElement?.getAttribute("data-approval-option");
  fireEvent.keyDown(menu, { key: "ArrowDown" });
  assert.equal(focused(), "auto");
  fireEvent.keyDown(menu, { key: "ArrowDown" });
  assert.equal(focused(), "ask");
  fireEvent.keyDown(menu, { key: "End" });
  assert.equal(focused(), "auto");
  // Escape closes it; so does Tab, which would otherwise leave it open behind the focus.
  const expanded = () =>
    screen.getByRole("button", { name: "Approval mode: Manual" }).getAttribute("aria-expanded");
  fireEvent.keyDown(menu, { key: "Escape" });
  await waitFor(() => assert.equal(expanded(), "false"));
  fireEvent.click(screen.getByRole("button", { name: "Approval mode: Manual" }));
  await waitFor(() => assert.equal(expanded(), "true"));
  fireEvent.keyDown(await screen.findByRole("menu"), { key: "Tab" });
  await waitFor(() => assert.equal(expanded(), "false"));
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
  await pick("Approval mode: Manual", "auto");
  await screen.findByRole("button", { name: "Approval mode: Auto" });
  await waitFor(() =>
    assert.equal(
      screen.getByRole("button", { name: "Approval mode: Auto" }).getAttribute("aria-expanded"),
      "false",
    ),
  );
  // Picking the mode already set changes nothing.
  await pick("Approval mode: Auto", "auto");
  await pick("Approval mode: Auto", "ask");
  await screen.findByRole("button", { name: "Approval mode: Manual" });
  await waitFor(() =>
    assert.deepEqual(
      net?.seen.filter((r) => r.method === "PUT").map((r) => r.body),
      [{ mode: "auto" }, { mode: "ask" }],
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
      thread={{ id: "th-1", approval: { mode: "auto", by: "mira", at: "2026-11-03T00:00:00Z" } }}
      me="ada"
      nameOf={() => "Mira"}
    />,
  );
  const control = screen.getByRole("button", { name: "Approval mode: Auto, chosen by Mira" });
  assert.equal(control.textContent, "Auto");
  fireEvent.click(control);
  const menu = await screen.findByRole("menu");
  assert.ok(menu.textContent?.includes(words.chosenBy("Mira")));
  fireEvent.click(screen.getByRole("menuitemradio", { name: /^Manual/ }));
  await screen.findByRole("alert");
  assert.ok(screen.getByRole("button", { name: "Approval mode: Auto, chosen by Mira" }));
});

test("with no session yet the choice is held, and an approver with no model says every action waits", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("gemini"),
    "GET /api/model-providers": () => providers,
  });
  const chosen: string[] = [];
  render(<ApprovalToggle me="ada" onPending={(m) => chosen.push(m)} />);
  fireEvent.click(screen.getByRole("button", { name: "Approval mode: Manual" }));
  const menu = await screen.findByRole("menu");
  await waitFor(() => assert.ok(menu.textContent?.includes(words.waiting)));
  fireEvent.click(screen.getByRole("menuitemradio", { name: /^Auto/ }));
  assert.deepEqual(chosen, ["auto"]);
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
});

test("a conversation's pill holds its place, disabled, until the session's mode is read", () => {
  net = stubFetch({});
  render(<ApprovalToggle disabled me="ada" />);
  const control = screen.getByRole("button", { name: words.label });
  assert.equal(control.getAttribute("aria-disabled"), "true");
  assert.equal(control.textContent, "Manual");
  fireEvent.click(control);
  assert.equal(screen.queryByRole("menu"), null);
});
