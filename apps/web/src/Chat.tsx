import { answerOfAction, answers } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import { WebSurface } from "@humagen/render-web";
import { AgentMessage, messageTextActionButton, Thread, useQueue } from "@humagen/web";
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Activity } from "./Activity.tsx";
import { useRunStop } from "./RunStop.tsx";
import { MessageTime } from "./MessageTime.tsx";
import { actedFor, citationOf, turnOf, type Run } from "./adapter.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { OutcomeActions } from "./OutcomeActions.tsx";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { ConversationForkPanels,type ConversationForkActions } from "./ConversationForkPanels.tsx";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { href, type ConversationPanel, type ConversationView } from "./route.ts";
import { ApiError, api, coworking, type ProjectView, type Revision, type Thread as ThreadRecord, type ThreadAsk, type ThreadPage, type ThreadSummary } from "./api.ts";
import { askedOf, copyFor } from "./asks.ts";
import { useCompletion } from "./completion.ts";
import { useThreadSelection } from "./selection.ts";
import { HomePage, type HomeAnnouncement } from "./HomePage.tsx";
import { emptySelection, MAX_SELECTED, PlusMenu, withAgent, placeholderFor, type Selection, SelectionChips } from "./Plus.tsx";
import { postedLine, ThreadRoom, topicsOf } from "./Room.tsx";
import { rememberSaid } from "./conflicts.ts";
import { useRunTurn } from "./Runs.tsx";
import { itemActions } from "./itemActions.tsx";
import { readFromStart } from "./turns.ts";
import { AgentMark, AgentTint, cause, isWorking, Page, Presence, Problem, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.threads;

/** The thread a page holds before one is read: nothing selected, nowhere. */
const noThread: ThreadRecord = { id: "", project: "", channel: "general", visibility: "private", participants: [], version: 0, created_by: "", created_at: "", updated_at: "" };
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
  let prev = turns.some((t) => (t.prev ?? "") === "") ? "" : (turns.find((t) => !read.has(t.prev ?? ""))?.prev ?? "");
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
  if (person || !run.actor || run.actor.bot || run.actor.user || run.origin !== "api") return person;
  const id = run.actor.id;
  const tag = run.tag_id ?? "";
  if (!tag.startsWith("api-r-") && !tag.startsWith(`api-${new TextEncoder().encode(id).length}-${id}-`)) return "";
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
  if (person) return `This turn acted for ${run.actor?.display || "someone else"}, so only they can retry it.`;
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
  actions?:ReactNode;
}) {
  const { run, control } = useRunStop(originalRun, canStop);
  const turn = useRunTurn(run);
  const index = siblings.findIndex((s) => s.id === run.id);
  const ended = run.status === "completed" || run.status === "failed";
  const refused = notAskable(run, me, siblings);
  const drawn = useMemo(
    () =>
      {
      const base = turn ?? turnOf(run);
      return run.citations !== undefined ? {...base,citations:run.citations.map(citationOf)} : base;
    },
    [turn, run],
  );
  // Neither Retry nor Regenerate is offered on a turn the reader may not ask
  // again (notAskable). A failure says why instead of offering Retry.
  const shown = useMemo(() => {
    const asked = askedOf(drawn, ask);
    return refused && asked.failure ? { ...asked, failure: { ...asked.failure, retryNotOffered: refused } } : asked;
  }, [drawn, refused, ask]);

  return (
    <article id={`turn-${run.id}`} className="flex flex-col gap-3" aria-label={`Turn answered by ${run.agent}`}>
      <div className="flex flex-col items-end gap-1">
        <p className="humagen-glass max-w-[80%] whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">{run.text}</p>
        <p className="flex flex-wrap items-center justify-end gap-x-2 text-aui-2xs text-aui-muted">
          <span className="inline-flex items-center gap-1">
            <AgentMark name={run.agent} size={14} />
            {run.agent}
          </span>
          {run.picked && <span>{run.picked}</span>}
        </p>
      </div>

      <AgentTint name={run.agent}>
        <AgentMessage actions={<>{control}{actions}</>} timestamp={<MessageTime at={run.created_at} />} turn={shown} copy={copyFor(ask)} itemActions={itemActions(run)} capabilities={{ regenerate: !refused }} onAskAgent={onAsk} onReply={onReply} onRedirect={onRedirect} />
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
export const STARTERS: { agent: string; label: string; text: string }[] = [
  { agent: "humagen-analyst", label: "How are my agents doing?", text: "How did my agents do today?" },
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
  plus?: (available: { name: string; description?: string }[] | null) => NonNullable<Parameters<typeof Composer>[0]["plus"]>;
}) {
  const tags = useCompletion(project, agents, homes, topicsOf(projects ?? [], root, project), me, handle);
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per list read
  useEffect(() => {
    if (tags.agents) onAvailable?.(tags.agents);
  }, [tags.agents]);
  return <Composer {...rest} tags={tags} plus={plus?.(tags.agents)} />;
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
  homes = {},
  projects,
  canRun,
  artifactActions,
  canLearn=false,
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
}: {
  /** Quiet lines under the prompt on Home: what needs the person, and what is new. */
  home?: ReactNode;
  banner?:HomeAnnouncement;
  findHelp?:ReactNode;
  personalProject?:string;
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
  /** Where each agent that lives in a project lives, so completion offers only what may answer here. */
  homes?: Record<string, string>;
  /** The project tree a new conversation can be filed in, or null while it loads. */
  projects: ProjectView[] | null;
  canRun: boolean;
  canLearn?:boolean;
  artifactActions?: {knowledge:boolean;memory:boolean};
  forkActions?:ConversationForkActions;
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
  const [seed, setSeed] = useState<ComposerSeed | undefined>(() => (startAsk ? { text: startAsk } : undefined));
  // What the + menu selected for a new conversation: an agent a link named, and where it lives.
  const [selection, setSelection] = useState<Selection>(() => ({
    ...emptySelection,
    agents: startAgent ? [startAgent] : [],
    project: startIn ?? personalProject ?? null,
    channel: startChannel || "general",
  }));

 useEffect(()=>{
  if(!threadId&&startIn===undefined&&personalProject!==undefined)setSelection(current=>current.project===null?{...current,project:personalProject}:current);
 },[threadId,startIn,personalProject]);
  const project = selection.project ?? personalProject ?? null;
  const select=(next:Selection)=>setSelection(next.project===null && personalProject!==undefined?{...next,project:personalProject,channel:"general"}:next);
  const [draft, setDraft] = useState("");
  // The agents that can answer in the picked project, once read.
  const [offered, setOffered] = useState<Set<string> | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  // The drawer of every run in the conversation, open on one of them or on the list.
  const [activity, setActivity] = useState<{ focus: string | null } | null>(null);
  const [localView, setLocalView] = useState<ConversationView>({});
  const shownView = onView ? view ?? {} : localView;
  const openPanel = (panel?: ConversationPanel, selection: Omit<ConversationView, "panel"> = {}) => {
    const next = { ...(panel==="activity"&&shownView.panel==="activity"?{filter:shownView.filter,showTools:shownView.showTools}:{}),...selection, panel };
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
  const [read, setRead] = useState<"reading" | "read" | "missing" | "failed">(threadId ? "reading" : "read");
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
  const asks = useMemo(() => Object.fromEntries((room?.turns ?? []).flatMap((t) => (t.run && t.ask ? [[t.run.id, t.ask]] : []))), [room]);
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
      const run = await api.createRun(to, text, after ? { after } : filed!==null ? { project: filed } : {});
      setTurns((t) => (t.some((x) => x.id === run.id) ? t : [...t, run]));
      setChosen((c) => ({ ...c, [run.prev ?? ""]: run.id }));
      onChanged();
      if (!threadId) onThread(run.thread);
      return run;
    },
    [threadId, project, onThread, onChanged],
  );

  const queue = useQueue({
    onStart: (m) => {
      setProblem(null);
      const done = () => {
        onChanged();
        queue.turnEnded("done");
      };
      const failed = (e: unknown) => {
        setProblem(e instanceof ApiError ? e.message : words.failed.post);
        queue.reject();
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
            ...(selection.agents.length > 0 ? { agents: selection.agents, mode: selection.mode } : {}),
          })
          .then((p) => {
            // What the first message did is said in the conversation it opens.
            rememberSaid(p.thread.id, postedLine(p));
            onThread(p.thread.id);
            done();
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
            r ? { ...r, turns: [...r.turns, { kind: "message", message: p.message }, ...p.runs.map((run) => ({ kind: "run" as const, run }))] } : r,
          );
          done();
        })
        .catch(failed);
    },
    // Stopping a run in flight is not built: the queue waits for it to end.
    onInterrupt: () => {},
  });

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
      api.answer(answer).catch((e) =>
        setProblem(e instanceof ApiError && e.status === 403 ? defaultCopy.noPermission : e instanceof ApiError ? e.message : defaultCopy.failure.internal()),
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
  const starters = STARTERS.filter((st) => agents.some((a) => a.spec.name === st.agent) && (offered === null || offered.has(st.agent)));

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
        placeholder={!threadId ? (startChannel ? words.startThread : placeholderFor(selection)) : runs.selection.agents.length > 0 ? placeholderFor(runs.selection) : words.message}
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
                const named = startAgent && !startIn && project === null ? homes[startAgent] : undefined;
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
              (available) => ({
                menu: (close, attach, leave) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={available}
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
                chips: <SelectionChips personalProject={personalProject} selection={runs.selection} projects={projects} workspace={workspaceName} onChange={runs.select} projectFixed />,
              })
            : (available) => ({
                menu: (close, attach, leave) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={available}
                    selection={selection}
                    onChange={select}
                    onAttach={attach}
                    onClose={close}
                    projects={projects}
                    personalProject={personalProject}
                    kinds={root?.kinds}
                    canCreateProject={canCreateProject}
                    onProjectCreated={onProjectCreated}
                  />
                ),
                chips: <SelectionChips personalProject={personalProject} selection={selection} projects={projects} workspace={workspaceName} onChange={select} projectFixed={selection.project===personalProject} />,
              })
        }
      />
    </div>
  );

  // A thread people have posted in is a room: their messages and the agents' answers, side by side.
  if (threadId && room && room.thread.id === threadId && (room.thread.read_only || room.turns.some((t) => t.message))) {
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
      <section aria-label="Conversation" aria-busy={read === "reading"} className="mx-auto flex w-full max-w-4xl flex-col gap-3 px-4 pt-4">
        {read === "failed" ? <Problem message={defaultCopy.threads.readFailed} /> : <SkeletonRows count={3} height="2.5rem" />}
      </section>
    );
  }

  if (!threadId || path.length === 0) {
    return (
      <HomePage banner={banner} greeting={greeting} canRun={canRun} hasAgents={agents.length > 0} below={<>{findHelp}{home}</>}>
        {composer}
        {/*
          Suggestions sit with the chat bar, as quiet text under it, until
          the person types. A pick writes its words and adds its agent to
          the conversation, as a chip, not an @mention.
        */}
        {canRun && starters.length > 0 && draft.trim() === "" && (
          // One line, never one left alone on a line of its own: a phone
          // shows the first two, which fit inside its gutter, and a wider
          // screen all of them.
          <ul aria-label={defaultCopy.chat.suggestions} className="mx-auto -mt-1 flex w-full max-w-4xl flex-nowrap gap-x-4 overflow-hidden px-7">
            {starters.map((st, i) => (
              <li key={st.agent} className={`shrink-0 ${i >= 2 ? "hidden sm:block" : ""}`}>
                <button
                  type="button"
                  onClick={() => {
                    // The same rule as the + menu: a full selection says so, and All of Them gives way past three.
                    const next = withAgent(selection, st.agent);
                    if (!next) return setProblem(defaultCopy.chat.maxAgents(MAX_SELECTED));
                    setSelection(next);
                    setSeed({ text: st.text });
                  }}
                  className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-aui py-1 text-aui-sm text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <AgentMark name={st.agent} size={12} />
                  <span>{st.label}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </HomePage>
    );
  }


  return (
    <section className="flex min-h-0 flex-1">
      <ConversationPage
        title={summary?.title ?? path[0]?.text ?? "Conversation"}
        project={projects?.find((p) => p.name === where)?.title || (where===personalProject?defaultCopy.projects.personalTitle:where) || workspaceName || defaultCopy.projects.workspace}
        private={room?.thread.visibility === "private"}
        agents={summary?.agents ?? [...new Set(turns.map((t) => t.agent))]}
        onPanel={(next) => openPanel(next)}
        onArtifact={(id) => openPanel("deliverables", { artifact: id })}
      >
      {room&&canRun&&!room.thread.read_only&&<ConversationForkPanels thread={room.thread} runs={turns} actions={forkActions}/>}
      <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
        <Thread className="mx-auto w-full max-w-4xl flex-1 px-4" busy={tailWorking} label="Conversation">
          {path.map((run, i) => (
            <TurnView
              key={run.id}
              run={run}
              canStop={canRun&&!room?.thread.read_only}
              actions={!room?.thread.read_only&&run.status==="completed"?<><OutcomeActions run={run} canWrite={canLearn}/>{canRun&&forkActions?.onFork&&<button type="button" className={messageTextActionButton} onClick={()=>forkActions.onFork?.(run.id)}>{defaultCopy.forking.fromHere}</button>}{canRun&&room?.thread.fork&&forkActions?.onBringBack&&<button type="button" className={messageTextActionButton} onClick={()=>forkActions.onBringBack?.(run.id)}>{defaultCopy.forking.bringBack}</button>}</>:undefined}
              me={me}
              ask={asks[run.id]}
              siblings={siblingsOf(turns, run)}
              live={i === path.length - 1 || (run.status !== "completed" && run.status !== "failed")}
              onSwitch={(id) => setChosen((c) => ({ ...c, [run.prev ?? ""]: id }))}
              onAsk={canRun ? () => setSeed({ text: `@${run.agent} `, mode: "prepend" }) : undefined}
              onReply={canRun ? () => setSeed({ text: `@${run.agent} following up on [this answer](${href({ page: "conversation", id: threadId, panel: "activity", run: run.id })}): `, mode: "prepend" }) : undefined}
              onRedirect={canRun ? () => setSeed({text:defaultCopy.conversation.redirectPrompt(run.agent,href({page:"conversation",id:threadId,panel:"activity",run:run.id})),mode:"prepend"}) : undefined}
            />
          ))}
        </Thread>
      </WebSurface>
      {composer}
      </ConversationPage>
      {room && <ConversationInfo panel={shownView.panel} thread={room.thread} project={projects?.find(project=>project.name===where)?.title || (where===personalProject?defaultCopy.projects.personalTitle:where) || workspaceName || defaultCopy.projects.workspace} onClose={() => openPanel()} />}
      {shownView.panel === "deliverables" && <Deliverables promotion={artifactActions ? {project:room?.thread.project??where??"",shared:room?.thread.visibility==="shared",...artifactActions}:undefined} thread={threadId} view={shownView} onView={(next) => openPanel("deliverables", next)} onClose={() => openPanel()} onRevise={canRun ? (file, slide) => { setSeed({ text: revisionPrompt(file, slide), mode: "prepend" }); openPanel(); } : undefined} />}
      <Presence when={activity !== null} ms={180}>
        {(leaving) => (
          <Activity
            filter={shownView.filter} showTools={shownView.showTools} onFilterChange={filter=>openPanel("activity",{run:shownView.run,filter})} onShowToolsChange={showTools=>openPanel("activity",{run:shownView.run,showTools})} isDecision={run=>run.outcome?.kind==="decision"||!!run.outcome?.decisions?.length}
            leaving={leaving}
            turns={path}
            stamp={turns.map((t) => `${t.id}:${t.calls}:${t.status}`).join("|")}
            focus={(activity ?? lastActivity.current)?.focus ?? null}
            onClose={() => openPanel()}
            onFocus={(run) => openPanel("activity", run ? { run } : {})}
            onJump={(id) => document.getElementById(`turn-${id}`)?.scrollIntoView({ block: "start", behavior: "smooth" })}
          />
        )}
      </Presence>
    </section>
  );
}
