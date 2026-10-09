import type { TaskStatus } from "@agent-ui/core";
import type { PrimitiveProps } from "@agent-ui/primitives";
import { useEffect, useRef, useState } from "react";
import {
  BanIcon,
  CheckCircleIcon,
  CheckIcon,
  CircleIcon,
  CopyIcon,
  FileIcon,
  HourglassIcon,
  Spinner,
  TerminalIcon,
  XCircleIcon,
} from "./icons";

/** Copy to clipboard with a short "Copied" confirmation that screen readers hear. */
export function CopyButton({
  text,
  label = "Copy",
  tone = "page",
}: {
  text: string;
  label?: string;
  tone?: "page" | "terminal";
}) {
  const [state, setState] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setState("copied");
    } catch {
      setState("failed");
    }
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setState("idle"), 1600);
  };
  const colors =
    tone === "terminal"
      ? "text-aui-term-muted hover:bg-aui-term-border hover:text-aui-term-fg"
      : "text-aui-muted hover:bg-aui-hover hover:text-aui-fg";
  return (
    <button
      type="button"
      onClick={copy}
      aria-label={state === "copied" ? "Copied" : label}
      title={label}
      className={`inline-flex h-6 items-center gap-1 rounded-aui-sm px-1.5 text-aui-xs transition-colors focus-visible:outline-2 focus-visible:outline-aui-accent ${colors}`}
    >
      {state === "copied" ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
      <span aria-live="polite">
        {state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : ""}
      </span>
    </button>
  );
}

const COLLAPSED_LINES = 16;

/** Code with a file/language header, copy, and a fold for long excerpts. */
export function CodeBlock({ text, language, title }: PrimitiveProps["Code"]) {
  const lines = text.replace(/\n$/, "").split("\n");
  const long = lines.length > COLLAPSED_LINES + 4;
  const [expanded, setExpanded] = useState(false);
  const shown = long && !expanded ? lines.slice(0, COLLAPSED_LINES) : lines;
  return (
    <figure
      data-aui="code"
      className="not-prose my-0 min-w-0 overflow-hidden rounded-aui border border-aui-border bg-aui-code"
    >
      <figcaption className="flex h-8 items-center gap-2 border-aui-border border-b bg-aui-surface pr-1.5 pl-3 text-aui-xs">
        {title ? (
          <>
            <FileIcon size={13} className="text-aui-muted" />
            <span className="min-w-0 truncate font-aui-mono text-aui-fg" title={title}>
              {title}
            </span>
          </>
        ) : null}
        {language && (
          <span
            className={`${title ? "" : "-ml-0.5"} rounded-aui-sm bg-aui-subtle px-1.5 py-0.5 font-aui-mono text-aui-2xs text-aui-muted`}
          >
            {language}
          </span>
        )}
        <span className="ml-auto">
          <CopyButton text={text} label={title ? `Copy ${title}` : "Copy code"} />
        </span>
      </figcaption>
      <pre
        // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be keyboard reachable
        tabIndex={0}
        className="overflow-x-auto px-0 py-2 font-aui-mono text-aui-xs leading-5 text-aui-fg outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset"
      >
        <code className="grid">
          {shown.map((line, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: lines are positional
            <span key={i} className="flex">
              <span
                aria-hidden
                className="inline-block w-10 shrink-0 select-none pr-3 text-right text-aui-muted opacity-60"
              >
                {i + 1}
              </span>
              <span className="whitespace-pre pr-4">{line || " "}</span>
            </span>
          ))}
        </code>
      </pre>
      {long && (
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((e) => !e)}
          className="flex w-full items-center justify-center border-aui-border border-t bg-aui-surface py-1.5 text-aui-muted text-aui-xs transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent"
        >
          {expanded ? "Show less" : `Show all ${lines.length} lines`}
        </button>
      )}
    </figure>
  );
}

const TAIL = 12;

function terminalState(status: TaskStatus, exitCode?: number) {
  if (status === "error" || (exitCode !== undefined && exitCode !== 0)) {
    return { label: "Failed", icon: <XCircleIcon size={13} />, className: "text-aui-bad" };
  }
  if (status === "in_progress") {
    return { label: "Running", icon: <Spinner size={13} />, className: "text-aui-accent" };
  }
  if (status === "waiting") {
    return {
      label: "Waiting for approval",
      icon: <HourglassIcon size={13} />,
      className: "text-aui-accent/80",
    };
  }
  if (status === "denied") {
    return { label: "Not run", icon: <BanIcon size={13} />, className: "text-aui-term-muted" };
  }
  if (status === "pending") {
    return { label: "Queued", icon: <CircleIcon size={13} />, className: "text-aui-term-muted" };
  }
  return {
    label: exitCode === 0 ? "Passed" : "Done",
    icon: <CheckCircleIcon size={13} />,
    className: "text-aui-ok",
  };
}

function formatDuration(ms: number) {
  return ms < 1000
    ? `${ms}ms`
    : ms < 60_000
      ? `${(ms / 1000).toFixed(1)}s`
      : `${Math.round(ms / 60_000)}m`;
}

/** A command and its output. Shows the tail; earlier lines are one click away. */
export function Terminal({
  command,
  cwd,
  status,
  exitCode,
  durationMs,
  output,
}: PrimitiveProps["Terminal"]) {
  const lines = output ? output.replace(/\n$/, "").split("\n") : [];
  const hidden = Math.max(0, lines.length - TAIL);
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? lines : lines.slice(-TAIL);
  const state = terminalState(status, exitCode);
  const body = useRef<HTMLPreElement>(null);

  // Follow live output, the way a terminal does.
  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll on every new chunk
  useEffect(() => {
    const el = body.current;
    if (el && status === "in_progress") el.scrollTop = el.scrollHeight;
  }, [output, status]);

  return (
    <figure
      data-aui="terminal"
      className="not-prose my-0 min-w-0 overflow-hidden rounded-aui border border-aui-border bg-aui-term text-aui-term-fg shadow-aui-sm"
    >
      <figcaption className="flex h-8 items-center gap-2 border-aui-term-border border-b pr-1.5 pl-3 text-aui-xs">
        <TerminalIcon size={13} className="text-aui-term-muted" />
        <span className={`inline-flex items-center gap-1.5 font-medium ${state.className}`}>
          {state.icon}
          {state.label}
        </span>
        {exitCode !== undefined && (
          <span className="font-aui-mono text-aui-term-muted">exit {exitCode}</span>
        )}
        {durationMs !== undefined && (
          <span className="text-aui-term-muted tabular-nums">{formatDuration(durationMs)}</span>
        )}
        {cwd && (
          <span className="ml-auto hidden min-w-0 truncate font-aui-mono text-aui-term-muted sm:block">
            {cwd}
          </span>
        )}
        <span className={cwd ? "" : "ml-auto"}>
          <CopyButton
            tone="terminal"
            text={output ? `$ ${command}\n${output}` : command}
            label="Copy command and output"
          />
        </span>
      </figcaption>
      {hidden > 0 && (
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((e) => !e)}
          className="flex w-full items-center gap-2 border-aui-term-border border-b px-3 py-1 text-left font-aui-mono text-aui-2xs text-aui-term-muted transition-colors hover:bg-aui-term-border hover:text-aui-term-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent"
        >
          {expanded ? "Show last lines only" : `… ${hidden} earlier lines`}
        </button>
      )}
      <pre
        ref={body}
        // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region must be keyboard reachable
        tabIndex={0}
        aria-busy={status === "in_progress"}
        className="max-h-96 overflow-auto px-3 py-2.5 font-aui-mono text-aui-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset"
      >
        <span className="select-none text-aui-accent">$ </span>
        <span className="font-semibold">{command}</span>
        {shown.length > 0 && "\n"}
        <span className="text-aui-term-fg/85">{shown.join("\n")}</span>
        {status === "in_progress" && (
          <span
            aria-hidden
            className="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-aui-term-fg/70 align-[-2px] motion-reduce:animate-none"
          />
        )}
      </pre>
    </figure>
  );
}
