import type { RunFeedback, SessionFeedback } from "../../../packages/client/src/generated/types.ts";

const feedback = new Map<string, RunFeedback[]>();
/** A bounded cache of the current person's feedback, loaded with the session. */
export function cacheSessionFeedback(run: string, own: SessionFeedback[]) {
  feedback.set(
    run,
    own.map((record) => ({ ...record, run, mine: true, by: "", by_name: "", updated_at: "" })),
  );
  while (feedback.size > 1000) feedback.delete(feedback.keys().next().value as string);
}
export function sessionFeedback(run: string): RunFeedback[] | undefined {
  return feedback.get(run);
}
