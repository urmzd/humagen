import { defaultCopy } from "@humagen/components";
import { toast } from "@humagen/render-web/ui/sonner";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type EvalCase,
  type EvalResult,
  type EvalRun,
  type Suite,
  type SuiteView,
} from "./api.ts";
import { Pill, type Tone } from "./cards.tsx";
import {
  ago,
  button,
  Confirm,
  card,
  Empty,
  field,
  Label,
  MoreMenu,
  Problem,
  primary,
  rows,
  Section,
  SkeletonRows,
  textAction,
} from "./ui.tsx";

const words = defaultCopy.corpora.evals;
const message = (e: unknown) => (e instanceof ApiError ? e.message : defaultCopy.corpora.failed);

/** How often a run in progress is read again, so its cases land as they finish. */
const POLL_MS = 2000;

const runTone: Record<EvalRun["status"], Tone> = {
  running: "accent",
  passed: "ok",
  failed: "bad",
  errored: "bad",
};

/** The suite as the server takes it: without the runs a listing adds to it. */
function bare(view: SuiteView): Suite {
  const { runs: _runs, ...suite } = view;
  return suite;
}

/** A case's id, which only has to differ from the suite's other cases. */
function caseId(): string {
  return `c${Date.now().toString(36)}${Math.floor(Math.random() * 1e4)}`;
}

/** The name of a corpus's first suite: the corpus's own name, kept within a suite name's 63 characters. */
function suiteName(corpus: string): string {
  return `${corpus.slice(0, 53)}-retrieval`;
}

/** A score from 0 to 1, to two places. A run that kept none shows a dash. */
function score(run: EvalRun, metric: string): string {
  const n = run.metrics?.[metric];
  return n === undefined ? "-" : n.toFixed(2);
}

/**
 * A corpus's Evals tab: its retrieval suites, each a list of golden cases (a
 * question and the documents it should return), and the scores of each run.
 * Someone who may write evals adds and removes cases and starts runs; anyone
 * else who may read them sees the same page without those controls.
 */
export function CorpusEvals({ corpus }: { corpus: string }) {
  // undefined while loading, null when the reader may not read evals.
  const [suites, setSuites] = useState<SuiteView[] | null | undefined>(undefined);
  const [canEdit, setCanEdit] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const load = useCallback(async () => {
    try {
      const all = await api.suites();
      setSuites(all.filter((s) => s.target === "corpus" && s.subject === corpus));
      setProblem(null);
    } catch {
      // A list already on view stays, under the reason it could not be read again.
      setSuites((was) => was ?? []);
      setProblem(words.loadFailed);
    }
  }, [corpus]);

  useEffect(() => {
    let live = true;
    api.me().then(
      (me) => {
        if (!live) return;
        if (!me.permissions.includes("evals:read")) {
          setSuites(null);
          return;
        }
        setCanEdit(me.permissions.includes("evals:write"));
        void load();
      },
      () => {
        if (!live) return;
        setSuites([]);
        setProblem(words.loadFailed);
      },
    );
    return () => {
      live = false;
    };
  }, [load]);

  // A run scores its cases in the background, so the page reads it again until it ends.
  const running = (suites ?? []).some((s) => s.runs[0]?.status === "running");
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => void load(), POLL_MS);
    return () => clearInterval(timer);
  }, [running, load]);

  if (suites === undefined) return <SkeletonRows count={3} height="3rem" />;
  if (suites === null) return <Empty title={words.noAccess} />;

  const create = async (c: EvalCase) => {
    await api.createSuite({
      name: suiteName(corpus),
      target: "corpus",
      subject: corpus,
      cases: [c],
    });
    setAdding(false);
    toast.success(words.added);
    await load();
  };

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <Problem message={problem} />
      {suites.length === 0 && !problem && (
        <div className="flex flex-col items-start gap-3">
          <Empty title={words.none} />
          {canEdit && !adding && (
            <button type="button" className={primary} onClick={() => setAdding(true)}>
              {words.addCase}
            </button>
          )}
          {adding && <CaseForm onSave={create} onClose={() => setAdding(false)} />}
        </div>
      )}
      {suites.map((s) => (
        <SuiteSection key={s.name} suite={s} canEdit={canEdit} onChanged={load} />
      ))}
    </div>
  );
}

/** One suite: its cases with how each fared in the latest run, and its runs, newest first. */
function SuiteSection({
  suite,
  canEdit,
  onChanged,
}: {
  suite: SuiteView;
  canEdit: boolean;
  onChanged: () => Promise<void>;
}) {
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  // What is waiting on a second step: deleting the suite, or removing its last case, which does the same.
  const [asking, setAsking] = useState<"delete" | "last" | null>(null);
  const last = suite.runs[0];
  const running = last?.status === "running";
  const results = new Map((last?.results ?? []).map((r) => [r.case_id, r]));

  const act = async (run: () => Promise<unknown>, done: string) => {
    setBusy(true);
    setProblem(null);
    try {
      await run();
      toast.success(done);
      await onChanged();
    } catch (e) {
      setProblem(message(e));
    } finally {
      setBusy(false);
    }
  };
  const save = (cases: EvalCase[], done: string) =>
    act(() => api.saveSuite({ ...bare(suite), cases }), done);
  const remove = (c: EvalCase) => {
    // A suite cannot be kept without a case, so removing the last one deletes it, after asking.
    if (suite.cases.length === 1) setAsking("last");
    else
      void save(
        suite.cases.filter((x) => x.id !== c.id),
        words.removed,
      );
  };

  return (
    <Section
      title={suite.name}
      count={suite.cases.length}
      actions={
        canEdit && (
          <>
            <button
              type="button"
              className={button}
              disabled={busy || adding}
              onClick={() => setAdding(true)}
            >
              {words.addCase}
            </button>
            <button
              type="button"
              className={primary}
              disabled={busy || running || suite.cases.length === 0}
              onClick={() => void act(() => api.runSuite(suite.name), words.started)}
            >
              {words.run}
            </button>
            <MoreMenu
              items={[
                {
                  label: words.deleteSuite,
                  destructive: true,
                  disabled: busy,
                  onSelect: () => setAsking("delete"),
                },
              ]}
            />
          </>
        )
      }
    >
      <Problem message={problem} />
      {asking && (
        <Confirm
          question={asking === "last" ? words.lastCaseAsk(suite.name) : words.deleteAsk(suite.name)}
          action={words.deleteSuite}
          onKeep={() => setAsking(null)}
          onConfirm={() => {
            setAsking(null);
            void act(() => api.deleteSuite(suite.name), words.deleted(suite.name));
          }}
        />
      )}
      {adding && (
        <CaseForm
          onClose={() => setAdding(false)}
          onSave={async (c) => {
            await api.saveSuite({ ...bare(suite), cases: [...suite.cases, c] });
            setAdding(false);
            toast.success(words.added);
            await onChanged();
          }}
        />
      )}
      {suite.cases.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.noCases}</p>
      ) : (
        <ul aria-label={words.cases} className={rows}>
          {suite.cases.map((c) => (
            <li
              key={c.id}
              aria-label={c.input}
              className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 py-2.5"
            >
              <div className="flex min-w-0 flex-1 basis-48 flex-col gap-0.5">
                <span className="text-aui-sm text-aui-fg">{c.input}</span>
                <span className="text-aui-xs text-aui-muted">{(c.expect ?? []).join(", ")}</span>
              </div>
              <CaseOutcome result={results.get(c.id)} />
              {canEdit && (
                <button
                  type="button"
                  className={textAction}
                  aria-label={words.removeCase(c.input)}
                  disabled={busy}
                  onClick={() => remove(c)}
                >
                  {words.remove}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <Runs runs={suite.runs} />
    </Section>
  );
}

/** How a case fared in the latest run: where its first expected document ranked, or why it was not scored. */
function CaseOutcome({ result }: { result?: EvalResult }) {
  if (!result) return null;
  if (result.error)
    return (
      <span className="flex min-w-0 items-center gap-2">
        <span className="min-w-0 truncate text-aui-xs text-aui-muted" title={result.error}>
          {result.error}
        </span>
        <Pill tone="bad">{words.caseErrored}</Pill>
      </span>
    );
  return (
    <span className="flex shrink-0 items-center gap-2">
      {result.rank !== undefined && result.rank > 0 && (
        <span className="text-aui-xs tabular-nums text-aui-muted">{words.rank(result.rank)}</span>
      )}
      <Pill tone={result.passed ? "ok" : "bad"}>
        {result.passed ? words.casePassed : words.caseMissed}
      </Pill>
    </span>
  );
}

/** A suite's runs as a table, newest first: when, how it ended, and its recall, precision and nDCG. */
function Runs({ runs }: { runs: EvalRun[] }) {
  if (runs.length === 0) return <p className="text-aui-xs text-aui-muted">{words.noRuns}</p>;
  const num = "py-1.5 pl-3 text-right tabular-nums";
  return (
    <div className="aui-scroll min-w-0 overflow-x-auto">
      <table aria-label={words.runs} className="w-full text-aui-sm">
        <thead>
          <tr className="text-left text-aui-2xs text-aui-muted">
            <th className="pb-1 font-medium">{words.when}</th>
            <th className="pb-1 pl-3 font-medium">{words.state}</th>
            <th className="pb-1 pl-3 font-medium">{words.result}</th>
            <th className="pb-1 pl-3 text-right font-medium">{words.recall}</th>
            <th className="pb-1 pl-3 text-right font-medium">{words.precision}</th>
            <th className="pb-1 pl-3 text-right font-medium">{words.ndcg}</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id} className="border-aui-border border-t align-top">
              <td className="whitespace-nowrap py-1.5 text-aui-muted">{ago(r.started_at)}</td>
              <td className="py-1.5 pl-3">
                <Pill tone={runTone[r.status]}>{words.status[r.status]}</Pill>
              </td>
              <td className="py-1.5 pl-3 text-aui-muted">
                {r.status === "running"
                  ? words.progress(r.results?.length ?? 0, r.total)
                  : r.status === "errored" && r.error
                    ? r.error
                    : words.passedOf(r.passed, r.total)}
              </td>
              <td className={num}>{score(r, "recall")}</td>
              <td className={num}>{score(r, "precision")}</td>
              <td className={num}>{score(r, "ndcg")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** A new case: the question, and the titles of the documents a search for it should return. */
function CaseForm({
  onSave,
  onClose,
}: {
  onSave: (c: EvalCase) => Promise<void>;
  onClose: () => void;
}) {
  const [input, setInput] = useState("");
  const [expect, setExpect] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const titles = expect
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await onSave({ id: caseId(), input: input.trim(), expect: titles });
    } catch (err) {
      // The form stays open with what was typed, under the server's reason.
      setProblem(message(err));
      setBusy(false);
    }
  };

  return (
    <form
      aria-label={words.newCase}
      className={`${card} flex w-full min-w-0 flex-col gap-3`}
      onSubmit={submit}
    >
      <Label text={words.query} hint={words.queryHint}>
        <input
          className={field}
          value={input}
          required
          onChange={(e) => setInput(e.target.value)}
        />
      </Label>
      <Label text={words.expect} hint={words.expectHint}>
        <input
          className={field}
          value={expect}
          required
          onChange={(e) => setExpect(e.target.value)}
        />
      </Label>
      <Problem message={problem} />
      <div className="flex flex-wrap gap-2">
        <button
          type="submit"
          className={primary}
          disabled={busy || !input.trim() || titles.length === 0}
        >
          {words.save}
        </button>
        <button type="button" className={button} onClick={onClose}>
          {words.cancel}
        </button>
      </div>
    </form>
  );
}
