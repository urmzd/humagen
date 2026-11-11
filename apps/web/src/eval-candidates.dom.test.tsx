import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { EvalCandidate, SuiteView } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { EvalCandidates } from "./EvalCandidates.tsx";

const candidate = (id: string, input: string, reason = "wrong"): EvalCandidate => ({
  id,
  run: `r-${id}`,
  agent: "helper",
  project: "",
  input,
  reason,
  comment: "It named the old report",
  at: "2026-11-10T00:00:00Z",
});
const suite: SuiteView = {
  name: "smoke",
  target: "agent",
  subject: "helper",
  cases: [{ id: "a", input: "hello", checks: [{ kind: "contains", value: "x" }] }],
  runs: [],
};

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

test("a reviewer adds a rated question to a suite with their own checks, or dismisses it", async () => {
  let changed = 0;
  net = stubFetch({
    "GET /api/eval-candidates": (_body, query) =>
      query.get("after")
        ? { candidates: [candidate("c3", "Where is the third report?")], next: "" }
        : {
            candidates: [
              candidate("c1", "Where is the first report?"),
              candidate("c2", "Why so slow?", "slow"),
            ],
            next: "page-2",
          },
    "POST /api/eval-candidates/c1/accept": () => suite,
    "DELETE /api/eval-candidates/c2": () => undefined,
  });
  render(<EvalCandidates agent="helper" suites={[suite]} canWrite onChanged={() => changed++} />);

  // Each row says what was asked, why it was rated down and which agent answered.
  await screen.findByText("Where is the first report?");
  assert.ok(screen.getAllByText("Wrong").length > 0);
  assert.ok(screen.getAllByText(/It named the old report/).length > 0);
  assert.ok(screen.getAllByText("helper").length > 0);
  assert.equal(net.seen[0]?.path, "/api/eval-candidates?agent=helper&limit=20");

  // The drafted check is empty, so nothing is added until the reviewer writes it.
  fireEvent.click(screen.getAllByRole("button", { name: "Add to Suite" })[0] as HTMLElement);
  const confirm = screen.getByRole("button", { name: "Add Case" }) as HTMLButtonElement;
  assert.equal(confirm.disabled, true);
  fireEvent.change(screen.getByLabelText("Value"), { target: { value: "the first report" } });
  assert.equal(confirm.disabled, false);
  fireEvent.click(confirm);
  await waitFor(() => assert.ok(!screen.queryByText("Where is the first report?")));
  const accepted = net.seen.find((r) => r.method === "POST");
  // Only the suite and the checks are sent: the server holds the question,
  // and no answer is on the page to send.
  assert.deepEqual(accepted?.body, {
    suite: "smoke",
    checks: [{ kind: "contains", value: "the first report" }],
  });
  assert.equal(changed, 1);

  fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
  await waitFor(() => assert.ok(!screen.queryByText("Why so slow?")));
  assert.equal(changed, 1);

  fireEvent.click(screen.getByRole("button", { name: "Load More" }));
  await screen.findByText("Where is the third report?");
  assert.ok(net.seen.some((r) => r.path.includes("after=page-2")));
});

test("a reader without evals:write sees the queue and no actions, and an empty queue shows nothing", async () => {
  net = stubFetch({
    "GET /api/eval-candidates": (_body, query) =>
      query.get("agent") === "helper"
        ? { candidates: [candidate("c1", "Where is the first report?")], next: "" }
        : { candidates: [], next: "" },
  });
  const { unmount } = render(
    <EvalCandidates agent="helper" suites={[suite]} canWrite={false} onChanged={() => {}} />,
  );
  await screen.findByText("Where is the first report?");
  assert.ok(!screen.queryByRole("button", { name: "Add to Suite" }));
  assert.ok(!screen.queryByRole("button", { name: "Dismiss" }));
  unmount();

  render(<EvalCandidates agent="quiet" suites={[]} canWrite onChanged={() => {}} />);
  await waitFor(() => assert.ok(!screen.queryByTestId("skeleton")));
  assert.ok(!screen.queryByText("From feedback"));
});
