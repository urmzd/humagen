import { defaultCopy } from "@humagen/components";
import { BanIcon, CheckCircleIcon, HourglassIcon, XCircleIcon } from "@humagen/render-web";
import { LatestButton, useLatest } from "@humagen/web";
import {
  type FormEvent,
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import type {
  ConversationSandboxes,
  SandboxCommandResult,
} from "../../../packages/client/src/generated/types.ts";
import { ApiError } from "./api.ts";
import { agentShown } from "./branding.ts";
import { took } from "./format.ts";
import { SkeletonRows } from "./ui.tsx";
import type { WorkbenchTabProps } from "./workbench.ts";
import {
  activityOf,
  type Command,
  type CommandState,
  commandLog,
  fromResult,
  personCommands,
} from "./workbenchTerminal.ts";

const words = defaultCopy.workbenchTerminal;

// The tab's own button: the console's outline button, on the terminal's tokens and at 14px.
const button =
  "inline-flex h-11 shrink-0 items-center justify-center whitespace-nowrap rounded-aui border border-aui-border px-4 font-medium text-aui-base text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-disabled:text-aui-muted aria-disabled:hover:bg-transparent";

/** What the tab asks the server. A test stands in for it. */
export interface TerminalApi {
  sandboxes: (thread: string) => Promise<ConversationSandboxes>;
  run: (thread: string, command: string) => Promise<SandboxCommandResult>;
}

async function ask<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: "same-origin",
    ...(body === undefined
      ? {}
      : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
  });
  const value = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new ApiError(response.status, value.error || `Request failed (${response.status})`);
  return value as T;
}

const at = (thread: string) => `/api/threads/${encodeURIComponent(thread)}/sandboxes`;
const server: TerminalApi = {
  sandboxes: (thread) => ask<ConversationSandboxes>("GET", at(thread)),
  run: (thread, command) =>
    ask<SandboxCommandResult>("POST", `${at(thread)}/commands`, { command }),
};

// More output than this is folded to its end, where a result and its errors are.
const FOLD_OVER = 40;
const FOLD_TO = 24;

const LIVE = new Set(["running", "accepted"]);

function stateWords(command: Command): string {
  const s: CommandState = command.state;
  if (s === "exited")
    return command.exit === undefined ? words.state.done : words.state.exited(command.exit);
  return words.state[s];
}

function StateMark({ command }: { command: Command }): ReactNode {
  const failed =
    command.state === "failed" ||
    command.state === "timed_out" ||
    (command.exit !== undefined && command.exit !== 0);
  if (failed) return <XCircleIcon size={16} aria-hidden className="shrink-0 text-aui-bad" />;
  if (command.state === "exited")
    return <CheckCircleIcon size={16} aria-hidden className="shrink-0 text-aui-ok" />;
  if (command.state === "denied" || command.state === "stopped")
    return <BanIcon size={16} aria-hidden className="shrink-0 text-aui-muted" />;
  if (command.state === "waiting")
    return <HourglassIcon size={16} aria-hidden className="shrink-0 text-aui-muted" />;
  // Running or queued: a still mark. The agent's own mark in the thread shows it working.
  return <span aria-hidden className="size-2 shrink-0 rounded-full bg-aui-accent" />;
}

function Entry({ command, current }: { command: Command; current: boolean }) {
  const [all, setAll] = useState(false);
  const item = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (current) item.current?.scrollIntoView?.({ block: "start" });
  }, [current]);
  const who =
    command.by === "person" ? words.you : command.agent ? agentShown(command.agent) : words.agent;
  const state = stateWords(command);
  const lines = command.output ? command.output.replace(/\n$/, "").split("\n") : [];
  const folded = lines.length > FOLD_OVER && !all;
  const shown = folded ? lines.slice(-FOLD_TO) : lines;
  const ended = !["running", "queued", "waiting"].includes(command.state);
  return (
    <li
      ref={item}
      data-command={command.id}
      data-by={command.by}
      data-state={command.state}
      aria-current={current ? "true" : undefined}
      aria-label={command.hidden ? words.hidden : words.entry(who, command.command, state)}
      className="border-aui-term-border border-b px-4 py-3 last:border-b-0 aria-[current=true]:bg-aui-term-border"
    >
      <p className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-aui-base text-aui-muted">
        <span className="font-medium text-aui-fg">{who}</span>
        <span className="inline-flex items-center gap-1.5 text-aui-fg">
          <StateMark command={command} />
          {state}
        </span>
        {command.elapsedMs !== undefined && ended && (
          <span className="tabular-nums">{took(command.elapsedMs)}</span>
        )}
      </p>
      {command.hidden ? (
        <p className="mt-1 text-aui-base text-aui-muted">{words.hidden}</p>
      ) : (
        <pre className="mt-1 whitespace-pre-wrap break-words font-aui-mono text-aui-base text-aui-fg">
          <span aria-hidden className="select-none text-aui-muted">
            ${" "}
          </span>
          <span className="font-semibold">{command.command}</span>
        </pre>
      )}
      {command.cut && !command.hidden && (
        <p className="mt-1 text-aui-base text-aui-muted">{words.cut}</p>
      )}
      {lines.length > FOLD_OVER && (
        <button
          type="button"
          aria-expanded={all}
          onClick={() => setAll((v) => !v)}
          className="mt-1 inline-flex min-h-9 items-center rounded-aui-sm text-aui-base text-aui-muted underline underline-offset-4 hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {all ? words.showLess : words.showAll(lines.length)}
        </button>
      )}
      {shown.length > 0 && (
        <pre
          data-testid="terminal-output"
          className="mt-1 whitespace-pre-wrap break-words font-aui-mono text-aui-base text-aui-fg"
        >
          {shown.join("\n")}
        </pre>
      )}
      {ended && lines.length === 0 && !command.hidden && command.state !== "denied" && (
        <p className="mt-1 text-aui-base text-aui-muted">{words.noOutput}</p>
      )}
      {command.files && command.files.length > 0 && (
        <p className="mt-1 break-words text-aui-base text-aui-muted">
          {words.changed(
            command.files.map((f) => (f.removed ? words.removed(f.path) : f.path)).join(", "),
          )}
        </p>
      )}
      {command.conflicts?.map((conflict) => (
        <p key={conflict} className="mt-1 break-words text-aui-base text-aui-fg">
          {conflict}
        </p>
      ))}
    </li>
  );
}

/**
 * The Terminal tab: every command the session's agents ran, with its output
 * and how it ended, as their answers' own streams report it, live while one
 * works. Where the deployment's sandbox is contained and the reader may
 * post, a command line runs a command of their own in the session's files;
 * each is recorded and goes with their next message. The terminal is dark in
 * both schemes, as the console's terminal tokens are.
 */
export function WorkbenchTerminal({
  thread,
  runs,
  responses,
  canPost,
  selection,
  openTab,
  record,
  api = server,
}: WorkbenchTabProps & { api?: TerminalApi }) {
  const mine = useSyncExternalStore(
    personCommands.subscribe,
    () => personCommands.list(thread),
    () => personCommands.list(thread),
  );
  const working = runs.some((r) => LIVE.has(r.status));
  const [boxes, setBoxes] = useState<ConversationSandboxes>();
  const [pending, setPending] = useState(false);
  const [draft, setDraft] = useState("");
  const [problem, setProblem] = useState("");
  const [said, setSaid] = useState("");
  const [back, setBack] = useState(0);
  const hint = useId();
  const count = useRef(0);

  // Whether the reader may run a command here is the server's to say, again
  // whenever a run starts or ends.
  const statuses = runs.map((r) => `${r.id}:${r.status}`).join("|");
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again when a run's status changes, and after a command.
  useEffect(() => {
    if (!canPost) {
      setBoxes(undefined);
      return;
    }
    let on = true;
    api
      .sandboxes(thread)
      .then((b) => on && setBoxes(b))
      .catch(() => on && setBoxes(undefined));
    return () => {
      on = false;
    };
  }, [thread, canPost, statuses, pending, api]);

  const log = useMemo(
    () =>
      commandLog(
        responses,
        mine,
        runs.filter((r) => !r.parent),
      ),
    [responses, mine, runs],
  );
  const latest = useLatest({ working: working || pending, follow: true, startAtEnd: !selection });

  const may = canPost && !!boxes?.session.commands;
  const open = may && !!boxes?.session.writable;
  // A terminal opens on its last line, and stays there while the pane
  // settles: the list is measured before its font is read and before the
  // command line is drawn under it. Once the reader moves the log it is theirs.
  const filled = responses !== undefined && log.length > 0;
  const { scroller, stuck } = latest;
  // biome-ignore lint/correctness/useExhaustiveDependencies: found again when the command line is drawn.
  useEffect(() => {
    const el = scroller.current;
    if (!filled || selection || !el) return;
    let mine = true;
    const moved = () => {
      mine = false;
    };
    const pin = () => {
      if (!mine) return;
      el.scrollTop = el.scrollHeight;
      stuck.current = true;
    };
    for (const type of ["wheel", "touchmove", "keydown"] as const)
      el.addEventListener(type, moved, { passive: true });
    pin();
    const frame = requestAnimationFrame(pin);
    document.fonts?.ready.then(pin);
    return () => {
      mine = false;
      cancelAnimationFrame(frame);
      for (const type of ["wheel", "touchmove", "keydown"] as const)
        el.removeEventListener(type, moved);
    };
  }, [filled, selection, may, scroller, stuck]);

  const closed = !may || open ? "" : working ? words.busy : (boxes?.session.reason ?? words.busy);

  const run = async (event: FormEvent) => {
    event.preventDefault();
    const command = draft.trim();
    if (!command || pending || !open) return;
    const id = `by-hand-${Date.now()}-${count.current++}`;
    const started = new Date().toISOString();
    setPending(true);
    setProblem("");
    setDraft("");
    setBack(0);
    personCommands.put(thread, {
      id,
      by: "person",
      command,
      state: "running",
      output: "",
      at: started,
    });
    latest.jump();
    try {
      const done = fromResult(id, started, await api.run(thread, command));
      personCommands.put(thread, done);
      // It goes with their next message; nothing is sent now.
      record(activityOf(done, words));
      const lines = done.output ? done.output.replace(/\n$/, "").split("\n").length : 0;
      setSaid(words.ended(stateWords(done), lines));
    } catch (error) {
      const why = error instanceof Error && error.message ? error.message : words.failedToRun;
      personCommands.put(thread, {
        id,
        by: "person",
        command,
        state: "failed",
        output: why,
        at: started,
      });
      setProblem(why);
    } finally {
      setPending(false);
    }
  };

  // Up and down walk the reader's own earlier commands, as a shell does.
  const recall = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    const next = Math.max(0, Math.min(mine.length, back + (event.key === "ArrowUp" ? 1 : -1)));
    if (next === back) return;
    event.preventDefault();
    setBack(next);
    setDraft(next === 0 ? "" : (mine[mine.length - next]?.command ?? ""));
  };

  return (
    <div
      data-theme="dark"
      data-testid="workbench-terminal"
      className="flex min-h-0 min-w-0 flex-1 flex-col bg-aui-term text-aui-fg"
    >
      <div className="relative flex min-h-0 flex-1 flex-col [--aui-bg:var(--aui-term-bg)]">
        <div
          ref={latest.scroller}
          onScroll={latest.onScroll}
          role="log"
          aria-live="off"
          aria-label={words.log}
          aria-busy={working || pending}
          // biome-ignore lint/a11y/noNoninteractiveTabindex: a scrolling region must be reachable from the keyboard.
          tabIndex={0}
          className="min-h-0 flex-1 overflow-y-auto outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset"
        >
          <div ref={latest.content}>
            {responses === undefined ? (
              <SkeletonRows count={4} className="px-4" />
            ) : log.length === 0 ? (
              <div className="flex flex-col items-start gap-3 px-4 py-6">
                <h3 className="font-medium text-aui-fg text-aui-lg">{words.empty}</h3>
                {!may && (
                  <button type="button" className={button} onClick={() => openTab("details")}>
                    {words.toDetails}
                  </button>
                )}
              </div>
            ) : (
              <ol className="m-0 list-none p-0">
                {log.map((command) => (
                  <Entry key={command.id} command={command} current={command.id === selection} />
                ))}
              </ol>
            )}
          </div>
        </div>
        <LatestButton look={latest.look} onJump={latest.jump} scroller={latest.scroller} />
      </div>
      <output className="sr-only">{said}</output>
      {may && (
        <form onSubmit={run} className="shrink-0 border-aui-term-border border-t px-4 py-3">
          <div className="flex items-center gap-2">
            <span aria-hidden className="select-none font-aui-mono text-aui-base text-aui-muted">
              $
            </span>
            <input
              type="text"
              value={draft}
              onChange={(e) => {
                setDraft(e.target.value);
                setBack(0);
              }}
              onKeyDown={recall}
              readOnly={pending}
              disabled={!open}
              aria-label={words.input}
              aria-describedby={hint}
              placeholder={words.placeholder}
              maxLength={2000}
              autoCapitalize="off"
              autoCorrect="off"
              autoComplete="off"
              spellCheck={false}
              enterKeyHint="go"
              className="h-11 min-w-0 flex-1 rounded-aui border border-aui-border bg-transparent px-3 font-aui-mono text-aui-base text-aui-fg placeholder:text-aui-muted focus-visible:outline-2 focus-visible:outline-aui-accent"
            />
            <button
              type="submit"
              className={button}
              aria-disabled={!open || pending || !draft.trim()}
            >
              {words.run}
            </button>
          </div>
          <p id={hint} className="mt-2 text-aui-base text-aui-muted">
            {closed || words.fresh}
          </p>
          {problem && (
            <p role="alert" className="mt-1 text-aui-base text-aui-fg">
              {problem}
            </p>
          )}
        </form>
      )}
    </div>
  );
}
