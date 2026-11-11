import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useState } from "react";
import { ApiError, api, type Check, type EvalCandidate, type SuiteView } from "./api.ts";
import { checksReady, draftChecks } from "./evalCandidates.ts";
import {
  AgentMark,
  ago,
  button,
  card,
  field,
  LoadMore,
  Menu,
  Problem,
  primary,
  SkeletonRows,
} from "./ui.tsx";

const words = defaultCopy.evalCandidates;

/** The reason a rating gave, as the feedback prompt names it. */
function reasonLabel(reason: string): string {
  return defaultCopy.feedbackPrompt.reasons.find((r) => r.id === reason)?.label ?? reason;
}

/** Choosing the suite and writing the checks for one candidate. */
function AddToSuite({
  candidate,
  suites,
  onClose,
  onAdded,
}: {
  candidate: EvalCandidate;
  suites: SuiteView[];
  onClose: () => void;
  onAdded: () => void;
}) {
  const [suite, setSuite] = useState(suites[0]?.name ?? "");
  const [checks, setChecks] = useState<Check[]>(() => draftChecks(candidate.reason));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const setCheck = (i: number, patch: Partial<Check>) =>
    setChecks((all) => all.map((c, n) => (n === i ? { ...c, ...patch } : c)));

  if (suites.length === 0)
    return (
      <div className="flex flex-col gap-2">
        <p className="text-aui-sm text-aui-muted">{words.noSuite}</p>
        <button type="button" className={`${button} self-start`} onClick={onClose}>
          {words.cancel}
        </button>
      </div>
    );

  const add = () => {
    setBusy(true);
    setProblem(null);
    api
      .acceptEvalCandidate(candidate.id, suite, checks)
      .then(onAdded)
      .catch((e) => {
        setBusy(false);
        setProblem(e instanceof ApiError ? e.message : words.acceptFailed);
      });
  };

  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        add();
      }}
    >
      <Menu
        label={words.suite}
        className="w-64"
        value={suite}
        options={suites.map((s) => ({ value: s.name, label: s.name }))}
        onChange={setSuite}
      />
      <span className="text-aui-xs font-medium text-aui-muted">{words.passesWhen}</span>
      {checks.map((ch, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: checks have no identity beyond their position
        <div key={i} className="flex gap-2">
          <Menu
            label={words.check}
            className="w-48 shrink-0"
            value={ch.kind}
            options={Object.entries(words.checkKinds).map(([value, label]) => ({
              value: value as Check["kind"],
              label,
            }))}
            onChange={(kind) => setCheck(i, { kind })}
          />
          <input
            aria-label={words.checkValue}
            className={field}
            value={ch.value}
            required
            onChange={(e) => setCheck(i, { value: e.target.value })}
          />
          {checks.length > 1 && (
            <button
              type="button"
              aria-label={words.removeCheck}
              className={`${button} shrink-0 px-2`}
              onClick={() => setChecks(checks.filter((_, n) => n !== i))}
            >
              ✕
            </button>
          )}
        </div>
      ))}
      <button
        type="button"
        className={`${button} h-7 self-start px-2 text-aui-xs`}
        onClick={() => setChecks([...checks, { kind: "contains", value: "" }])}
      >
        {words.addCheck}
      </button>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} onClick={onClose}>
          {words.cancel}
        </button>
        <button type="submit" className={primary} disabled={busy || !checksReady(checks)}>
          {words.confirm}
        </button>
      </div>
    </form>
  );
}

/**
 * The questions behind an agent's answers that someone rated down, waiting
 * for a reviewer to add each to one of the agent's suites or dismiss it. It
 * shows only what the reader may read of the runs they came from, and
 * nothing at all while the queue is empty.
 */
export function EvalCandidates({
  agent,
  suites,
  canWrite,
  onChanged,
}: {
  agent: string;
  suites: SuiteView[];
  canWrite: boolean;
  onChanged: () => void;
}) {
  const [candidates, setCandidates] = useState<EvalCandidate[] | null>(null);
  const [next, setNext] = useState("");
  const [more, setMore] = useState(false);
  const [adding, setAdding] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  const read = useCallback(
    (after: string) =>
      api.evalCandidates(agent, after).then((page) => {
        setCandidates((have) => (after ? [...(have ?? []), ...page.candidates] : page.candidates));
        setNext(page.next);
      }),
    [agent],
  );
  useEffect(() => {
    let live = true;
    setCandidates(null);
    setProblem(null);
    read("").catch((e) => {
      if (!live) return;
      setCandidates([]);
      setProblem(e instanceof ApiError ? e.message : words.loadFailed);
    });
    return () => {
      live = false;
    };
  }, [read]);

  const leave = (id: string) => setCandidates((have) => (have ?? []).filter((c) => c.id !== id));
  const dismiss = (id: string) => {
    setProblem(null);
    api
      .dismissEvalCandidate(id)
      .then(() => leave(id))
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.dismissFailed));
  };

  if (candidates === null) return <SkeletonRows count={2} />;
  // A page emptied by the reviewer still leads to the pages after it.
  if (candidates.length === 0 && !problem && !next) return null;
  return (
    <section className={`${card} flex flex-col gap-3`} aria-label={words.title}>
      <h3 className="text-aui-base font-medium text-aui-fg">{words.title}</h3>
      <Problem message={problem} />
      <ul className="flex flex-col">
        {candidates.map((c) => (
          <li
            key={c.id}
            className="flex flex-col gap-2 border-aui-border border-t py-3 first:border-t-0"
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">
                  <span className="sr-only">{words.asked}: </span>
                  {c.input}
                </p>
                <p className="text-aui-xs text-aui-muted">
                  <span className="font-medium">{reasonLabel(c.reason)}</span>
                  {c.comment ? `: ${c.comment}` : ""}
                </p>
                <p className="flex items-center gap-1.5 text-aui-xs text-aui-muted">
                  <AgentMark name={c.agent} size={16} />
                  <span>{c.agent}</span>
                  <time dateTime={c.at}>{ago(c.at)}</time>
                </p>
              </div>
              {canWrite && adding !== c.id && (
                <div className="flex shrink-0 gap-2">
                  <button type="button" className={primary} onClick={() => setAdding(c.id)}>
                    {words.addToSuite}
                  </button>
                  <button type="button" className={button} onClick={() => dismiss(c.id)}>
                    {words.dismiss}
                  </button>
                </div>
              )}
            </div>
            {adding === c.id && (
              <AddToSuite
                candidate={c}
                suites={suites}
                onClose={() => setAdding(null)}
                onAdded={() => {
                  setAdding(null);
                  leave(c.id);
                  onChanged();
                }}
              />
            )}
          </li>
        ))}
      </ul>
      {next && (
        <LoadMore
          busy={more}
          onMore={() => {
            setMore(true);
            read(next)
              .catch((e) => setProblem(e instanceof ApiError ? e.message : words.loadFailed))
              .finally(() => setMore(false));
          }}
        />
      )}
    </section>
  );
}
