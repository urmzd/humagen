import type { RunDetails } from "@humagen/core";
import type { Run } from "./run.ts";

const ENDED = new Set<Run["status"]>(["completed", "failed", "stopped"]);

/**
 * What a run used, from its record (`GET /api/runs/{id}`): the run's `total`
 * usage, which counts its own model calls and the attached runs it waited
 * on, the model of its latest call, and the time from its creation to its
 * last change. A field the record does not carry is left out, never zeroed:
 * a run with no priced call has no cost, and one whose cost leaves calls out
 * (no price, or priced in another currency) says it is a lower bound.
 */
export function runDetails(run: Run): RunDetails {
  const total = run.usage?.total;
  const cost = total?.cost;
  const started = Date.parse(run.created_at);
  const ended = Date.parse(run.updated_at);
  const elapsed = ended - started;
  return {
    agent: run.agent,
    ...(run.usage?.context?.model ? { model: run.usage.context.model } : {}),
    ...(total?.prompt_tokens !== undefined ? { inputTokens: total.prompt_tokens } : {}),
    ...(total?.cached_prompt_tokens ? { cachedTokens: total.cached_prompt_tokens } : {}),
    ...(total?.completion_tokens !== undefined ? { outputTokens: total.completion_tokens } : {}),
    ...(cost
      ? {
          cost: {
            micros: cost.micros,
            currency: cost.currency,
            ...((total?.unpriced_calls ?? 0) > 0 || (total?.other_costs?.length ?? 0) > 0
              ? { atLeast: true }
              : {}),
          },
        }
      : {}),
    // A run still going has no elapsed time to report: its last change is not its end.
    ...(ENDED.has(run.status) && Number.isFinite(elapsed) && elapsed >= 0
      ? { elapsedMs: elapsed }
      : {}),
  };
}
