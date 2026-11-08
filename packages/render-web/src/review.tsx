import type { Tone } from "@humagen/core";
import type { PrimitiveProps } from "@humagen/primitives";
import { createContext, useContext, useEffect, useId, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import { Button, DenseContext } from "./buttons";
import { Feedback } from "./feedback";
import { highlight, type TokenKind } from "./highlight";
import {
  AlertOctagonIcon,
  AlertTriangleIcon,
  CheckIcon,
  ChevronRightIcon,
  CopyIcon,
  ExternalLinkIcon,
  InfoIcon,
  ListChecksIcon,
} from "./icons";

const toneText: Record<Tone, string> = {
  ok: "text-aui-ok",
  warn: "text-aui-warn",
  bad: "text-aui-bad",
  info: "text-aui-info",
  neutral: "text-aui-muted",
};
const toneDot: Record<Tone, string> = {
  ok: "bg-aui-ok",
  warn: "bg-aui-warn",
  bad: "bg-aui-bad",
  info: "bg-aui-info",
  neutral: "bg-aui-muted",
};
const ToneIcon = ({ tone }: { tone: Tone }) =>
  tone === "bad" ? (
    <AlertOctagonIcon size={14} />
  ) : tone === "warn" ? (
    <AlertTriangleIcon size={14} />
  ) : (
    <InfoIcon size={14} />
  );

/** Model words: emphasis and code, nothing that links, embeds or lays out. */
function Words({ text }: { text: string }) {
  return (
    <ReactMarkdown
      components={{ p: ({ children }) => <>{children}</> }}
      allowedElements={["p", "strong", "em", "code", "del"]}
      unwrapDisallowed
    >
      {text}
    </ReactMarkdown>
  );
}

const link =
  "inline-flex min-h-6 pointer-coarse:min-h-11 items-center rounded-aui-sm underline decoration-aui-border underline-offset-2 hover:text-aui-fg hover:decoration-current focus-visible:outline-2 focus-visible:outline-aui-accent";

/** Copies the review's Markdown; the label stays put and a note says it worked. */
function CopyMarkdown({ label, done, text }: { label: string; done: string; text: string }) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  return (
    <span className="ml-auto inline-flex items-center gap-1.5">
      <output className="text-aui-2xs text-aui-muted">
        {state === "copied" ? done : state === "failed" ? "Couldn't copy" : ""}
      </output>
      <button
        type="button"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setState("copied");
          } catch {
            setState("failed");
          }
          if (timer.current) clearTimeout(timer.current);
          timer.current = setTimeout(() => setState("idle"), 1600);
        }}
        className="inline-flex min-h-8 pointer-coarse:min-h-11 items-center gap-1.5 rounded-aui-sm px-2 text-aui-xs text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        {state === "copied" ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
        {label}
      </button>
    </span>
  );
}

type Finding = PrimitiveProps["Review"]["groups"][number]["findings"][number];
type Evidence = Finding["evidence"][number];
type CodeRef = NonNullable<Evidence["source"]["code"]>;

/** Lines of a file at a commit, as the host reads them: numbered from `first`. */
export interface SourceExcerpt {
  first: number;
  start: number;
  end: number;
  lines: string[];
  url: string;
}

/**
 * How the host reads the lines a review's evidence cites. Reading belongs to
 * the host (it calls its API); without one, evidence is a link to GitHub.
 */
export type SourceLoader = (ref: CodeRef) => Promise<SourceExcerpt>;
export const SourceContext = createContext<SourceLoader | null>(null);

const tokenClass: Record<TokenKind, string> = {
  plain: "",
  comment: "text-aui-muted italic",
  string: "text-aui-ok",
  number: "text-aui-warn",
  keyword: "text-aui-accent font-medium",
};

/** The cited lines in place: numbered, the cited ones marked, with a link to GitHub. */
function CodeExcerpt({
  id,
  code,
  load,
  labels,
}: {
  id: string;
  code: CodeRef;
  load: SourceLoader;
  labels: NonNullable<PrimitiveProps["Review"]["labels"]["source"]>;
}) {
  const [state, setState] = useState<
    { kind: "loading" } | { kind: "ready"; excerpt: SourceExcerpt } | { kind: "failed" }
  >({ kind: "loading" });
  // The card is drawn again as a turn changes; the lines are read once per place.
  const { repo, sha, path, start, end } = code;
  useEffect(() => {
    let live = true;
    load({ repo, sha, path, start, end }).then(
      (excerpt) => live && setState({ kind: "ready", excerpt }),
      () => live && setState({ kind: "failed" }),
    );
    return () => {
      live = false;
    };
  }, [load, repo, sha, path, start, end]);
  const github = `https://github.com/${code.repo}/blob/${code.sha}/${code.path}#L${code.start}${code.end !== code.start ? `-L${code.end}` : ""}`;
  return (
    <figure
      id={id}
      data-aui="source-excerpt"
      className="not-prose my-0 min-w-0 overflow-hidden rounded-aui border border-aui-border bg-aui-code"
    >
      <figcaption className="flex min-h-8 min-w-0 items-center gap-2 border-aui-border border-b bg-aui-surface pr-1 pl-3 text-aui-2xs">
        <span className="min-w-0 flex-1 truncate font-aui-mono text-aui-fg" title={code.path}>
          {code.path}
        </span>
        <a
          href={state.kind === "ready" ? state.excerpt.url : github}
          target="_blank"
          rel="noreferrer"
          className={`shrink-0 gap-1 px-1.5 text-aui-muted ${link}`}
        >
          {labels.open}
          <ExternalLinkIcon size={11} />
        </a>
      </figcaption>
      {state.kind === "loading" ? (
        <p className="px-3 py-2 text-aui-2xs text-aui-muted">{labels.loading}</p>
      ) : state.kind === "failed" ? (
        <output className="block px-3 py-2 text-aui-2xs text-aui-muted">{labels.failed}</output>
      ) : (
        <div className="max-h-80 overflow-auto">
          <table className="w-full border-collapse font-aui-mono text-aui-2xs leading-5">
            <tbody>
              {highlight(code.path, state.excerpt.lines).map((tokens, i) => {
                const n = state.excerpt.first + i;
                const cited = n >= state.excerpt.start && n <= state.excerpt.end;
                return (
                  <tr
                    key={n}
                    data-cited={cited || undefined}
                    className={cited ? "bg-aui-accent/10" : undefined}
                  >
                    <td
                      className={`select-none border-aui-border border-r px-2 text-right align-top tabular-nums ${cited ? "text-aui-fg" : "text-aui-muted"}`}
                    >
                      {n}
                    </td>
                    <td className="whitespace-pre px-3 align-top text-aui-fg">
                      {tokens.map((t, j) => (
                        // biome-ignore lint/suspicious/noArrayIndexKey: tokens of a fixed line
                        <span key={j} className={tokenClass[t.kind]}>
                          {t.text}
                        </span>
                      ))}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </figure>
  );
}

/**
 * One piece of evidence: what was observed and where. Lines at a commit open
 * in place when the host can read them; another link goes to its page; a
 * command reads as code.
 */
function EvidenceItem({
  evidence: e,
  labels,
}: {
  evidence: Evidence;
  labels: PrimitiveProps["Review"]["labels"];
}) {
  const load = useContext(SourceContext);
  const [open, setOpen] = useState(false);
  const excerptId = useId();
  const code = e.source.code;
  const inPlace = code && load && labels.source;
  return (
    <>
      <span className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="min-w-0 break-words">
          <Words text={e.observation} />
        </span>
        {e.source.action ? (
          <Button label={e.source.label} action={e.source.action} />
        ) : inPlace ? (
          <button
            type="button"
            aria-expanded={open}
            aria-controls={open ? excerptId : undefined}
            aria-label={`${labels.source?.show} ${e.source.label}`}
            title={e.source.url}
            onClick={() => setOpen((o) => !o)}
            className="inline-flex min-h-6 pointer-coarse:min-h-11 min-w-0 max-w-full items-center gap-1 rounded-aui-sm border border-aui-border bg-aui-code px-1.5 font-aui-mono text-aui-2xs text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <ChevronRightIcon
              size={11}
              className={`shrink-0 text-aui-muted ${open ? "rotate-90" : ""}`}
            />
            <span className="min-w-0 truncate">{e.source.label}</span>
          </button>
        ) : e.source.url ? (
          <a
            href={e.source.url}
            target="_blank"
            rel="noreferrer"
            title={e.source.url}
            className={`min-w-0 max-w-full gap-1 font-aui-mono text-aui-2xs text-aui-fg ${link}`}
          >
            <span className="min-w-0 truncate">{e.source.label}</span>
            <ExternalLinkIcon size={11} className="shrink-0 text-aui-muted" />
          </a>
        ) : (
          <code className="min-w-0 break-all rounded-aui-sm bg-aui-code px-1 font-aui-mono text-aui-2xs text-aui-fg">
            {e.source.label}
          </code>
        )}
      </span>
      {inPlace && open && labels.source && (
        <CodeExcerpt id={excerptId} code={code} load={load} labels={labels.source} />
      )}
    </>
  );
}

function FindingItem({
  finding,
  tone,
  labels,
}: {
  finding: Finding;
  tone: Tone;
  labels: PrimitiveProps["Review"]["labels"];
}) {
  const titleId = useId();
  return (
    <li
      aria-labelledby={titleId}
      className="flex min-w-0 flex-col gap-2 border-aui-border border-t px-3.5 py-3"
    >
      <p
        id={titleId}
        className="flex min-w-0 items-start gap-2 font-medium text-aui-fg text-aui-sm"
      >
        <span aria-hidden className={`mt-0.5 shrink-0 ${toneText[tone]}`}>
          <ToneIcon tone={tone} />
        </span>
        <span className="min-w-0 break-words">
          <Words text={finding.title} />
        </span>
      </p>
      {finding.foundBy && (
        <p className="pl-[1.375rem] text-aui-muted text-aui-xs">{finding.foundBy}</p>
      )}
      <div className="flex min-w-0 flex-col gap-2 pl-[1.375rem] text-aui-sm">
        <p className="max-w-[72ch] break-words text-aui-fg/90 leading-relaxed [&_code]:font-aui-mono [&_code]:text-[0.85em]">
          <span className="sr-only">{labels.suggestion}: </span>
          <Words text={finding.suggestion} />
        </p>
        {finding.location &&
          (finding.location.url ? (
            <a href={finding.location.url} target="_blank" rel="noreferrer" className={link}>
              {finding.location.label}
            </a>
          ) : (
            <span className="font-aui-mono text-aui-xs">{finding.location.label}</span>
          ))}
        {finding.change !== undefined && (
          <pre className="overflow-x-auto rounded-aui bg-aui-code p-3 text-aui-xs">
            <code>{finding.change}</code>
          </pre>
        )}
        <ul aria-label={labels.evidence} className="flex min-w-0 flex-col gap-1.5">
          {finding.evidence.map((e, i) => (
            <li
              // Evidence has no id of its own; its place in the list is stable.
              // biome-ignore lint/suspicious/noArrayIndexKey: see above
              key={i}
              className="flex min-w-0 flex-col gap-1.5 border-aui-border border-l-2 pl-2.5 text-aui-xs text-aui-muted [&_code]:font-aui-mono"
            >
              <EvidenceItem evidence={e} labels={labels} />
            </li>
          ))}
        </ul>
        {finding.feedback && <Feedback {...finding.feedback} />}
        {finding.followUp ? (
          <div className="flex">
            <DenseContext.Provider value>
              <Button
                label={finding.followUp.label}
                action={finding.followUp.action}
                accessibilityLabel={finding.followUp.accessibilityLabel}
              />
            </DenseContext.Provider>
          </div>
        ) : (
          <p className="text-aui-2xs text-aui-muted">
            {labels.followUpPrompt}: <code className="font-aui-mono">{finding.prompt}</code>
          </p>
        )}
      </div>
    </li>
  );
}

/**
 * A structured review as one card: what was reviewed, the point, the counts,
 * the findings by severity with their evidence, then notes and related links.
 * A review still arriving shows what it has and holds still.
 */
export function Review(p: PrimitiveProps["Review"]) {
  const titleId = useId();
  return (
    <section
      data-aui="review"
      data-pending={p.pending || undefined}
      aria-labelledby={titleId}
      className="flex min-w-0 flex-col overflow-hidden rounded-aui-lg border border-aui-border bg-aui-surface"
    >
      <header className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 px-3.5 py-2">
        <ListChecksIcon size={15} className="shrink-0 text-aui-muted" />
        <h3 id={titleId} className="font-semibold text-aui-fg text-aui-sm">
          {p.title}
        </h3>
        {p.openReview && <Button {...p.openReview} />}
        {p.pullRequest && (
          <a
            href={p.pullRequest.url}
            target="_blank"
            rel="noreferrer"
            className={`min-w-0 truncate text-aui-muted text-aui-xs ${link}`}
          >
            {p.pullRequest.label}
          </a>
        )}
        {p.commit &&
          (p.commit.url ? (
            <a
              href={p.commit.url}
              target="_blank"
              rel="noreferrer"
              className={`font-aui-mono text-aui-2xs text-aui-muted ${link}`}
            >
              {p.commit.label}
            </a>
          ) : (
            <span className="font-aui-mono text-aui-2xs text-aui-muted">{p.commit.label}</span>
          ))}
        {p.copyMarkdown && !p.pending && <CopyMarkdown {...p.copyMarkdown} />}
      </header>
      <div className="flex min-w-0 flex-col gap-2.5 border-aui-border border-t px-3.5 py-3">
        {p.pending && p.pendingLabel && (
          <p className="text-aui-muted text-aui-xs">{p.pendingLabel}</p>
        )}
        {p.summary && (
          <p className="max-w-[72ch] break-words text-aui-fg text-aui-sm leading-relaxed [&_code]:font-aui-mono [&_code]:text-[0.85em]">
            <Words text={p.summary} />
          </p>
        )}
        {p.counts.length > 0 && (
          <ul
            aria-label={p.labels.severity}
            className="flex flex-wrap items-center gap-x-4 gap-y-1"
          >
            {p.counts.map((c) => (
              <li
                key={c.id}
                className={`inline-flex items-center gap-1.5 text-aui-xs tabular-nums ${c.count ? "text-aui-fg" : "text-aui-muted"}`}
              >
                <span
                  aria-hidden
                  className={`size-2 rounded-full ${c.count ? toneDot[c.tone] : "bg-aui-border"}`}
                />
                {c.label}
              </li>
            ))}
          </ul>
        )}
        {!p.pending && p.groups.length === 0 && (
          <p className="text-aui-muted text-aui-xs">{p.labels.noFindings}</p>
        )}
      </div>
      {p.groups.map((g) => (
        <section key={g.id} aria-label={g.label} className="flex min-w-0 flex-col">
          <h4 className="flex items-center gap-2 border-aui-border border-t bg-aui-subtle/60 px-3.5 py-1.5 font-medium text-aui-2xs text-aui-muted">
            <span aria-hidden className={`size-1.5 rounded-full ${toneDot[g.tone]}`} />
            {g.label}
            <span className="tabular-nums">{g.findings.length}</span>
          </h4>
          <ol className="flex min-w-0 flex-col">
            {g.findings.map((f, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: findings have no id; the report is fixed once parsed
              <FindingItem key={i} finding={f} tone={g.tone} labels={p.labels} />
            ))}
          </ol>
        </section>
      ))}
      {p.comments && p.comments.items.length > 0 && (
        <section
          aria-label={p.comments.label}
          className="flex min-w-0 flex-col gap-1.5 border-aui-border border-t px-3.5 py-3"
        >
          <h4 className="font-medium text-aui-2xs text-aui-muted">{p.comments.label}</h4>
          <ul className="flex list-disc flex-col gap-1 pl-4 text-aui-muted text-aui-xs marker:text-aui-border [&_code]:font-aui-mono">
            {p.comments.items.map((c) => (
              <li key={c} className="break-words">
                <Words text={c} />
              </li>
            ))}
          </ul>
        </section>
      )}
      {p.related && p.related.items.length > 0 && (
        <nav
          aria-label={p.related.label}
          className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 border-aui-border border-t px-3.5 py-2.5 text-aui-xs"
        >
          <span className="font-medium text-aui-2xs text-aui-muted">{p.related.label}</span>
          {p.related.items.map((l) => (
            <a
              key={l.url}
              href={l.url}
              target="_blank"
              rel="noreferrer"
              className={`min-w-0 break-words text-aui-muted ${link}`}
            >
              {l.title}
            </a>
          ))}
        </nav>
      )}
    </section>
  );
}
