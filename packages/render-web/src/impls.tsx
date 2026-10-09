import { TIME_TOKEN, type Tone } from "@agent-ui/core";
import type { PrimitiveImpls, PrimitiveProps } from "@agent-ui/primitives";
import { Children, createContext, isValidElement, type ReactNode, useContext } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Actions, Button, Feedback } from "./buttons";
import { CitationChip, hostOf, SourceMark, useRegisterCitations } from "./citations";
import { CodeBlock, Terminal } from "./code";
import {
  AlertOctagonIcon,
  AlertTriangleIcon,
  CheckCircleIcon,
  ExternalLinkIcon,
  InfoIcon,
} from "./icons";
import { Plan, TaskItem } from "./task";

export { ActionContext } from "./buttons";

/**
 * Context lines a surrounding web frame already shows (e.g. the agent name and
 * status that `AgentMessage` puts in its header). Matching lines are skipped.
 */
export const SuppressContext = createContext<ReadonlySet<string>>(new Set());

const toneLabel: Record<Tone, string> = {
  ok: "Success",
  warn: "Warning",
  bad: "Error",
  info: "Note",
  neutral: "Note",
};

const toneIcon: Record<Tone, ReactNode> = {
  ok: <CheckCircleIcon size={16} />,
  warn: <AlertTriangleIcon size={16} />,
  bad: <AlertOctagonIcon size={16} />,
  info: <InfoIcon size={16} />,
  neutral: <InfoIcon size={16} />,
};

const toneText: Record<Tone, string> = {
  ok: "text-aui-ok",
  warn: "text-aui-warn",
  bad: "text-aui-bad",
  info: "text-aui-info",
  neutral: "text-aui-muted",
};

/** Expand core time tokens in the reader's locale and timezone. */
export function localizeTimes(text: string): string {
  return text.replace(TIME_TOKEN, (_m, iso: string) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime())
      ? iso
      : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  });
}

function textOf(node: ReactNode): string {
  return Children.toArray(node)
    .map((c) =>
      typeof c === "string" || typeof c === "number"
        ? String(c)
        : isValidElement<{ children?: ReactNode }>(c)
          ? textOf(c.props.children)
          : "",
    )
    .join("");
}

const CITE = /^\[?(\d{1,3})\]?$/;

/** Links whose text is a bare `[n]` are citation markers from `linkCitations`. */
const linkRenderer: Components["a"] = ({ href, children }) => {
  const label = textOf(children);
  const cite = CITE.exec(label);
  if (cite?.[1]) return <CitationChip href={href} label={cite[1]} />;
  return (
    <a href={href} target="_blank" rel="noreferrer">
      {children}
    </a>
  );
};

const markdownComponents: Components = {
  a: linkRenderer,
  pre: ({ children }) => {
    const code = Children.toArray(children)[0];
    if (isValidElement<{ className?: string; children?: ReactNode }>(code)) {
      const language = /language-(\S+)/.exec(code.props.className ?? "")?.[1];
      return (
        <div className="my-3">
          <CodeBlock text={textOf(code.props.children)} language={language} />
        </div>
      );
    }
    return <pre>{children}</pre>;
  },
  table: ({ children }) => (
    <div className="my-3 overflow-x-auto rounded-aui border border-aui-border">
      <table className="[&_tr:last-child_td]:border-0">{children}</table>
    </div>
  ),
};

function Inline({ text }: { text: string }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{ p: ({ children }) => <>{children}</>, a: linkRenderer }}
      allowedElements={["p", "strong", "em", "code", "a", "del"]}
      unwrapDisallowed
    >
      {localizeTimes(text)}
    </ReactMarkdown>
  );
}

function Markdown({ text, streaming }: PrimitiveProps["Markdown"]) {
  return (
    <div
      data-aui="markdown"
      className={`aui-prose ${streaming ? "aui-streaming" : ""}`}
      aria-busy={streaming || undefined}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
        {localizeTimes(text)}
      </ReactMarkdown>
    </div>
  );
}

function Context({ items }: PrimitiveProps["Context"]) {
  const suppressed = useContext(SuppressContext);
  const shown = items.filter((t) => !suppressed.has(t));
  if (shown.length === 0) return null;
  return (
    <p
      data-aui="context"
      className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-aui-muted text-aui-xs [&_a]:underline [&_a]:underline-offset-2 hover:[&_a]:text-aui-fg [&_strong]:font-medium [&_strong]:text-aui-fg"
    >
      {shown.map((t, i) => (
        <span key={t} className="inline-flex items-center gap-1.5">
          {i > 0 && (
            <span aria-hidden className="opacity-50">
              •
            </span>
          )}
          <span>
            <Inline text={t} />
          </span>
        </span>
      ))}
    </p>
  );
}

function Fields({ items }: PrimitiveProps["Fields"]) {
  return (
    <dl
      data-aui="fields"
      className="grid grid-cols-[minmax(6rem,max-content)_1fr] gap-x-4 text-aui-sm"
    >
      {items.map((f) => (
        <div
          key={f.label}
          className="col-span-2 grid grid-cols-subgrid border-aui-border border-b py-1.5 last:border-0"
        >
          <dt className="text-aui-muted">{f.label}</dt>
          <dd className="min-w-0 break-words font-medium text-aui-fg">{f.value}</dd>
        </div>
      ))}
    </dl>
  );
}

function Callout({ tone, title, text }: PrimitiveProps["Callout"]) {
  return (
    <div
      data-aui="callout"
      data-tone={tone}
      role={tone === "bad" ? "alert" : "status"}
      className="flex gap-2.5 rounded-aui border border-[var(--aui-callout-border)] bg-[var(--aui-callout-bg)] px-3.5 py-3 text-aui-sm"
    >
      <span className={`mt-0.5 ${toneText[tone]}`}>{toneIcon[tone]}</span>
      <div className="min-w-0 flex-1">
        <p className="font-semibold text-aui-fg">{title ?? toneLabel[tone]}</p>
        <p className="mt-0.5 text-aui-muted [&_a]:underline [&_code]:font-aui-mono [&_code]:text-[0.85em]">
          <Inline text={text} />
        </p>
      </div>
    </div>
  );
}

function Sources({ items }: PrimitiveProps["Sources"]) {
  useRegisterCitations(items);
  return (
    <section data-aui="sources" aria-label="Sources" className="flex flex-col gap-2">
      <h4 className="font-medium text-aui-muted text-aui-xs">
        {items.length === 1 ? "1 source" : `${items.length} sources`}
      </h4>
      <ol className="grid gap-2 sm:grid-cols-2">
        {items.map((c, i) => {
          const origin = c.source ?? hostOf(c.url);
          const inner = (
            <>
              <span className="flex items-center gap-2 text-aui-muted text-aui-xs">
                <span className="inline-flex size-4 items-center justify-center rounded-full bg-aui-subtle font-medium text-aui-2xs text-aui-fg tabular-nums">
                  {i + 1}
                </span>
                <SourceMark label={origin} className="size-3.5 text-[0.55rem]" />
                <span className="truncate">{origin}</span>
                {c.url && (
                  <ExternalLinkIcon
                    size={12}
                    className="ml-auto opacity-0 transition group-hover:opacity-100 group-focus-visible:opacity-100"
                  />
                )}
              </span>
              <span className="line-clamp-1 font-medium text-aui-fg text-aui-sm">{c.title}</span>
              {c.snippet && (
                <span className="line-clamp-2 text-aui-muted text-aui-xs">{c.snippet}</span>
              )}
            </>
          );
          const cls =
            "group flex h-full flex-col gap-1 rounded-aui border border-aui-border bg-aui-surface px-3 py-2.5";
          return (
            <li key={c.id} className="min-w-0">
              {c.url ? (
                <a
                  href={c.url}
                  target="_blank"
                  rel="noreferrer"
                  className={`${cls} transition-colors hover:border-aui-accent/50 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent`}
                >
                  {inner}
                </a>
              ) : (
                <div className={cls}>{inner}</div>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}

function Table({ columns, rows }: PrimitiveProps["Table"]) {
  return (
    <div data-aui="table" className="overflow-x-auto rounded-aui border border-aui-border">
      <table className="w-full text-left text-aui-sm">
        <thead className="bg-aui-surface">
          <tr className="border-aui-border border-b">
            {columns.map((c) => (
              <th
                key={c.key}
                scope="col"
                className="px-3 py-2 font-medium text-aui-muted text-aui-xs"
                style={{ textAlign: c.align }}
              >
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr
              // biome-ignore lint/suspicious/noArrayIndexKey: rows have no identity
              key={i}
              className="border-aui-border border-b transition-colors last:border-0 hover:bg-aui-hover"
            >
              {columns.map((c) => (
                <td key={c.key} className="px-3 py-2 tabular-nums" style={{ textAlign: c.align }}>
                  {r[c.key]}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export const webImpls: PrimitiveImpls = {
  Header: ({ text }) => (
    <h3 data-aui="header" className="font-semibold text-aui-fg text-aui-lg">
      {text}
    </h3>
  ),
  Text: ({ text }) => (
    <p data-aui="text" className="aui-prose whitespace-pre-line">
      <Inline text={text} />
    </p>
  ),
  Markdown,
  Fields,
  Context,
  Divider: () => <hr data-aui="divider" className="border-aui-border" />,
  Code: CodeBlock,
  Terminal,
  Callout,
  Button,
  Actions,
  Task: TaskItem,
  Plan,
  Sources,
  Feedback,
  Table,
  // For notifications and screen readers on chat surfaces. The web shows the layout itself,
  // and a live region here would re-announce every streamed token.
  Summary: () => null,
};
