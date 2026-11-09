import { defaultCopy } from "@humagen/components";
import {
  allTools,
  applyResponse,
  changes,
  emptyResponse,
  isAwaiting,
  isTerminal,
  type ResponseBlock,
  type ResponseState,
  type ResponseTool,
  waiting,
  worked,
} from "@humagen/core";
import { ChevronRightIcon, MaximizeIcon } from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { useEffect, useRef, useState } from "react";
import { api, call } from "./api.ts";
import { took, usd } from "./format.ts";
import { Chips, inApp } from "./refChips.tsx";
import type { ResponseSource } from "./responseSource.ts";
import { href } from "./route.ts";
import type { Chip, Reference } from "./trace.ts";
import { AgentMark, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.trace;

/** The most often the live region speaks: once every five seconds. */
export const ANNOUNCE_MS = 5000;

type Snapshot = { status: ResponseState["status"]; tools: Record<string, { status: string }> };
const snapshot = (s: ResponseState): Snapshot => ({
  status: s.status,
  tools: Object.fromEntries(Object.values(s.tools).map((t) => [t.id, { status: t.status }])),
});

/**
 * A response folded from its source as events arrive. `version` changes with
 * every event; `said` is what the live region last announced: state changes
 * only, gathered and spoken at most every `ANNOUNCE_MS`.
 */
export function useResponse(source: ResponseSource | undefined) {
  const state = useRef<ResponseState>(emptyResponse());
  const [version, setVersion] = useState(0);
  const [ended, setEnded] = useState(false);
  const [said, setSaid] = useState("");
  const pending = useRef<string[]>([]);
  const last = useRef(0);
  const key = source?.key;
  // biome-ignore lint/correctness/useExhaustiveDependencies: a source is the same stream while its key is.
  useEffect(() => {
    if (!source) return;
    state.current = emptyResponse();
    pending.current = [];
    setEnded(false);
    setVersion((v) => v + 1);
    let timer: ReturnType<typeof setTimeout> | undefined;
    const flush = () => {
      timer = undefined;
      if (!pending.current.length) return;
      last.current = Date.now();
      setSaid(pending.current.join(". "));
      pending.current = [];
    };
    // Only a live stream is announced; a replayed one is read, not spoken.
    const speaks = !!source.live;
    const stop = source.open(
      (e) => {
        const before = snapshot(state.current);
        applyResponse(state.current, e);
        if (speaks) {
          pending.current.push(...changes(before as unknown as ResponseState, state.current));
          if (pending.current.length && !timer)
            timer = setTimeout(flush, Math.max(0, ANNOUNCE_MS - (Date.now() - last.current)));
        }
        setVersion((v) => v + 1);
      },
      () => setEnded(true),
    );
    return () => {
      stop();
      if (timer) clearTimeout(timer);
    };
  }, [key]);
  return { state: state.current, version, ended, said };
}

/** "42s", or "Working 12s" while it runs. */
function timeWords(state: ResponseState): string {
  return took(worked(state).ms);
}

/**
 * Under an answer: "Worked 42s · 3 agents · 7 tools", a button that opens the
 * response's activity beside it. A response that used no tool and called no
 * agent has none. While a person is awaited, it says so.
 */
export function WorkedButton({
  state,
  open,
  onToggle,
}: {
  state: ResponseState;
  open: boolean;
  onToggle: () => void;
}) {
  const w = worked(state);
  if (!w.agents && !w.tools) return null;
  const awaited = waiting(state).length > 0;
  const label =
    state.status === "running"
      ? awaited
        ? words.waitingForYou
        : words.working(timeWords(state))
      : outcomeOf(state);
  return (
    <button
      type="button"
      aria-expanded={open}
      onClick={onToggle}
      data-testid="worked"
      className={`inline-flex min-h-7 items-center gap-1 self-start rounded-aui px-1.5 text-aui-xs hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent ${awaited ? "text-aui-warn" : "text-aui-muted"}`}
    >
      <ChevronRightIcon
        size={12}
        aria-hidden
        className={`shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
      />
      {label}
    </button>
  );
}

/**
 * A response's references, as chips under its answer: where it started (on
 * the root message only), what `get_context` pulled, and what the answer
 * cites. Work and tools live in the activity.
 */
export function responseChips(state: ResponseState, origin?: Reference): Chip[] {
  const chips: Chip[] = [];
  const seen = new Set<string>();
  const add = (c: Chip) => {
    const key = `${c.kind}:${c.url ?? c.label}`;
    if (seen.has(key)) return;
    seen.add(key);
    chips.push(c);
  };
  if (origin) add({ kind: "origin", label: origin.label, url: origin.url });
  for (const t of allTools(state))
    if (t.kind === "context")
      for (const s of t.result?.sources ?? [])
        if (s.title || s.url)
          add({
            kind: "context",
            label: s.title || s.url || "",
            url: s.url,
            detail: typeof t.input.request === "string" ? t.input.request : undefined,
          });
  for (const b of state.content)
    if (b.type === "text")
      for (const c of b.citations)
        if (c.title || c.url) add({ kind: "source", label: c.title || c.url || "", url: c.url });
  return chips;
}

const statusTone = (t: ResponseTool) =>
  t.status === "error" || t.status === "denied"
    ? "text-aui-bad"
    : isAwaiting(t.status)
      ? "text-aui-warn"
      : "text-aui-muted";

function Json({ value }: { value: unknown }) {
  return (
    <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-aui bg-aui-hover p-2 font-mono text-aui-2xs text-aui-fg">
      {typeof value === "string" ? value : JSON.stringify(value, null, 2)}
    </pre>
  );
}

/** A question waiting on a person, answered here. */
function Ask({
  tool,
  messageId,
  onAnswered,
}: {
  tool: ResponseTool;
  messageId?: string;
  onAnswered?: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const q = tool.question;
  if (!q) return null;
  const choices = q.choices?.length ? q.choices : [words.approve, words.deny];
  // A question that names its run is answered on that run, as the thread's
  // own cards answer it; the message's answer route takes the rest.
  const onRun = q.run && q.id ? { run: q.run, question: q.id } : undefined;
  const canAnswer = !!onRun || !!messageId;
  const answer = async (choice: string) => {
    if (!canAnswer) return;
    setBusy(true);
    setFailed(false);
    const approval = q.kind === "approval";
    const verdict = choice === choices[0] ? "approve" : "deny";
    try {
      if (onRun)
        await api.answer({
          ...onRun,
          body: approval ? { action: verdict } : { action: "choose", choice },
        });
      else
        await call("POST", `/api/messages/${encodeURIComponent(messageId ?? "")}/answer`, {
          tool_use_id: tool.id,
          answer: approval ? verdict : choice,
        });
      onAnswered?.();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <fieldset
      aria-label={q.prompt}
      className="m-0 flex min-w-0 flex-col gap-2 rounded-aui border border-aui-border p-3"
    >
      <p className="text-aui-fg text-aui-sm">{q.prompt}</p>
      <div className="flex flex-wrap gap-2">
        {choices.map((c, i) => (
          <button
            key={c}
            type="button"
            disabled={busy || !canAnswer}
            onClick={() => void answer(c)}
            className={`inline-flex h-8 items-center rounded-aui border px-3 text-aui-xs focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 ${i === 0 ? "border-transparent bg-aui-fg text-aui-bg" : "border-aui-border text-aui-fg hover:bg-aui-hover"}`}
          >
            {c}
          </button>
        ))}
      </div>
      {failed && <p className="text-aui-bad text-aui-xs">{words.answerFailed}</p>}
    </fieldset>
  );
}

/** One tool call in a lane: closed unless it failed or waits on a person. */
function ToolPart({
  tool,
  messageId,
  onAnswered,
}: {
  tool: ResponseTool;
  messageId?: string;
  onAnswered?: () => void;
}) {
  const urgent = tool.status === "error" || isAwaiting(tool.status);
  const meta = [
    words.status[tool.status],
    tool.elapsedMs !== undefined && isTerminal(tool.status) ? took(tool.elapsedMs) : "",
    tool.decidedBy ? words.by(tool.decidedBy) : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <li data-testid="activity-part" data-tool={tool.id} data-status={tool.status}>
      <details open={urgent} className="group/part">
        <summary className="flex min-h-7 cursor-pointer list-none items-center gap-1.5 rounded-aui text-aui-xs hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent">
          <ChevronRightIcon
            size={12}
            aria-hidden
            className="shrink-0 text-aui-muted transition-transform group-open/part:rotate-90"
          />
          <span className="min-w-0 truncate text-aui-fg">{tool.title || tool.name}</span>
          <span className={`shrink-0 ${statusTone(tool)}`}>{meta}</span>
        </summary>
        <div className="mt-1 flex flex-col gap-1.5 pl-4">
          {isAwaiting(tool.status) && (
            <Ask tool={tool} messageId={messageId} onAnswered={onAnswered} />
          )}
          {Object.keys(tool.input).length > 0 && (
            <details className="group/input">
              <summary className="flex min-h-6 cursor-pointer list-none items-center gap-1 text-aui-2xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent">
                <ChevronRightIcon
                  size={10}
                  aria-hidden
                  className="shrink-0 transition-transform group-open/input:rotate-90"
                />
                {words.input}
              </summary>
              <div className="mt-0.5">
                <Json value={tool.input} />
              </div>
            </details>
          )}
          {/* A stopped call's result only repeats that it stopped, which its row already says. */}
          {tool.result && tool.status !== "stopped" && (
            <div>
              <p className="mb-0.5 text-aui-2xs text-aui-muted">{words.result}</p>
              {tool.result.isError ? (
                <p className="text-aui-bad text-aui-xs">{tool.result.text}</p>
              ) : (
                <Json value={tool.result.text} />
              )}
            </div>
          )}
          <Chips
            chips={(tool.result?.sources ?? [])
              .filter((s) => s.title || s.url)
              .map((s) => ({
                kind: "context" as const,
                label: s.title || s.url || "",
                url: s.url,
              }))}
          />
        </div>
      </details>
    </li>
  );
}

/**
 * One agent's lane: its mark, what it was asked, its parts in order, and the
 * agents it called as lanes nested under it. The root's own final text is the
 * answer beside the panel, so its lane leaves it out.
 */
function Lane({
  agent,
  title,
  status,
  tool,
  blocks,
  root,
  messageId,
  onAnswered,
}: {
  agent: string;
  title?: string;
  status?: ResponseTool["status"];
  tool?: ResponseTool;
  blocks: ResponseBlock[];
  root?: boolean;
  messageId?: string;
  onAnswered?: () => void;
}) {
  // The root's text after its last tool block is the answer, shown beside the panel.
  let lastTool = -1;
  blocks.forEach((b, i) => {
    if (b.type === "tool" || b.type === "result") lastTool = i;
  });
  const hidden = blocks.filter(
    (b) => b.type === "hidden" || (b.type === "tool" && b.hidden),
  ).length;
  // A lane that runs while someone under it waits or failed says so.
  const shown = tool?.attention && status === "running" ? tool.attention.status : status;
  return (
    <section
      data-testid="activity-lane"
      data-agent={agent}
      aria-label={agent}
      className={
        root
          ? "flex flex-col gap-2"
          : "flex flex-col gap-2 border-[var(--humagen-rule)] border-l pl-3"
      }
    >
      <p className="flex min-w-0 items-center gap-2 text-aui-sm">
        <AgentMark name={agent} size={18} />
        <span className="max-w-[50%] shrink-0 truncate font-medium text-aui-fg">{agent}</span>
        {title && (
          <span className="min-w-0 flex-1 truncate text-aui-muted text-aui-xs">{title}</span>
        )}
        {shown && shown !== "done" && (
          <span
            className={`ml-auto shrink-0 text-aui-xs ${shown === "error" || shown === "denied" ? "text-aui-bad" : isAwaiting(shown) ? "text-aui-warn" : "text-aui-muted"}`}
          >
            {words.status[shown]}
          </span>
        )}
        {tool?.elapsedMs !== undefined && status === "done" && (
          <span className="ml-auto shrink-0 text-aui-muted text-aui-xs tabular-nums">
            {took(tool.elapsedMs)}
          </span>
        )}
        {tool?.runId && (
          <a
            href={href({ page: "message", id: tool.runId })}
            onClick={inApp}
            aria-label={words.focus(agent)}
            title={words.focus(agent)}
            className="inline-flex size-7 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <MaximizeIcon size={14} />
          </a>
        )}
      </p>
      {tool?.progress && shown === "running" && (
        <p className="text-aui-muted text-aui-xs">{tool.progress}</p>
      )}
      <ul className="flex flex-col gap-1.5">
        {/* A block's index is its identity: blocks are only appended, and a resent one keeps its index. */}
        {blocks.map((b, i) => {
          if (b.type === "thinking")
            return (
              // biome-ignore lint/suspicious/noArrayIndexKey: a block's index is its identity in the stream.
              <li key={`t${i}`}>
                <details className="group/think">
                  <summary className="flex min-h-7 cursor-pointer list-none items-center gap-1.5 text-aui-muted text-aui-xs hover:text-aui-fg">
                    <ChevronRightIcon
                      size={12}
                      aria-hidden
                      className="transition-transform group-open/think:rotate-90"
                    />
                    {words.thinking}
                  </summary>
                  <p className="mt-1 whitespace-pre-wrap pl-4 text-aui-muted text-aui-xs">
                    {b.text}
                  </p>
                </details>
              </li>
            );
          if (b.type === "text") {
            if (root && i > lastTool) return null;
            return (
              // biome-ignore lint/suspicious/noArrayIndexKey: a block's index is its identity in the stream.
              <li key={`x${i}`} className="whitespace-pre-wrap break-words text-aui-fg text-aui-sm">
                {b.text}
              </li>
            );
          }
          if (b.type === "tool" && !b.hidden) {
            if (b.kind === "agent")
              return (
                <li key={b.id}>
                  <Lane
                    agent={b.agent ?? b.name}
                    title={b.title}
                    status={b.status}
                    tool={b}
                    blocks={b.content}
                    messageId={messageId}
                    onAnswered={onAnswered}
                  />
                  {b.result &&
                    b.status !== "stopped" &&
                    b.content.every((c) => c.type !== "text") && (
                      <p
                        className={`mt-1 whitespace-pre-wrap pl-3 text-aui-sm ${b.result.isError ? "text-aui-bad" : "text-aui-fg"}`}
                      >
                        {b.result.text}
                      </p>
                    )}
                </li>
              );
            return <ToolPart key={b.id} tool={b} messageId={messageId} onAnswered={onAnswered} />;
          }
          return null;
        })}
      </ul>
      {hidden > 0 && <p className="text-aui-muted text-aui-xs">{words.privateSteps(hidden)}</p>}
    </section>
  );
}

/** One agent call's lane on its own, as an agent's calls are listed in Details. */
export function AgentCallLane({ tool, messageId }: { tool: ResponseTool; messageId?: string }) {
  return (
    <Lane
      agent={tool.agent ?? tool.name}
      title={tool.title}
      status={tool.status}
      tool={tool}
      blocks={tool.content}
      messageId={messageId}
    />
  );
}

/** A response's activity as lanes: one per agent call, nested by who called whom. */
export function ActivityLanes({
  state,
  messageId,
  onAnswered,
}: {
  state: ResponseState;
  messageId?: string;
  onAnswered?: () => void;
}) {
  return (
    <div data-testid="activity-lanes" className="flex flex-col gap-3">
      <Lane
        agent={state.agent ?? ""}
        blocks={state.content}
        root
        messageId={messageId}
        onAnswered={onAnswered}
      />
    </div>
  );
}

/**
 * How a response ended, in plain words, once: "Worked 42s · 3 agents · 7
 * tools", or "Stopped after 3.0s · 1 of 1 agents unfinished".
 */
export function outcomeOf(state: ResponseState): string {
  const w = worked(state);
  const time = took(w.ms);
  if (state.status === "running") return words.working(time);
  if (state.status === "stopped" || state.status === "failed") {
    const calls = allTools(state).filter((t) => t.kind === "agent");
    const unfinished = calls.filter((t) => t.status !== "done").length;
    return (state.status === "stopped" ? words.stoppedAfter : words.failedAfter)(
      time,
      unfinished,
      calls.length,
    );
  }
  return words.worked(time, w.agents, w.tools);
}

/** What a response cost and the models it ran on: "$0.02 · claude-sonnet". */
export function costOf(state: ResponseState): string {
  return [
    state.costUsd ? usd(Math.round(state.costUsd * 1_000_000)) : "",
    ...Object.keys(state.byModel ?? {}).map((m) => m.split("/").pop() ?? m),
  ]
    .filter(Boolean)
    .join(" · ");
}

/** The outcome, cost and models together, for a line of its own (Details › References). */
export function summaryOf(state: ResponseState): string {
  return [outcomeOf(state), costOf(state)].filter(Boolean).join(" · ");
}

/**
 * A response's activity in the Details panel, live while it runs and replayed
 * after: the summary, then its lanes. One polite live region says when a call
 * starts waiting on someone, fails, or the response ends, at most every five
 * seconds.
 */
export function ResponseActivityPanel({
  source,
  messageId,
  leaving,
  onClose,
}: {
  source: ResponseSource;
  /** The message to answer questions on; without it, questions show but cannot be answered here. */
  messageId?: string;
  leaving?: boolean;
  onClose: () => void;
}) {
  const { state, ended, said, version } = useResponse(source);
  return (
    <Inspector open={!leaving} title={words.activity} wide onClose={onClose}>
      <div data-testid="response-activity" data-version={version} className="flex flex-col gap-3">
        <output aria-live="polite" className="sr-only">
          {said}
        </output>
        {!ended && !state.content.length ? (
          <SkeletonRows count={3} />
        ) : (
          <>
            {costOf(state) && (
              <p className="text-aui-muted text-aui-xs" data-testid="activity-summary">
                {costOf(state)}
              </p>
            )}
            {state.error && <p className="text-aui-bad text-aui-sm">{state.error}</p>}
            <ActivityLanes state={state} messageId={messageId} />
          </>
        )}
      </div>
    </Inspector>
  );
}
