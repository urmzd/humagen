import { defaultCopy } from "@humagen/components";
import { InfoIcon, RefreshIcon, Tooltip } from "@humagen/render-web";
import { buttonVariants } from "@humagen/render-web/ui/button";
import { type MouseEvent, type ReactNode, useEffect, useState } from "react";
import type {
  AgentChange,
  AgentEvaluationHealth,
} from "../../../packages/client/src/generated/types.ts";
import { ApiError, api, call, type SuiteView } from "./api.ts";
import { follow, Pill, type Tone } from "./cards.tsx";
import { checkName, onlineSummary, scheduleLine } from "./evalHealth.ts";
import { href } from "./route.ts";
import { ago, button, card, Popover, Problem, SkeletonRows, textLink, took } from "./ui.tsx";

const words = defaultCopy.agentHealth;

export const healthLabel = (status: AgentEvaluationHealth["status"]) => words.status[status];

const healthTone: Record<AgentEvaluationHealth["status"], Tone> = {
  untested: "neutral",
  healthy: "ok",
  needs_review: "warn",
};

const outcomeTone: Record<SuiteView["runs"][number]["status"], Tone> = {
  running: "neutral",
  passed: "ok",
  failed: "warn",
  errored: "bad",
};

const iconButton = `${buttonVariants({ variant: "ghost", size: "icon-sm" })} pointer-coarse:size-11 text-aui-muted hover:text-aui-fg`;

/**
 * A link to a part of the agent's Evals tab: it opens the tab, then brings
 * the part (a suite, the review queue) into view.
 */
function TabLink({ agent, part, children }: { agent: string; part: string; children: ReactNode }) {
  const go = (e: MouseEvent<HTMLAnchorElement>) => {
    follow(e);
    if (!e.defaultPrevented) return;
    let tries = 0;
    const reveal = () => {
      const el = document.getElementById(part);
      if (el) el.scrollIntoView({ block: "start" });
      else if (tries++ < 20) requestAnimationFrame(reveal);
    };
    requestAnimationFrame(reveal);
  };
  return (
    <a
      className={textLink}
      href={`${href({ page: "agent", name: agent, tab: "evals" })}#${part}`}
      onClick={go}
    >
      {children}
    </a>
  );
}

/** "How evaluation works", behind an info icon: what is checked, and the docs. */
function About() {
  return (
    <Popover
      label={words.about}
      tooltip={words.about}
      className={iconButton}
      button={<InfoIcon size={16} />}
    >
      {() => (
        <div className="flex flex-col gap-2 p-2 text-aui-sm text-aui-fg">
          <p className="font-medium">{words.about}</p>
          <p className="text-aui-muted">{words.aboutRuns}</p>
          <p className="text-aui-muted">{words.aboutSuites}</p>
          <p className="text-aui-muted">{words.aboutFeedback}</p>
          <a className={textLink} href={words.docsUrl} target="_blank" rel="noreferrer">
            {words.docs}
          </a>
        </div>
      )}
    </Popover>
  );
}

/** One fact on a row: its name at the left, its value at the right. */
function Fact({ name, children }: { name: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5 py-2 first:pt-0 last:pb-0">
      <dt className="text-aui-xs text-aui-muted">{name}</dt>
      <dd className="min-w-0 text-right text-aui-sm text-aui-fg tabular-nums">{children}</dd>
    </div>
  );
}

/** What the checks on runs as they finish say: the pass rate, its window, what fails most, and the review queue. */
function RecentRuns({ health, agent }: { health: AgentEvaluationHealth; agent: string }) {
  const s = onlineSummary(health.results);
  return (
    <section aria-label={words.runs} className={`${card} flex min-w-0 flex-col gap-3`}>
      <h3 className="font-medium text-aui-sm">{words.runs}</h3>
      {s.samples === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.noRuns}</p>
      ) : (
        <dl className="flex flex-col divide-y divide-[var(--humagen-rule)]">
          <Fact name={words.passRate}>{s.rate}%</Fact>
          <Fact name={words.window(s.samples, ago(s.oldest ?? ""))}>
            {words.result(s.passed, s.samples)}
          </Fact>
          <Fact name={words.topFailing}>
            {s.top ? (
              <>
                {checkName(s.top.check)}{" "}
                <span className="text-aui-muted">{words.failingCount(s.top.count)}</span>
              </>
            ) : (
              <span className="text-aui-muted">{words.noFailing}</span>
            )}
          </Fact>
        </dl>
      )}
      {health.flagged > 0 && (
        <p className="text-aui-sm">
          <TabLink agent={agent} part="eval-feedback">
            {words.flagged(health.flagged)}
          </TabLink>
        </p>
      )}
    </section>
  );
}

/** Each suite's last result and schedule, with Run Now. */
function Suites({
  agent,
  suites,
  canRun,
  onRan,
}: {
  agent: string;
  suites: SuiteView[];
  canRun: boolean;
  onRan: () => void;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  const [starting, setStarting] = useState("");
  const run = (name: string) => {
    setProblem(null);
    setStarting(name);
    api
      .runSuite(name)
      .then(onRan)
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.runFailed))
      .finally(() => setStarting(""));
  };
  return (
    <section aria-label={words.suites} className={`${card} flex min-w-0 flex-col gap-3`}>
      <h3 className="font-medium text-aui-sm">{words.suites}</h3>
      <Problem message={problem} />
      {suites.length === 0 ? (
        <div className="flex flex-col items-start gap-1 text-aui-sm">
          <p className="text-aui-sm text-aui-muted">{words.noSuites}</p>
          {canRun && (
            <TabLink agent={agent} part="eval-suites">
              {words.addSuite}
            </TabLink>
          )}
        </div>
      ) : (
        <ul className="flex flex-col divide-y divide-[var(--humagen-rule)]">
          {suites.map((s) => {
            const last = s.runs[0];
            const busy = last?.status === "running" || starting === s.name;
            return (
              <li
                key={s.name}
                className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5 py-2.5 first:pt-0 last:pb-0"
              >
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate text-aui-sm text-aui-fg">{s.name}</span>
                    {last ? (
                      <Pill tone={outcomeTone[last.status]}>
                        {last.status === "running"
                          ? words.outcome.running
                          : words.result(last.passed, last.total)}
                      </Pill>
                    ) : (
                      <Pill>{words.notRun}</Pill>
                    )}
                  </div>
                  <p className="text-aui-xs text-aui-muted">
                    {scheduleLine(s)}
                    {!s.every_seconds && canRun && (
                      <>
                        {", "}
                        <TabLink agent={agent} part={`schedule-${s.name}`}>
                          {defaultCopy.evalSchedule.set}
                        </TabLink>
                      </>
                    )}
                  </p>
                </div>
                {canRun && (
                  <button
                    type="button"
                    className={`${button} h-8 shrink-0 px-3 text-aui-xs`}
                    aria-label={words.runNowFor(s.name)}
                    disabled={busy}
                    onClick={() => run(s.name)}
                  >
                    {busy ? words.outcome.running : words.runNow}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

/**
 * An agent's evaluation health: one row with its state, the recent pass rate,
 * the page's action and a refresh, then the checks on recent runs beside its
 * eval suites, then what failed and what changed. It reads again when
 * `version` changes, as it does when an eval runs. Without `suites` it leaves
 * the suites out, as the Evals tab lists them itself.
 */
export function AgentHealth({
  name,
  version,
  actions,
  suites,
  canRun = false,
  onSuitesChanged,
}: {
  name: string;
  version?: string;
  actions?: ReactNode;
  suites?: SuiteView[];
  canRun?: boolean;
  onSuitesChanged?: () => void;
}) {
  const [health, setHealth] = useState<AgentEvaluationHealth>();
  const [changes, setChanges] = useState<AgentChange[]>([]);
  const [problem, setProblem] = useState("");
  const [loading, setLoading] = useState(true);
  const [readAt, setReadAt] = useState<string>();
  const [tick, setTick] = useState(0);
  // A minute's tick keeps "Updated 2 minutes ago" true while the page stays open.
  const [, setNow] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setNow((n) => n + 1), 60_000);
    return () => clearInterval(t);
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: version and tick only say when to read again
  useEffect(() => {
    let live = true;
    setProblem("");
    setLoading(true);
    Promise.all([
      call<AgentEvaluationHealth>("GET", `/api/agents/${encodeURIComponent(name)}/health`),
      call<AgentChange[]>("GET", `/api/agents/${encodeURIComponent(name)}/changes`),
    ])
      .then(
        ([health, changes]) => {
          if (!live) return;
          setHealth(health);
          setChanges(changes ?? []);
          setReadAt(new Date().toISOString());
        },
        (error: unknown) => {
          if (live) setProblem(error instanceof Error ? error.message : words.failed);
        },
      )
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [name, version, tick]);
  const refresh = () => setTick((t) => t + 1);
  const updated = readAt ? words.updated(ago(readAt)) : "";
  const failures = (health?.results ?? []).filter((result) => !result.passed).slice(0, 3);
  return (
    <section aria-label={words.label} aria-busy={loading} className="flex min-w-0 flex-col gap-3">
      <div className="flex min-h-9 items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1">
          {problem ? (
            <>
              <Problem message={problem} />
              <button type="button" className={button} onClick={refresh}>
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
                  ? words.summary(health.passed, health.samples, Math.round(health.pass_rate))
                  : words.none}
              </span>
            </>
          )}
          <About />
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          {actions}
          {updated && !problem && (
            <span className="hidden text-aui-2xs text-aui-muted sm:inline">{updated}</span>
          )}
          {!problem && (
            <Tooltip label={updated ? `${words.refresh}. ${updated}` : words.refresh}>
              <button
                type="button"
                className={iconButton}
                aria-label={words.refresh}
                disabled={loading}
                onClick={refresh}
              >
                <RefreshIcon size={16} className={loading ? "motion-safe:animate-spin" : ""} />
              </button>
            </Tooltip>
          )}
        </div>
      </div>
      {health && (
        <div className={`grid min-w-0 gap-3 ${suites ? "md:grid-cols-2" : ""}`}>
          <RecentRuns health={health} agent={name} />
          {suites && (
            <Suites
              agent={name}
              suites={suites}
              canRun={canRun}
              onRan={() => {
                onSuitesChanged?.();
                refresh();
              }}
            />
          )}
        </div>
      )}
      {failures.length > 0 && (
        <section aria-label={words.failures} className={`${card} flex flex-col gap-3 text-aui-sm`}>
          <h3 className="font-medium text-aui-sm">{words.failures}</h3>
          <ul className="flex flex-col gap-3">
            {failures.map((result) => (
              <li key={result.run} className="flex flex-col gap-1">
                <p className="text-aui-xs text-aui-muted">
                  {words.revisionAt(result.version, ago(result.at))}
                </p>
                <ul className="list-inside list-disc text-aui-fg">
                  {result.verdicts
                    .filter((verdict) => !verdict.passed)
                    .map((verdict) => (
                      <li key={verdict.check}>{verdict.reason || checkName(verdict.check)}</li>
                    ))}
                </ul>
              </li>
            ))}
          </ul>
        </section>
      )}
      {changes.length > 0 && (
        <section aria-label={words.changes} className={`${card} flex flex-col gap-3`}>
          <h3 className="font-medium text-aui-sm">{words.changes}</h3>
          <ul className="flex flex-col divide-y divide-[var(--humagen-rule)]">
            {changes.slice(0, 5).map((change) => (
              <li
                key={change.id}
                className="flex flex-col gap-1.5 py-2.5 text-aui-sm first:pt-0 last:pb-0"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Pill tone={!change.evaluated ? "neutral" : change.passed ? "ok" : "warn"}>
                    {!change.evaluated
                      ? words.change.incomplete
                      : change.passed
                        ? words.change.passed
                        : words.change.regressed}
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
                    {!score.passed && <Pill tone="warn">{words.regression}</Pill>}
                  </p>
                ))}
                {change.override_reason && (
                  <p className="text-aui-muted">{words.override(change.override_reason)}</p>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
    </section>
  );
}

/** The figures under an agent's health: each a value or "—", with what it counts on hover or focus. */
export function AgentStats({
  stats,
  openAlerts,
  suites,
}: {
  stats?: { runs: number; completed: number; failed: number; p50_ms?: number };
  openAlerts: number;
  suites: number;
}) {
  const w = defaultCopy.agentStats;
  const runs = stats?.runs ?? 0;
  const ended = stats ? stats.completed + stats.failed : 0;
  const tiles: { label: string; value: string; tip: string }[] = [
    { label: w.runs, value: String(runs), tip: runs ? w.label : w.noRuns },
    {
      label: w.success,
      value: ended ? `${Math.round((100 * (stats?.completed ?? 0)) / ended)}%` : w.none,
      tip: ended ? w.successOf(stats?.completed ?? 0, ended) : w.noEnded,
    },
    {
      label: w.median,
      value: stats?.p50_ms ? took(stats.p50_ms) : w.none,
      tip: stats?.p50_ms ? w.medianOf : w.noRuns,
    },
    { label: w.alerts, value: String(openAlerts), tip: w.alertsOf },
    { label: w.suites, value: String(suites), tip: w.suitesOf },
  ];
  return (
    <dl aria-label={w.label} className="grid grid-cols-2 gap-3 sm:grid-cols-5">
      {tiles.map((t) => (
        <div key={t.label} className={`${card} flex min-w-0 flex-col gap-1 py-3`}>
          <dt className="text-aui-xs text-aui-muted">{t.label}</dt>
          <dd>
            <Tooltip label={t.tip}>
              <span
                // biome-ignore lint/a11y/noNoninteractiveTabindex: the figure's meaning is on focus as on hover
                tabIndex={0}
                className="rounded-aui-sm text-aui-lg font-semibold text-aui-fg tabular-nums focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                {t.value === w.none ? (
                  <>
                    <span aria-hidden>{t.value}</span>
                    <span className="sr-only">{t.tip}</span>
                  </>
                ) : (
                  t.value
                )}
              </span>
            </Tooltip>
          </dd>
        </div>
      ))}
    </dl>
  );
}
