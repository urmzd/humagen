import { defaultCopy } from "@humagen/components";
import { readReview, reviewPullRequest } from "@humagen/core";
import { Markdown } from "@humagen/primitives";
import { ChangeReview } from "@humagen/web";
import { useEffect, useMemo, useRef, useState } from "react";
import type { GitHubIssue, GitHubPull } from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { githubRef } from "./github-ref.ts";
import { openTab, type PaneTab } from "./pane.ts";
import { reviewPins } from "./review-pins.ts";
import type { ConversationView } from "./route.ts";

export const reviewView = (view: ConversationView, ref: string): ConversationView => ({
  ...openTab(view, { kind: "review", ref }),
  expanded: true,
});

export function reviewText(runs: { answer?: string }[], ref?: string): string {
  return (
    runs.find((run) => {
      const read = readReview(run.answer ?? "");
      return read.kind === "review" && reviewPullRequest(read.report)?.label === ref;
    })?.answer ?? ""
  );
}

const w = defaultCopy.githubTabs;
const button =
  "min-h-9 rounded-aui px-2 text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11";
export function GitHubPicker({
  onOpen,
  close,
  repo,
}: {
  onOpen: (tab: PaneTab) => void;
  close: () => void;
  repo?: string;
}) {
  const [text, setText] = useState("");
  const [repository, setRepository] = useState(repo ?? "");
  const [invalid, setInvalid] = useState(false);
  return (
    <form
      className="flex max-w-72 flex-col gap-2 border-t border-aui-border p-2"
      onSubmit={(e) => {
        e.preventDefault();
        const ref = githubRef(text, repository);
        if (!ref) {
          setInvalid(true);
          return;
        }
        onOpen({ kind: ref.kind, ref: `${ref.repo}#${ref.number}` });
        close();
      }}
    >
      <label className="text-aui-xs">
        {w.repository}
        <input
          className="w-full bg-aui-bg p-2"
          value={repository}
          onChange={(e) => setRepository(e.target.value)}
          placeholder="owner/repo"
        />
      </label>
      <label className="text-aui-xs">
        {w.address}
        <input
          className="w-full bg-aui-bg p-2"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="https://github.com/owner/repo/pull/1"
        />
      </label>
      {invalid && <p role="alert">{w.invalid}</p>}
      <button className={button} type="submit">
        {w.open}
      </button>
    </form>
  );
}
export function GitHubTab({ tab, review = "" }: { tab: PaneTab; review?: string }) {
  const ref = useMemo(() => githubRef(tab.ref ?? ""), [tab.ref]);
  const region = useRef<HTMLElement>(null);
  const pull = tab.kind !== "issue";
  const [value, setValue] = useState<GitHubPull | GitHubIssue>();
  const [problem, setProblem] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [page, setPage] = useState(1);
  const [phone, setPhone] = useState("changes");
  const [file, setFile] = useState("");
  useEffect(() => {
    void attempt;
    let live = true;
    setValue(undefined);
    setProblem(false);
    if (!ref) {
      setProblem(true);
      return;
    }
    const read = pull ? api.githubPull : api.githubIssue;
    read(ref.repo, ref.number, page).then(
      (v) => {
        if (live) setValue(v);
      },
      () => {
        if (live) setProblem(true);
      },
    );
    return () => {
      live = false;
    };
  }, [ref, pull, attempt, page]);
  if (problem)
    return (
      <div role="alert" className="p-4">
        {w.failed}
        <button type="button" className={button} onClick={() => setAttempt((n) => n + 1)}>
          {w.retry}
        </button>
      </div>
    );
  if (!value) return <output className="p-4">{w.loading}</output>;
  const parsed = readReview(review);
  const summary = parsed.kind === "review" ? parsed.report.summary : undefined;
  const pins = reviewPins(review);
  const files = "workflows" in value ? (value.files ?? []) : [];
  const selected = files.find((f) => f.filename === file) ?? files[0];
  const jump = (path: string, line: number) => {
    setFile(path);
    setPhone("changes");
    setTimeout(() => {
      region.current
        ?.querySelector(`[data-review-line="${line}"]`)
        ?.scrollIntoView?.({ block: "center" });
    }, 0);
  };
  return (
    <section
      ref={region}
      className="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto"
      aria-label={pull ? w.pull : w.issue}
    >
      <header className="border-b border-aui-border p-3">
        <h2 className="font-medium">
          {value.subject.title} #{value.subject.number}
        </h2>
        <p className="text-aui-xs text-aui-muted">
          {ref?.repo} · {value.subject.state}
          {value.subject.head?.sha && (
            <>
              {" "}
              ·{" "}
              <a
                className="underline"
                href={`https://github.com/${ref?.repo}/commit/${value.subject.head.sha}`}
              >
                {value.subject.head.sha.slice(0, 7)}
              </a>
            </>
          )}
        </p>
        <a className="underline" href={value.subject.html_url} target="_blank" rel="noreferrer">
          {w.github}
        </a>
      </header>
      {pull && (
        <div role="tablist" aria-label={w.sections} className="flex lg:hidden">
          {["changes", "findings"].map((t) => (
            <button
              type="button"
              key={t}
              role="tab"
              aria-selected={phone === t}
              className={button}
              onClick={() => setPhone(t)}
            >
              {t === "changes" ? w.changes : w.findings}
            </button>
          ))}
        </div>
      )}
      <div
        className={
          pull
            ? "grid min-h-0 min-w-0 flex-1 lg:grid-cols-[minmax(0,3fr)_minmax(16rem,2fr)]"
            : "p-4"
        }
      >
        {pull && (
          <div
            className={`${phone !== "changes" ? "hidden lg:block" : ""} min-w-0 overflow-auto border-aui-border lg:border-r`}
          >
            <label className="block p-2 text-aui-xs">
              {w.files}
              <select
                className="ml-2 max-w-full bg-aui-bg p-2"
                value={selected?.filename ?? ""}
                onChange={(e) => setFile(e.target.value)}
              >
                {files.map((f) => (
                  <option key={f.filename}>{f.filename}</option>
                ))}
              </select>
            </label>
            {selected?.patch ? (
              <ChangeReview
                key={selected.filename}
                patch={`diff --git a/${selected.filename} b/${selected.filename}\n--- a/${selected.filename}\n+++ b/${selected.filename}\n${selected.patch}`}
                pins={pins}
                layout="unified"
              />
            ) : (
              <p className="p-3">{w.noPatch}</p>
            )}
          </div>
        )}
        <div
          className={`${pull && phone !== "findings" ? "hidden lg:block" : ""} min-w-0 overflow-auto p-4`}
        >
          {pull && (
            <>
              {summary && <Markdown text={summary} />}
              <h3 className="font-medium">{w.ci}</h3>
              {"workflows" in value &&
                (value.workflows ?? []).map((run) => (
                  <p key={run.id} className="text-aui-xs">
                    <a className="underline" href={run.html_url} target="_blank" rel="noreferrer">
                      {run.name}
                    </a>
                    : {run.conclusion || run.status}
                  </p>
                ))}
              {"workflows" in value && !value.workflows?.length && <p>{w.noCI}</p>}
              <h3 className="mt-4 font-medium">{w.findings}</h3>
              {["error", "warning", "info"].map((severity) => (
                <div key={severity}>
                  {pins
                    .filter((p) => p.severity === severity)
                    .map((p, i) => (
                      <button
                        type="button"
                        key={`${p.path}:${p.line}:${i}`}
                        className={`${button} block w-full text-left`}
                        onClick={() => jump(p.path, p.line)}
                      >
                        <strong>{severity}</strong>: {p.text}
                        <span className="block text-aui-muted">
                          {p.path}:{p.line}
                        </span>
                      </button>
                    ))}
                </div>
              ))}
              {!pins.length && <p>{w.noFindings}</p>}
            </>
          )}
          {"labels" in value && (
            <div className="flex flex-wrap gap-2">
              {value.labels.map((label) => (
                <span
                  key={label}
                  className="rounded-aui border border-aui-border px-2 py-1 text-aui-xs"
                >
                  {label}
                </span>
              ))}
            </div>
          )}
          <h3 className="mt-4 font-medium">{w.description}</h3>
          <Markdown text={value.subject.body} />
          <h3 className="mt-4 font-medium">{w.comments}</h3>
          {value.comments?.map((comment) => (
            <article key={comment.id} className="mt-3 border-t border-aui-border pt-3">
              <a href={comment.html_url} className="text-aui-xs underline">
                {comment.user.login}
              </a>
              <Markdown text={comment.body} />
            </article>
          ))}
        </div>
      </div>
      <nav className="flex gap-2 p-2">
        <button
          type="button"
          className={button}
          disabled={page === 1}
          onClick={() => setPage((n) => n - 1)}
        >
          {w.previous}
        </button>
        {value.next_page && (
          <button type="button" className={button} onClick={() => setPage(value.next_page!)}>
            {w.next}
          </button>
        )}
      </nav>
    </section>
  );
}
