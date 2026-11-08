import { ActionId, FEEDBACK_CLEAR, type Turn, type UiAction } from "@humagen/core";
import { ActionContext, RatingSelectionContext } from "@humagen/render-web";
import { Modal } from "@humagen/web";
import { type ReactNode, useContext, useEffect, useRef, useState } from "react";
import type {
  RunFeedback,
  SaveRunFeedbackBody,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
import { cacheSessionFeedback, sessionFeedback } from "./sessionFeedback.ts";

type Feedback = RunFeedback & { finding_id?: string };
type Records = Record<string, Feedback | undefined>;

/** Own feedback is persisted by the host, never by a shared renderer. */
export function RunFeedbackHost({
  turn,
  children,
}: {
  turn: Turn;
  children: (turn: Turn) => ReactNode;
}) {
  const forward = useContext(ActionContext);
  const [records, setRecords] = useState<Records>({});
  const [problem, setProblem] = useState("");
  const [report, setReport] = useState(false);
  const latest = useRef<Records>({});
  const revision = useRef(0);
  const findingRevision = useRef<Record<string, number>>({});
  const pending = useRef(Promise.resolve());
  const terminal = ["done", "failed", "stopped"].includes(turn.status);
  useEffect(() => {
    if (!terminal) return;
    let live = true;
    const before = revision.current;
    const cached = sessionFeedback(turn.id);
    (cached
      ? Promise.resolve({ feedback: cached })
      : call<{ feedback: Feedback[] }>("GET", `/api/runs/${encodeURIComponent(turn.id)}/feedback`)
    )
      .then((page) => {
        if (!live || before !== revision.current) return;
        const mine = Object.fromEntries(
          page.feedback.filter((r) => r.mine).map((r) => [r.finding_id ?? "", r]),
        );
        latest.current = mine;
        setRecords(mine);
      })
      .catch(() => {
        /* A failed read does not block a new rating. */
      });
    return () => {
      live = false;
    };
  }, [turn.id, terminal]);

  const onAction = (action: UiAction) => {
    if (action.id === ActionId.report) {
      setReport(true);
      return;
    }
    if (action.id === ActionId.reconnect) {
      window.location.assign("/connections");
      return;
    }
    if (action.id !== ActionId.feedback && action.id !== ActionId.feedbackComment)
      return forward(action);
    const rating = action.payload.target;
    if (rating !== "up" && rating !== "down" && rating !== FEEDBACK_CLEAR) return;
    const finding = action.payload.findingId ?? "";
    const at = ++revision.current;
    findingRevision.current[finding] = at;
    const was = latest.current[finding];
    setProblem("");
    const body: (SaveRunFeedbackBody & { finding_id?: string }) | undefined =
      rating === FEEDBACK_CLEAR
        ? undefined
        : {
            rating,
            ...(finding ? { finding_id: finding } : {}),
            ...(action.id === ActionId.feedbackComment
              ? { comment: action.payload.text, reason: action.payload.option }
              : {}),
          };
    const optimistic = body
      ? ({ ...was, ...body, run: turn.id, mine: true } as Feedback)
      : undefined;
    latest.current = { ...latest.current, [finding]: optimistic };
    setRecords(latest.current);
    // A comment must land after its rating. Rapid changes keep their original order.
    pending.current = pending.current
      .catch(() => {})
      .then(async () => {
        try {
          const path = `/api/runs/${encodeURIComponent(turn.id)}/feedback${!body && finding ? `?finding_id=${encodeURIComponent(finding)}` : ""}`;
          const saved = await call<Feedback | undefined>(body ? "PUT" : "DELETE", path, body);
          if (at === findingRevision.current[finding]) {
            latest.current = { ...latest.current, [finding]: saved };
            setRecords(latest.current);
            cacheSessionFeedback(
              turn.id,
              Object.values(latest.current)
                .filter((record): record is Feedback => Boolean(record))
                .map((record) => ({ ...record, rating: record.rating as "up" | "down" })),
            );
          }
        } catch (error) {
          if (at !== findingRevision.current[finding]) return;
          latest.current = { ...latest.current, [finding]: was };
          setRecords(latest.current);
          setProblem(
            error instanceof Error
              ? `Feedback was not saved: ${error.message}`
              : "Feedback was not saved. Try again.",
          );
        }
      });
  };
  const issueTitle = turn.failure?.title || "Agent run failed";
  const issueBody = `An agent run failed.\n\nAgent: ${turn.agent.name}\nReference: ${turn.id}\n\nSteps to reproduce:\n\nExpected behavior:\n`;
  return (
    <ActionContext.Provider value={onAction}>
      <RatingSelectionContext.Provider
        value={(action) => records[action.payload.findingId ?? ""]?.rating as Turn["feedback"]}
      >
        {children({ ...turn, feedback: records[""]?.rating as Turn["feedback"] })}
        {problem && (
          <p role="alert" className="mt-1 text-aui-xs text-aui-bad">
            {problem}
          </p>
        )}
        {report && (
          <Modal title="Report issue" onClose={() => setReport(false)}>
            <div className="flex flex-col gap-4 p-5">
              <h2 className="text-aui-base font-medium">Report issue</h2>
              <p className="text-aui-sm">
                Review the report on GitHub before submitting it. Your messages and credentials are
                not included.
              </p>
              <pre className="whitespace-pre-wrap rounded-aui bg-aui-hover p-3 text-aui-xs">
                {issueBody}
              </pre>
              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={() => setReport(false)}
                  className="rounded-aui px-3 py-2"
                >
                  Cancel
                </button>
                <a
                  href={`https://github.com/urmzd/humagen/issues/new?${new URLSearchParams({ title: issueTitle, body: issueBody })}`}
                  target="_blank"
                  rel="noreferrer"
                  className="rounded-aui bg-aui-primary px-3 py-2 text-aui-primary-fg"
                >
                  Review on GitHub
                </a>
              </div>
            </div>
          </Modal>
        )}
      </RatingSelectionContext.Provider>
    </ActionContext.Provider>
  );
}
