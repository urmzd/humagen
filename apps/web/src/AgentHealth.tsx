import { useEffect, useState } from "react";
import type {
  AgentChange,
  AgentEvaluationHealth,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
import { ago, Problem, SkeletonRows } from "./ui.tsx";

export const healthLabel = (status: AgentEvaluationHealth["status"]) =>
  ({ untested: "Not evaluated", healthy: "Healthy", needs_review: "Needs review" })[status];

export function AgentHealth({ name }: { name: string }) {
  const [health, setHealth] = useState<AgentEvaluationHealth>();
  const [changes, setChanges] = useState<AgentChange[]>([]);
  const [problem, setProblem] = useState("");
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    setHealth(undefined);
    setChanges([]);
    setProblem("");
    Promise.all([
      call<AgentEvaluationHealth>("GET", `/api/agents/${encodeURIComponent(name)}/health`),
      call<AgentChange[]>("GET", `/api/agents/${encodeURIComponent(name)}/changes`),
    ]).then(
      ([health, changes]) => {
        if (live) {
          setHealth(health);
          setChanges(changes ?? []);
        }
      },
      (error: unknown) => {
        if (live)
          setProblem(error instanceof Error ? error.message : "Agent health could not be loaded.");
      },
    );
    return () => {
      live = false;
    };
  }, [name, tick]);
  return (
    <section
      aria-label="Agent health"
      className="flex min-w-0 flex-col gap-4 rounded-aui border border-aui-border p-4"
    >
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-aui-base font-medium">Health and evaluations</h2>
        <button
          type="button"
          className="min-h-9 rounded-aui px-2 text-aui-sm underline"
          onClick={() => setTick(tick + 1)}
        >
          Refresh health
        </button>
      </div>
      {problem ? (
        <Problem message={problem} />
      ) : !health ? (
        <SkeletonRows count={2} />
      ) : (
        <>
          <p className={health.status === "needs_review" ? "text-aui-warn" : "text-aui-fg"}>
            {healthLabel(health.status)}
            {health.samples > 0
              ? ` · ${health.passed} of ${health.samples} recent evaluations passed (${Math.round(health.pass_rate)}%)`
              : " · No evaluated runs yet"}
          </p>
          {health.results
            .filter((result) => !result.passed)
            .slice(0, 3)
            .map((result) => (
              <div key={result.run} className="text-aui-sm">
                <p className="text-aui-muted">
                  Revision {result.version} · {ago(result.at)}
                </p>
                <ul className="list-inside list-disc">
                  {result.verdicts
                    .filter((verdict) => !verdict.passed)
                    .map((verdict) => (
                      <li key={verdict.check}>{verdict.reason || verdict.check}</li>
                    ))}
                </ul>
              </div>
            ))}
        </>
      )}
      {changes.length > 0 && (
        <div className="flex min-w-0 flex-col gap-3">
          <h3 className="text-aui-sm font-medium">Recent proposed changes</h3>
          {changes.slice(0, 5).map((change) => (
            <article
              key={change.id}
              className="rounded-aui border border-aui-border p-3 text-aui-sm"
            >
              <div className="flex flex-wrap justify-between gap-2">
                <strong>
                  {!change.evaluated
                    ? "Evaluation incomplete"
                    : change.passed
                      ? "Evaluations passed"
                      : "Regression requires an override"}
                </strong>
                <span className="text-aui-muted">{ago(change.at)}</span>
              </div>
              {change.error && <p role="alert">{change.error}</p>}
              {change.scores?.map((score) => (
                <p key={score.suite}>
                  {score.suite}: {score.before}% → {score.after}% ·{" "}
                  {score.passed ? "Passed" : "Regression"}
                </p>
              ))}
              {change.override_reason && <p>Override: {change.override_reason}</p>}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
