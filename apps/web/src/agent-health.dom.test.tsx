import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { AgentHealth, AgentStats } from "./AgentHealth.tsx";
import type { EvalRun, SuiteView } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { Evals } from "./Evals.tsx";

const words = defaultCopy.agentHealth;
const sched = defaultCopy.evalSchedule;
const hour = 3_600_000;
const iso = (msFromNow: number) => new Date(Date.now() + msFromNow).toISOString();

const result = (run: string, failing: string[] = []) => ({
  run,
  agent: "writer",
  version: 2,
  passed: failing.length === 0,
  words: 80,
  word_budget: 120,
  at: iso(-hour),
  signals: {},
  verdicts: [
    { check: "completed", passed: true },
    ...failing.map((check) => ({ check, passed: false, reason: `${check} failed` })),
  ],
});

const health = (over: Record<string, unknown> = {}) => ({
  agent: "writer",
  status: "untested",
  samples: 0,
  passed: 0,
  pass_rate: 0,
  results: [],
  flagged: 0,
  ...over,
});

const scheduled: SuiteView = {
  name: "smoke",
  target: "agent",
  subject: "writer",
  every_seconds: 6 * 3600,
  created_by: "u-ana",
  created_by_name: "Ana",
  next_run_at: iso(2 * hour + 5 * 60_000),
  cases: [{ id: "a", input: "hello", checks: [{ kind: "contains", value: "hi" }] }],
  runs: [
    {
      id: "e1",
      suite: "smoke",
      target: "agent",
      subject: "writer",
      status: "passed",
      passed: 1,
      total: 1,
      pass_rate: 100,
      threshold: 100,
      results: [],
      started_at: iso(-3 * hour - 5 * 60_000),
    },
  ],
};
const unscheduled: SuiteView = {
  ...scheduled,
  name: "tone",
  every_seconds: undefined,
  next_run_at: undefined,
  runs: [],
};

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

test("an agent nobody has checked says what happens next, without separators", async () => {
  net = stubFetch({
    "GET /api/agents/writer/health": () => health(),
    "GET /api/agents/writer/changes": () => [],
  });
  render(<AgentHealth name="writer" suites={[]} canRun />);
  await screen.findByText(words.none);
  assert.ok(screen.getByText(words.status.untested));
  assert.ok(screen.getByText(words.noRuns));
  assert.ok(screen.getByText(words.noSuites));
  const add = screen.getByRole("link", { name: words.addSuite }) as HTMLAnchorElement;
  assert.equal(add.getAttribute("href"), "/agents/writer/evals#eval-suites");
  const card = screen.getByRole("region", { name: words.label });
  assert.ok(!card.textContent?.includes("|"), card.textContent ?? "");
  // How evaluation works is behind an icon, with the docs, not a sentence on the page.
  assert.equal(screen.queryByText(words.aboutRuns), null);
  fireEvent.click(screen.getByRole("button", { name: words.about }));
  assert.ok(await screen.findByText(words.aboutRuns));
  assert.equal(screen.getByRole("link", { name: words.docs }).getAttribute("href"), words.docsUrl);
});

test("recent runs show their pass rate, window and the check that fails most, beside each suite's schedule", async () => {
  net = stubFetch({
    "GET /api/agents/writer/health": () =>
      health({
        status: "needs_review",
        samples: 4,
        passed: 1,
        pass_rate: 25,
        flagged: 3,
        results: [
          result("r1", ["word_budget"]),
          result("r2", ["word_budget", "answer_first"]),
          result("r3", ["tool_errors"]),
          result("r4"),
        ],
      }),
    "GET /api/agents/writer/changes": () => [],
    "POST /api/evals/tone/run": () => ({ ...unscheduled.runs, status: "running" }),
  });
  let changed = 0;
  render(
    <AgentHealth
      name="writer"
      suites={[scheduled, unscheduled]}
      canRun
      onSuitesChanged={() => changed++}
    />,
  );
  const runs = await screen.findByRole("region", { name: words.runs });
  assert.ok(within(runs).getByText("25%"));
  assert.ok(within(runs).getByText(/Last 4 runs, since/));
  assert.ok(within(runs).getByText(words.checks.word_budget as string));
  assert.ok(within(runs).getByText(words.failingCount(2)));
  const flagged = within(runs).getByRole("link", { name: words.flagged(3) });
  assert.equal(flagged.getAttribute("href"), "/agents/writer/evals#eval-feedback");

  const suites = screen.getByRole("region", { name: words.suites });
  assert.ok(within(suites).getByText("Every 6 hours, next in 2 hours, last run 3 hours ago"));
  assert.ok(within(suites).getByText(words.result(1, 1)));
  assert.ok(within(suites).getByText(words.notRun));
  assert.equal(
    within(suites).getByRole("link", { name: sched.set }).getAttribute("href"),
    "/agents/writer/evals#schedule-tone",
  );
  fireEvent.click(within(suites).getByRole("button", { name: words.runNowFor("tone") }));
  await waitFor(() => assert.equal(changed, 1));
  assert.ok(net.seen.some((r) => r.method === "POST" && r.path === "/api/evals/tone/run"));
});

test("refresh is an icon button that spins while it reads, keeps what was shown, and says when it last read", async () => {
  let release: (() => void) | undefined;
  let reads = 0;
  net = stubFetch({
    "GET /api/agents/writer/health": async () => {
      reads++;
      if (reads > 1) await new Promise<void>((ok) => (release = ok));
      return health({
        status: "healthy",
        samples: 1,
        passed: 1,
        pass_rate: 100,
        results: [result("r1")],
      });
    },
    "GET /api/agents/writer/changes": () => [],
  });
  render(<AgentHealth name="writer" />);
  await screen.findByText(words.summary(1, 1, 100));
  assert.ok(screen.getByText(words.updated("just now")));
  const refresh = screen.getByRole("button", { name: words.refresh }) as HTMLButtonElement;
  assert.equal(refresh.textContent, "");
  fireEvent.click(refresh);
  await waitFor(() => assert.equal(refresh.disabled, true));
  assert.equal(screen.getByRole("region", { name: words.label }).getAttribute("aria-busy"), "true");
  assert.ok(refresh.querySelector("svg")?.getAttribute("class")?.includes("animate-spin"));
  // What was read before stays on view while it reads again.
  assert.ok(screen.getByText(words.summary(1, 1, 100)));
  release?.();
  await waitFor(() => assert.equal(refresh.disabled, false));
  assert.equal(reads, 2);
});

test("a failed read offers Retry in place of refresh", async () => {
  net = stubFetch({
    "GET /api/agents/writer/health": () =>
      new Response(JSON.stringify({ error: "Health is down" }), { status: 503 }),
    "GET /api/agents/writer/changes": () => [],
  });
  render(<AgentHealth name="writer" />);
  await screen.findByText("Health is down");
  assert.ok(screen.getByRole("button", { name: defaultCopy.retry }));
  assert.equal(screen.queryByRole("button", { name: words.refresh }), null);
});

test("the figures say an explicit dash with its reason when there is nothing to count", () => {
  const w = defaultCopy.agentStats;
  render(<AgentStats stats={{ runs: 0, completed: 0, failed: 0 }} openAlerts={2} suites={1} />);
  const figures = screen.getByLabelText(w.label);
  for (const label of [w.runs, w.success, w.median, w.alerts, w.suites])
    assert.ok(within(figures).getByText(label), label);
  // A dash is read out as why there is no figure.
  assert.ok(within(figures).getByText(w.noEnded));
  assert.equal(within(figures).getAllByText(w.none).length, 2);
  assert.ok(within(figures).getByText("2"));
  assert.ok(within(figures).getByText("1"));
});

test("a person sets, changes and turns off a suite's schedule within its bounds, and sees who it runs as", async () => {
  const saved: unknown[] = [];
  net = stubFetch({
    "PUT /api/evals/smoke": (body) => {
      saved.push(body);
      return body;
    },
    "GET /api/eval-candidates": () => ({ candidates: [], next: "" }),
  });
  let changed = 0;
  render(
    <Evals
      target="agent"
      subject="writer"
      suites={[scheduled]}
      canWrite
      onChanged={() => changed++}
    />,
  );
  assert.ok(screen.getByText("Every 6 hours, next in 2 hours, last run 3 hours ago, runs as Ana"));
  fireEvent.click(screen.getByRole("button", { name: sched.edit }));
  const editor = screen.getByRole("form", { name: sched.editFor("smoke") });
  assert.equal(
    within(editor).getByRole("button", { name: sched.presets.six }).getAttribute("aria-pressed"),
    "true",
  );
  assert.ok(within(editor).getByText(new RegExp(sched.actsFor("Ana"))));

  // A custom period outside an hour to thirty days cannot be saved.
  fireEvent.click(within(editor).getByRole("button", { name: sched.presets.custom }));
  const hours = within(editor).getByRole("spinbutton", { name: /^Every \(hours\)/ });
  fireEvent.change(hours, { target: { value: "721" } });
  const save = within(editor).getByRole("button", { name: sched.save }) as HTMLButtonElement;
  assert.equal(save.disabled, true);
  assert.ok(within(editor).getByText(sched.invalid));
  fireEvent.change(hours, { target: { value: "12" } });
  assert.equal(save.disabled, false);

  fireEvent.click(within(editor).getByRole("button", { name: sched.presets.daily }));
  fireEvent.click(save);
  await waitFor(() => assert.equal(changed, 1));
  const body = saved[0] as Record<string, unknown>;
  assert.equal(body.every_seconds, 86_400);
  // What a listing adds is left off, as the server takes no unknown field.
  for (const key of ["runs", "created_by_name", "next_run_at"]) assert.ok(!(key in body), key);

  // Off saves a zero period, which turns the schedule off.
  fireEvent.click(screen.getByRole("button", { name: sched.edit }));
  fireEvent.click(screen.getByRole("button", { name: sched.presets.off }));
  fireEvent.click(screen.getByRole("button", { name: sched.save }));
  await waitFor(() => assert.equal(changed, 2));
  assert.equal((saved[1] as Record<string, unknown>).every_seconds, 0);
});

test("pass-rate bars are neutral when a run reached its threshold, warn below it, bad only when it could not run, and labelled by when it ran", () => {
  const run = (
    id: string,
    status: "passed" | "failed" | "errored",
    rate: number,
    hoursAgo: number,
  ) => ({
    ...(scheduled.runs[0] as EvalRun),
    id,
    status,
    pass_rate: rate,
    threshold: 80,
    started_at: iso(-hoursAgo * hour - 60_000),
  });
  render(
    <Evals
      target="agent"
      subject="writer"
      suites={[
        {
          ...scheduled,
          runs: [run("c", "errored", 0, 2), run("b", "failed", 60, 5), run("a", "passed", 90, 9)],
        },
      ]}
      canWrite={false}
      onChanged={() => {}}
    />,
  );
  const bars = [...document.querySelectorAll("[data-tone]")].map((el) =>
    el.getAttribute("data-tone"),
  );
  assert.deepEqual(bars, ["bg-aui-neutral", "bg-aui-warn", "bg-aui-bad"]);
  const labels = [...document.querySelectorAll("figure time")].map((el) => el.textContent);
  assert.deepEqual(labels, ["9h", "5h", "2h"]);
});
