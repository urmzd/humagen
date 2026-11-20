import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ApprovalToggle, forgetApprover } from "./ApprovalToggle.tsx";
import { stubFetch } from "./dom.ts";
import { forgetConfiguredProviders } from "./ModelChain.tsx";

// A session's approval mode: a two-state icon button under the composer,
// always shown, ✋ Ask Me or ⏩ Run, with what it does in its tooltip.

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

test("Run is one press away and saved for the session", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("anthropic"),
    "GET /api/model-providers": () => providers,
    "PUT /api/threads/th-1/approval": (body) =>
      (body as { mode: string }).mode === "auto"
        ? { id: "th-1", approval: { mode: "auto", by: "ada", at: "2026-11-03T00:00:00Z" } }
        : { id: "th-1" },
  });
  render(<ApprovalToggle thread={{ id: "th-1" }} me="ada" />);
  const ask = screen.getByRole("button", { name: "Approvals: Ask Me" });
  assert.match(ask.getAttribute("title") ?? "", /waits for you/);
  fireEvent.click(ask);
  const run = await screen.findByRole("button", { name: "Approvals: Run" });
  await waitFor(() => assert.match(run.getAttribute("title") ?? "", /approval agent decides/));
  await waitFor(() =>
    assert.deepEqual(
      net?.seen.filter((r) => r.method === "PUT").map((r) => r.body),
      [{ mode: "auto" }],
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
  fireEvent.click(screen.getByRole("button", { name: "Approvals: Run, chosen by Mira" }));
  await screen.findByRole("alert");
  assert.ok(screen.getByRole("button", { name: "Approvals: Run, chosen by Mira" }));
});

test("with no session yet the choice is held, and an approver with no model says every action waits", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("gemini"),
    "GET /api/model-providers": () => providers,
  });
  const chosen: string[] = [];
  const { rerender } = render(<ApprovalToggle me="ada" onPending={(m) => chosen.push(m)} />);
  fireEvent.click(screen.getByRole("button", { name: "Approvals: Ask Me" }));
  assert.deepEqual(chosen, ["auto"]);
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
  rerender(<ApprovalToggle me="ada" pending="auto" onPending={(m) => chosen.push(m)} />);
  const run = screen.getByRole("button", { name: "Approvals: Run" });
  await waitFor(() =>
    assert.match(run.getAttribute("title") ?? "", /every action waits for a person/),
  );
});

test("Answer Questions is a secondary option of Run, off until pressed and saved as decide", async () => {
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
  // Ask Me offers no answering: it is Run's to add.
  assert.equal(screen.queryByRole("button", { name: "Answer Questions" }), null);
  fireEvent.click(screen.getByRole("button", { name: "Approvals: Ask Me" }));
  const answers = await screen.findByRole("button", { name: "Answer Questions" });
  assert.equal(answers.getAttribute("aria-pressed"), "false");
  fireEvent.click(answers);
  const decide = await screen.findByRole("button", { name: "Approvals: Run and Answer" });
  await waitFor(() => assert.match(decide.getAttribute("title") ?? "", /also answers a question/));
  assert.equal(
    screen.getByRole("button", { name: "Answer Questions" }).getAttribute("aria-pressed"),
    "true",
  );
  // Pressed again it goes back to Run; Ask Me from Run and Answer turns both off.
  fireEvent.click(screen.getByRole("button", { name: "Answer Questions" }));
  await screen.findByRole("button", { name: "Approvals: Run" });
  fireEvent.click(screen.getByRole("button", { name: "Answer Questions" }));
  fireEvent.click(await screen.findByRole("button", { name: "Approvals: Run and Answer" }));
  await screen.findByRole("button", { name: "Approvals: Ask Me" });
  assert.equal(screen.queryByRole("button", { name: "Answer Questions" }), null);
  await waitFor(() =>
    assert.deepEqual(
      net?.seen.filter((r) => r.method === "PUT").map((r) => r.body),
      [{ mode: "auto" }, { mode: "decide" }, { mode: "auto" }, { mode: "decide" }, { mode: "ask" }],
    ),
  );
});

test("someone else's Run and Answer is named, and with no session the choice is held", async () => {
  net = stubFetch({
    "GET /api/agents": () => approver("anthropic"),
    "GET /api/model-providers": () => providers,
  });
  const { unmount } = render(
    <ApprovalToggle
      thread={{ id: "th-1", approval: { mode: "decide", by: "mira", at: "2026-11-03T00:00:00Z" } }}
      me="ada"
      nameOf={() => "Mira"}
    />,
  );
  assert.ok(screen.getByRole("button", { name: "Approvals: Run and Answer, chosen by Mira" }));
  assert.equal(
    screen.getByRole("button", { name: "Answer Questions" }).getAttribute("aria-pressed"),
    "true",
  );
  unmount();
  const chosen: string[] = [];
  render(<ApprovalToggle me="ada" pending="auto" onPending={(m) => chosen.push(m)} />);
  fireEvent.click(screen.getByRole("button", { name: "Answer Questions" }));
  assert.deepEqual(chosen, ["decide"]);
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
});
