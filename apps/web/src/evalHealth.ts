import { defaultCopy } from "@humagen/components";
import type { OnlineEvaluation } from "../../../packages/client/src/generated/types.ts";
import type { SuiteView } from "./api.ts";
import { ago } from "./format.ts";

const words = defaultCopy.evalSchedule;

/** The schedule bounds the server enforces (`evals.Suite.Validate`). */
export const MIN_EVERY = 3600;
export const MAX_EVERY = 30 * 24 * 3600;

/** The periods a person picks from; anything else is a custom number of hours. */
export const PRESETS = {
  off: 0,
  hourly: 3600,
  six: 6 * 3600,
  daily: 24 * 3600,
  weekly: 7 * 24 * 3600,
} as const;
export type Preset = keyof typeof PRESETS | "custom";

/** The preset a saved period is, or custom. */
export function presetOf(seconds: number | undefined): Preset {
  const found = (Object.keys(PRESETS) as (keyof typeof PRESETS)[]).find(
    (k) => PRESETS[k] === (seconds ?? 0),
  );
  return found ?? "custom";
}

/** Whether a period can be saved: off, or from an hour to thirty days. */
export function validEvery(seconds: number): boolean {
  return (
    seconds === 0 || (Number.isInteger(seconds) && seconds >= MIN_EVERY && seconds <= MAX_EVERY)
  );
}

/** A suite's schedule in one line: "Every 6 hours, next in 2 hours, last run 3 hours ago". */
export function scheduleLine(suite: SuiteView, now: number = Date.now()): string {
  if (!suite.every_seconds) return words.off;
  const next =
    !suite.next_run_at || Date.parse(suite.next_run_at) - now < 60_000
      ? words.dueNow
      : words.next(ago(suite.next_run_at, now));
  const started = suite.runs[0]?.started_at;
  const last = started ? words.last(ago(started, now)) : words.never;
  return [words.every(suite.every_seconds), next, last].join(", ");
}

/** What the recent checked runs say: how many passed, over what window, and which check fails most. */
export interface OnlineSummary {
  samples: number;
  passed: number;
  rate: number;
  oldest?: string;
  top?: { check: string; count: number };
}

export function onlineSummary(results: OnlineEvaluation[]): OnlineSummary {
  const failing = new Map<string, number>();
  let oldest: string | undefined;
  let passed = 0;
  for (const r of results) {
    if (r.passed) passed++;
    if (!oldest || Date.parse(r.at) < Date.parse(oldest)) oldest = r.at;
    for (const v of r.verdicts)
      if (!v.passed) failing.set(v.check, (failing.get(v.check) ?? 0) + 1);
  }
  let top: OnlineSummary["top"];
  for (const [check, count] of failing)
    if (!top || count > top.count || (count === top.count && check < top.check))
      top = { check, count };
  return {
    samples: results.length,
    passed,
    rate: results.length ? Math.round((100 * passed) / results.length) : 0,
    oldest,
    top,
  };
}

/** A check's name in words, or its own name when the console does not know it. */
export function checkName(check: string): string {
  return defaultCopy.agentHealth.checks[check] ?? check;
}
