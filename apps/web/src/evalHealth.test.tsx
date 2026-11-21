import assert from "node:assert/strict";
import { test } from "node:test";
import type { SuiteView } from "./api.ts";
import { onlineSummary, presetOf, scheduleLine, validEvery } from "./evalHealth.ts";

const now = Date.parse("2026-11-20T12:00:00Z");
const at = (h: number) => new Date(now + h * 3_600_000).toISOString();

test("a period is a preset or custom, and valid from an hour to thirty days or off", () => {
  assert.equal(presetOf(undefined), "off");
  assert.equal(presetOf(0), "off");
  assert.equal(presetOf(21_600), "six");
  assert.equal(presetOf(604_800), "weekly");
  assert.equal(presetOf(7_200), "custom");
  assert.ok(validEvery(0) && validEvery(3_600) && validEvery(30 * 86_400));
  assert.ok(!validEvery(3_599) && !validEvery(30 * 86_400 + 1) && !validEvery(5_400.5));
});

test("a schedule reads as its period, its next run and its last run", () => {
  const suite = {
    name: "s",
    target: "agent",
    subject: "a",
    cases: [],
    every_seconds: 21_600,
    next_run_at: at(2),
    runs: [{ started_at: at(-3) }],
  } as unknown as SuiteView;
  assert.equal(scheduleLine(suite, now), "Every 6 hours, next in 2 hours, last run 3 hours ago");
  assert.equal(
    scheduleLine({ ...suite, next_run_at: at(0), runs: [] }, now),
    "Every 6 hours, next within a minute, not run yet",
  );
  assert.equal(
    scheduleLine({ ...suite, every_seconds: 3 * 86_400 }, now).split(",")[0],
    "Every 3 days",
  );
  assert.equal(scheduleLine({ ...suite, every_seconds: 0 }, now), "Not scheduled");
});

test("the online summary counts passes, the oldest run and the check that fails most", () => {
  const r = (passed: boolean, checks: string[], hours: number) => ({
    run: String(hours),
    agent: "a",
    version: 1,
    passed,
    words: 1,
    word_budget: 1,
    at: at(hours),
    signals: {},
    verdicts: checks.map((check) => ({ check, passed: false })),
  });
  const s = onlineSummary([
    r(false, ["word_budget"], -1),
    r(false, ["word_budget", "answer_first"], -5),
    r(false, ["answer_first", "completed"], -2),
    r(true, [], -3),
  ] as never);
  assert.deepEqual(
    { samples: s.samples, passed: s.passed, rate: s.rate, oldest: s.oldest, top: s.top },
    { samples: 4, passed: 1, rate: 25, oldest: at(-5), top: { check: "answer_first", count: 2 } },
  );
  assert.deepEqual(onlineSummary([]), {
    samples: 0,
    passed: 0,
    rate: 0,
    oldest: undefined,
    top: undefined,
  });
});
