import type { Check } from "./api.ts";

/**
 * The checks a reviewer starts from for a candidate, by the reason its rating
 * gave: an answer that was too slow is held to a time, any other to words it
 * must contain. The value is left for the reviewer to write, so nothing
 * passes until a person has said what a good answer is.
 */
export function draftChecks(reason: string): Check[] {
  return [{ kind: reason === "slow" ? "within_ms" : "contains", value: "" }];
}

/** Whether every check says something, so the case can be added. */
export function checksReady(checks: Check[]): boolean {
  return (
    checks.length > 0 &&
    checks.every((c) =>
      c.kind === "within_ms" ? /^[1-9]\d*$/.test(c.value.trim()) : c.value.trim() !== "",
    )
  );
}
