import { TIME_TOKEN, type Tone } from "@vultus/core";
import type { PrimitiveImpls, PrimitiveProps } from "@vultus/primitives";
import { Children, isValidElement, type ReactNode, useContext, useEffect, useState } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Actions, Button, Menu } from "./buttons";
import { CitationChip, Favicon, hostOf, useRegisterCitations } from "./citations";
import { CodeBlock, Terminal } from "./code";
import { Feedback } from "./feedback";
import { FileTile } from "./file";
import { Checkboxes } from "./form";
import {
  AlertOctagonIcon,
  AlertTriangleIcon,
  CheckCircleIcon,
  CheckIcon,
  ClockIcon,
  CopyIcon,
  InfoIcon,
} from "./icons";
import { SuppressContext, timeLeft, useNow, useTurnMeta } from "./meta";

export { SuppressContext } from "./meta";

import { Plan, TaskItem } from "./task";

export { ActionContext } from "./buttons";

const toneLabel: Record<Tone, string> = {
  ok: "Success",
  warn: "Warning",
  bad: "Error",
  info: "Note",
  neutral: "Note",
};

const toneIcon: Record<Tone, ReactNode> = {
  ok: <CheckCircleIcon size={14} />,
  warn: <AlertTriangleIcon size={14} />,
  bad: <AlertOctagonIcon size={14} />,
  info: <InfoIcon size={14} />,
  neutral: <InfoIcon size={14} />,
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

function Text({ text }: PrimitiveProps["Text"]) {
  if (useContext(SuppressContext).has(text)) return null;
  return (
    <p data-aui="text" className="aui-prose whitespace-pre-line">
      <Inline text={text} />
    </p>
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

const EXPIRES = /^Expires \{\{time:([^}]+)\}\}$/;
const REFERENCE = /^Reference: (\S+)$/;

/** "42m left" countdown for an approval's expiry; warns inside the last hour. */
function ExpiryChip({ iso }: { iso: string }) {
  const now = useNow();
  const left = timeLeft(iso, now);
  const exact = localizeTimes(`{{time:${iso}}}`);
  if (!left) return <span>Expired · {exact}</span>;
  return (
    <span
      title={`Expires ${exact}`}
      className={`inline-flex h-4.5 items-center gap-1 rounded-full px-1.5 font-medium tabular-nums ${left.soon ? "bg-aui-warn/15 text-aui-fg" : "bg-aui-subtle text-aui-muted"}`}
    >
      <ClockIcon size={11} className={left.soon ? "text-aui-warn" : ""} />
      {left.text}
    </span>
  );
}

/** A failure reference to quote in a bug report: one click copies it. */
function ReferenceChip({ reference }: { reference: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1200);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <button
      type="button"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(reference);
        } catch {}
        setCopied(true);
      }}
      aria-label={copied ? "Reference copied" : `Copy reference ${reference}`}
      className="inline-flex h-5 items-center gap-1 rounded-aui-sm border border-aui-border px-1.5 font-aui-mono text-aui-2xs transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
    >
      <span className="font-aui-sans text-aui-muted">Ref</span>
      {reference}
      {copied ? <CheckIcon size={11} className="text-aui-ok" /> : <CopyIcon size={11} />}
      <span aria-live="polite" className="sr-only">
        {copied ? "Copied" : ""}
      </span>
    </button>
  );
}

function ContextItem({ text }: { text: string }) {
  const expires = EXPIRES.exec(text);
  if (expires?.[1]) return <ExpiryChip iso={expires[1]} />;
  const ref = REFERENCE.exec(text);
  if (ref?.[1]) return <ReferenceChip reference={ref[1]} />;
  return <Inline text={text} />;
}

function Context({ items }: PrimitiveProps["Context"]) {
  const suppressed = useContext(SuppressContext);
  const turn = useTurnMeta();
  const shown = items.filter((t) => !suppressed.has(t));
  if (shown.length === 0) return null;
  const http = shown.some((t) => REFERENCE.test(t)) ? turn?.failure?.httpStatus : undefined;
  return (
    <p
      data-aui="context"
      className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-aui-2xs text-aui-muted [&_a]:underline [&_a]:underline-offset-2 hover:[&_a]:text-aui-fg [&_strong]:font-medium [&_strong]:text-aui-fg"
    >
      {shown.map((t, i) => (
        <span key={t} className="inline-flex min-w-0 items-center gap-1.5">
          {i > 0 && (
            <span aria-hidden className="opacity-50">
              ·
            </span>
          )}
          <span className="min-w-0">
            <ContextItem text={t} />
          </span>
        </span>
      ))}
      {http !== undefined && (
        <span className="ml-auto font-aui-mono tabular-nums" title="HTTP status">
          HTTP {http}
        </span>
      )}
    </p>
  );
}

function Fields({ items }: PrimitiveProps["Fields"]) {
  return (
    <dl
      data-aui="fields"
      className="grid grid-cols-[minmax(5rem,max-content)_1fr] gap-x-3 text-aui-xs"
    >
      {items.map((f) => (
        <div key={f.label} className="col-span-2 grid grid-cols-subgrid py-0.5">
          <dt className="text-aui-muted">{f.label}</dt>
          <dd className="min-w-0 break-words text-aui-fg">{f.value}</dd>
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
      className="flex gap-2 rounded-aui border border-[var(--aui-callout-border)] bg-[var(--aui-callout-bg)] px-2.5 py-1.5 text-aui-sm"
    >
      <span className={`mt-0.5 ${toneText[tone]}`}>{toneIcon[tone]}</span>
      <p className="min-w-0 flex-1 text-aui-muted [&_a]:underline [&_code]:font-aui-mono [&_code]:text-[0.85em]">
        <span className="font-medium text-aui-fg">{title ?? toneLabel[tone]}</span>
        {text && (
          <>
            <span aria-hidden className="mx-1.5 opacity-50">
              ·
            </span>
            <Inline text={text} />
          </>
        )}
      </p>
    </div>
  );
}

/**
 * Sources, visible by default: one compact strip of numbered favicon + title
 * chips under the answer. Hover shows the snippet; the number matches the
 * inline citation.
 */
function Sources({ items }: PrimitiveProps["Sources"]) {
  useRegisterCitations(items);
  return (
    <section data-aui="sources" aria-label="Sources" className="flex flex-col gap-1">
      <h4 className="font-medium text-aui-2xs text-aui-muted">
        {items.length === 1 ? "1 source" : `${items.length} sources`}
      </h4>
      <ol className="flex flex-wrap gap-1">
        {items.map((c, i) => {
          const origin = c.source ?? hostOf(c.url);
          const inner = (
            <>
              <span className="text-aui-muted tabular-nums">{i + 1}</span>
              <Favicon url={c.url} label={origin} className="size-3.5" />
              <span className="min-w-0 max-w-[16rem] truncate text-aui-fg">{c.title}</span>
              <span className="hidden truncate text-aui-muted sm:inline">{origin}</span>
            </>
          );
          const cls =
            "inline-flex h-6 max-w-full items-center gap-1.5 rounded-full border border-aui-border px-2 text-aui-xs";
          const tip = [c.title, origin, c.snippet].filter(Boolean).join("\n");
          return (
            <li key={c.id} className="min-w-0 max-w-full">
              {c.url ? (
                <a
                  href={c.url}
                  target="_blank"
                  rel="noreferrer"
                  title={tip}
                  className={`${cls} transition-colors hover:border-aui-accent/60 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent`}
                >
                  {inner}
                </a>
              ) : (
                <span className={cls} title={tip}>
                  {inner}
                </span>
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
    <h3 data-aui="header" className="font-semibold text-aui-base text-aui-fg">
      {text}
    </h3>
  ),
  Text,
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
  Checkboxes,
  File: FileTile,
  Menu,
};
