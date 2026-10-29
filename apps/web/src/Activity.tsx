import { answerOfAction } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import { CheckIcon, ChevronRightIcon, CopyIcon, WebSurface } from "@humagen/render-web";
import { AgentMessage, Inspector, StoppableMark } from "@humagen/web";
import { useEffect, useMemo, useState } from "react";
import { type Run, turnOf } from "./adapter.ts";
import { ApiError, api, type Span } from "./api.ts";
import { statusWord } from "./copyas.ts";
import { itemActions } from "./itemActions.tsx";
import { LimitsPanel } from "./Limits.tsx";
import { forRun, type Limits } from "./limits.ts";
import { MessageTime } from "./MessageTime.tsx";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { AgentMark, AgentTint, Empty, isEnded, Mentioned, SkeletonRows } from "./ui.tsx";

/** One turn of the conversation and everything it called: the runs, and what each did. */
export type Tree = { root: string; runs: Run[]; spans: Span[] };

const live = (r: Run) => !isEnded(r);

/** A tree's runs with each parent before its children, so indentation reads as who called whom. */
export function ordered(tree: Tree): Run[] {
  const kids = new Map<string, Run[]>();
  for (const r of tree.runs) {
    const key = r.id === tree.root ? "" : (r.parent ?? tree.root);
    kids.set(key, [...(kids.get(key) ?? []), r]);
  }
  const out: Run[] = [];
  const walk = (key: string) => {
    for (const r of (kids.get(key) ?? []).sort((a, b) =>
      a.created_at.localeCompare(b.created_at),
    )) {
      if (out.includes(r)) continue;
      out.push(r);
      walk(r.id);
    }
  };
  walk("");
  // A run whose parent is missing from the tree is still shown, at the end.
  for (const r of tree.runs) if (!out.includes(r)) out.push(r);
  return out;
}

/** A run and everything it called, directly or through others. */
export function descendants(all: Run[], run: Run): Run[] {
  const out = [run];
  for (let i = 0; i < out.length; i++)
    for (const r of all) if (r.parent === out[i]?.id && !out.includes(r)) out.push(r);
  return out;
}

/** "oncall 2" for the second time an agent runs in a conversation. The first time is just its name. */
export function ordinals(trees: Tree[]): Map<string, number> {
  const seen = new Map<string, number>();
  const out = new Map<string, number>();
  for (const t of trees) {
    for (const r of ordered(t)) {
      const n = (seen.get(r.agent) ?? 0) + 1;
      seen.set(r.agent, n);
      out.set(r.id, n);
    }
  }
  return out;
}

/** From the turn that started it down to a run: the trail shown above its conversation. */
export function trailOf(runs: Map<string, Run>, id: string): Run[] {
  const path: Run[] = [];
  const guard = new Set<string>();
  let at = runs.get(id);
  while (at && !guard.has(at.id)) {
    guard.add(at.id);
    path.unshift(at);
    at = at.parent ? runs.get(at.parent) : undefined;
  }
  return path;
}

/** Every agent that ran, in the order it first did, with its runs oldest first. */
export function agentsOf(trees: Tree[]): { agent: string; runs: Run[] }[] {
  const by = new Map<string, Run[]>();
  for (const t of trees)
    for (const r of ordered(t)) by.set(r.agent, [...(by.get(r.agent) ?? []), r]);
  return [...by.entries()].map(([agent, runs]) => ({
    agent,
    runs: runs.sort((a, b) => a.created_at.localeCompare(b.created_at)),
  }));
}

/** Who gave a run its message: the agent that called it, or the person. */
function askedBy(run: Run, runs: Map<string, Run>): string {
  const caller = run.parent ? runs.get(run.parent) : undefined;
  return caller && run.depth > 0 ? caller.agent : "";
}

/** A conversation as Markdown: each message an agent was given, and what it answered. */
export function conversationMarkdown(agent: string, list: Run[], runs: Map<string, Run>): string {
  return list
    .map((r) =>
      [
        `**${askedBy(r, runs) || "You"} → ${agent}**`,
        r.text || "No request text",
        ...(r.answer ? [`**${agent}**`, r.answer] : []),
        ...(r.status !== "completed" ? [`_${statusWord(r)}_`] : []),
      ].join("\n\n"),
    )
    .join("\n\n---\n\n");
}

/** What a run says about where it stands, only when a still mark would not say it. */
function standing(run: Run): string | null {
  switch (run.status) {
    case "failed":
      return "Failed";
    case "stopped":
      return "Stopped";
    case "parked":
      return "Waiting on a person";
    default:
      return null;
  }
}

/**
 * One run in the list: its agent's mark, which stops it while it works, who
 * asked it, the last thing it said, and when.
 */
function RunRow({ run, by, onOpen }: { run: Run; by: string; onOpen: () => void }) {
  const { run: shown, stop } = useRunStop(run, live(run));
  const state =
    shown.status === "failed"
      ? "error"
      : shown.status === "parked"
        ? "waiting"
        : live(shown)
          ? "thinking"
          : "idle";
  const said = shown.answer?.trim() || shown.text || "";
  const word = live(shown) ? null : standing(shown);
  return (
    <li className="humagen-enter flex min-w-0 items-start gap-2 px-5 py-2">
      <span className="pt-1.5">
        <StoppableMark agent={run.agent} state={state} stop={stop}>
          <AgentMark name={run.agent} size={18} state={state} />
        </StoppableMark>
      </span>
      <button
        type="button"
        data-testid="activity-row"
        data-run={run.id}
        data-status={shown.status}
        onClick={onOpen}
        className="flex min-w-0 flex-1 items-start gap-2 rounded-aui py-1 pr-1 pl-1 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline gap-2">
            <span className="truncate text-aui-sm font-medium text-aui-fg">{run.agent}</span>
            {by && (
              <span className="shrink-0 text-aui-2xs text-aui-muted">
                {defaultCopy.conversation.callAsked(by)}
              </span>
            )}
            {word && <span className="shrink-0 text-aui-2xs text-aui-fg">{word}</span>}
            <span className="ml-auto shrink-0 text-aui-2xs text-aui-muted">
              <MessageTime at={run.created_at} />
            </span>
          </span>
          <span className="block truncate text-aui-xs text-aui-muted">{said}</span>
        </span>
        <ChevronRightIcon size={14} className="mt-1 shrink-0 text-aui-muted" />
      </button>
    </li>
  );
}

/**
 * One call of an agent, as chat: the message it was given and by whom, then
 * its answer, tool steps and calls to other agents, drawn by the component
 * the conversation uses. A call to another agent opens that agent here.
 */
function Call({ run, by, onOpen }: { run: Run; by: string; onOpen: (id: string) => void }) {
  const streamed = useRunTurn(run);
  const turn = useMemo(() => streamed ?? turnOf(run), [streamed, run]);
  const { stop } = useRunStop(run, live(run));
  const [problem, setProblem] = useState<string | null>(null);
  const onAction = (a: UiAction) => {
    if (a.id === ActionId.openRun) return onOpen(a.payload.target);
    // A run called by another can wait on a person too, and is answered here.
    const answer = answerOfAction(a);
    if (!answer) return;
    setProblem(null);
    api
      .answer(answer)
      .catch((e) =>
        setProblem(
          e instanceof ApiError && e.status === 403
            ? defaultCopy.noPermission
            : e instanceof ApiError
              ? e.message
              : defaultCopy.failure.internal(),
        ),
      );
  };
  return (
    <section
      id={`activity-call-${run.id}`}
      data-testid="activity-conversation"
      className="flex scroll-mt-4 flex-col gap-3"
    >
      <div className="flex flex-col items-end gap-1">
        <p className="text-aui-2xs text-aui-muted">
          {by ? defaultCopy.conversation.callAsked(by) : defaultCopy.conversation.activityYouAsked}{" "}
          <MessageTime at={run.created_at} />
        </p>
        {run.input ? (
          <div className="flex w-full flex-col gap-2" aria-label="Helper input">
            {/* What it was given, as given: a replay can use it again. */}
            <p className="text-aui-xs text-aui-muted">
              Return format: {run.input.input.response_format.type.replaceAll("_", " ")}
            </p>
            {run.input.messages.map((message, index) => (
              <div
                // biome-ignore lint/suspicious/noArrayIndexKey: a recorded input never reorders
                key={index}
                className="humagen-glass ml-auto max-w-[90%] whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg"
              >
                <p className="mb-1 text-aui-xs text-aui-muted">
                  {message.role === "assistant" ? "Example response" : "Input message"}
                </p>
                {message.text}
              </div>
            ))}
          </div>
        ) : (
          <p className="humagen-glass max-w-[90%] whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">
            {run.text ? <Mentioned text={run.text} /> : "No request text"}
          </p>
        )}
      </div>
      <AgentTint name={run.agent}>
        <WebSurface onAction={onAction}>
          <AgentMessage
            turn={turn}
            itemActions={itemActions(run)}
            stop={stop}
            disclaimer={false}
            capabilities={{ regenerate: false, feedback: false }}
          />
        </WebSurface>
      </AgentTint>
      {problem && (
        <p role="alert" className="text-aui-xs text-aui-bad">
          {problem}
        </p>
      )}
    </section>
  );
}

/**
 * Every agent a conversation set to work, and each one's conversation.
 * The list has one row per agent. A row opens that agent's calls, oldest
 * first, as one chat; a call it made opens the agent it called. The trail
 * at the top leads back up. This is the panel with its data given.
 * `Activity` loads the data.
 */
export function ActivityPanel({
  trees,
  selected,
  loading,
  leaving,
  limits,
  onSelect,
  onClose,
}: {
  /** The conversation's limits and usage. A selected run then shows what it draws on. */
  limits?: Limits;
  /** The panel is closing: it slides back out before it is removed. See `Presence` in ui.tsx. */
  leaving?: boolean;
  /** One per turn of the conversation, oldest first. */
  trees: Tree[];
  /** The call open, by its run. Its agent's whole conversation is shown, at that call. */
  selected: string | null;
  loading?: boolean;
  onSelect: (id: string | null) => void;
  onClose: () => void;
}) {
  const runs = useMemo(
    () => new Map(trees.flatMap((t) => t.runs.map((r) => [r.id, r] as const))),
    [trees],
  );
  const agents = useMemo(() => agentsOf(trees), [trees]);
  // Every run, oldest first: the list a reader picks from.
  const timeline = useMemo(
    () => [...runs.values()].sort((a, b) => a.created_at.localeCompare(b.created_at)),
    [runs],
  );
  const run = selected ? runs.get(selected) : undefined;
  const calls = run ? (agents.find((a) => a.agent === run.agent)?.runs ?? [run]) : [];
  const trail = run ? trailOf(runs, run.id) : [];
  const [copied, setCopied] = useState(false);

  // Opening a call scrolls the agent's conversation to it.
  useEffect(() => {
    if (!run || calls.length < 2) return;
    document
      .getElementById(`activity-call-${run.id}`)
      ?.scrollIntoView({ block: "start", behavior: "instant" as ScrollBehavior });
  }, [run?.id, calls.length]);

  const copy = () => {
    const text = run
      ? conversationMarkdown(run.agent, calls, runs)
      : agents.map((a) => conversationMarkdown(a.agent, a.runs, runs)).join("\n\n---\n\n");
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  return (
    <Inspector
      open={!leaving}
      title={defaultCopy.conversation.activity}
      onClose={onClose}
      onEscape={() => (selected ? onSelect(null) : onClose())}
      actions={
        agents.length > 0 && (
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
      <div data-testid="activity" className="-mx-5 -mt-5 flex min-h-0 flex-col">
        {run ? (
          <div key={run.agent} className="humagen-fade flex min-h-0 flex-1 flex-col">
            <nav
              aria-label="Called by"
              className="flex flex-wrap items-center gap-x-1 gap-y-0.5 px-5 pt-3 pb-2 text-aui-xs text-aui-muted"
            >
              <button
                type="button"
                onClick={() => onSelect(null)}
                className="rounded-aui-sm hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                {defaultCopy.conversation.activityAgents}
              </button>
              {trail.map((r, i) => (
                <span key={r.id} className="inline-flex items-center gap-1">
                  <ChevronRightIcon size={12} />
                  {i === trail.length - 1 ? (
                    <span
                      aria-current="page"
                      className="inline-flex items-center gap-1 text-aui-fg"
                    >
                      <AgentMark name={r.agent} size={12} />
                      {r.agent}
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={() => onSelect(r.id)}
                      className="inline-flex items-center gap-1 rounded-aui-sm hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
                    >
                      <AgentMark name={r.agent} size={12} />
                      {r.agent}
                    </button>
                  )}
                </span>
              ))}
            </nav>
            <div className="flex flex-col gap-4 px-5 pb-5">
              {calls.map((c, i) => (
                <div key={c.id} className="flex flex-col gap-4">
                  {i > 0 && (
                    <hr aria-label={`Call ${i + 1}`} className="border-[var(--humagen-rule)]" />
                  )}
                  <Call run={c} by={askedBy(c, runs)} onOpen={onSelect} />
                </div>
              ))}
              {limits && (
                <details
                  data-testid="activity-limits"
                  className="border-[var(--humagen-rule)] border-y py-1"
                >
                  <summary className="cursor-pointer list-none rounded-aui-sm py-1 text-aui-xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent">
                    Limits it draws on
                  </summary>
                  <LimitsPanel data={forRun(limits, run.id)} />
                </details>
              )}
            </div>
          </div>
        ) : agents.length === 0 ? (
          <div className="px-5 pt-5">
            {loading ? (
              <SkeletonRows count={4} height="3rem" />
            ) : (
              <Empty title={defaultCopy.conversation.activityEmpty}>
                {defaultCopy.conversation.activityEmptyBody}
              </Empty>
            )}
          </div>
        ) : (
          <ul
            aria-label={defaultCopy.conversation.activityAgents}
            className="flex flex-col divide-y divide-[var(--humagen-rule)] py-1"
          >
            {timeline.map((r) => (
              <RunRow key={r.id} run={r} by={askedBy(r, runs)} onOpen={() => onSelect(r.id)} />
            ))}
          </ul>
        )}
      </div>
    </Inspector>
  );
}

/**
 * The conversation's activity, loaded and kept current. One panel for the
 * whole conversation: it takes the turns on screen and fetches what each one
 * called. `stamp` should change whenever a run in the conversation does.
 */
export function Activity({
  turns,
  stamp,
  focus,
  leaving,
  limits,
  onClose,
  onFocus,
}: {
  limits?: Limits;
  leaving?: boolean;
  turns: Run[];
  stamp: string;
  /** A run to open first, such as the turn whose activity was asked for. */
  focus?: string | null;
  onClose: () => void;
  onFocus?: (id: string | null) => void;
}) {
  const [trees, setTrees] = useState<Tree[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<string | null>(focus ?? null);
  const select = (id: string | null) => {
    setSelected(id);
    onFocus?.(id);
  };
  const ids = turns.map((t) => t.id).join(",");

  // biome-ignore lint/correctness/useExhaustiveDependencies: the turns are named by ids, and stamp says when they changed
  useEffect(() => {
    let on = true;
    Promise.all(
      turns.map((t) =>
        api
          .trace(t.id)
          .then(
            (d): Tree => ({ root: t.id, runs: d.runs.length > 0 ? d.runs : [t], spans: d.spans }),
          )
          // A turn whose events are no longer held is still listed, from its record.
          .catch((): Tree => ({ root: t.id, runs: [t], spans: [] })),
      ),
    ).then((next) => {
      if (!on) return;
      setTrees(next);
      setLoading(false);
    });
    return () => {
      on = false;
    };
  }, [ids, stamp]);

  useEffect(() => {
    setSelected(focus ?? null);
  }, [focus]);

  // Escape steps back out: from a run to the list, from the list to the conversation.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      e.preventDefault();
      if (selected) select(null);
      else onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selected, onClose, onFocus]);

  return (
    <ActivityPanel
      trees={trees}
      selected={selected}
      loading={loading}
      leaving={leaving}
      limits={limits}
      onSelect={select}
      onClose={onClose}
    />
  );
}
