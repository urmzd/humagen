import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { Toaster } from "@humagen/render-web/ui/sonner";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Corpus, EvalRun, SuiteView } from "./api.ts";
import { CorpusPage } from "./Corpora.tsx";
import { CorpusEvals } from "./CorpusEvals.tsx";
import { stubFetch } from "./dom.ts";

const words = defaultCopy.corpora.evals;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const at = "2026-11-07T12:00:00Z";
const me = (...permissions: string[]) => ({
  principal: { subject: "ada", tenant: "acme", kind: "user" },
  permissions,
});
const editor = () => me("evals:read", "evals:write");
const run = (over: Partial<EvalRun> = {}): EvalRun => ({
  id: "run-1",
  suite: "hr-policies-retrieval",
  target: "corpus",
  subject: "hr-policies",
  status: "passed",
  passed: 2,
  total: 2,
  pass_rate: 100,
  threshold: 100,
  metrics: { hit_rate: 1, mrr: 0.75, recall: 0.8, precision: 0.4, ndcg: 0.66 },
  results: [
    { case_id: "leave", input: "parental leave", passed: true, rank: 2, duration_ms: 12 },
    { case_id: "expenses", input: "expense limits", passed: true, rank: 1, duration_ms: 9 },
  ],
  started_at: at,
  ended_at: at,
  ...over,
});
const suite = (over: Partial<SuiteView> = {}): SuiteView => ({
  name: "hr-policies-retrieval",
  target: "corpus",
  subject: "hr-policies",
  cases: [
    { id: "leave", input: "parental leave", expect: ["Leave policy"] },
    { id: "expenses", input: "expense limits", expect: ["Expenses", "Travel"] },
  ],
  runs: [],
  ...over,
});
const page = () => (
  <>
    <Toaster />
    <CorpusEvals corpus="hr-policies" />
  </>
);
/** A case as it is sent, without the id the page makes up for it. */
const sent = (cases: { id: string; input: string; expect?: string[] }[]) =>
  cases.map(({ id: _id, ...rest }) => rest);

test("a corpus with no suite is a title and Add Case, and the first case creates its suite", async () => {
  let suites: SuiteView[] = [];
  net = stubFetch({
    "GET /api/me": editor,
    // A suite of another corpus, or of a source of the same name, is not this corpus's.
    "GET /api/evals": () => ({
      suites: [
        ...suites,
        suite({ name: "other", subject: "finance" }),
        suite({ name: "source", target: "knowledge" }),
      ],
    }),
    "POST /api/evals": (body) => {
      suites = [{ ...(body as SuiteView), runs: [] }];
      return body;
    },
  });
  render(page());
  assert.ok(await screen.findByText(words.none));
  assert.equal(screen.queryAllByText("other").length, 0);
  assert.equal(screen.queryAllByText("source").length, 0);
  fireEvent.click(screen.getByRole("button", { name: words.addCase }));
  const form = screen.getByRole("form", { name: words.newCase });
  fireEvent.change(within(form).getByLabelText(words.query, { exact: false }), {
    target: { value: " parental leave " },
  });
  fireEvent.change(within(form).getByLabelText(words.expect, { exact: false }), {
    target: { value: "Leave policy, Handbook ," },
  });
  fireEvent.click(within(form).getByRole("button", { name: words.save }));
  await screen.findByRole("listitem", { name: "parental leave" });
  const body = net.seen.find((r) => r.method === "POST")?.body as SuiteView;
  assert.deepEqual(
    { ...body, cases: sent(body.cases) },
    {
      name: "hr-policies-retrieval",
      target: "corpus",
      subject: "hr-policies",
      cases: [{ input: "parental leave", expect: ["Leave policy", "Handbook"] }],
    },
  );
  assert.ok(body.cases[0]?.id);
  assert.equal(screen.queryAllByText(words.none).length, 0);
});

test("adding a case replaces the suite with its cases and the new one, without its runs", async () => {
  let current = suite({ runs: [run()] });
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: [current] }),
    "PUT /api/evals/hr-policies-retrieval": (body) => {
      current = { ...(body as SuiteView), runs: current.runs };
      return body;
    },
  });
  render(page());
  const section = await screen.findByRole("region", { name: /hr-policies-retrieval/ });
  fireEvent.click(within(section).getByRole("button", { name: words.addCase }));
  const form = within(section).getByRole("form", { name: words.newCase });
  fireEvent.change(within(form).getByLabelText(words.query, { exact: false }), {
    target: { value: "sick days" },
  });
  fireEvent.change(within(form).getByLabelText(words.expect, { exact: false }), {
    target: { value: "Absence" },
  });
  fireEvent.click(within(form).getByRole("button", { name: words.save }));
  await screen.findByRole("listitem", { name: "sick days" });
  const body = net.seen.find((r) => r.method === "PUT")?.body as SuiteView;
  assert.equal("runs" in body, false);
  assert.equal(body.name, "hr-policies-retrieval");
  assert.equal(body.target, "corpus");
  assert.equal(body.subject, "hr-policies");
  assert.deepEqual(sent(body.cases).at(-1), { input: "sick days", expect: ["Absence"] });
  assert.deepEqual(
    body.cases.slice(0, 2),
    suite().cases,
    "the cases already there are sent back as they were",
  );
});

test("a refused case keeps the form open under the server's reason", async () => {
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: [] }),
    "POST /api/evals": () =>
      new Response(JSON.stringify({ error: "a suite of that name exists" }), { status: 409 }),
  });
  render(page());
  fireEvent.click(await screen.findByRole("button", { name: words.addCase }));
  const form = screen.getByRole("form", { name: words.newCase });
  fireEvent.change(within(form).getByLabelText(words.query, { exact: false }), {
    target: { value: "leave" },
  });
  fireEvent.change(within(form).getByLabelText(words.expect, { exact: false }), {
    target: { value: "Leave policy" },
  });
  fireEvent.click(within(form).getByRole("button", { name: words.save }));
  assert.ok(await within(form).findByRole("alert"));
  assert.equal(
    (within(form).getByLabelText(words.query, { exact: false }) as HTMLInputElement).value,
    "leave",
  );
});

test("each run shows its recall, precision and nDCG, and each case how it fared in the latest", async () => {
  const older = run({
    id: "run-0",
    status: "failed",
    passed: 1,
    metrics: { recall: 0.5, precision: 0.25, ndcg: 0.31 },
    results: [],
  });
  const latest = run({
    results: [
      { case_id: "leave", input: "parental leave", passed: true, rank: 2, duration_ms: 12 },
      { case_id: "expenses", input: "expense limits", passed: false, duration_ms: 9 },
    ],
    passed: 1,
    status: "failed",
  });
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: [suite({ runs: [latest, older] })] }),
  });
  render(page());
  const table = await screen.findByRole("table", { name: words.runs });
  const [, first, second] = within(table).getAllByRole("row");
  assert.ok(first && second);
  assert.deepEqual(
    within(first)
      .getAllByRole("cell")
      .slice(1)
      .map((c) => c.textContent),
    [words.status.failed, words.passedOf(1, 2), "0.80", "0.40", "0.66"],
  );
  assert.deepEqual(
    within(second)
      .getAllByRole("cell")
      .slice(3)
      .map((c) => c.textContent),
    ["0.50", "0.25", "0.31"],
  );
  for (const head of [words.recall, words.precision, words.ndcg])
    assert.ok(within(table).getByRole("columnheader", { name: head }));
  const leave = screen.getByRole("listitem", { name: "parental leave" });
  assert.ok(within(leave).getByText(words.casePassed));
  assert.ok(within(leave).getByText(words.rank(2)));
  assert.ok(within(leave).getByText("Leave policy"));
  const expenses = screen.getByRole("listitem", { name: "expense limits" });
  assert.ok(within(expenses).getByText(words.caseMissed));
  assert.ok(within(expenses).getByText("Expenses, Travel"));
});

test("Run Evals starts a run, which reads as running and cannot be started twice", async () => {
  let current = suite();
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: [current] }),
    "POST /api/evals/hr-policies-retrieval/run": () => {
      const started = run({ status: "running", passed: 0, metrics: undefined, results: [] });
      current = suite({ runs: [started] });
      return started;
    },
  });
  render(page());
  assert.ok(await screen.findByText(words.noRuns));
  fireEvent.click(screen.getByRole("button", { name: words.run }));
  const table = await screen.findByRole("table", { name: words.runs });
  assert.ok(within(table).getByText(words.status.running));
  assert.ok(within(table).getByText(words.progress(0, 2)));
  assert.equal(
    (screen.getByRole("button", { name: words.run }) as HTMLButtonElement).disabled,
    true,
  );
  assert.ok(net.seen.some((r) => r.method === "POST" && r.path.endsWith("/run")));
});

test("a run that errored says why, and a case that could not be searched says so", async () => {
  const errored = run({
    status: "errored",
    passed: 0,
    error: "the suite's corpus: not found",
    metrics: undefined,
    results: [
      {
        case_id: "leave",
        input: "parental leave",
        passed: false,
        duration_ms: 0,
        error: "no such corpus",
      },
    ],
  });
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: [suite({ runs: [errored] })] }),
  });
  render(page());
  const table = await screen.findByRole("table", { name: words.runs });
  assert.ok(within(table).getByText(words.status.errored));
  assert.ok(within(table).getByText("the suite's corpus: not found"));
  const leave = screen.getByRole("listitem", { name: "parental leave" });
  assert.ok(within(leave).getByText(words.caseErrored));
  assert.ok(within(leave).getByText("no such corpus"));
});

test("removing a case saves the rest; removing the last asks first, then deletes the suite", async () => {
  let current: SuiteView | null = suite();
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: current ? [current] : [] }),
    "PUT /api/evals/hr-policies-retrieval": (body) => {
      current = { ...(body as SuiteView), runs: [] };
      return body;
    },
    "DELETE /api/evals/hr-policies-retrieval": () => {
      current = null;
      return undefined;
    },
  });
  render(page());
  fireEvent.click(await screen.findByRole("button", { name: words.removeCase("expense limits") }));
  await waitFor(() =>
    assert.equal(screen.queryAllByRole("listitem", { name: "expense limits" }).length, 0),
  );
  assert.deepEqual(
    (net.seen.find((r) => r.method === "PUT")?.body as SuiteView).cases,
    suite().cases.slice(0, 1),
  );
  // The controls are held while the change is saved and the suite read again.
  const removeLast = screen.getByRole("button", { name: words.removeCase("parental leave") });
  await waitFor(() => assert.equal((removeLast as HTMLButtonElement).disabled, false));
  fireEvent.click(removeLast);
  const ask = screen.getByRole("group", { name: words.lastCaseAsk("hr-policies-retrieval") });
  assert.equal(
    net.seen.some((r) => r.method === "DELETE"),
    false,
  );
  fireEvent.click(within(ask).getByRole("button", { name: words.deleteSuite }));
  assert.ok(await screen.findByText(words.none));
  assert.ok(net.seen.some((r) => r.method === "DELETE"));
});

test("a reader who may not write evals sees the cases and scores with no editing controls", async () => {
  net = stubFetch({
    "GET /api/me": () => me("evals:read"),
    "GET /api/evals": () => ({ suites: [suite({ runs: [run()] })] }),
  });
  render(page());
  assert.ok(await screen.findByRole("listitem", { name: "parental leave" }));
  assert.ok(screen.getByRole("table", { name: words.runs }));
  assert.equal(screen.queryAllByRole("button").length, 0);
  assert.equal(screen.queryAllByRole("form").length, 0);
});

test("a reader without a suite sees the title alone; one who may not read evals is told so", async () => {
  net = stubFetch({
    "GET /api/me": () => me("evals:read"),
    "GET /api/evals": () => ({ suites: [] }),
  });
  render(page());
  assert.ok(await screen.findByText(words.none));
  assert.equal(screen.queryAllByRole("button").length, 0);
  cleanup();
  net.restore();
  net = stubFetch({ "GET /api/me": () => me("knowledge:read") });
  render(page());
  assert.ok(await screen.findByText(words.noAccess));
  assert.equal(
    net.seen.some((r) => r.path === "/api/evals"),
    false,
  );
});

test("evals that cannot be loaded say so, after the rows that stand in while they load", async () => {
  net = stubFetch({
    "GET /api/me": editor,
    "GET /api/evals": () => new Response(JSON.stringify({ error: "down" }), { status: 500 }),
  });
  render(page());
  assert.ok(screen.getByTestId("skeleton"));
  assert.equal((await screen.findByRole("alert")).textContent, words.loadFailed);
  assert.equal(screen.queryAllByText(words.none).length, 0);
});

test("a corpus page has an Evals tab that shows the corpus's suite", async () => {
  const corpus: Corpus = {
    name: "hr-policies",
    title: "HR policies",
    owner: "ada",
    readers: [],
    members: [],
    agents: [],
    label: "internal",
    links: "isolated",
    created_at: at,
    updated_at: at,
    version: 1,
    mine: true,
  };
  const tabs: string[] = [];
  net = stubFetch({
    "GET /api/corpora/hr-policies": () => corpus,
    "GET /api/people": () => ({ people: [], next: null }),
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/me": editor,
    "GET /api/evals": () => ({ suites: [suite()] }),
  });
  const props = { name: "hr-policies", sources: [], canWrite: true };
  const view = render(
    <CorpusPage
      {...props}
      tab="sources"
      onTab={(t) => tabs.push(t)}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  fireEvent.click(await screen.findByRole("tab", { name: defaultCopy.corpora.tabs.evals }));
  assert.deepEqual(tabs, ["evals"]);
  view.rerender(
    <CorpusPage
      {...props}
      tab="evals"
      onTab={() => {}}
      onBack={() => {}}
      onOpenSource={() => {}}
    />,
  );
  assert.ok(await screen.findByRole("region", { name: /hr-policies-retrieval/ }));
});
