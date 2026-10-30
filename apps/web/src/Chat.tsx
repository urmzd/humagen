import { answerOfAction, answers } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import { BringBackIcon, ForkIcon, WebSurface } from "@humagen/render-web";
import { AgentMessage, messageActionButton, Thread, useQueue } from "@humagen/web";
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Activity } from "./Activity.tsx";
import { actedFor, citationOf, type Run, turnOf } from "./adapter.ts";
import {
  ApiError,
  api,
  coworking,
  type ProjectView,
  type Revision,
  type ThreadAsk,
  type ThreadPage,
  type Thread as ThreadRecord,
  type ThreadSummary,
} from "./api.ts";
import { askedOf, copyFor } from "./asks.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { type ConversationForkActions, ConversationForkPanels } from "./ConversationForkPanels.tsx";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { type Completion, useCompletion } from "./completion.ts";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { FindHelp, type HelpSuggestion } from "./FindHelp.tsx";
import { type HomeAnnouncement, HomePage } from "./HomePage.tsx";
import { itemActions } from "./itemActions.tsx";
import { MessageTime } from "./MessageTime.tsx";
import { OutcomeActions } from "./OutcomeActions.tsx";
import {
  emptySelection,
  MAX_SELECTED,
  PlusMenu,
  placeholderFor,
  type Selection,
  SelectionChips,
  withAgent,
} from "./Plus.tsx";
import { ProjectPicker } from "./ProjectPicker.tsx";
import { ThreadRoom, topicsOf } from "./Room.tsx";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { type ConversationPanel, type ConversationView, href } from "./route.ts";
import { Scroller } from "./Scroller.tsx";
import { useThreadSelection } from "./selection.ts";
import { readFromStart } from "./turns.ts";
import {
  AgentMark,
  AgentTint,
  Checkpoint,
  cause,
  isWorking,
  Mentioned,
  Page,
  Presence,
  Problem,
} from "./ui.tsx";

const words = defaultCopy.threads;

/** The thread a page holds before one is read: nothing selected, nowhere. */
const noThread: ThreadRecord = {
  id: "",
  project: "",
  channel: "general",
  visibility: "private",
  participants: [],
  version: 0,
  created_by: "",
  created_at: "",
  updated_at: "",
};
/** The answers to one point in the conversation, oldest first. */
function siblingsOf(turns: Run[], run: Run): Run[] {
  return turns.filter((t) => (t.prev ?? "") === (run.prev ?? ""));
}

/**
 * Walk the conversation from its start, taking one answer at each point: the
 * one the reader chose, or the newest. That walk is the branch on screen. A
 * conversation too long to be read whole is walked from the oldest turn read.
 */
function pathOf(turns: Run[], chosen: Record<string, string>): Run[] {
  const path: Run[] = [];
  const read = new Set(turns.map((t) => t.id));
  let prev = turns.some((t) => (t.prev ?? "") === "")
    ? ""
    : (turns.find((t) => !read.has(t.prev ?? ""))?.prev ?? "");
  for (;;) {
    const options = turns.filter((t) => (t.prev ?? "") === prev);
    const next = options.find((t) => t.id === chosen[prev]) ?? options[options.length - 1];
    if (!next || path.includes(next)) return path;
    path.push(next);
    prev = next.id;
  }
}

/**
 * The person whose turn it was, to ask again: whom it acted for, as its
 * record says. A turn recorded before humagen said whom a run acts for names no
 * user, so one a person asked under their own name, from the console (an
 * `api-r-` id) or the API with their own key, is theirs, unless its words
 * were someone else's: an earlier turn's at the same point (siblings, oldest
 * first) asked by anyone else, which is what the console's Retry and
 * Regenerate asked then, under the presser's name. A machine asking for a
 * person (its own key) and a retry humagen asked for nobody (`api-retry-`)
 * name whoever it was for, or pressed it, though the words may not have
 * been theirs, so those are nobody's.
 */
function ownerOf(run: Run, siblings: Run[]): string {
  const person = actedFor(run);
  if (person || !run.actor || run.actor.bot || run.actor.user || run.origin !== "api")
    return person;
  const id = run.actor.id;
  const tag = run.tag_id ?? "";
  if (
    !tag.startsWith("api-r-") &&
    !tag.startsWith(`api-${new TextEncoder().encode(id).length}-${id}-`)
  )
    return "";
  const at = siblings.findIndex((s) => s.id === run.id);
  const earlier = at < 0 ? siblings : siblings.slice(0, at);
  return earlier.some((s) => s.text === run.text && s.actor?.id !== id) ? "" : id;
}

/**
 * Why the reader may not ask a turn again in the conversation, with Retry or
 * Regenerate, or "" when they may. A turn asked again here is asked as the
 * reader, so it acts for them, and is offered only on a turn that acted for
 * them. A turn that acted for someone else is theirs alone to retry, and one
 * that acted for nobody is retried for nobody (from the inbox, or where it
 * was asked), so nobody's words are run with the reader's credentials
 * without their typing them. siblings are the turns at its point in the
 * conversation, oldest first.
 */
export function notAskable(run: Run, me: string, siblings: Run[]): string {
  const person = ownerOf(run, siblings);
  if (me !== "" && person === me) return "";
  // The person by what the run calls them, never by subject, which can hold
  // an address: a run recorded with no name for them names nobody.
  if (person)
    return `This turn acted for ${run.actor?.display || "someone else"}, so only they can retry it.`;
  return "This turn acted for nobody, so it is not retried here, where it would act for you.";
}

function TurnView({
  run: originalRun,
  me,
  siblings,
  ask,
  onSwitch,
  onAsk,
  onReply,
  onRedirect,
  actions,
  canStop = false,
}: {
  run: Run;
  canStop?: boolean;
  /** The reader's subject. */
  me: string;
  siblings: Run[];
  /** The question the run waits on, as the reader stands to it: it is offered only when it is theirs. */
  ask?: ThreadAsk;
  /** Follow the run's events. Off for settled turns, whose record is enough. */
  live: boolean;
  onSwitch: (id: string) => void;
  onAsk?: () => void;
  onReply?: () => void;
  onRedirect?: () => void;
  actions?: ReactNode;
}) {
  const { run, stop } = useRunStop(originalRun, canStop);
  const turn = useRunTurn(run);
  const index = siblings.findIndex((s) => s.id === run.id);
  const ended = run.status === "completed" || run.status === "failed";
  const refused = notAskable(run, me, siblings);
  const drawn = useMemo(() => {
    const base = turn ?? turnOf(run);
    return run.citations !== undefined
      ? { ...base, citations: run.citations.map(citationOf) }
      : base;
  }, [turn, run]);
  // Neither Retry nor Regenerate is offered on a turn the reader may not ask
  // again (notAskable). A failure says why instead of offering Retry.
  const shown = useMemo(() => {
    const asked = askedOf(drawn, ask);
    return refused && asked.failure
      ? { ...asked, failure: { ...asked.failure, retryNotOffered: refused } }
      : asked;
  }, [drawn, refused, ask]);

  return (
    <article
      id={`turn-${run.id}`}
      className="flex flex-col gap-3"
      aria-label={`Turn answered by ${run.agent}`}
    >
      <p className="humagen-glass max-w-[80%] self-end whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">
        <Mentioned text={run.text} />
      </p>
      {/* The answer names its agent. Only a router's pick, which it does not show, is said. */}
      {run.picked && <Checkpoint agent={run.agent}>{run.picked}</Checkpoint>}

      <AgentTint name={run.agent}>
        <AgentMessage
          actions={actions}
          stop={stop}
          disclaimer={false}
          timestamp={<MessageTime at={run.created_at} />}
          turn={shown}
          copy={copyFor(ask)}
          itemActions={itemActions(run)}
          capabilities={{ regenerate: !refused }}
          onAskAgent={onAsk}
          onReply={onReply}
          onRedirect={onRedirect}
        />
      </AgentTint>

      <div className="flex flex-wrap items-center gap-2 pl-11 text-aui-xs text-aui-muted">
        {siblings.length > 1 && (
          <span className="inline-flex items-center gap-0.5">
            <button
              type="button"
              aria-label="Previous answer"
              disabled={index <= 0}
              onClick={() => onSwitch(siblings[index - 1]?.id ?? run.id)}
              className="inline-flex size-6 items-center justify-center rounded-aui hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40"
            >
              ‹
            </button>
            <span className="tabular-nums" title={siblings.map((s) => s.agent).join(", ")}>
              {index + 1} / {siblings.length}
            </span>
            <button
              type="button"
              aria-label="Next answer"
              disabled={index >= siblings.length - 1}
              onClick={() => onSwitch(siblings[index + 1]?.id ?? run.id)}
              className="inline-flex size-6 items-center justify-center rounded-aui hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40"
            >
              ›
            </button>
          </span>
        )}
        {run.calls > 0 && <span>{run.calls} calls to other agents</span>}
        {!ended && run.error && <span>Last attempt failed, retrying: {cause(run.error)}</span>}
      </div>
    </article>
  );
}

/**
 * Ways into the platform's own agents, shown on an empty conversation when
 * the workspace has them. A pick writes the agent's tag and the start of a
 * message in the chat bar.
 */
export type Starter = { agent: string; label: string; text: string };

export const STARTERS: Starter[] = [
  {
    agent: "humagen-analyst",
    label: "How are my agents doing?",
    text: "How did my agents do today?",
  },
  { agent: "humagen-builder", label: "Build an agent", text: "Help me build an agent that " },
  { agent: "humagen-writer", label: "Write a report", text: "Write a one-page report on " },
];

/**
 * The chat bar with completion for a project: its lookups live here, so
 * they are made only while the bar is on screen. `plus` builds the + menu
 * from the agents that may answer in the project.
 */
function ChatBar({
  project,
  agents,
  homes,
  projects,
  root,
  me,
  handle,
  plus,
  onAvailable,
  ...rest
}: Omit<Parameters<typeof Composer>[0], "tags" | "plus"> & {
  /** Called with the agents that may answer in the project, once they are read. */
  onAvailable?: (available: { name: string }[]) => void;
  project: string;
  agents: Revision[];
  homes: Record<string, string>;
  projects: ProjectView[] | null;
  root: ProjectView | null;
  me: string;
  handle?: string;
  plus?: (
    choices: Pick<Completion, "agents" | "skills" | "skillsFailed">,
  ) => NonNullable<Parameters<typeof Composer>[0]["plus"]>;
}) {
  const tags = useCompletion(
    project,
    agents,
    homes,
    topicsOf(projects ?? [], root, project),
    me,
    handle,
  );
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per list read
  useEffect(() => {
    if (tags.agents) onAvailable?.(tags.agents);
  }, [tags.agents]);
  return <Composer {...rest} tags={tags} plus={plus?.(tags)} />;
}

/**
 * Home's chat bar and a conversation of runs. Home's bar starts a private
 * conversation (`POST /api/threads`) in the picked project and opens it at
 * its address; a message there runs an agent only when it tags one. A
 * conversation people have posted in is a room (ThreadRoom); one of runs
 * alone, from before threads, shows its turns along the branch on screen,
 * and a message posted to it makes it a room. `threadId` null is Home.
 */
export function Chat({
  threadId,
  me,
  handle = "",
  startIn,
  startAgent,
  startAsk,
  startChannel,
  focus,
  sharing,
  onSharing,
  view,
  onView,
  root,
  workspaceName,
  onOpenChannel,
  summary,
  tick,
  agents,
  agentsRead = true,
  homes = {},
  projects,
  canRun,
  artifactActions,
  canLearn = false,
  forkActions,
  canCreateProject,
  greeting,
  onThread,
  onChanged,
  onProjectCreated,
  home,
  banner,
  findHelp,
  personalProject,
  starters: ownStarters,
}: {
  /** The workspace's suggestions on an empty conversation. Null or left out is the console's own. */
  starters?: Starter[] | null;
  /** Quiet lines under the prompt on Home: what needs the person, and what is new. */
  home?: ReactNode;
  banner?: HomeAnnouncement;
  findHelp?: (description: string) => Promise<HelpSuggestion[]>;
  personalProject?: string;
  threadId: string | null;
  /** The reader's subject: Retry asks a turn again as them, so only on a turn that acted for them. */
  me: string;
  /** How the platform names the reader (GET /api/me handle), which a thread names its starter by. */
  handle?: string;
  /** The project a new conversation starts in, when it was started from one. */
  startIn?: string;
  /** The agent a new conversation is for, and its first message, when a link or a starter picked them. */
  startAgent?: string;
  startAsk?: string;
  /** The channel of the project a new conversation starts in, as a shared thread. */
  startChannel?: string;
  /** A message of a shared thread to open at. */
  focus?: string;
  /** Whether the conversation's Share dialog is open, and a way to open or close it at its address. */
  sharing?: boolean;
  onSharing?: (open: boolean) => void;
  view?: ConversationView;
  onView?: (view: ConversationView) => void;
  /** The workspace's own project, which heads the tree, and its name. */
  root?: ProjectView | null;
  workspaceName?: string;
  onOpenChannel?: (project: string, channel: string) => void;
  /** The conversation at a glance, once it exists. */
  summary: ThreadSummary | undefined;
  /** Changes whenever any run starts or ends. */
  tick: number;
  agents: Revision[];
  /** Whether the agents have been read. Until they are, Home holds the composer's place rather than saying none are set up. */
  agentsRead?: boolean;
  /** Where each agent that lives in a project lives, so completion offers only what may answer here. */
  homes?: Record<string, string>;
  /** The project tree a new conversation can be filed in, or null while it loads. */
  projects: ProjectView[] | null;
  canRun: boolean;
  canLearn?: boolean;
  artifactActions?: { knowledge: boolean; memory: boolean };
  forkActions?: ConversationForkActions;
  /** May make a project from the picker. */
  canCreateProject: boolean;
  greeting: string;
  onThread: (id: string) => void;
  onChanged: () => void;
  onProjectCreated: (project: ProjectView) => void;
}) {
  const [turns, setTurns] = useState<Run[]>([]);
  // The thread as the platform reads it: its record, and its people's messages besides its runs.
  const [room, setRoom] = useState<ThreadPage | null>(null);
  // The + selection of a conversation of runs, saved on its thread as a room's is.
  const runs = useThreadSelection(threadId ?? "", room?.thread ?? noThread, {
    saved: (t) => {
      setRoom((r) => (r ? { ...r, thread: t } : r));
      onChanged();
    },
    reread: (p) => setRoom((r) => (r ? { ...r, thread: p.thread } : r)),
    problem: (why) => setProblem(why),
  });
  const [chosen, setChosen] = useState<Record<string, string>>({});
  const [seed, setSeed] = useState<ComposerSeed | undefined>(() =>
    startAsk ? { text: startAsk } : undefined,
  );
  // What the + menu selected for a new conversation: an agent a link named, and where it lives.
  const [selection, setSelection] = useState<Selection>(() => ({
    ...emptySelection,
    agents: startAgent ? [startAgent] : [],
    project: startIn ?? personalProject ?? null,
    channel: startChannel || "general",
  }));

  useEffect(() => {
    if (!threadId && startIn === undefined && personalProject !== undefined)
      setSelection((current) =>
        current.project === null ? { ...current, project: personalProject } : current,
      );
  }, [threadId, startIn, personalProject]);
  const project = selection.project ?? personalProject ?? null;
  const select = (next: Selection) => {
    if (next.project !== selection.project) setOffered(null);
    setSelection(
      next.project === null && personalProject !== undefined
        ? { ...next, project: personalProject, channel: "general" }
        : next,
    );
  };
  const [draft, setDraft] = useState("");
  // The agents that can answer in the picked project, once read.
  const [offered, setOffered] = useState<Set<string> | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  // The drawer of every run in the conversation, open on one of them or on the list.
  const [activity, setActivity] = useState<{ focus: string | null } | null>(null);
  const [localView, setLocalView] = useState<ConversationView>({});
  const shownView = onView ? (view ?? {}) : localView;
  const openPanel = (
    panel?: ConversationPanel,
    selection: Omit<ConversationView, "panel"> = {},
  ) => {
    const next = {
      ...selection,
      panel,
    };
    if (onView) onView(next);
    else setLocalView(next);
  };
  useEffect(() => {
    setActivity(shownView.panel === "activity" ? { focus: shownView.run ?? null } : null);
  }, [shownView.panel, shownView.run]);
  // What the drawer was showing, kept for the moment it takes to leave.
  const lastActivity = useRef(activity);
  if (activity) lastActivity.current = activity;

  // What reading the conversation came to. One not read yet is not a new
  // conversation, and one the reader may not see is not there: the server
  // answers both alike (404), and so does the page.
  const [read, setRead] = useState<"reading" | "read" | "missing" | "failed">(
    threadId ? "reading" : "read",
  );
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    if (!threadId) {
      setRoom(null);
      setRead("read");
      return setTurns([]);
    }
    let live = true;
    // The branch on screen is walked from the conversation's start, so every
    // page is read, not only the newest.
    readFromStart((before) => coworking.thread(threadId, before))
      .then((p) => {
        if (!live) return;
        setRoom(p);
        setTurns(p.turns.flatMap((t) => (t.run ? [t.run] : [])));
        setRead("read");
      })
      .catch((e) => {
        if (!live) return;
        if (e instanceof ApiError && e.status === 404) {
          setRoom(null);
          setTurns([]);
          setRead("missing");
          return;
        }
        // A refresh that fails keeps what was read.
        setRead((was) => (was === "reading" ? "failed" : was));
      });
    return () => {
      live = false;
    };
  }, [threadId, tick]);

  // A different conversation starts from its newest branch.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset only when the conversation changes
  useEffect(() => {
    setChosen({});
    setProblem(null);
  }, [threadId]);

  const path = useMemo(() => pathOf(turns, chosen), [turns, chosen]);
  // What each parked run asks, as the reader stands to it.
  const asks = useMemo(
    () =>
      Object.fromEntries(
        (room?.turns ?? []).flatMap((t) => (t.run && t.ask ? [[t.run.id, t.ask]] : [])),
      ),
    [room],
  );
  const tail = path[path.length - 1];
  // A run waiting on a person is still, like one that has ended.
  const tailWorking = !!tail && isWorking(tail.status);
  // Where the next message goes, for completion: the conversation's project, or the one picked for a new one.
  const where = threadId ? (tail?.project ?? "") : (project ?? "");

  // Retry and Regenerate ask a turn again: one that follows another joins its
  // conversation, and the first is filed in `filed`, where the turn was.
  const post = useCallback(
    async (text: string, to: string, after: string | undefined, filed: string | null = project) => {
      setProblem(null);
      const run = await api.createRun(
        to,
        text,
        after ? { after } : filed !== null ? { project: filed } : {},
      );
      setTurns((t) => (t.some((x) => x.id === run.id) ? t : [...t, run]));
      setChosen((c) => ({ ...c, [run.prev ?? ""]: run.id }));
      onChanged();
      if (!threadId) onThread(run.thread);
      return run;
    },
    [threadId, project, onThread, onChanged],
  );

  // The runs the message in flight started. The queue holds the next
  // message until the ledger says each has ended, and Stop stops them.
  const replying = useRef<{ ids: string[]; stop: boolean }>({ ids: [], stop: false });
  const [awaiting, setAwaiting] = useState<string[]>([]);
  const stopRuns = (ids: string[]) =>
    Promise.all(ids.map((id) => api.stopRun(id))).catch((e) =>
      setProblem(e instanceof ApiError ? e.message : defaultCopy.conversation.stopDelayed),
    );
  const queue = useQueue({
    onStart: (m) => {
      setProblem(null);
      replying.current = { ids: [], stop: false };
      const done = (runs: Run[] = []) => {
        onChanged();
        const ids = runs.filter((r) => isWorking(r.status)).map((r) => r.id);
        if (ids.length === 0) return queue.turnEnded("done");
        // Stop was asked while the message was being posted.
        if (replying.current.stop) void stopRuns(ids);
        replying.current = { ...replying.current, ids };
        setAwaiting(ids);
      };
      const failed = (e: unknown) => {
        setProblem(e instanceof ApiError ? e.message : words.failed.post);
        queue.reject();
        // The first message opens the conversation, so nothing may queue
        // behind it: one that did not post goes back in the box to send again.
        if (!threadId) {
          queue.clear();
          setSeed({ text: m.text });
        }
      };
      // Home starts a private conversation in the picked project, or a
      // shared thread when it was opened from a channel. Either opens at
      // its address, where the next message goes.
      if (!threadId) {
        coworking
          .start({
            project: project ?? "",
            channel: selection.channel,
            text: m.text,
            visibility: startChannel ? "shared" : "private",
            ...(selection.agents.length > 0
              ? { agents: selection.agents, mode: selection.mode }
              : {}),
          })
          .then((p) => {
            onThread(p.thread.id);
            done(p.runs);
          })
          .catch(failed);
        return;
      }
      // A conversation of runs takes the message after the turn on screen.
      // Posted, it is a room: the message, and any run its tags started.
      coworking
        .post(threadId, { text: m.text, ...(tail ? { after: tail.id } : {}) })
        .then((p) => {
          setRoom((r) =>
            r
              ? {
                  ...r,
                  turns: [
                    ...r.turns,
                    { kind: "message", message: p.message },
                    ...p.runs.map((run) => ({ kind: "run" as const, run })),
                  ],
                }
              : r,
          );
          done(p.runs);
        })
        .catch(failed);
    },
    onInterrupt: () => {
      replying.current = { ...replying.current, stop: true };
      if (replying.current.ids.length > 0) void stopRuns(replying.current.ids);
    },
  });

  // A reply already in flight when the conversation is read, such as the
  // first message sent from Home, is the running turn: new messages queue
  // behind it and Stop stops it. Only the reader's own last message counts.
  // biome-ignore lint/correctness/useExhaustiveDependencies: adopt once per read
  useEffect(() => {
    if (!room || awaiting.length > 0 || queue.state.status !== "idle") return;
    const all = room.turns;
    let last = -1;
    for (let i = all.length - 1; i >= 0; i--)
      if (all[i]?.kind === "message") {
        last = i;
        break;
      }
    const asked = last >= 0 ? all[last]?.message : undefined;
    if (asked && asked.by !== me) return;
    const runs = all
      .slice(last + 1)
      .flatMap((t) => (t.run && isWorking(t.run.status) ? [t.run] : []));
    const fallback = !asked && tail && isWorking(tail.status) ? [tail] : [];
    const live = runs.length > 0 ? runs : fallback;
    if (live.length === 0) return;
    const ids = live.map((r) => r.id);
    replying.current = { ids, stop: false };
    setAwaiting(ids);
    queue.adopt(asked?.text ?? live[0]?.text ?? "");
  }, [room]);

  // The ledger decides when the reply is over: each run it started has ended
  // or waits on a person, as the conversation was last read.
  // biome-ignore lint/correctness/useExhaustiveDependencies: the queue is read, not watched
  useEffect(() => {
    if (awaiting.length === 0) return;
    const read = awaiting.map((id) => turns.find((t) => t.id === id));
    if (read.some((r) => !r || isWorking(r.status))) return;
    setAwaiting([]);
    replying.current = { ids: [], stop: false };
    queue.turnEnded(
      read.some((r) => r?.status === "failed")
        ? "failed"
        : read.some((r) => r?.status === "stopped")
          ? "stopped"
          : "done",
    );
  }, [turns, awaiting]);

  // A branch of the first turn starts the conversation again, so it is filed
  // where the turn was: the picker's project is only for a new conversation.
  const branch = (run: Run, to: string) => {
    post(run.text, to, run.prev, run.project ?? null).catch((e) =>
      setProblem(e instanceof ApiError ? e.message : "The branch could not be started."),
    );
  };

  const onAction = (a: UiAction) => {
    // An agent drawn inside a turn opens the drawer on its run.
    if (a.id === ActionId.openRun) return openPanel("activity", { run: a.payload.target });
    // A question the run waits on is answered in place. The run's own events
    // settle the card once the answer is recorded, whoever's it was.
    if (answers(a)) {
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
      return;
    }
    if (a.id !== ActionId.retry || !canRun) return;
    const run = path.find((r) => r.id === a.payload.turnId);
    if (!run) return;
    // Asked again here, a turn acts for the reader, so only one that acted for them is.
    const refused = notAskable(run, me, siblingsOf(turns, run));
    if (refused) return setProblem(refused);
    branch(run, run.agent);
  };

  // A suggestion is offered only for an agent that can answer where the conversation goes.
  const starters = (ownStarters ?? STARTERS).filter(
    (st) =>
      agents.some((a) => a.spec.name === st.agent) && offered !== null && offered.has(st.agent),
  );

  const composer = canRun && agents.length > 0 && (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-2 px-4 pb-4">
      <Problem message={problem} />
      <ChatBar
        project={where}
        agents={agents}
        homes={homes}
        projects={projects}
        root={root ?? null}
        me={me}
        handle={handle}
        placeholder={
          !threadId
            ? startChannel
              ? words.startThread
              : placeholderFor(selection)
            : runs.selection.agents.length > 0
              ? placeholderFor(runs.selection)
              : words.message
        }
        queue={queue}
        autoFocus={!threadId}
        seed={seed}
        onDraft={setDraft}
        // An agent the project picked does not offer is not kept for it.
        onAvailable={
          threadId
            ? undefined
            : (available) => {
                const names = new Set(available.map((a) => a.name));
                setOffered(names);
                // An agent a link named that lives in a project opens that project, the first time.
                const named =
                  startAgent && !startIn && project === null ? homes[startAgent] : undefined;
                if (named && !names.has(startAgent ?? "")) {
                  setSelection((sel) => ({ ...sel, project: named }));
                  return;
                }
                const dropped = selection.agents.filter((a) => !names.has(a));
                if (dropped.length > 0) setProblem(defaultCopy.chat.notHere(dropped));
                setSelection((sel) => {
                  const kept = sel.agents.filter((a) => names.has(a));
                  return kept.length === sel.agents.length ? sel : { ...sel, agents: kept };
                });
              }
        }
        plus={
          threadId
            ? // A conversation of runs selects its agents as a room does, so a
              // follow-up goes to them; its project is fixed by its runs.
              ({ agents: available, skills, skillsFailed }) => ({
                menu: (close, attach, leave) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={available}
                    skills={skills}
                    skillsFailed={skillsFailed}
                    onSkill={(name) => {
                      setSeed({ text: `$${name} `, mode: "prepend" });
                    }}
                    selection={runs.selection}
                    onChange={runs.select}
                    onAttach={attach}
                    onClose={close}
                    projects={projects}
                    personalProject={personalProject}
                    kinds={root?.kinds}
                    projectFixed={defaultCopy.threads.shareProjectFixed}
                  />
                ),
                chips: (
                  <SelectionChips
                    personalProject={personalProject}
                    selection={runs.selection}
                    projects={projects}
                    workspace={workspaceName}
                    onChange={runs.select}
                    projectFixed
                  />
                ),
              })
            : ({ agents: available, skills, skillsFailed }) => ({
                menu: (close, attach, leave) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={available}
                    skills={skills}
                    skillsFailed={skillsFailed}
                    onSkill={(name) => {
                      setSeed({ text: `$${name} `, mode: "prepend" });
                    }}
                    selection={selection}
                    onChange={select}
                    onAttach={attach}
                    onClose={close}
                    projects={projects}
                    personalProject={personalProject}
                    kinds={root?.kinds}
                    canCreateProject={canCreateProject}
                    onProjectCreated={onProjectCreated}
                    finder={
                      findHelp ? (
                        <FindHelp
                          initialDescription={draft}
                          onSearch={findHelp}
                          canChoose={canRun}
                          onChoose={(match, description) => {
                            select({
                              ...selection,
                              project: match.project,
                              channel: "general",
                              agents: [match.agent],
                              mode: "join",
                            });
                            if (!draft.trim()) setSeed({ text: description });
                            close();
                          }}
                        />
                      ) : undefined
                    }
                  />
                ),
                chips: (
                  <SelectionChips
                    personalProject={personalProject}
                    selection={selection}
                    projects={projects}
                    workspace={workspaceName}
                    onChange={select}
                    projectControl={
                      <ProjectPicker
                        projects={projects ?? null}
                        kinds={root?.kinds}
                        value={selection.project}
                        personalProject={personalProject}
                        channel={selection.channel}
                        canCreate={canCreateProject}
                        onCreated={onProjectCreated}
                        onChange={(project) =>
                          select({ ...selection, project, channel: "general" })
                        }
                      />
                    }
                  />
                ),
              })
        }
      />
    </div>
  );

  // A thread people have posted in is a room: their messages and the agents' answers, side by side.
  if (
    threadId &&
    room &&
    room.thread.id === threadId &&
    (room.thread.read_only || room.turns.some((t) => t.message))
  ) {
    return (
      <ThreadRoom
        key={threadId}
        id={threadId}
        me={me}
        handle={handle}
        initial={room}
        tick={tick}
        agents={agents}
        homes={homes}
        projects={projects ?? []}
        root={root ?? null}
        workspaceName={workspaceName ?? ""}
        canRun={canRun}
        personalProject={personalProject}
        artifactActions={artifactActions}
        canLearn={canLearn}
        forkActions={forkActions}
        focus={focus}
        sharing={sharing}
        onSharing={onSharing}
        view={view}
        onView={onView}
        onOpenThread={onThread}
        onOpenChannel={(p, c) => onOpenChannel?.(p, c)}
        onChanged={onChanged}
      />
    );
  }

  if (threadId && read === "missing") {
    return (
      <Page title={defaultCopy.threads.missingTask} lede={defaultCopy.projects.missingHint}>
        {null}
      </Page>
    );
  }
  if (threadId && path.length === 0 && (read === "reading" || read === "failed")) {
    return (
      <ConversationLoading title={summary?.title} busy={read === "reading"}>
        {read === "failed" ? (
          <Problem message={defaultCopy.threads.readFailed} />
        ) : (
          <ConversationSkeleton />
        )}
      </ConversationLoading>
    );
  }

  if (!threadId || path.length === 0) {
    return (
      <HomePage
        banner={banner}
        greeting={greeting}
        canRun={canRun}
        hasAgents={!agentsRead || agents.length > 0}
        below={home}
      >
        {composer}
        {/*
          Suggestions stay with the chat bar while the person writes. A pick writes its words and adds its agent to
          the conversation, as a chip, not an @mention.
        */}
        {canRun && starters.length > 0 && (
          // Centered under the bar; past its width the row scrolls, with chevrons.
          <Scroller
            label={defaultCopy.chat.suggestions}
            className="mx-auto w-full max-w-4xl px-4 sm:px-5"
          >
            {starters.map((st) => (
              <li key={`${st.agent}:${st.label}`} className="shrink-0 snap-start">
                <button
                  type="button"
                  onClick={() => {
                    // The same rule as the + menu: a full selection says so, and All of Them gives way past three.
                    const next = withAgent(selection, st.agent);
                    if (!next) return setProblem(defaultCopy.chat.maxAgents(MAX_SELECTED));
                    setSelection(next);
                    setSeed({ text: st.text });
                  }}
                  className="inline-flex min-h-10 items-center gap-2 whitespace-nowrap rounded-aui border border-aui-border px-3 py-2 text-left text-aui-xs text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <AgentMark name={st.agent} size={14} />
                  <span>{st.label}</span>
                </button>
              </li>
            ))}
          </Scroller>
        )}
      </HomePage>
    );
  }

  return (
    <section className="flex min-h-0 flex-1">
      <ConversationPage
        title={summary?.title ?? path[0]?.text ?? "Conversation"}
        project={
          projects?.find((p) => p.name === where)?.title ||
          (where === personalProject ? defaultCopy.projects.personalTitle : where) ||
          workspaceName ||
          defaultCopy.projects.workspace
        }
        private={room?.thread.visibility === "private"}
        agents={summary?.agents ?? [...new Set(turns.map((t) => t.agent))]}
        people={room?.thread.participants?.filter((p) => p.kind === "person").map((p) => p.name)}
        onPanel={(next) => openPanel(next)}
        onArtifact={(id) => openPanel("deliverables", { artifact: id })}
      >
        {room && canRun && !room.thread.read_only && (
          <ConversationForkPanels thread={room.thread} runs={turns} actions={forkActions} />
        )}
        <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
          <Thread
            className="mx-auto w-full max-w-4xl flex-1 px-4"
            busy={tailWorking}
            label="Conversation"
          >
            {path.map((run, i) => (
              <TurnView
                key={run.id}
                run={run}
                canStop={canRun && !room?.thread.read_only}
                actions={
                  !room?.thread.read_only && run.status === "completed" ? (
                    <>
                      <OutcomeActions run={run} canWrite={canLearn} />
                      {canRun && forkActions?.onFork && (
                        <button
                          type="button"
                          className={messageActionButton}
                          aria-label={defaultCopy.forking.fromHere}
                          title={defaultCopy.forking.fromHere}
                          onClick={() => forkActions.onFork?.(run.id)}
                        >
                          <ForkIcon size={16} />
                        </button>
                      )}
                      {canRun && room?.thread.fork && forkActions?.onBringBack && (
                        <button
                          type="button"
                          className={messageActionButton}
                          aria-label={defaultCopy.forking.bringBack}
                          title={defaultCopy.forking.bringBack}
                          onClick={() => forkActions.onBringBack?.(run.id)}
                        >
                          <BringBackIcon size={16} />
                        </button>
                      )}
                    </>
                  ) : undefined
                }
                me={me}
                ask={asks[run.id]}
                siblings={siblingsOf(turns, run)}
                live={
                  i === path.length - 1 || (run.status !== "completed" && run.status !== "failed")
                }
                onSwitch={(id) => setChosen((c) => ({ ...c, [run.prev ?? ""]: id }))}
                onAsk={
                  canRun ? () => setSeed({ text: `@${run.agent} `, mode: "prepend" }) : undefined
                }
                onReply={
                  canRun
                    ? () =>
                        setSeed({
                          text: `@${run.agent} following up on [this answer](${href({ page: "conversation", id: threadId, panel: "activity", run: run.id })}): `,
                          mode: "prepend",
                        })
                    : undefined
                }
                onRedirect={
                  canRun
                    ? () =>
                        setSeed({
                          text: defaultCopy.conversation.redirectPrompt(
                            run.agent,
                            href({
                              page: "conversation",
                              id: threadId,
                              panel: "activity",
                              run: run.id,
                            }),
                          ),
                          mode: "prepend",
                        })
                    : undefined
                }
              />
            ))}
          </Thread>
        </WebSurface>
        {composer}
      </ConversationPage>
      {room && (
        <ConversationInfo
          panel={shownView.panel}
          contextTab={shownView.contextTab}
          onContextTab={(contextTab) => openPanel("context", { contextTab })}
          thread={room.thread}
          project={
            projects?.find((project) => project.name === where)?.title ||
            (where === personalProject ? defaultCopy.projects.personalTitle : where) ||
            workspaceName ||
            defaultCopy.projects.workspace
          }
          onClose={() => openPanel()}
        />
      )}
      <Presence when={shownView.panel === "deliverables"} ms={220}>
        {(leaving) => (
          <Deliverables
            open={!leaving}
            promotion={
              artifactActions
                ? {
                    project: room?.thread.project ?? where ?? "",
                    shared: room?.thread.visibility === "shared",
                    ...artifactActions,
                  }
                : undefined
            }
            thread={threadId}
            view={shownView}
            onView={(next) => openPanel("deliverables", next)}
            onClose={() => openPanel()}
            onRevise={
              canRun
                ? (file, slide) => {
                    setSeed({ text: revisionPrompt(file, slide), mode: "prepend" });
                    openPanel();
                  }
                : undefined
            }
          />
        )}
      </Presence>
      <Presence when={activity !== null} ms={220}>
        {(leaving) => (
          <Activity
            leaving={leaving}
            turns={path}
            stamp={turns.map((t) => `${t.id}:${t.calls}:${t.status}`).join("|")}
            focus={(activity ?? lastActivity.current)?.focus ?? null}
            onClose={() => openPanel()}
            onFocus={(run) => openPanel("activity", run ? { run } : {})}
          />
        )}
      </Presence>
    </section>
  );
}

/**
 * A conversation while it is read: its header, with the title the sidebar
 * already knows, the column its messages will fill and the composer's place,
 * so nothing moves when it lands.
 */
function ConversationLoading({
  title,
  busy,
  children,
}: {
  title?: string;
  busy: boolean;
  children: ReactNode;
}) {
  return (
    <section aria-label="Conversation" aria-busy={busy} className="flex min-h-0 min-w-0 flex-1">
      <Page
        layout="conversation"
        title={
          title ?? (
            <span aria-hidden className="aui-skeleton aui-skeleton-delay inline-block h-4 w-48" />
          )
        }
      >
        <div className="mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col px-4 py-4">
          {children}
        </div>
        <div className="mx-auto w-full max-w-4xl px-4 pb-4">
          <div
            aria-hidden
            className="aui-skeleton-delay h-[var(--humagen-composer-h)] rounded-aui-lg border border-aui-border"
          />
        </div>
      </Page>
    </section>
  );
}

/** Messages that stand in for a conversation's: a mark, a name and lines of text. */
function ConversationSkeleton() {
  return (
    <div role="status" data-testid="skeleton" className="aui-skeleton-delay flex flex-col gap-7">
      <span className="sr-only">Loading</span>
      {[
        { key: "first", lines: 3 },
        { key: "second", lines: 2 },
        { key: "third", lines: 4 },
      ].map(({ key, lines }) => (
        <div key={key} aria-hidden className="flex gap-3">
          <span className="aui-skeleton size-7 shrink-0 rounded-full" />
          <div className="flex min-w-0 flex-1 flex-col gap-2 pt-1">
            <span className="aui-skeleton block h-3 w-28" />
            {Array.from({ length: lines }, (_, j) => `${key}-${j}`).map((line, j) => (
              <span
                key={line}
                className="aui-skeleton block h-3"
                style={{ width: `${j === lines - 1 ? 48 : 92 - j * 6}%` }}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
