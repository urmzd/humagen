import { answerOfAction, answers } from "@warp/adapter/question";
import { defaultCopy } from "@warp/components";
import { ActionId, type UiAction } from "@warp/core";
import { CheckIcon, ChevronDownIcon, WebSurface } from "@warp/render-web";
import { AgentMessage, Thread, useQueue } from "@warp/web";
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Activity } from "./Activity.tsx";
import { actedFor, type Run } from "./adapter.ts";
import { Composer } from "./Composer.tsx";
import { ApiError, api, type Choice, coworking, type ProjectAgentsView, type ProjectView, type Revision, type ThreadAsk, type ThreadPage, type ThreadSummary } from "./api.ts";
import { askedOf, copyFor } from "./asks.ts";
import { answersLine, AUTO, availableOf } from "./availability.ts";
import { HomePage } from "./HomePage.tsx";
import { ProjectPicker } from "./ProjectPicker.tsx";
import { ThreadRoom } from "./Room.tsx";
import { useRunTurn } from "./Runs.tsx";
import { itemActions } from "./itemActions.tsx";
import { SessionGaugeFor } from "./SessionGauge.tsx";
import { readFromStart } from "./turns.ts";
import { AgentMark, AgentTint, cause, isWorking, Page, Popover, Presence, Problem, SkeletonRows } from "./ui.tsx";

const auto = defaultCopy.auto;
// Controls in the dock are text, not boxes: they sit under the composer and
// should not compete with it.
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
 * record says. A turn recorded before warp said whom a run acts for names no
 * user, so one a person asked under their own name, from the console (an
 * `api-r-` id) or the API with their own key, is theirs, unless its words
 * were someone else's: an earlier turn's at the same point (siblings, oldest
 * first) asked by anyone else, which is what the console's Retry and
 * Regenerate asked then, under the presser's name. A machine asking for a
 * person (its own key) and a retry warp asked for nobody (`api-retry-`)
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
  run,
  me,
  siblings,
  live,
  ask,
  onSwitch,
}: {
  run: Run;
  /** The reader's subject. */
  me: string;
  siblings: Run[];
  /** The question the run waits on, as the reader stands to it: it is offered only when it is theirs. */
  ask?: ThreadAsk;
  /** Follow the run's events. Off for settled turns, whose record is enough. */
  live: boolean;
  onSwitch: (id: string) => void;
}) {
  const turn = useRunTurn(live ? run : undefined);
  const index = siblings.findIndex((s) => s.id === run.id);
  const ended = run.status === "completed" || run.status === "failed";
  const refused = notAskable(run, me, siblings);
  const drawn = useMemo(
    () =>
      turn ?? {
        id: run.id,
        agent: { name: run.agent },
        status: run.status === "completed" ? ("done" as const) : run.status === "failed" ? ("failed" as const) : ("working" as const),
        text: run.answer ?? "",
        tasks: [],
        citations: [],
        interrupts: [],
        notices: [],
        failure:
          run.status === "failed"
            ? { kind: "internal" as const, message: cause(run.error) || "The run failed.", reference: run.id }
            : undefined,
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
        <p className="warp-glass max-w-[80%] whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">{run.text}</p>
        <p className="flex flex-wrap items-center justify-end gap-x-2 text-aui-2xs text-aui-muted">
          <span className="inline-flex items-center gap-1">
            <AgentMark name={run.agent} size={14} />
            {run.agent}
          </span>
          {run.picked && <span>{run.picked}</span>}
        </p>
      </div>

      <AgentTint name={run.agent}>
        <AgentMessage turn={shown} copy={copyFor(ask)} itemActions={itemActions(run)} capabilities={{ regenerate: !refused }} />
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
 * One conversation: its turns along the branch on screen, and the box that
 * adds the next one. `threadId` null is a conversation not yet started.
 */
/** Ways into the platform's own agents, shown on an empty conversation when the workspace has them. */
export const STARTERS: { agent: string; label: string; text: string }[] = [
  { agent: "warp-analyst", label: "How did my agents do today?", text: "How did my agents do today?" },
  { agent: "warp-builder", label: "Build a new agent", text: "Help me build an agent that " },
  { agent: "warp-writer", label: "Write a report", text: "Write a one-page report on " },
];

export function Chat({
  threadId,
  me,
  startIn,
  startAgent,
  startAsk,
  startChannel,
  focus,
  root,
  workspaceName,
  onOpenChannel,
  summary,
  tick,
  agents,
  homes = {},
  projects,
  canRun,
  canCreateProject,
  greeting,
  onThread,
  onChanged,
  onProjectCreated,
  home,
}: {
  /** Quiet lines under the prompt on Home: what needs the person, and what is new. */
  home?: ReactNode;
  threadId: string | null;
  /** The reader's subject: Retry asks a turn again as them, so only on a turn that acted for them. */
  me: string;
  /** The project a new conversation starts in, when it was started from one. */
  startIn?: string;
  /** The agent a new conversation is for, and its first message, when a link or a starter picked them. */
  startAgent?: string;
  startAsk?: string;
  /** The channel of the project a new conversation starts in, as a shared thread. */
  startChannel?: string;
  /** A message of a shared thread to open at. */
  focus?: string;
  /** The workspace's own project, which heads the tree, and its name. */
  root?: ProjectView | null;
  workspaceName?: string;
  onOpenChannel?: (project: string, channel: string) => void;
  /** The conversation at a glance, once it exists. */
  summary: ThreadSummary | undefined;
  /** Changes whenever any run starts or ends. */
  tick: number;
  agents: Revision[];
  /** Where each agent that lives in a project lives, so the menu offers only what may answer here. */
  homes?: Record<string, string>;
  /** The project tree a new conversation can be filed in, or null while it loads. */
  projects: ProjectView[] | null;
  canRun: boolean;
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
  // Who the person chose: an agent, AUTO for @auto, or "" for whoever answers untagged messages here.
  const [agent, setAgent] = useState(startAgent ?? "");
  const [seed, setSeed] = useState<{ text: string } | undefined>(() => (startAsk ? { text: startAsk } : undefined));
  const [project, setProject] = useState<string | null>(startIn ?? null);
  const [preview, setPreview] = useState<Choice | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  // The drawer of every run in the conversation, open on one of them or on the list.
  const [activity, setActivity] = useState<{ focus: string | null } | null>(null);
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
  // What may answer where the next message goes, and who answers it untagged.
  const where = threadId ? (tail?.project ?? "") : (project ?? "");
  const [here, setHere] = useState<ProjectAgentsView | null>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    let live = true;
    api
      .projectAgents(where)
      .then((v) => live && setHere(v))
      .catch(() => live && setHere(null));
    return () => {
      live = false;
    };
  }, [where, tick]);
  const offered = useMemo(() => {
    const names = availableOf(
      agents.map((a) => a.spec.name),
      here,
      homes,
    );
    return agents.filter((a) => names.includes(a.spec.name));
  }, [agents, here, homes]);
  const fallback = here?.default_resolved?.agent ?? "";
  // A follow-up goes to the agent that took the last turn, unless the person
  // picks another, while it may still answer here. It is named only when it
  // is not who answers untagged anyway.
  const ran = threadId && tail && offered.some((a) => a.spec.name === tail.agent) && tail.agent !== fallback ? tail.agent : "";
  // The agent that answers: the one chosen, the one that ran, or the place's default.
  const answering = agent || ran || fallback;
  const isAuto = answering === AUTO;
  const named = isAuto ? "" : answering;
  // What is sent: "" leaves the agent out, so the platform's default chain decides, as the person did.
  const target = agent || ran;
  // Who answers the next message, in words under the composer, once it is known.
  const answersHere = agent || here ? answersLine(answering, isAuto ? preview?.agent : undefined) : "";

  // What this conversation's queue is waiting on, so it knows when to go on.
  const waiting = useRef<string | null>(null);

  // A turn that follows another joins its conversation, filed where that is.
  // One that starts a conversation is filed in `filed`: by default the project
  // picked for a new one.
  const post = useCallback(
    async (text: string, to: string, after: string | undefined, filed: string | null = project) => {
      setProblem(null);
      const run = await api.createRun(to, text, after ? { after } : filed ? { project: filed } : {});
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
      // Started in a channel, a conversation is a shared thread: the message
      // is the person's, and its tags say who answers.
      if (!threadId && startChannel) {
        setProblem(null);
        coworking
          .start({ project: project ?? "", channel: startChannel, text: m.text })
          .then((p) => {
            onChanged();
            onThread(p.thread.id);
            queue.turnEnded("done");
          })
          .catch((e) => {
            setProblem(e instanceof ApiError ? e.message : "The message could not be sent.");
            queue.turnEnded("failed");
          });
        return;
      }
      post(m.text, target, tail?.id)
        .then((run) => {
          waiting.current = run.id;
        })
        .catch((e) => {
          setProblem(e instanceof ApiError ? e.message : "The message could not be sent.");
          queue.turnEnded("failed");
        });
    },
    // Stopping a run in flight is not built: the queue waits for it to end.
    onInterrupt: () => {},
  });

  // When the run the queue started settles, the queue may send the next one.
  // biome-ignore lint/correctness/useExhaustiveDependencies: react to the watched run's status only
  useEffect(() => {
    const id = waiting.current;
    const run = id ? turns.find((t) => t.id === id) : undefined;
    if (!run || (run.status !== "completed" && run.status !== "failed")) return;
    waiting.current = null;
    queue.turnEnded(run.status === "completed" ? "done" : "failed");
  }, [turns]);

  // A branch of the first turn starts the conversation again, so it is filed
  // where the turn was: the picker's project is only for a new conversation.
  const branch = (run: Run, to: string) => {
    post(run.text, to, run.prev, run.project ?? null).catch((e) =>
      setProblem(e instanceof ApiError ? e.message : "The branch could not be started."),
    );
  };

  const onAction = (a: UiAction) => {
    // An agent drawn inside a turn opens the drawer on its run.
    if (a.id === ActionId.openRun) return setActivity({ focus: a.payload.target });
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

  // Show who @auto would send the next message to, before it is sent.
  const [draft, setDraft] = useState("");
  useEffect(() => {
    if (!isAuto || draft.trim().length < 3) return setPreview(null);
    const timer = window.setTimeout(() => {
      api
        .pick(draft, where)
        .then(setPreview)
        .catch(() => setPreview(null));
    }, 250);
    return () => window.clearTimeout(timer);
  }, [isAuto, draft, where]);

  const starters = STARTERS.filter((st) => agents.some((a) => a.spec.name === st.agent));

  const composer = canRun && agents.length > 0 && (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-2 px-4 pb-4">
      <Problem message={problem} />
      <Composer
        placeholder={isAuto ? "Ask anything" : named ? `Ask ${named}` : auto.tagToAsk}
        queue={queue}
        autoFocus={!threadId}
        onDraft={setDraft}
        seed={seed}
        leading={
          <>
        {threadId ? (
          <span className="max-w-40 truncate px-1.5 text-aui-sm text-aui-muted" title={tail?.project || "Projectless"}>{tail?.project || "Projectless"}</span>
        ) : (
          <ProjectPicker
            value={project}
            projects={projects}
            kinds={root?.kinds}
            canCreate={canCreateProject}
            onChange={setProject}
            onCreated={onProjectCreated}
          />
        )}
          </>
        }
        trailing={
          <>
        {/* The reason for a pick is one hover away, and announced, without taking a line. */}
        <span className="sr-only" aria-live="polite">
          {isAuto && preview ? `${preview.agent}. ${preview.reason}` : ""}
        </span>
        <Popover
          label={isAuto && preview ? `${auto.choose}. ${preview.reason}` : `${auto.choose}. ${answersLine(answering, preview?.agent)}`}
          align="right"
          up
          className="inline-flex h-7 items-center gap-1.5 rounded-aui px-1.5 text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          button={
            <>
              {(named || preview) && <AgentMark name={isAuto ? (preview?.agent ?? "?") : named} size={16} />}
              <span className="max-w-16 truncate sm:max-w-32">
                {isAuto ? (preview?.agent ?? auto.handle) : named || auto.choose}
              </span>
              {isAuto && preview && <span className="hidden text-aui-xs text-aui-muted sm:inline">{auto.handle}</span>}
              <ChevronDownIcon size={14} className="text-aui-muted" />
            </>
          }
        >
          {(close) => {
            // @auto is first when it is the default here, last otherwise.
            const autoRow = (
              <li key={AUTO}>
                <button
                  type="button"
                  aria-pressed={agent === AUTO}
                  onClick={() => {
                    setAgent(AUTO);
                    close();
                  }}
                  className="flex w-full items-center gap-2.5 rounded-aui px-2.5 py-2 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover"
                >
                  <span className="w-[18px]" aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2 text-aui-sm text-aui-fg">
                      {auto.handle}
                      {fallback === AUTO && (
                        <span className="rounded-full border border-aui-border px-1.5 text-aui-2xs text-aui-muted">{auto.defaultChip}</span>
                      )}
                    </span>
                    <span className="block truncate text-aui-xs text-aui-muted">{auto.description}</span>
                  </span>
                  {agent === AUTO && <CheckIcon size={14} className="text-aui-fg" />}
                </button>
              </li>
            );
            return (
              <div className="flex flex-col">
                <p className="px-2.5 py-2 text-aui-xs text-aui-muted">{answersLine(fallback)}</p>
                <div className="my-1 h-px bg-aui-border" />
                <ul className="aui-scroll max-h-72 overflow-y-auto" aria-label="Agents">
                  {fallback === AUTO && autoRow}
                  {offered.map((a) => {
                    const on = (agent || ran) === a.spec.name;
                    return (
                      <li key={a.spec.name}>
                        <button
                          type="button"
                          aria-pressed={on}
                          onClick={() => {
                            setAgent(a.spec.name);
                            close();
                          }}
                          className="flex w-full items-center gap-2.5 rounded-aui px-2.5 py-2 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover"
                        >
                          <AgentMark name={a.spec.name} size={18} />
                          <span className="min-w-0 flex-1">
                            <span className="flex items-center gap-2 text-aui-sm text-aui-fg">
                              {a.spec.name}
                              {a.spec.name === fallback && (
                                <span className="rounded-full border border-aui-border px-1.5 text-aui-2xs text-aui-muted">{auto.defaultChip}</span>
                              )}
                            </span>
                            <span className="block truncate text-aui-xs text-aui-muted">{a.spec.description || "No description"}</span>
                          </span>
                          {on && <CheckIcon size={14} className="text-aui-fg" />}
                        </button>
                      </li>
                    );
                  })}
                  {fallback !== AUTO && autoRow}
                </ul>
              </div>
            );
          }}
        </Popover>
          </>
        }
      />
      {answersHere && <p className="px-3 text-aui-xs text-aui-muted">{answersHere}</p>}
    </div>
  );

  // A thread people have posted in is a room: their messages and the agents' answers, side by side.
  if (threadId && room && room.thread.id === threadId && room.turns.some((t) => t.message)) {
    return (
      <ThreadRoom
        key={threadId}
        id={threadId}
        me={me}
        initial={room}
        tick={tick}
        agents={agents}
        homes={homes}
        projects={projects ?? []}
        root={root ?? null}
        workspaceName={workspaceName ?? ""}
        canRun={canRun}
        focus={focus}
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
      <HomePage greeting={greeting} canRun={canRun} hasAgents={agents.length > 0} below={home}>
        {composer}
        {canRun && starters.length > 0 && (
          <ul aria-label="Start with" className="mx-auto flex w-full max-w-4xl flex-wrap justify-center gap-2 px-4">
            {starters.map((st) => (
              <li key={st.agent}>
                <button
                  type="button"
                  onClick={() => {
                    setAgent(st.agent);
                    setSeed({ text: st.text });
                  }}
                  className="inline-flex h-9 items-center gap-2 rounded-full border border-aui-border px-3.5 text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
                >
                  <AgentMark name={st.agent} size={14} />
                  {st.label}
                </button>
              </li>
            ))}
          </ul>
        )}
      </HomePage>
    );
  }

  const running = turns.filter((t) => t.status !== "completed" && t.status !== "failed").length;

  return (
    <section className="flex min-h-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      {/* Arrives with the conversation: its name, then who has answered in it. */}
      {/*
        Above the thread, so a panel opened from it is not painted over by the first message.
        The arrival animation is on the title and the agents, not the header: an animated
        ancestor stops a glass panel from blurring the page behind it.
      */}
      {/* On a narrow screen the title has a line of its own, and the rest wraps under it. */}
      <header className="relative z-10 mx-auto flex w-full max-w-4xl flex-wrap items-center gap-x-4 gap-y-1 px-4 pt-4 pb-2">
        <h1 className="aui-fade-up w-full min-w-0 truncate text-aui-lg font-semibold text-aui-fg sm:w-auto sm:flex-1">{summary?.title ?? path[0]?.text}</h1>
        <ul className="aui-fade-up flex min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 text-aui-xs text-aui-muted sm:shrink-0" aria-label="Agents in this conversation">
          {(summary?.agents ?? []).map((a) => (
            <li key={a} className="inline-flex items-center gap-1">
              <AgentMark name={a} size={14} />
              {a}
            </li>
          ))}
          {tail?.project && <li>{tail.project}</li>}
        </ul>
        <SessionGaugeFor tail={tail} />
        <button
          type="button"
          aria-expanded={activity !== null}
          onClick={() => setActivity(activity ? null : { focus: null })}
          className="shrink-0 whitespace-nowrap rounded-aui px-2 py-1 text-aui-xs text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:text-aui-fg"
        >
          Execution history{running > 0 ? ` ${running}` : ""}
        </button>
      </header>
      <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
        <Thread className="mx-auto w-full max-w-4xl flex-1 px-4" busy={tailWorking} label="Conversation">
          {path.map((run, i) => (
            <TurnView
              key={run.id}
              run={run}
              me={me}
              ask={asks[run.id]}
              siblings={siblingsOf(turns, run)}
              live={i === path.length - 1 || (run.status !== "completed" && run.status !== "failed")}
              onSwitch={(id) => setChosen((c) => ({ ...c, [run.prev ?? ""]: id }))}
            />
          ))}
        </Thread>
      </WebSurface>
      {composer}
      </div>
      <Presence when={activity !== null} ms={180}>
        {(leaving) => (
          <Activity
            leaving={leaving}
            turns={path}
            stamp={turns.map((t) => `${t.id}:${t.calls}:${t.status}`).join("|")}
            focus={(activity ?? lastActivity.current)?.focus ?? null}
            onClose={() => setActivity(null)}
            onJump={(id) => document.getElementById(`turn-${id}`)?.scrollIntoView({ block: "start", behavior: "smooth" })}
          />
        )}
      </Presence>
    </section>
  );
}
