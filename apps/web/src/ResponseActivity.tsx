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
import { CheckIcon, ChevronRightIcon, CopyIcon, MaximizeIcon, Shimmer } from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import {
  createContext,
  type FormEvent,
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import type { Run } from "./adapter.ts";
import { api, call } from "./api.ts";
import { took, usd } from "./format.ts";
import { Chips, inApp } from "./refChips.tsx";
import {
  defaultTraceLoader,
  messageIdOf,
  type ResponseSource,
  runsOf,
  type TraceLoader,
  traceSource,
} from "./responseSource.ts";
import { href } from "./route.ts";
import { DetailsNavigation } from "./SessionDetails.tsx";
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
    const say = (what: string[]) => {
      pending.current.push(...what);
      if (pending.current.length && !timer)
        timer = setTimeout(flush, Math.max(0, ANNOUNCE_MS - (Date.now() - last.current)));
    };
    // Only a live stream is announced; a replayed one is read, not spoken.
    const speaks = !!source.live;
    const stop = source.open(
      (e) => {
        const before = snapshot(state.current);
        applyResponse(state.current, e);
        if (speaks) say(changes(before as unknown as ResponseState, state.current));
        setVersion((v) => v + 1);
      },
      () => setEnded(true),
      // The response read again: it replaces what came before, and what changed is said.
      (events) => {
        const next = emptyResponse();
        for (const e of events) applyResponse(next, e);
        say(changes(state.current, next));
        state.current = next;
        setVersion((v) => v + 1);
      },
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

/**
 * What the panel was opened on: a call, by the run it started, and a step,
 * by its tool call. Every lane on the way down to it is open, and it is
 * scrolled to.
 */
const Focus = createContext<{ run?: string; step?: string }>({});

const marks = (t: ResponseTool, f: { run?: string; step?: string }) =>
  (f.run !== undefined && t.runId === f.run) || (f.step !== undefined && t.id === f.step);

/** The calls from the root down to the one focused, or none when it is not here. */
export function pathTo(
  blocks: ResponseBlock[],
  focus: { run?: string; step?: string },
): ResponseTool[] {
  for (const b of blocks) {
    if (b.type !== "tool") continue;
    if (marks(b, focus)) return [b];
    const under = pathTo(b.content, focus);
    if (under.length) return [b, ...under];
  }
  return [];
}

/** Scrolls the focused item into view once it is drawn. */
function useArrive(on: boolean) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (on) ref.current?.scrollIntoView?.({ block: "nearest" });
  }, [on]);
  return ref;
}

/** Stops the run behind a call that is still working, from its own line. */
function LaneStop({ run, agent }: { run: string; agent: string }) {
  const [state, setState] = useState<"idle" | "pending" | "failed">("idle");
  return (
    <button
      type="button"
      data-testid="lane-stop"
      disabled={state === "pending"}
      onClick={(e) => {
        // The line is a summary: stopping must not also fold it.
        e.preventDefault();
        setState("pending");
        api.stopRun(run).catch(() => setState("failed"));
      }}
      className={`inline-flex min-h-7 shrink-0 items-center rounded-aui px-1.5 text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-60 ${state === "failed" ? "text-aui-bad" : "text-aui-muted hover:text-aui-fg"}`}
    >
      {state === "pending"
        ? defaultCopy.conversation.stoppingAgent(agent)
        : state === "failed"
          ? defaultCopy.conversation.stopDelayed
          : defaultCopy.conversation.stopAgent(agent)}
    </button>
  );
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

/**
 * A question waiting on a person, answered here: an approval by Approve or
 * Deny, a choice by its options, a reply in words. Answered by the call it
 * waits at, anywhere in the message's conversation.
 */
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
  const [sent, setSent] = useState(false);
  const [text, setText] = useState("");
  const q = tool.question;
  if (!q) return null;
  const approval = q.kind === "approval";
  const options = approval
    ? q.choices?.length
      ? q.choices
      : [words.approve, words.deny]
    : (q.choices ?? []);
  // A question with no options to pick from takes words.
  const replies = !approval && options.length === 0;
  type Body =
    | { action: "approve" | "deny" }
    | { action: "choose"; choice: string }
    | { action: "reply"; text: string };
  const send = async (body: Body) => {
    if (!messageId) return;
    setBusy(true);
    setFailed(false);
    try {
      await call("POST", `/api/messages/${encodeURIComponent(messageId)}/answer`, {
        tool_use_id: tool.id,
        answer: body,
      });
      setSent(true);
      onAnswered?.();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const pick = (option: string) =>
    void send(
      approval
        ? { action: option === options[0] ? "approve" : "deny" }
        : { action: "choose", choice: option },
    );
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (text.trim()) void send({ action: "reply", text: text.trim() });
  };
  const control =
    "inline-flex h-8 items-center rounded-aui border px-3 text-aui-xs focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 pointer-coarse:h-11";
  const first = "border-transparent bg-aui-fg text-aui-bg";
  const rest = "border-aui-border text-aui-fg hover:bg-aui-hover";
  return (
    <fieldset
      aria-label={q.prompt}
      className="m-0 flex min-w-0 flex-col gap-2 rounded-aui border border-aui-border p-3"
    >
      <p className="text-aui-fg text-aui-sm">{q.prompt}</p>
      {sent ? (
        <p className="text-aui-muted text-aui-xs" role="status">
          {words.answerSent}
        </p>
      ) : replies ? (
        <form onSubmit={submit} className="flex min-w-0 flex-col gap-2">
          <textarea
            aria-label={words.yourReply}
            value={text}
            onChange={(e) => setText(e.target.value)}
            rows={2}
            disabled={busy || !messageId}
            className="min-h-16 w-full min-w-0 resize-y rounded-aui border border-aui-border bg-transparent p-2 text-aui-fg text-aui-sm focus-visible:outline-2 focus-visible:outline-aui-accent"
          />
          <button
            type="submit"
            disabled={busy || !messageId || !text.trim()}
            className={`${control} ${first} self-start`}
          >
            {words.send}
          </button>
        </form>
      ) : (
        <div className="flex flex-wrap gap-2">
          {options.map((o, i) => (
            <button
              key={o}
              type="button"
              disabled={busy || !messageId}
              onClick={() => pick(o)}
              className={`${control} ${approval && i === 0 ? first : rest}`}
            >
              {o}
            </button>
          ))}
        </div>
      )}
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
  const focused = useContext(Focus).step === tool.id;
  const arrive = useArrive(focused);
  const urgent = focused || tool.status === "error" || isAwaiting(tool.status);
  const meta = [
    words.status[tool.status],
    tool.elapsedMs !== undefined && isTerminal(tool.status) ? took(tool.elapsedMs) : "",
    tool.decidedBy ? words.by(tool.decidedBy) : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <li
      ref={arrive as React.Ref<HTMLLIElement>}
      data-testid="activity-part"
      data-tool={tool.id}
      data-status={tool.status}
      data-focused={focused || undefined}
    >
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
  result,
  messageId,
  onAnswered,
}: {
  agent: string;
  title?: string;
  status?: ResponseTool["status"];
  tool?: ResponseTool;
  blocks: ResponseBlock[];
  root?: boolean;
  /** What the call came back with, when its own blocks do not already say it. */
  result?: ReactNode;
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
  const running = shown === "running";
  // A called agent is one item: its line, and under it what it did. It is
  // open while it works, waits on someone or failed, and closed once done.
  const focus = useContext(Focus);
  const focused = !!tool && marks(tool, focus);
  const arrive = useArrive(focused);
  // A call that does not wait is delegated: the agent carries on by itself
  // and answers later, not back into its caller's turn.
  const delegated = tool?.input.background === true;
  const urgent =
    focused ||
    pathTo(blocks, focus).length > 0 ||
    running ||
    shown === "error" ||
    shown === "denied" ||
    (shown !== undefined && isAwaiting(shown));
  const line = (
    <>
      {!root && (
        <ChevronRightIcon
          size={12}
          aria-hidden
          className="shrink-0 text-aui-muted transition-transform group-open/lane:rotate-90"
        />
      )}
      <AgentMark name={agent} size={18} />
      <span className="max-w-[50%] shrink-0 truncate font-medium text-aui-fg">
        {running && !root ? <Shimmer>{agent}</Shimmer> : agent}
      </span>
      {delegated && (
        <span className="shrink-0 rounded-full border border-aui-border px-1.5 text-aui-2xs text-aui-muted">
          {words.delegated}
        </span>
      )}
      {title && <span className="min-w-0 flex-1 truncate text-aui-muted text-aui-xs">{title}</span>}
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
      {tool?.runId && running && !root && <LaneStop run={tool.runId} agent={agent} />}
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
    </>
  );
  const body = (
    <>
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
                    result={
                      b.result &&
                      b.status !== "stopped" &&
                      b.content.every((c) => c.type !== "text") && (
                        <p
                          className={`whitespace-pre-wrap text-aui-sm ${b.result.isError ? "text-aui-bad" : "text-aui-fg"}`}
                        >
                          {b.result.text}
                        </p>
                      )
                    }
                  />
                </li>
              );
            return <ToolPart key={b.id} tool={b} messageId={messageId} onAnswered={onAnswered} />;
          }
          return null;
        })}
      </ul>
      {hidden > 0 && <p className="text-aui-muted text-aui-xs">{words.privateSteps(hidden)}</p>}
      {result}
    </>
  );
  if (root)
    return (
      <section
        data-testid="activity-lane"
        data-agent={agent}
        aria-label={agent}
        className="flex flex-col gap-2"
      >
        <p className="flex min-w-0 items-center gap-2 text-aui-sm">{line}</p>
        {body}
      </section>
    );
  return (
    <section
      ref={arrive}
      data-testid="activity-lane"
      data-agent={agent}
      data-focused={focused || undefined}
      data-delegated={delegated || undefined}
      aria-label={agent}
      className="scroll-mt-4"
    >
      <details open={urgent} className="group/lane">
        <summary
          className={`-mx-1.5 flex min-h-9 pointer-coarse:min-h-11 min-w-0 cursor-pointer list-none items-center gap-2 rounded-aui px-1.5 text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent [&::-webkit-details-marker]:hidden ${focused ? "bg-aui-hover" : ""}`}
        >
          {line}
        </summary>
        <div className="mt-1 ml-1.5 flex flex-col gap-2 border-[var(--humagen-rule)] border-l pl-3">
          {body}
        </div>
      </details>
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
        messageId={messageId ?? state.id}
        onAnswered={onAnswered}
      />
    </div>
  );
}

/** A response's activity as Markdown: each agent under who called it, with what it did and said. */
export function activityMarkdown(state: ResponseState): string {
  const lane = (
    agent: string,
    title: string | undefined,
    blocks: ResponseBlock[],
    depth: number,
  ) => {
    const out = [`${"#".repeat(Math.min(depth + 2, 6))} ${agent}${title ? `: ${title}` : ""}`];
    for (const b of blocks) {
      if (b.type === "text" && b.text.trim()) out.push(b.text.trim());
      if (b.type !== "tool" || b.hidden) continue;
      if (b.kind === "agent") {
        out.push(lane(b.agent ?? b.name, b.title, b.content, depth + 1));
        if (b.result && b.content.every((c) => c.type !== "text")) out.push(b.result.text.trim());
      } else out.push(`- ${b.title || b.name} (${words.status[b.status]})`);
    }
    return out.join("\n\n");
  };
  return `${lane(state.agent ?? "", undefined, state.content, 0)}\n`;
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
  focus,
  step,
  leaving,
  onFocus,
  onClose,
}: {
  source: ResponseSource;
  /** The message to answer questions on; without it, questions show but cannot be answered here. */
  messageId?: string;
  /** A call to open on, by the run it started: every lane down to it is open. */
  focus?: string;
  /** A step to open on, by its tool call. */
  step?: string;
  leaving?: boolean;
  /** Steps along the path to the focused call, or back to the root with nothing. */
  onFocus?: (run?: string) => void;
  onClose: () => void;
}) {
  const { state, ended, said, version } = useResponse(source);
  const navigation = useContext(DetailsNavigation);
  const [copied, setCopied] = useState(false);
  const path = focus || step ? pathTo(state.content, { run: focus, step }) : [];
  // The trail ends at the agent the focus is in: a step is in its caller's lane.
  const trail = path.filter((t) => t.kind === "agent");
  const copy = () =>
    void navigator.clipboard?.writeText(activityMarkdown(state)).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  return (
    <Inspector
      open={!leaving}
      title={navigation ? "Details" : words.activity}
      lead={navigation}
      working={!ended}
      onClose={onClose}
      onEscape={() => (focus && onFocus ? onFocus() : onClose())}
      actions={
        state.content.length > 0 && (
          <button
            type="button"
            data-testid="activity-copy"
            onClick={copy}
            className="inline-flex size-9 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
            aria-label={
              copied ? defaultCopy.conversation.copied : defaultCopy.conversation.copyConversation
            }
            title={
              copied ? defaultCopy.conversation.copied : defaultCopy.conversation.copyConversation
            }
          >
            {copied ? <CheckIcon size={16} /> : <CopyIcon size={16} />}
          </button>
        )
      }
    >
      <div data-testid="response-activity" data-version={version} className="flex flex-col gap-3">
        <output aria-live="polite" className="sr-only">
          {said}
        </output>
        {!ended && !state.content.length ? (
          <SkeletonRows count={3} />
        ) : (
          <>
            {trail.length > 0 && (
              <nav
                aria-label="Called by"
                data-testid="activity-trail"
                className="flex flex-wrap items-center gap-x-1 gap-y-0.5 text-aui-muted text-aui-xs"
              >
                <button
                  type="button"
                  onClick={() => onFocus?.()}
                  className="inline-flex items-center gap-1 rounded-aui-sm hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <AgentMark name={state.agent ?? ""} size={12} />
                  {state.agent}
                </button>
                {trail.map((t, i) => (
                  <span key={t.id} className="inline-flex items-center gap-1">
                    <ChevronRightIcon size={12} aria-hidden />
                    {i === trail.length - 1 ? (
                      <span
                        aria-current="page"
                        className="inline-flex items-center gap-1 text-aui-fg"
                      >
                        <AgentMark name={t.agent ?? t.name} size={12} />
                        {t.agent ?? t.name}
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => onFocus?.(t.runId)}
                        className="inline-flex items-center gap-1 rounded-aui-sm hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                      >
                        <AgentMark name={t.agent ?? t.name} size={12} />
                        {t.agent ?? t.name}
                      </button>
                    )}
                  </span>
                ))}
              </nav>
            )}
            {costOf(state) && (
              <p className="text-aui-muted text-aui-xs" data-testid="activity-summary">
                {costOf(state)}
              </p>
            )}
            {state.error && <p className="text-aui-bad text-aui-sm">{state.error}</p>}
            <Focus.Provider value={{ run: focus, step }}>
              <ActivityLanes state={state} messageId={messageId} />
            </Focus.Provider>
          </>
        )}
      </div>
    </Inspector>
  );
}

/**
 * A conversation's activity: the tree of one turn, rooted at the agent that
 * answered it. A turn is opened as its own root. Any other run is a call
 * somewhere under a turn: the turn that holds it is found and its tree opens
 * on that call. With no run named, the latest turn shows.
 */
export function ConversationActivity({
  turns,
  focus,
  step,
  leaving,
  loader = defaultTraceLoader,
  onFocus,
  onClose,
}: {
  /** The conversation's turns, oldest first. */
  turns: Run[];
  focus?: string | null;
  step?: string;
  leaving?: boolean;
  loader?: TraceLoader;
  onFocus?: (run: string | null) => void;
  onClose: () => void;
}) {
  const own = turns.find((t) => t.id === focus);
  const latest = turns[turns.length - 1];
  const [found, setFound] = useState<{ focus: string; root: string }>();
  const ids = turns.map((t) => t.id).join(",");
  // biome-ignore lint/correctness/useExhaustiveDependencies: the turns are named by ids
  useEffect(() => {
    if (!focus || own) return;
    let on = true;
    (async () => {
      // Newest first: a call is most often asked about on the turn just read.
      for (const turn of [...turns].reverse()) {
        const trace = await loader.trace(messageIdOf(turn)).catch(() => undefined);
        if (!on) return;
        if (trace && runsOf(trace).has(focus)) return setFound({ focus, root: turn.id });
      }
    })();
    return () => {
      on = false;
    };
  }, [focus, ids, !!own]);
  const root =
    own ??
    (focus && found?.focus === focus ? turns.find((t) => t.id === found.root) : undefined) ??
    latest;
  if (!root) return null;
  return (
    <ResponseActivityPanel
      source={traceSource(root, loader)}
      focus={focus && focus !== root.id ? focus : undefined}
      step={step}
      leaving={leaving}
      onFocus={(run) => onFocus?.(run ?? root.id)}
      onClose={onClose}
    />
  );
}
