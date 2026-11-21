import { defaultCopy } from "@humagen/components";
import { type FormEvent, type ReactNode, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type Check,
  type EvalCase,
  type EvalRun,
  type Suite,
  type SuiteView,
} from "./api.ts";
import { EvalCandidates } from "./EvalCandidates.tsx";
import {
  MAX_EVERY,
  MIN_EVERY,
  PRESETS,
  type Preset,
  presetOf,
  scheduleLine,
  validEvery,
} from "./evalHealth.ts";
import { age } from "./format.ts";
import { ago, button, Chip, card, field, Label, Menu, Problem, primary, took } from "./ui.tsx";

const sched = defaultCopy.evalSchedule;

/**
 * Setting, changing or turning off how often a suite runs on its own. Saving
 * makes the schedule act for the person who saves, as every save of a suite
 * does, so the editor says who it runs as now and that saving changes it.
 */
function ScheduleEditor({
  suite,
  onClose,
  onSaved,
}: {
  suite: SuiteView;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [preset, setPreset] = useState<Preset>(presetOf(suite.every_seconds));
  const [hours, setHours] = useState(String(Math.round((suite.every_seconds || 2 * 3600) / 3600)));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const seconds = preset === "custom" ? Math.round(Number(hours) * 3600) : PRESETS[preset];
  const valid = validEvery(seconds) && !(preset === "custom" && seconds === 0);
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (!valid) return;
    setBusy(true);
    setProblem(null);
    api
      .saveSuite({ ...suite, every_seconds: seconds })
      .then(onSaved)
      .catch((err) => {
        setBusy(false);
        setProblem(err instanceof ApiError ? err.message : sched.failed);
      });
  };
  return (
    <form
      id={`schedule-${suite.name}`}
      aria-label={sched.editFor(suite.name)}
      onSubmit={save}
      className="flex flex-col gap-3 rounded-aui border border-aui-border p-3"
    >
      {/* The presets wrap rather than scroll, so every one is in view at phone width. */}
      <fieldset className="flex min-w-0 flex-wrap gap-1">
        <legend className="sr-only">{sched.label}</legend>
        {(["off", "hourly", "six", "daily", "weekly", "custom"] as const).map((id) => (
          <button
            key={id}
            type="button"
            aria-pressed={preset === id}
            onClick={() => setPreset(id)}
            className="inline-flex h-8 pointer-coarse:h-11 shrink-0 items-center whitespace-nowrap rounded-aui-sm border border-aui-border px-2.5 text-aui-xs text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:border-aui-fg aria-pressed:bg-aui-hover aria-pressed:text-aui-fg motion-reduce:transition-none"
          >
            {sched.presets[id]}
          </button>
        ))}
      </fieldset>
      {preset === "custom" && (
        <Label text={sched.hours} hint={sched.hoursHint}>
          <input
            className={`${field} w-32`}
            type="number"
            inputMode="numeric"
            min={MIN_EVERY / 3600}
            max={MAX_EVERY / 3600}
            step={1}
            value={hours}
            aria-invalid={!valid}
            onChange={(e) => setHours(e.target.value)}
          />
        </Label>
      )}
      {!valid && <p className="text-aui-sm text-aui-bad">{sched.invalid}</p>}
      <p className="text-aui-xs text-aui-muted">
        {suite.created_by_name ? `${sched.actsFor(suite.created_by_name)} ` : ""}
        {sched.actsForYou}
      </p>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} onClick={onClose}>
          {sched.cancel}
        </button>
        <button type="submit" className={primary} disabled={busy || !valid}>
          {sched.save}
        </button>
      </div>
    </form>
  );
}

const checkLabel: Record<Check["kind"], string> = {
  contains: "Contains",
  not_contains: "Does not contain",
  matches: "Matches pattern",
  equals: "Equals",
  within_ms: "Answers within (ms)",
};

const metricLabel: Record<string, string> = {
  hit_rate: "Hit rate",
  mrr: "MRR",
  recall: "Recall",
  precision: "Precision",
  ndcg: "nDCG",
};

/** An outcome as a mark and a word. */
function Outcome({ status }: { status: EvalRun["status"] | "pass" | "fail" }) {
  const good = status === "passed" || status === "pass";
  const running = status === "running";
  return (
    <span className="inline-flex items-center gap-1.5 text-aui-xs text-aui-muted">
      <span
        aria-hidden
        className={`size-1.5 rounded-full ${running ? "bg-aui-neutral" : good ? "bg-aui-ok" : status === "failed" ? "bg-aui-warn" : "bg-aui-bad"}`}
      />
      {running ? "Running" : good ? "Passed" : status === "errored" ? "Errored" : "Failed"}
    </span>
  );
}

/** A run's bar colour: neutral when it reached its threshold, warn below it, bad only when it could not run. */
export function barTone(run: Pick<EvalRun, "status" | "pass_rate" | "threshold">): string {
  if (run.status === "errored") return "bg-aui-bad";
  return run.pass_rate >= run.threshold ? "bg-aui-neutral" : "bg-aui-warn";
}

/**
 * Pass rate across the last runs, oldest first, each labelled with how long
 * ago it ran. The height is the measure and the line marks the rate a run has
 * to reach; colour says only whether it reached it (neutral), fell short
 * (warn) or could not run (bad).
 */
function History({ runs, threshold }: { runs: EvalRun[]; threshold: number }) {
  const ended = runs
    .filter((r) => r.status !== "running")
    .slice(0, 12)
    .reverse();
  if (ended.length < 2) return null;
  return (
    <figure className="flex flex-col gap-1">
      <figcaption className="text-aui-xs text-aui-muted">
        Pass rate, last {ended.length} runs
      </figcaption>
      <div className="relative flex h-16 items-end gap-1">
        <span
          aria-hidden
          className="absolute inset-x-0 border-aui-muted/50 border-t border-dashed"
          style={{ bottom: `${threshold}%` }}
        />
        {ended.map((r) => (
          <div
            key={r.id}
            className="flex h-full w-6 flex-col justify-end"
            title={`${Math.round(r.pass_rate)}%${r.rev ? `, revision ${r.rev}` : ""}, ${ago(r.started_at)}`}
          >
            <span
              data-tone={barTone(r)}
              className={`w-full rounded-t-sm ${barTone(r)}`}
              style={{ height: `${Math.max(r.pass_rate, 2)}%` }}
            />
          </div>
        ))}
      </div>
      <div className="flex gap-1 text-aui-2xs tabular-nums text-aui-muted">
        {ended.map((r) => (
          <time key={r.id} dateTime={r.started_at} className="w-6 text-center">
            {age(r.started_at)}
          </time>
        ))}
      </div>
    </figure>
  );
}

function Results({ run, onOpenRun }: { run: EvalRun; onOpenRun?: (id: string) => void }) {
  return (
    <ul className="flex flex-col">
      {run.results.map((r) => (
        <li key={r.case_id} className="border-aui-border border-t py-2.5 first:border-t-0">
          <details>
            <summary className="flex cursor-pointer list-none items-center gap-3 text-aui-sm">
              <Outcome status={r.passed ? "pass" : "fail"} />
              <span className="min-w-0 flex-1 truncate text-aui-fg">{r.input}</span>
              {r.rank !== undefined && r.rank > 0 && <Chip>Rank {r.rank}</Chip>}
              <span className="shrink-0 text-aui-xs tabular-nums text-aui-muted">
                {took(r.duration_ms)}
              </span>
            </summary>
            <div className="flex flex-col gap-2 pt-2 pl-4 text-aui-xs">
              {r.error && <p className="text-aui-fg">{r.error}</p>}
              {r.output && (
                <p className="whitespace-pre-wrap rounded-aui border border-aui-border p-2 text-aui-muted">
                  {r.output}
                </p>
              )}
              {(r.verdicts ?? []).map((v) => (
                <p key={v.check} className="flex items-baseline gap-2">
                  <span className="shrink-0 text-aui-muted">{v.passed ? "Passed" : "Failed"}</span>
                  <code className="font-aui-mono text-aui-fg">{v.check}</code>
                  {v.reason && <span className="text-aui-muted">{v.reason}</span>}
                </p>
              ))}
              {(r.hits ?? []).length > 0 && (
                <ol className="flex flex-col gap-0.5">
                  {(r.hits ?? []).map((h, i) => (
                    <li key={`${h.title}:${i}`} className="flex items-baseline gap-2">
                      <span className="w-4 tabular-nums text-aui-muted">{i + 1}.</span>
                      <span className="text-aui-fg">{h.title}</span>
                      {h.expected && <span className="text-aui-muted">expected</span>}
                    </li>
                  ))}
                </ol>
              )}
              {r.hits && r.hits.length === 0 && (
                <p className="text-aui-muted">Nothing was retrieved.</p>
              )}
              {r.run_id && onOpenRun && (
                <button
                  type="button"
                  onClick={() => onOpenRun(r.run_id as string)}
                  className="self-start rounded-aui-sm text-aui-muted underline-offset-2 hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  Open the run
                </button>
              )}
            </div>
          </details>
        </li>
      ))}
      {run.status === "running" && run.results.length < run.total && (
        <li className="border-aui-border border-t py-2.5 text-aui-xs text-aui-muted">
          {run.results.length} of {run.total} cases done
        </li>
      )}
    </ul>
  );
}

function slug(): string {
  return `c${Date.now().toString(36)}${Math.floor(Math.random() * 1e4)}`;
}

function SuiteForm({
  target,
  subject,
  initial,
  onClose,
  onSaved,
}: {
  target: Suite["target"];
  subject: string;
  initial: Suite | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const blankCase = (): EvalCase =>
    target === "agent"
      ? { id: slug(), input: "", checks: [{ kind: "contains", value: "" }] }
      : { id: slug(), input: "", expect: [""] };
  const [suite, setSuite] = useState<Suite>(
    initial ?? {
      name: "",
      target,
      subject,
      cases: [blankCase()],
      pass_rate: 100,
      k: target === "knowledge" ? 5 : undefined,
    },
  );
  const [problem, setProblem] = useState<string | null>(null);
  const setCase = (i: number, patch: Partial<EvalCase>) =>
    setSuite((s) => ({ ...s, cases: s.cases.map((c, n) => (n === i ? { ...c, ...patch } : c)) }));

  const save = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    (initial ? api.saveSuite(suite) : api.createSuite(suite))
      .then(onSaved)
      .catch((err) =>
        setProblem(err instanceof ApiError ? err.message : "The suite could not be saved."),
      );
  };

  return (
    <form onSubmit={save} className={`${card} flex flex-col gap-3`}>
      <h3 className="text-aui-base font-medium text-aui-fg">
        {initial ? `Edit ${initial.name}` : "New eval"}
      </h3>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Label text="Name" hint="Lowercase letters, digits, dashes.">
          <input
            className={field}
            value={suite.name}
            disabled={!!initial}
            required
            onChange={(e) => setSuite({ ...suite, name: e.target.value })}
          />
        </Label>
        <Label text="Passes when at least (%)" hint="Share of cases that must pass.">
          <input
            className={field}
            type="number"
            min={1}
            max={100}
            value={suite.pass_rate ?? 100}
            onChange={(e) => setSuite({ ...suite, pass_rate: Number(e.target.value) })}
          />
        </Label>
        {target === "knowledge" && (
          <Label text="Judge the top" hint="How many results count.">
            <input
              className={field}
              type="number"
              min={1}
              max={50}
              value={suite.k ?? 5}
              onChange={(e) => setSuite({ ...suite, k: Number(e.target.value) })}
            />
          </Label>
        )}
      </div>

      <ol className="flex flex-col gap-3">
        {suite.cases.map((c, i) => (
          <li key={c.id} className="flex flex-col gap-2 rounded-aui border border-aui-border p-3">
            <div className="flex items-start gap-2">
              <Label
                text={
                  target === "agent"
                    ? `Case ${i + 1}: send the agent`
                    : `Case ${i + 1}: ask the source`
                }
              >
                <textarea
                  className={`${field} min-h-10`}
                  value={c.input}
                  required
                  onChange={(e) => setCase(i, { input: e.target.value })}
                />
              </Label>
              {suite.cases.length > 1 && (
                <button
                  type="button"
                  className={`${button} mt-5 h-7 shrink-0 px-2 text-aui-xs`}
                  onClick={() =>
                    setSuite({ ...suite, cases: suite.cases.filter((_, n) => n !== i) })
                  }
                >
                  Remove
                </button>
              )}
            </div>
            {target === "agent" ? (
              <div className="flex flex-col gap-1.5">
                <span className="text-aui-xs font-medium text-aui-muted">
                  The answer passes when it
                </span>
                {(c.checks ?? []).map((ch, n) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: checks have no identity beyond their position
                  <div key={n} className="flex gap-2">
                    <Menu
                      label="Check"
                      className="w-48 shrink-0"
                      value={ch.kind}
                      options={Object.entries(checkLabel).map(([k, l]) => ({
                        value: k as Check["kind"],
                        label: l,
                      }))}
                      onChange={(kind) =>
                        setCase(i, {
                          checks: (c.checks ?? []).map((x, m) => (m === n ? { ...x, kind } : x)),
                        })
                      }
                    />
                    <input
                      aria-label="Value"
                      className={field}
                      value={ch.value}
                      required
                      onChange={(e) =>
                        setCase(i, {
                          checks: (c.checks ?? []).map((x, m) =>
                            m === n ? { ...x, value: e.target.value } : x,
                          ),
                        })
                      }
                    />
                    {(c.checks ?? []).length > 1 && (
                      <button
                        type="button"
                        aria-label="Remove check"
                        className={`${button} shrink-0 px-2`}
                        onClick={() =>
                          setCase(i, { checks: (c.checks ?? []).filter((_, m) => m !== n) })
                        }
                      >
                        ✕
                      </button>
                    )}
                  </div>
                ))}
                <button
                  type="button"
                  className={`${button} h-7 self-start px-2 text-aui-xs`}
                  onClick={() =>
                    setCase(i, { checks: [...(c.checks ?? []), { kind: "contains", value: "" }] })
                  }
                >
                  Add Check
                </button>
              </div>
            ) : (
              <Label
                text="Should retrieve the document titled"
                hint="Separate several titles with a comma. One in the top results is a pass."
              >
                <input
                  className={field}
                  value={(c.expect ?? []).join(", ")}
                  required
                  onChange={(e) =>
                    setCase(i, { expect: e.target.value.split(",").map((x) => x.trim()) })
                  }
                />
              </Label>
            )}
          </li>
        ))}
      </ol>
      <button
        type="button"
        className={`${button} self-start`}
        onClick={() => setSuite({ ...suite, cases: [...suite.cases, blankCase()] })}
      >
        Add Case
      </button>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className={primary}>
          {initial ? "Save Eval" : "Create Eval"}
        </button>
      </div>
    </form>
  );
}

/** Evals for one agent or one knowledge source. */
export function Evals({
  target,
  subject,
  suites,
  canWrite,
  onChanged,
  onOpenRun,
  header,
}: {
  target: Suite["target"];
  subject: string;
  suites: SuiteView[];
  canWrite: boolean;
  onChanged: () => void;
  onOpenRun?: (id: string) => void;
  /** Draws the tab's top row around its New Eval action, as an agent's health does. */
  header?: (action: ReactNode) => ReactNode;
}) {
  const mine = suites.filter((s) => s.target === target && s.subject === subject);
  const [editing, setEditing] = useState<Suite | "new" | null>(null);
  // A link to a suite's schedule (#schedule-<name>, from the agent's health) opens its editor.
  const [scheduling, setScheduling] = useState<string | null>(() => {
    const hash = typeof window === "undefined" ? "" : decodeURIComponent(window.location.hash);
    return hash.startsWith("#schedule-") ? hash.slice("#schedule-".length) : null;
  });
  useEffect(() => {
    const open = () => {
      const hash = decodeURIComponent(window.location.hash);
      if (hash.startsWith("#schedule-")) setScheduling(hash.slice("#schedule-".length));
    };
    window.addEventListener("popstate", open);
    return () => window.removeEventListener("popstate", open);
  }, []);
  const [problem, setProblem] = useState<string | null>(null);
  const act = (p: Promise<unknown>, fallback: string) => {
    setProblem(null);
    p.then(onChanged).catch((e) => setProblem(e instanceof ApiError ? e.message : fallback));
  };

  const newEval =
    canWrite && !editing ? (
      <button type="button" className={primary} onClick={() => setEditing("new")}>
        New Eval
      </button>
    ) : null;

  return (
    <div id="eval-suites" className="flex flex-col gap-4">
      {header ? (
        header(newEval)
      ) : newEval ? (
        <div className="flex justify-end">{newEval}</div>
      ) : null}
      <Problem message={problem} />
      {editing && (
        <SuiteForm
          key={editing === "new" ? "new" : editing.name}
          target={target}
          subject={subject}
          initial={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            onChanged();
          }}
        />
      )}
      {target === "agent" && (
        <EvalCandidates agent={subject} suites={mine} canWrite={canWrite} onChanged={onChanged} />
      )}
      {mine.length === 0 && !editing && (
        <p className={`${card} text-aui-sm text-aui-muted`}>No evals yet.</p>
      )}
      {mine.map((s) => {
        const last = s.runs[0];
        const { runs: _runs, ...suite } = s;
        return (
          <section
            key={s.name}
            id={`suite-${s.name}`}
            className={`${card} flex scroll-mt-4 flex-col gap-3`}
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <h3 className="text-aui-base font-medium text-aui-fg">{s.name}</h3>
                  {last && <Outcome status={last.status} />}
                  {last && last.status !== "running" && (
                    <span className="text-aui-xs tabular-nums text-aui-muted">
                      {last.passed} of {last.total} passed
                      {last.rev ? `, revision ${last.rev}` : ""}, {ago(last.started_at)}
                    </span>
                  )}
                </div>
                <p className="text-aui-xs text-aui-muted">
                  {s.cases.length} {s.cases.length === 1 ? "case" : "cases"}, passes at{" "}
                  {s.pass_rate || 100}%
                </p>
                <p className="text-aui-xs text-aui-muted">
                  {scheduleLine(s)}
                  {s.every_seconds && s.created_by_name
                    ? `, ${sched.runsAs(s.created_by_name)}`
                    : ""}
                </p>
              </div>
              {canWrite && (
                <div className="flex min-w-0 flex-wrap gap-2">
                  <button
                    type="button"
                    className={primary}
                    disabled={last?.status === "running"}
                    onClick={() => act(api.runSuite(s.name), "The eval could not be started.")}
                  >
                    {last?.status === "running" ? "Running" : "Run Eval"}
                  </button>
                  <button
                    type="button"
                    className={button}
                    aria-expanded={scheduling === s.name}
                    onClick={() => setScheduling(scheduling === s.name ? null : s.name)}
                  >
                    {sched.edit}
                  </button>
                  <button type="button" className={button} onClick={() => setEditing(suite)}>
                    Edit
                  </button>
                  <button
                    type="button"
                    className={button}
                    onClick={() => act(api.deleteSuite(s.name), "The eval could not be deleted.")}
                  >
                    Delete
                  </button>
                </div>
              )}
            </div>
            {canWrite && scheduling === s.name && (
              <ScheduleEditor
                suite={s}
                onClose={() => setScheduling(null)}
                onSaved={() => {
                  setScheduling(null);
                  onChanged();
                }}
              />
            )}
            {last?.metrics && (
              <dl className="flex flex-wrap gap-x-6 gap-y-1 text-aui-xs">
                {Object.entries(metricLabel).map(([k, l]) => (
                  <div key={k} className="flex items-baseline gap-1.5">
                    <dt className="text-aui-muted">{l}</dt>
                    <dd className="tabular-nums text-aui-fg">
                      {(last.metrics?.[k] ?? 0).toFixed(2)}
                    </dd>
                  </div>
                ))}
              </dl>
            )}
            <History runs={s.runs} threshold={s.pass_rate || 100} />
            {last ? (
              <Results run={last} onOpenRun={onOpenRun} />
            ) : (
              <p className="text-aui-xs text-aui-muted">Not run yet.</p>
            )}
          </section>
        );
      })}
    </div>
  );
}
