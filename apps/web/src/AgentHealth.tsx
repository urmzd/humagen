import { defaultCopy } from "@humagen/components";
import { type ReactNode, useEffect, useState } from "react";
import type {
  AgentChange,
  AgentEvaluationHealth,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
import { Pill, type Tone } from "./cards.tsx";
import { ago, button, card, Problem, SkeletonRows } from "./ui.tsx";

export const healthLabel = (status: AgentEvaluationHealth["status"]) =>
  ({ untested: "Not evaluated", healthy: "Healthy", needs_review: "Needs review" })[status];

const healthTone: Record<AgentEvaluationHealth["status"], Tone> = {
  untested: "neutral",
  healthy: "ok",
  needs_review: "warn",
};

/**
 * An agent's evaluation health: one line with its state and recent pass rate,
 * the page's action at the right of it, then what failed and what changed.
 * It reads again when `version` changes, as it does when an eval runs.
 */
export function AgentHealth({
  name,
  version,
  actions,
}: {
  name: string;
  version?: string;
  actions?: ReactNode;
}) {
  const [health, setHealth] = useState<AgentEvaluationHealth>();
  const [changes, setChanges] = useState<AgentChange[]>([]);
  const [problem, setProblem] = useState("");
  const [tick, setTick] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: version and tick only say when to read again
  useEffect(() => {
    let live = true;
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
  }, [name, version, tick]);
  const failures = (health?.results ?? []).filter((result) => !result.passed).slice(0, 3);
  return (
    <section aria-label="Agent health" className="flex min-w-0 flex-col gap-3">
      <div className="flex min-h-9 flex-wrap items-center gap-x-3 gap-y-2">
        {problem ? (
          <>
            <Problem message={problem} />
            <button type="button" className={button} onClick={() => setTick(tick + 1)}>
              {defaultCopy.retry}
            </button>
          </>
        ) : !health ? (
          <div className="w-48">
            <SkeletonRows count={1} />
          </div>
        ) : (
          <>
            <Pill tone={healthTone[health.status]}>{healthLabel(health.status)}</Pill>
            <span className="text-aui-sm text-aui-muted tabular-nums">
              {health.samples > 0
                ? `${health.passed} of ${health.samples} recent evaluations passed (${Math.round(health.pass_rate)}%)`
                : "No evaluated runs yet"}
            </span>
          </>
        )}
        {actions && <div className="ml-auto flex shrink-0 gap-2">{actions}</div>}
      </div>
      {failures.length > 0 && (
        <ul className={`${card} flex flex-col gap-3 text-aui-sm`}>
          {failures.map((result) => (
            <li key={result.run} className="flex flex-col gap-1">
              <p className="text-aui-xs text-aui-muted">
                Revision {result.version}, {ago(result.at)}
              </p>
              <ul className="list-inside list-disc text-aui-fg">
                {result.verdicts
                  .filter((verdict) => !verdict.passed)
                  .map((verdict) => (
                    <li key={verdict.check}>{verdict.reason || verdict.check}</li>
                  ))}
              </ul>
            </li>
          ))}
        </ul>
      )}
      {changes.length > 0 && (
        <section aria-label="Recent proposed changes" className={`${card} flex flex-col gap-3`}>
          <h3 className="font-medium text-aui-sm">Recent proposed changes</h3>
          <ul className="flex flex-col divide-y divide-[var(--humagen-rule)]">
            {changes.slice(0, 5).map((change) => (
              <li
                key={change.id}
                className="flex flex-col gap-1.5 py-2.5 text-aui-sm first:pt-0 last:pb-0"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Pill tone={!change.evaluated ? "neutral" : change.passed ? "ok" : "warn"}>
                    {!change.evaluated
                      ? "Evaluation incomplete"
                      : change.passed
                        ? "Evaluations passed"
                        : "Regression requires an override"}
                  </Pill>
                  <span className="text-aui-xs text-aui-muted">{ago(change.at)}</span>
                </div>
                {change.error && <p role="alert">{change.error}</p>}
                {change.scores?.map((score) => (
                  <p key={score.suite} className="flex flex-wrap items-center gap-2">
                    <span className="text-aui-fg">{score.suite}</span>
                    <span className="tabular-nums text-aui-muted">
                      {score.before}% → {score.after}%
                    </span>
                    {!score.passed && <Pill tone="warn">Regression</Pill>}
                  </p>
                ))}
                {change.override_reason && (
                  <p className="text-aui-muted">Override: {change.override_reason}</p>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
    </section>
  );
}
