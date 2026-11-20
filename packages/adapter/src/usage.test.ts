import assert from "node:assert/strict";
import { test } from "node:test";
import type { Run } from "./run.ts";
import { runDetails } from "./usage.ts";

const run = (over: Partial<Run>): Run =>
  ({
    id: "r1",
    agent: "lead",
    status: "completed",
    created_at: "2026-11-19T00:00:00Z",
    updated_at: "2026-11-19T00:00:52Z",
    ...over,
  }) as Run;

test("a run's details are its total usage, its latest model and its elapsed time", () => {
  const details = runDetails(
    run({
      usage: {
        own: { prompt_tokens: 100, completion_tokens: 10 },
        total: {
          prompt_tokens: 48_210,
          cached_prompt_tokens: 39_000,
          completion_tokens: 1_840,
          cost: { micros: 42_100, currency: "USD" },
        },
        context: { model: "claude-sonnet-4-5", tokens: 9_000 },
      },
    }),
  );
  assert.deepEqual(details, {
    agent: "lead",
    model: "claude-sonnet-4-5",
    inputTokens: 48_210,
    cachedTokens: 39_000,
    outputTokens: 1_840,
    cost: { micros: 42_100, currency: "USD" },
    elapsedMs: 52_000,
  });
});

test("a cost that leaves calls out is a lower bound", () => {
  const unpriced = runDetails(
    run({ usage: { own: {}, total: { cost: { micros: 5, currency: "USD" }, unpriced_calls: 2 } } }),
  );
  assert.equal(unpriced.cost?.atLeast, true);
  const mixed = runDetails(
    run({
      usage: {
        own: {},
        total: { cost: { micros: 5, currency: "USD" }, other_costs: [{ micros: 9, currency: "EUR" }] },
      },
    }),
  );
  assert.equal(mixed.cost?.atLeast, true);
});

test("what the run did not record is left out, never zeroed", () => {
  assert.deepEqual(runDetails(run({})), { agent: "lead", elapsedMs: 52_000 });
  const unpriced = runDetails(run({ usage: { own: {}, total: { prompt_tokens: 9, completion_tokens: 1 } } }));
  assert.equal(unpriced.cost, undefined);
  assert.equal(unpriced.model, undefined);
});

test("a run still going has no elapsed time", () => {
  assert.equal(runDetails(run({ status: "running" })).elapsedMs, undefined);
});
