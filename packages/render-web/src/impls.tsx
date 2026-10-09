import { type ActionHandler, type TaskStatus, TIME_TOKEN, type Tone } from "@agent-ui/core";
import type { ButtonProps, PrimitiveImpls, PrimitiveProps } from "@agent-ui/primitives";
import { createContext, useContext, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

export const ActionContext = createContext<ActionHandler>(() => {});

const toneText: Record<Tone, string> = {
  ok: "text-aui-ok",
  warn: "text-aui-warn",
  bad: "text-aui-bad",
  info: "text-aui-info",
  neutral: "text-aui-neutral",
};

const toneBorder: Record<Tone, string> = {
  ok: "border-l-aui-ok",
  warn: "border-l-aui-warn",
  bad: "border-l-aui-bad",
  info: "border-l-aui-info",
  neutral: "border-l-aui-neutral",
};

const toneLabel: Record<Tone, string> = {
  ok: "Success",
  warn: "Warning",
  bad: "Error",
  info: "Note",
  neutral: "Note",
};

const statusGlyph: Record<TaskStatus, { glyph: string; className: string; label: string }> = {
  pending: { glyph: "○", className: "text-aui-muted", label: "Pending" },
  in_progress: {
    glyph: "◐",
    className: "text-aui-accent",
    label: "In progress",
  },
  complete: { glyph: "●", className: "text-aui-ok", label: "Complete" },
  error: { glyph: "✕", className: "text-aui-bad", label: "Error" },
};

const prose =
  "text-sm leading-6 text-aui-fg [&_a]:text-aui-accent [&_a]:underline [&_code]:rounded [&_code]:bg-aui-code [&_code]:px-1 [&_code]:font-mono [&_code]:text-[0.85em] [&_ol]:list-decimal [&_ol]:pl-5 [&_p]:my-1 [&_ul]:list-disc [&_ul]:pl-5";

/** Expand core time tokens in the reader's locale and timezone. */
function localizeTimes(text: string): string {
  return text.replace(TIME_TOKEN, (_m, iso: string) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime())
      ? iso
      : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
  });
}

function Inline({ text }: { text: string }) {
  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{ p: ({ children }) => <>{children}</> }}
      allowedElements={["p", "strong", "em", "code", "a", "del"]}
      unwrapDisallowed
    >
      {localizeTimes(text)}
    </ReactMarkdown>
  );
}

function Button({ label, action, url, style, confirm, accessibilityLabel }: ButtonProps) {
  const onAction = useContext(ActionContext);
  const [confirming, setConfirming] = useState(false);
  const base =
    "inline-flex h-8 items-center rounded-md border px-3 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent";
  const variant =
    style === "primary"
      ? "border-transparent bg-aui-primary text-aui-primary-fg hover:opacity-90"
      : style === "danger"
        ? "border-transparent bg-aui-bad text-white hover:opacity-90"
        : "border-aui-border bg-aui-surface text-aui-fg hover:bg-aui-code";
  if (url) {
    return (
      <a className={`${base} ${variant}`} href={url} aria-label={accessibilityLabel}>
        {label}
      </a>
    );
  }
  const fire = () => action && onAction({ id: action.id, payload: action.payload });
  if (confirming && confirm) {
    return (
      <fieldset className="m-0 inline-flex items-center gap-2 border-0 p-0 text-sm">
        <legend className="sr-only">{confirm.title}</legend>
        <span className="text-aui-muted">{confirm.text}</span>
        <button
          type="button"
          className={`${base} border-transparent bg-aui-bad text-white`}
          onClick={() => {
            setConfirming(false);
            fire();
          }}
        >
          {confirm.confirm}
        </button>
        <button type="button" className={`${base} ${variant}`} onClick={() => setConfirming(false)}>
          {confirm.deny}
        </button>
      </fieldset>
    );
  }
  return (
    <button
      type="button"
      className={`${base} ${variant}`}
      aria-label={accessibilityLabel}
      onClick={() => (confirm ? setConfirming(true) : fire())}
    >
      {label}
    </button>
  );
}

/**
 * Square, centered status icon. Spinning needs a square box whose center is the
 * glyph's center; a text glyph in a line-height box orbits instead of spinning.
 */
function StatusIcon({ status }: { status: TaskStatus }) {
  const s = statusGlyph[status];
  return (
    <span aria-hidden className="mt-0.5 flex size-4 shrink-0 items-center justify-center">
      {status === "in_progress" ? (
        <span className="aui-spin block size-3 animate-spin rounded-full border-2 border-aui-accent border-t-transparent" />
      ) : (
        <span className={`text-xs leading-none ${s.className}`}>{s.glyph}</span>
      )}
    </span>
  );
}

function Task({ title, status, details, output, sources }: PrimitiveProps["Task"]) {
  const s = statusGlyph[status];
  return (
    <li className="flex gap-2 py-1 text-sm">
      <StatusIcon status={status} />
      <div className="min-w-0">
        <span className="text-aui-fg">{title}</span>
        <span className="sr-only"> ({s.label})</span>
        {details && <p className="text-aui-muted">{details}</p>}
        {output && <p className="text-aui-muted">{output}</p>}
        {sources && sources.length > 0 && (
          <p className="text-xs text-aui-muted">
            {sources.map((c, i) => (
              <span key={c.id}>
                {i > 0 && " · "}
                {c.url ? (
                  <a className="underline" href={c.url}>
                    {c.title}
                  </a>
                ) : (
                  c.title
                )}
              </span>
            ))}
          </p>
        )}
      </div>
    </li>
  );
}

const terminalState: Record<TaskStatus, string> = {
  pending: "Queued",
  in_progress: "Running",
  complete: "Done",
  error: "Failed",
};

/** Terminal output stays dark in both themes: it reads as a terminal, not as page content. */
function Terminal({
  command,
  cwd,
  status,
  exitCode,
  durationMs,
  output,
}: PrimitiveProps["Terminal"]) {
  const passed = exitCode === 0 || (exitCode === undefined && status === "complete");
  const failed = (exitCode !== undefined && exitCode !== 0) || status === "error";
  const state = failed ? "Failed" : passed && exitCode === 0 ? "Passed" : terminalState[status];
  const lines = (output ?? "").split("\n");
  const tail = lines.slice(-40);
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? lines : tail;
  return (
    <figure className="overflow-hidden rounded-md border border-aui-border bg-[oklch(0.17_0_0)] text-[oklch(0.93_0_0)]">
      <figcaption className="flex items-center gap-2 border-b border-white/10 px-3 py-1.5 text-xs">
        <span
          className={`rounded px-1.5 py-0.5 font-medium ${
            failed
              ? "bg-aui-bad/20 text-aui-bad"
              : state === "Running"
                ? "bg-aui-accent-soft text-aui-accent"
                : "bg-aui-ok/20 text-aui-ok"
          }`}
        >
          {state}
        </span>
        {exitCode !== undefined && <span className="text-white/60">exit {exitCode}</span>}
        {durationMs !== undefined && (
          <span className="text-white/60">{(durationMs / 1000).toFixed(1)}s</span>
        )}
        {cwd && <span className="ml-auto font-mono text-white/50">{cwd}</span>}
      </figcaption>
      <pre className="max-h-96 overflow-auto p-3 font-mono text-xs leading-5">
        <span className="text-aui-accent">$ </span>
        {command}
        {"\n"}
        {lines.length > tail.length && !expanded && (
          <button
            type="button"
            className="text-white/50 underline"
            onClick={() => setExpanded(true)}
          >
            … {lines.length - tail.length} earlier lines
          </button>
        )}
        {lines.length > tail.length && !expanded && "\n"}
        {shown.join("\n")}
        {status === "in_progress" && <span className="animate-pulse">▍</span>}
      </pre>
    </figure>
  );
}

export const webImpls: PrimitiveImpls = {
  Header: ({ text }) => <h3 className="text-base font-semibold text-aui-fg">{text}</h3>,
  Text: ({ text }) => (
    <p className={prose}>
      <Inline text={text} />
    </p>
  ),
  Markdown: ({ text, streaming }) => (
    <div className={`${prose} ${streaming ? "aui-streaming" : ""}`} aria-busy={streaming}>
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{localizeTimes(text)}</ReactMarkdown>
    </div>
  ),
  Fields: ({ items }) => (
    <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
      {items.map((f) => (
        <div key={f.label}>
          <dt className="font-medium text-aui-fg">{f.label}</dt>
          <dd className="text-aui-muted">{f.value}</dd>
        </div>
      ))}
    </dl>
  ),
  Context: ({ items }) => (
    <p className="flex flex-wrap gap-x-3 text-xs text-aui-muted [&_a]:underline">
      {items.map((t) => (
        <span key={t}>
          <Inline text={t} />
        </span>
      ))}
    </p>
  ),
  Divider: () => <hr className="border-aui-border" />,
  Code: ({ text, title, language }) => (
    <figure className="overflow-hidden rounded-md border border-aui-border">
      {title && (
        <figcaption className="flex justify-between border-b border-aui-border bg-aui-subtle px-3 py-1.5 font-mono text-xs text-aui-muted">
          <span>{title}</span>
          {language && <span>{language}</span>}
        </figcaption>
      )}
      <pre className="overflow-x-auto bg-aui-code p-3 font-mono text-xs text-aui-fg">{text}</pre>
    </figure>
  ),
  Terminal,
  Callout: ({ tone, title, text }) => (
    <div
      role={tone === "bad" ? "alert" : "status"}
      className={`rounded-md border border-l-4 border-aui-border bg-aui-surface p-3 text-sm ${toneBorder[tone]}`}
    >
      <p className={`font-medium ${toneText[tone]}`}>{title ?? toneLabel[tone]}</p>
      <p className="text-aui-fg">
        <Inline text={text} />
      </p>
    </div>
  ),
  Button,
  Actions: ({ children }) => <div className="flex flex-wrap gap-2">{children}</div>,
  Task: (props) => (
    <ul className="list-none">
      <Task {...props} />
    </ul>
  ),
  Plan: ({ title, children }) => (
    <section className="rounded-md border border-l-4 border-aui-border border-l-aui-accent bg-aui-surface p-3">
      <h4 className="mb-1 text-sm font-semibold text-aui-fg">{title}</h4>
      <ul className="list-none">{children}</ul>
    </section>
  ),
  Sources: ({ items }) => (
    <ol className="space-y-0.5 text-xs text-aui-muted">
      {items.map((c, i) => (
        <li key={c.id}>
          [{i + 1}]{" "}
          {c.url ? (
            <a className="underline" href={c.url}>
              {c.title}
            </a>
          ) : (
            c.title
          )}
          {c.source && ` · ${c.source}`}
        </li>
      ))}
    </ol>
  ),
  Feedback,
  Table: TableImpl,
  // The summary is for notifications and screen readers on chat surfaces; the web shows the layout itself.
  Summary: () => null,
};

function Feedback({ up, down, selected, labels }: PrimitiveProps["Feedback"]) {
  const onAction = useContext(ActionContext);
  const btn = (on: boolean) =>
    `inline-flex size-8 items-center justify-center rounded-md border text-sm ${on ? "border-aui-fg" : "border-aui-border"} hover:bg-aui-code`;
  return (
    <div className="flex gap-1">
      <button
        type="button"
        aria-pressed={selected === "up"}
        aria-label={labels?.up ?? "Helpful"}
        className={btn(selected === "up")}
        onClick={() => onAction(up)}
      >
        👍
      </button>
      <button
        type="button"
        aria-pressed={selected === "down"}
        aria-label={labels?.down ?? "Not helpful"}
        className={btn(selected === "down")}
        onClick={() => onAction(down)}
      >
        👎
      </button>
    </div>
  );
}

function TableImpl({ columns, rows }: PrimitiveProps["Table"]) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead>
          <tr className="border-b border-aui-border">
            {columns.map((c) => (
              <th key={c.key} className="px-2 py-1 font-medium" style={{ textAlign: c.align }}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: rows have no identity
            <tr key={i} className="border-b border-aui-border last:border-0">
              {columns.map((c) => (
                <td key={c.key} className="px-2 py-1" style={{ textAlign: c.align }}>
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
