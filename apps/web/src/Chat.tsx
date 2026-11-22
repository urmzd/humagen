import { answerOfAction, answers } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import { ActionId, type QueuedMessage, type UiAction } from "@humagen/core";
import { railEntry, useQueue } from "@humagen/web";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { type ApprovalMode, ApprovalToggle } from "./ApprovalToggle.tsx";
import { actedFor, type Run, turnOf } from "./adapter.ts";
import {
  ApiError,
  api,
  coworking,
  type ProjectView,
  type Revision,
  type ThreadPage,
  type ThreadSummary,
} from "./api.ts";
import { asksOf } from "./asks.ts";
import { aiNoticeHeight, Composer, type ComposerSeed } from "./Composer.tsx";
import type { ConversationForkActions } from "./ConversationForkPanels.tsx";
import { type Completion, useCompletion } from "./completion.ts";
import { rememberDock, useDockArrival } from "./dockArrival.ts";
import { FindHelp, type HelpSuggestion } from "./FindHelp.tsx";
import { type HomeAnnouncement, HomePage } from "./HomePage.tsx";
import { LazyFrame } from "./lazy.tsx";
import { ProjectPicker } from "./ProjectPicker.tsx";
import { PlusMenu, RunsConversation, SessionSetupPickers, ThreadRoom } from "./pages.ts";
import { emptySelection, mentionPicks, type Selection } from "./plusSelection.ts";
import { type ConversationPanel, type ConversationView, href } from "./route.ts";
import { Scroller } from "./Scroller.tsx";
import { ComposerFooter } from "./SessionUsage.tsx";
import { DEFAULT_SETUP, type SessionSetup, useSessionOptions } from "./sessionOptions.ts";
import { mentionsIn } from "./tags.ts";
import { topicsOf } from "./topics.ts";
import { readFromStart } from "./turns.ts";
import { AgentMark, isWorking, Page, Problem, textAction } from "./ui.tsx";
import { useVoiceNotes } from "./useVoiceNotes.ts";

const words = defaultCopy.threads;

/** The answers to one point in the conversation, oldest first. */
export function siblingsOf(turns: Run[], run: Run): Run[] {
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
  { agent: "humagen-writer", label: "Write a report", text: "Write a one-page report on " },
  { agent: "code-reviewer", label: "Review a pull request", text: "Review the pull request " },
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
  agentsRead,
  ...rest
}: Omit<Parameters<typeof Composer>[0], "tags" | "plus"> & {
  /** Called with the agents that may answer in the project, once they are read. */
  onAvailable?: (available: { name: string }[]) => void;
  project: string;
  agents: Revision[];
  /** Whether the workspace's agents are read. The bar is drawn before, and the project's list read beside them. */
  agentsRead: boolean;
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
    agentsRead,
  );
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per list read
  useEffect(() => {
    if (tags.agents) onAvailable?.(tags.agents);
  }, [tags.agents]);
  const voice = useVoiceNotes();
  return <Composer {...rest} tags={tags} plus={plus?.(tags)} voice={voice} />;
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
  composerOnly = false,
  placeholder: placeholderFor,
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
  agentsFailed = false,
  onRetryAgents,
  homes = {},
  projects,
  canRun,
  artifactActions,
  canLearn = false,
  alertAccess,
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
  /**
   * Only the composer that starts a session, as Home draws it but without its
   * greeting, suggestions or lists: a project's page starts sessions with it.
   */
  composerOnly?: boolean;
  /** What the empty composer says, over Home's own words. */
  placeholder?: string;
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
  /**
   * Whether the agents have been read. Until they are, the chat bar is drawn
   * and takes typing; a message sent waits for them, and @ says they are loading.
   */
  agentsRead?: boolean;
  /** The agents could not be read: the bar says so, keeps the draft, and offers Retry. */
  agentsFailed?: boolean;
  onRetryAgents?: () => void;
  /** Where each agent that lives in a project lives, so completion offers only what may answer here. */
  homes?: Record<string, string>;
  /** The project tree a new conversation can be filed in, or null while it loads. */
  projects: ProjectView[] | null;
  canRun: boolean;
  canLearn?: boolean;
  alertAccess?: { write: boolean };
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
  const [chosen, setChosen] = useState<Record<string, string>>({});
  // An agent a link named is mentioned at the start of the draft, ahead of what it asks.
  // The end of the conversation, where messages that wait are shown.
  const [queuePlace, setQueuePlace] = useState<HTMLElement | null>(null);
  const [seed, setSeed] = useState<ComposerSeed | undefined>(() =>
    startAgent || startAsk
      ? { text: `${startAgent ? `@${startAgent} ` : ""}${startAsk ?? ""}` }
      : undefined,
  );
  // Where the + menu files a new conversation.
  const [selection, setSelection] = useState<Selection>(() => ({
    ...emptySelection,
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
  // Read when the project's agents arrive, not drawn: kept out of state so a
  // key typed does not render the conversation again.
  const draft = useRef("");
  // Home's bar, remembered as it leaves for the conversation its first message opens.
  const bar = useRef<HTMLDivElement>(null);
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
    (threadId.startsWith("s_")
      ? coworking.thread(threadId, undefined, focus).catch((error) => {
          if (focus && error instanceof ApiError && error.status === 404) {
            if (live)
              setProblem("That message is unavailable. Showing the latest session messages.");
            return coworking.thread(threadId);
          }
          throw error;
        })
      : readFromStart((before) => coworking.thread(threadId, before))
    )
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

  // A conversation's room is read beside its messages, and a conversation of
  // runs becomes a room with its next message, so the room is on hand by then.
  useEffect(() => {
    if (threadId) void ThreadRoom.preload();
  }, [threadId]);

  // A different conversation starts from its newest branch.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset only when the conversation changes
  useEffect(() => {
    setChosen({});
    setProblem(null);
  }, [threadId]);

  const path = useMemo(() => pathOf(turns, chosen), [turns, chosen]);
  // The conversation's wave: a tick per turn, as long as the answer is, previewing the prompt that asked it.
  const wave = useMemo(
    () =>
      path.map((run) => ({
        ...railEntry(turnOf(run), run.created_at),
        preview: run.text.replace(/\s+/g, " ").trim(),
      })),
    [path],
  );
  // What each parked run asks, as the reader stands to it.
  const asks = useMemo(
    () =>
      Object.fromEntries(
        (room?.turns ?? []).flatMap((t) =>
          t.run && asksOf(t).length ? [[t.run.id, asksOf(t)]] : [],
        ),
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
  // A message not sent goes back to the person: in the box on Home, where
  // the first message opens the conversation and nothing may queue behind
  // it, and held in the queue in a conversation, to send again.
  const seedOf = (m: QueuedMessage): ComposerSeed => ({
    text: m.text,
    files: m.uploads?.map((f, i) => ({
      id: `retry-${i}`,
      name: f.name,
      text: "",
      data: f.data,
    })),
  });
  const giveBack = (m: QueuedMessage, why: string | null) => {
    setProblem(why);
    queue.reject();
    if (!threadId) {
      queue.clear();
      setSeed(seedOf(m));
    }
  };
  // The chat bar is drawn before the agents are read, so a message sent
  // then waits here, and goes once they arrive: the workspace's, and the
  // ones that may answer where it goes. It stays in the box, read-only,
  // so it is never out of sight; Cancel or Escape gives it back to edit.
  const agentsReady = agentsRead && !agentsFailed && (threadId !== null || offered !== null);
  const held = useRef<QueuedMessage | null>(null);
  const [holding, setHolding] = useState(false);
  const hold = (m: QueuedMessage | null) => {
    held.current = m;
    setHolding(m !== null);
  };
  // A held message let go unsent: its words are already in the box, now editable.
  const unhold = () => {
    hold(null);
    queue.reject();
    queue.clear();
  };
  const queue = useQueue({
    onStart: (m) => {
      if (agentsFailed) return giveBack(m, null);
      if (!agentsReady) {
        hold(m);
        // Sent, the box emptied: the words go back in it while they wait.
        setSeed(seedOf(m));
        return;
      }
      begin(m);
    },
    onInterrupt: () => {
      // Stopped before it was sent: the message goes back, never lost.
      if (held.current) return unhold();
      replying.current = { ...replying.current, stop: true };
      if (replying.current.ids.length > 0) void stopRuns(replying.current.ids);
    },
  });
  // biome-ignore lint/correctness/useExhaustiveDependencies: the held message is read, not watched
  useEffect(() => {
    const waiting = held.current;
    if (!waiting) return;
    // Failed, or no agent to answer it: the words stay in the box to edit.
    if (agentsFailed || (agentsReady && agents.length === 0)) unhold();
    else if (agentsReady) {
      hold(null);
      setSeed({ text: "", files: [] });
      begin(waiting);
    }
  }, [agentsReady, agentsFailed]);

  function begin(m: QueuedMessage) {
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
    const failed = (e: unknown) =>
      giveBack(m, e instanceof ApiError ? e.message : words.failed.post);
    // Home starts a private conversation in the picked project, or a
    // shared thread when it was opened from a channel. Either opens at
    // its address, where the next message goes.
    if (!threadId) {
      coworking
        .start({
          project: project ?? "",
          channel: selection.channel,
          text: m.text,
          uploads: m.uploads,
          visibility: startChannel ? "shared" : "private",
          ...(startChannel ? {} : setup),
        })
        .then((p) => {
          // Run, chosen before the session started, is applied to it now.
          if (pendingApproval !== "ask")
            void coworking.setApprovalMode(p.thread.id, pendingApproval).catch(() => undefined);
          // The bar and its footer move down into the conversation as one piece.
          rememberDock(bar.current?.querySelector(".humagen-composer-container") ?? null);
          onThread(p.thread.id);
          done(p.runs);
        })
        .catch(failed);
      return;
    }
    // A conversation of runs takes the message after the turn on screen.
    // Posted, it is a room: the message, and any run its tags started.
    coworking
      .post(threadId, { text: m.text, uploads: m.uploads, ...(tail ? { after: tail.id } : {}) })
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
  }

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
    if (a.id === ActionId.openRun)
      return openPanel("activity", { run: a.payload.target, step: a.payload.option });
    // A review's Ask Follow-up puts its words, tagging the agent, in the box. Nothing is sent.
    if (a.id === ActionId.reviewFollowUp) {
      if (canRun && a.payload.text) setSeed({ text: `${a.payload.text} `, mode: "prepend" });
      return;
    }
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

  // A session from Home is filed in the person's Personal project, and one
  // from a project's page in that project: the composer picks no project.
  // Its approval mode is held until the session starts.
  const [pendingApproval, setPendingApproval] = useState<ApprovalMode>("ask");
  // So are its image and repositories, sent with its first message: the
  // image beside the plus, the repositories as context from the plus menu.
  const [setup, setSetup] = useState<SessionSetup>(DEFAULT_SETUP);
  const sessionOptions = useSessionOptions(!startChannel);
  // A message here is its text alone: the agent it goes to is the one its
  // tags or the conversation name, never a pick the composer carries.
  const sending: typeof queue = {
    ...queue,
    send: (text, _pick, uploads) => queue.send(text, undefined, uploads),
    sendNow: (text, _pick, uploads) => queue.sendNow(text, undefined, uploads),
  };
  // Drawn with the page: only sending and mentions wait for the agents.
  const composer = canRun && (agents.length > 0 || !agentsRead || agentsFailed) && (
    <div
      ref={bar}
      className={
        composerOnly
          ? "flex w-full min-w-0 flex-col gap-2"
          : "mx-auto flex w-full max-w-4xl flex-col gap-2 px-4 pb-4"
      }
    >
      <Problem message={problem} />
      {agentsFailed && (
        <Problem
          message={defaultCopy.chat.agentsFailed}
          action={
            onRetryAgents && (
              <button type="button" className={textAction} onClick={onRetryAgents}>
                {defaultCopy.retry}
              </button>
            )
          }
        />
      )}
      {holding ? (
        <div className="flex items-center justify-between gap-3 px-3 text-aui-xs text-aui-muted">
          <p role="status" aria-label={defaultCopy.chat.gettingReady}>
            {defaultCopy.chat.gettingReady}
          </p>
          <button
            type="button"
            className={textAction}
            title={defaultCopy.chat.cancelHeldHint}
            onClick={unhold}
          >
            {defaultCopy.cancel}
          </button>
        </div>
      ) : (
        !agentsReady &&
        !agentsFailed && (
          <p role="status" aria-label={defaultCopy.chat.loadingAgents} className="sr-only">
            {defaultCopy.chat.loadingAgents}
          </p>
        )
      )}
      <ChatBar
        project={where}
        agents={agents}
        agentsRead={agentsRead && !agentsFailed}
        homes={homes}
        projects={projects}
        root={root ?? null}
        me={me}
        handle={handle}
        placeholder={
          placeholderFor ??
          (!threadId
            ? startChannel
              ? words.startThread
              : defaultCopy.chat.placeholder
            : words.message)
        }
        emptyText={placeholderFor}
        queue={sending}
        // In a conversation, messages that wait are shown at its end, not in the box.
        queuePlace={threadId && !composerOnly ? queuePlace : undefined}
        held={holding}
        onCancelHeld={unhold}
        autoFocus={!threadId}
        seed={seed}
        onDraft={(text) => {
          draft.current = text;
        }}
        // Under the bar, one quiet row: on the left where the conversation is
        // filed and its approval mode; the bar itself holds only +, the words,
        // Send and the microphone.
        footer={
          <ComposerFooter
            start={
              threadId ? (
                room?.thread && <ApprovalToggle thread={room.thread} me={handle || me} />
              ) : (
                <>
                  <ProjectPicker
                    projects={projects}
                    value={project}
                    personalProject={personalProject}
                    personalLabel={defaultCopy.noProject}
                    channel={selection.channel}
                    onChange={(name) => select({ ...selection, project: name, channel: "general" })}
                    onCreated={onProjectCreated}
                  />
                  {/* The image a new session runs in. */}
                  {!startChannel && <SessionSetupPickers value={setup} onChange={setSetup} />}
                  <ApprovalToggle
                    me={handle || me}
                    pending={pendingApproval}
                    onPending={setPendingApproval}
                  />
                </>
              )
            }
          />
        }
        // An agent the project picked does not offer is not kept for it.
        onAvailable={
          threadId
            ? undefined
            : (available) => {
                const names = new Set(available.map((a) => a.name));
                setOffered(names);
                // A mention of an agent that cannot answer here says so; its words stay the person's.
                const dropped = mentionsIn(
                  draft.current,
                  agents.map((a) => a.spec.name),
                ).filter((a) => !names.has(a));
                if (dropped.length > 0) setProblem(defaultCopy.chat.notHere(dropped));
              }
        }
        plus={
          threadId
            ? // A conversation of runs selects its agents as a room does, so a
              // follow-up goes to them; its project is fixed by its runs.
              ({ agents: available, skills, skillsFailed }) => ({
                menu: (close, attach, leave, draft) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={available}
                    {...mentionPicks(draft, available, setSeed)}
                    skills={skills}
                    skillsFailed={skillsFailed}
                    onSkill={(name) => {
                      setSeed({ text: `/${name} `, mode: "prepend" });
                    }}
                    onAttach={attach}
                    onClose={close}
                  />
                ),
              })
            : ({ agents: available, skills, skillsFailed }) => ({
                menu: (close, attach, leave, draft) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={available}
                    {...mentionPicks(draft, available, setSeed)}
                    skills={skills}
                    skillsFailed={skillsFailed}
                    onSkill={(name) => {
                      setSeed({ text: `/${name} `, mode: "prepend" });
                    }}
                    onAttach={attach}
                    onClose={close}
                    repos={
                      startChannel || sessionOptions === null
                        ? undefined
                        : {
                            options: sessionOptions?.repos ?? null,
                            value: setup.repos,
                            onChange: (repos) => setSetup({ ...setup, repos }),
                            connectHref: href({ page: "settings", section: "connections" }),
                          }
                    }
                    finder={
                      findHelp ? (
                        <FindHelp
                          initialDescription={draft}
                          onSearch={findHelp}
                          canChoose={canRun}
                          onChoose={(match, description) => {
                            // The match is mentioned: ahead of what was described, or where the cursor was.
                            setSeed(
                              draft.trim()
                                ? { mention: match.agent }
                                : { text: `@${match.agent} ${description}` },
                            );
                            close();
                          }}
                        />
                      ) : undefined
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
    (threadId.startsWith("s_") ||
      room.thread.read_only ||
      room.thread.fork ||
      room.thread.import ||
      room.turns.some((t) => t.message))
  ) {
    return (
      <LazyFrame
        resetKey={threadId}
        fallback={
          <ConversationLoading title={summary?.title} busy>
            <ConversationSkeleton />
          </ConversationLoading>
        }
      >
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
          alertAccess={alertAccess}
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
      </LazyFrame>
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

  if (!threadId && composerOnly) return composer || null;

  if (!threadId || path.length === 0) {
    return (
      <HomePage
        banner={banner}
        greeting={greeting}
        canRun={canRun}
        hasAgents={!agentsRead || agentsFailed || agents.length > 0}
        below={home}
      >
        {composer}
        {/*
          Suggestions stay with the chat bar while the person writes. A pick writes its words with its agent
          @mentioned at the start, the one way a message names who answers it.
        */}
        {canRun && starters.length > 0 && (
          // Centered under the bar; past its width the row scrolls, with chevrons.
          <Scroller
            label={defaultCopy.chat.suggestions}
            className="mx-auto w-full max-w-4xl px-4 sm:px-5"
          >
            {starters.map((st) => (
              <li key={`${st.agent}:${st.label}`} className="shrink-0">
                <button
                  type="button"
                  onClick={() => setSeed({ text: `@${st.agent} ${st.text}`, mode: "append" })}
                  className="inline-flex min-h-10 pointer-coarse:min-h-11 max-w-[calc(100vw-2rem)] items-center gap-2 whitespace-nowrap rounded-aui border border-aui-border px-3 py-2 text-left text-aui-xs text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <AgentMark name={st.agent} size={14} />
                  <span className="min-w-0 truncate">{st.label}</span>
                </button>
              </li>
            ))}
          </Scroller>
        )}
      </HomePage>
    );
  }

  return (
    <LazyFrame
      resetKey={threadId}
      fallback={
        <ConversationLoading title={summary?.title} busy>
          <ConversationSkeleton />
        </ConversationLoading>
      }
    >
      <RunsConversation
        threadId={threadId}
        summary={summary}
        path={path}
        turns={turns}
        room={room}
        projects={projects}
        where={where}
        personalProject={personalProject}
        workspaceName={workspaceName}
        shownView={shownView}
        openPanel={openPanel}
        canRun={canRun}
        canLearn={canLearn}
        me={me}
        forkActions={forkActions}
        artifactActions={artifactActions}
        onAction={onAction}
        tailWorking={tailWorking}
        wave={wave}
        asks={asks}
        setChosen={setChosen}
        setSeed={setSeed}
        composer={composer}
        onQueuePlace={setQueuePlace}
        activity={activity ?? lastActivity.current}
        activityOpen={activity !== null}
      />
    </LazyFrame>
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
        <DockPlaceholder />
      </Page>
    </section>
  );
}

/**
 * The composer's place while a conversation loads. Arriving from Home's first
 * message, it is Home's own bar and footer, drawn inert, sliding down to
 * here (dockArrival.ts); otherwise a quiet outline of the bar and its footer.
 */
function DockPlaceholder() {
  const dock = useRef<HTMLDivElement>(null);
  const picture = useRef<HTMLDivElement>(null);
  const html = useDockArrival(dock);
  useLayoutEffect(() => {
    if (html && picture.current) picture.current.innerHTML = html;
  }, [html]);
  return (
    <div
      ref={dock}
      data-layer="composer-placeholder"
      className="humagen-composer-placeholder mx-auto flex w-full max-w-4xl flex-col gap-1.5 px-4 pb-3"
    >
      {html ? (
        <div ref={picture} inert aria-hidden className="pointer-events-none" />
      ) : (
        <>
          <div
            aria-hidden
            className="aui-skeleton-delay h-[var(--humagen-composer-h)] rounded-[1.25rem] border border-aui-border"
          />
          <div aria-hidden className="h-7" />
          {/* The AI notice's line. */}
          <div aria-hidden data-layer="composer-placeholder-notice" className={aiNoticeHeight} />
        </>
      )}
    </div>
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
