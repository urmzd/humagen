import { answerOfAction, answers as isAnswer } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import {
  ActionId,
  postWithHumanActivity,
  type QueuedMessage,
  splitHumanActivity,
  type UiAction,
} from "@humagen/core";
import { ForkIcon, GitHubRefKindsContext, githubRefKinds, WebSurface } from "@humagen/render-web";
import {
  AgentMessage,
  FileLinkContext,
  type FileLinks,
  MessageActionRow,
  messageActionButton,
  type RetryCall,
  RetryCallContext,
  type RetryCalls,
  railEntry,
  Thread as ThreadList,
  type TurnRailEntry,
  useQueue,
} from "@humagen/web";
import { Fragment, type ReactNode, useContext, useEffect, useMemo, useRef, useState } from "react";
import { ApprovalToggle } from "./ApprovalToggle.tsx";
import { actedFor, citationOf, type Run, turnOf } from "./adapter.ts";
import {
  ApiError,
  api,
  type BackgroundTask,
  type Channel,
  coworking,
  type ProjectView,
  type Revision,
  type ThreadAnnouncement,
  type ThreadAnswers,
  type ThreadAsk,
  type ThreadMention,
  type ThreadMessage,
  type ThreadPage,
  type ThreadPosted,
  type ThreadSummary,
  type ThreadTurn,
} from "./api.ts";
import { askedOf, asksOf, copyFor, type Refusal, stillAsks } from "./asks.ts";
import { answersLine } from "./availability.ts";
import { agentShown } from "./branding.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { type ConversationForkActions, ConversationForkPanels } from "./ConversationForkPanels.tsx";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { Reference } from "./cards.tsx";
import { type Completion, useCompletion } from "./completion.ts";
import { deriveTitle } from "./conversationList.ts";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { useDockArrival } from "./dockArrival.ts";
import { ForkContext, ImportContext } from "./ForkConversation.tsx";
import { GitHubTab, reviewText, reviewView } from "./GitHubTab.tsx";
import { HumanActivityList, HumanActivityNote } from "./humanActivity.tsx";
import { itemActions } from "./itemActions.tsx";
import { OLD_SYSTEM_SENDER } from "./legacy.ts";
import { AnswerRefs, CallThreads } from "./MessageRefs.tsx";
import { MessageTime } from "./MessageTime.tsx";
import { OutcomeActions } from "./OutcomeActions.tsx";
import { PersonMark } from "./PersonMark.tsx";
import { mentionPicks, PlusMenu } from "./Plus.tsx";
import { openPane } from "./pane.ts";
import { ConversationActivity, ResponseActivityPanel } from "./ResponseActivity.tsx";
import { RunFeedbackHost } from "./RunFeedback.tsx";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { traceSource } from "./responseSource.ts";
import { followRoom } from "./roomFollow.ts";
import {
  type ConversationPanel,
  type ConversationView,
  href,
  type WorkbenchTabId,
} from "./route.ts";
import { SessionActions } from "./SessionActions.tsx";
import {
  alertAnchor,
  duplicatesInlineApproval,
  SessionAlerts,
  useSessionAlerts,
} from "./SessionAlerts.tsx";
import { SessionDetailsProvider } from "./SessionDetails.tsx";
import { sessionFiles, sessionProgress } from "./SessionPanel.tsx";
import { OriginButton, referenceOfPlace, SessionPlace, usePlace } from "./SessionPlace.tsx";
import { ComposerFooter, UsageRing } from "./SessionUsage.tsx";
import {
  type SessionView,
  SessionViews,
  useUnavailableViews,
  useViewShortcuts,
} from "./SessionViews.tsx";
import { ShareDialog } from "./Share.tsx";
import { useItemStops, useSessionBackground, useSessionStop } from "./stops.tsx";
import { topicsOf } from "./topics.ts";
import { mergeTurns } from "./turns.ts";
import {
  AgentMark,
  AgentTint,
  ago,
  button,
  Checkpoint,
  field,
  isWorking,
  Label,
  Mentioned,
  Page,
  Presence,
  Problem,
  primary,
} from "./ui.tsx";
import { useVoiceNotes } from "./useVoiceNotes.ts";
import { SessionWorkbench } from "./Workbench.tsx";
import { BrowserCard } from "./WorkbenchBrowserCard.tsx";
import { Workspace } from "./Workspace.tsx";
import { openBench, WorkbenchOpen } from "./workbench.ts";

/**
 * Conversations, channels and shared threads: where people and agents work
 * together in a project. A person's message is posted as them and starts no
 * run of its own: an agent runs when the message tags it (`@name` or
 * `$skill`), or, when it tags none, when the agent decides to join.
 */

const words = defaultCopy.threads;

/**
 * Whether the reader posts in a project, as the server holds them to it: a
 * member or an owner of it. A viewer reads its threads and answers only what
 * they were asked, from the inbox. The workspace asks nothing more than
 * starting runs, and a project whose role is not known here is the
 * server's to decide.
 */
/**
 * The wave down a thread's left edge: a tick per message and per answer, as
 * long as what it says. A message previews its own words; an answer, its
 * first line, or what was asked when it has none yet.
 */
export function waveOf(turns: ThreadTurn[]): TurnRailEntry[] {
  return turns.flatMap((t): TurnRailEntry[] => {
    if (t.message) {
      const text = splitHumanActivity(t.message.text).text.replace(/\s+/g, " ").trim();
      return [
        {
          id: t.message.id,
          agent: t.message.name || t.message.by,
          kind: "person",
          preview: text,
          status: "done",
          weight: text.length + 40,
          at: t.message.at,
        },
      ];
    }
    if (t.run) {
      const entry = railEntry(turnOf(t.run), t.run.created_at);
      return [{ ...entry, preview: entry.preview || t.run.text.replace(/\s+/g, " ").trim() }];
    }
    return [];
  });
}

export function postsIn(projects: ProjectView[], name: string): boolean {
  if (name === "") return true;
  const role = projects.find((p) => p.name === name)?.access?.role;
  return role === undefined || role === "member" || role === "owner";
}

/** The title of a project, as a thread's meta line names it. */
function projectTitle(
  projects: ProjectView[],
  name: string,
  workspace: string,
  personalProject?: string,
): string {
  if (name === "") return workspace || defaultCopy.projects.workspace;
  const p = projects.find((x) => x.name === name);
  return p?.title || (name === personalProject ? defaultCopy.projects.personalTitle : name);
}

/** Who answers an untagged message in a channel, as its row in a list says it. */
function answersShort(a: ThreadAnswers | undefined): string {
  return answersLine(a?.agent || undefined);
}

/**
 * A refused tag as the message wrote it: `$review`, `@sme:billing`, or an
 * item of a mention kind with its kind (`@role:finance`, `@team:finance`),
 * read from the message's own words where they name it.
 */
function written(r: ThreadMention, text: string): string {
  if (r.kind === "sme") return `@sme${r.name ? `:${r.name}` : ""}`;
  if (r.kind === "skill") return text.includes(`$${r.name}`) ? `$${r.name}` : `@skill:${r.name}`;
  if (r.kind === "role" || r.kind === "person") {
    const esc = r.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const kind = new RegExp(`@([a-z][a-z0-9-]{0,31}):${esc}(?![A-Za-z0-9._-])`).exec(text)?.[1];
    if (kind) return `@${kind}:${r.name}`;
    if (r.kind === "role") return `@role:${r.name}`;
  }
  return `@${r.name}`;
}

/**
 * What a posted message did, in one line: who it was sent to, why a tag
 * started nothing, and why the conversation's selected agents did not all
 * answer. A message that tags nobody, where nothing is selected, is just
 * posted, so it says nothing: nobody was meant to answer it.
 */
export function postedLine(p: ThreadPosted): string {
  const parts: string[] = [];
  if (p.notified.length > 0) parts.push(words.notified(p.notified));
  for (const r of p.refused)
    parts.push(words.refused(written(r, p.message?.text ?? ""), r.refused ?? ""));
  if (p.rule === "routed" && p.reason) parts.push(p.reason);
  return parts.join(" ");
}

/**
 * The chat bar of a conversation or a channel: the box, with completion of
 * `@` and `$`, and once a message is posted, what it did. Nothing under it
 * says who answers: nobody does until a message tags an agent.
 */
function RoomComposer({
  thread,
  queuePlace,
  placeholder,
  tags,
  autoFocus,
  seed,
  plus,
  trailing,
  onDraft,
  onPost,
  runs = [],
  sessionRuns = [],
  background = [],
  onBackgroundChanged,
  footer,
  sessionAgents,
}: {
  runs?: Run[];
  /** Every run in the session, whoever asked for it: the composer's Stop stops them all. */
  sessionRuns?: Run[];
  /** The reader's background work started from this session, which Stop also ends. */
  background?: BackgroundTask[];
  onBackgroundChanged?: () => void;
  /** The row under the box. */
  footer?: ReactNode;
  sessionAgents?: string[];
  /** Text controls in the bar, before Send: the conversation's approval mode. */
  trailing?: ReactNode;
  /** The conversation it posts in, whose first message, posted where it started, may have said something. */
  thread?: string;
  placeholder: string;
  tags: Completion;
  autoFocus?: boolean;
  seed?: ComposerSeed;
  /** Called as the text changes, so the + menu shows the agents it mentions. */
  onDraft?: (text: string) => void;
  plus?: Parameters<typeof Composer>[0]["plus"];
  /** Where the conversation shows the messages that wait (Composer's queuePlace). */
  queuePlace?: HTMLElement | null;
  onPost: (
    text: string,
    pick?: QueuedMessage["pick"],
    uploads?: QueuedMessage["uploads"],
  ) => Promise<ThreadPosted>;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  const [waiting, setWaiting] = useState<string[]>([]);
  const waitingNow = useRef<string[]>([]);
  // What is typed so far, for the room it leaves what the person did by hand.
  const [draft, setDraft] = useState("");
  // People's messages never wait for an agent: each is posted as it is sent.
  const voice = useVoiceNotes();
  const session = useSessionStop({
    session: thread,
    runs: sessionRuns,
    background,
    onBackgroundChanged,
  });
  const stopAll = session.stopAll;
  const queue = useQueue({
    onStart: (m) => {
      setProblem(null);
      // What the person did by hand in the workbench since their last message
      // goes with this one, and is cleared once it is posted.
      const post = (text: string) => onPost(text, m.pick, m.uploads);
      (thread ? postWithHumanActivity(thread, m.text, post) : post(m.text))
        .then((p) => {
          setProblem(postedLine(p) || null);
          const ids = p.runs.filter((run) => isWorking(run.status)).map((run) => run.id);
          waitingNow.current = ids;
          setWaiting(ids);
          if (!ids.length) queue.turnEnded("done");
        })
        .catch((e) => {
          setProblem(e instanceof ApiError ? e.message : words.failed.post);
          queue.reject();
        });
    },
    // Stop, Esc and a message sent now stop everything in the session, not
    // only the reply to this person's last message.
    onInterrupt: () => {
      if (thread) void stopAll();
      else
        void Promise.all(waitingNow.current.map((id) => api.stopRun(id))).catch((error) =>
          setProblem(error instanceof Error ? error.message : "Stop failed."),
        );
    },
  });
  useEffect(() => {
    if (waiting.length) {
      const current = waiting.map((id) => runs.find((run) => run.id === id));
      if (current.every((run) => run && !isWorking(run.status))) {
        waitingNow.current = [];
        setWaiting([]);
        queue.turnEnded(
          current.some((run) => run?.status === "failed")
            ? "failed"
            : current.some((run) => run?.status === "stopped")
              ? "stopped"
              : "done",
        );
      }
    } else if (!queue.running) {
      const active = runs.filter((run) => isWorking(run.status));
      if (active.length) {
        waitingNow.current = active.map((run) => run.id);
        setWaiting(waitingNow.current);
        queue.adopt(active[0]?.text || "Agent reply");
      }
    }
  }, [runs, waiting, queue.running, queue.adopt, queue.turnEnded]);
  return (
    <div className="flex flex-col gap-2">
      <Problem message={problem ?? session.problem} />
      {thread && <HumanActivityList conversation={thread} draft={draft} />}
      <Composer
        placeholder={placeholder}
        queue={queue}
        queuePlace={queuePlace}
        autoFocus={autoFocus}
        seed={seed}
        onDraft={(text) => {
          setDraft(text);
          onDraft?.(text);
        }}
        tags={tags}
        sessionAgents={sessionAgents}
        plus={plus}
        voice={voice}
        trailing={trailing}
        footer={footer}
        stopAll={
          thread
            ? { busy: session.busy, pending: session.pending, onStop: () => void stopAll() }
            : undefined
        }
      />
    </div>
  );
}

/**
 * What a message posted while an agent worked did to that work, in a few
 * words: queued until it ran, replacing the answer, or stopping it with its
 * reason. A queued message that has run says nothing more.
 */
export function followupNote(m: ThreadMessage): string {
  const f = m.followup;
  if (!f) return "";
  if (f.outcome === "stop") return words.followup.stopped(f.reason);
  if (f.outcome === "replace") return words.followup.replaced;
  return f.handled ? "" : words.followup.queued;
}

/**
 * A person's message: who posted it, when, and what it says. The reader's own
 * sit on the right as a compact bubble with no mark, as in any chat; everyone
 * else's sit on the left under their mark and name. A message that follows
 * one from the same person leaves the mark and name out, but still says what
 * it did to work going on (followupNote).
 */
function MessageView({
  m,
  focused,
  mine = false,
  continued = false,
  fromWeb = false,
  refs,
  onReply,
  actions,
}: {
  m: ThreadMessage;
  focused: boolean;
  /** Its references, as chips under it: the root message's origin. */
  refs?: ReactNode;
  /** Posted from the console in a session that started in Slack or GitHub, so the record says it did not come from there. */
  fromWeb?: boolean;
  /** Posted by the reader. */
  mine?: boolean;
  /** Follows a message from the same person. */
  continued?: boolean;
  onReply?: () => void;
  actions?: ReactNode;
}) {
  const who = m.name || m.by;
  const followup = followupNote(m);
  // What the person did by hand before sending is drawn closed, apart from their words.
  const { text: said, activity } = splitHumanActivity(m.text);
  const byHand = activity ? <HumanActivityNote activity={activity} mine={mine} /> : null;
  const uploads = m.attachments?.length ? (
    <ul className="flex max-w-full flex-wrap gap-1" aria-label={defaultCopy.details.used.uploads}>
      {m.attachments.map((file, index) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: A retained message's files are immutable and may share both name and bytes.
        <li key={index} className="min-w-0 max-w-full">
          <Reference
            kind="file"
            label={file.name || file.alt || file.media_type}
            title={file.unread || file.media_type}
          />
        </li>
      ))}
    </ul>
  ) : null;
  const notes = (
    <>
      {m.surface_id && <span>{words.fromSlack}</span>}
      {fromWeb && <span>{defaultCopy.trace.postedFromWeb}</span>}
      {m.answers && <span>{words.answered}</span>}
      {followup && <span data-followup={m.followup?.outcome}>{followup}</span>}
    </>
  );
  const row = (
    <MessageActionRow
      text={m.text}
      copyLabel={defaultCopy.conversation.copyMessage}
      onReply={onReply}
    >
      {actions}
    </MessageActionRow>
  );
  if (mine)
    return (
      <article
        id={`message-${m.id}`}
        data-turn-id={m.id}
        data-side="mine"
        aria-label={`Message from ${who}`}
        tabIndex={-1}
        className={`group/message flex flex-col items-end gap-0.5 rounded-aui ${continued ? "-mt-2" : ""} ${focused ? "outline-2 outline-offset-4 outline-aui-accent" : ""}`}
      >
        <p className="humagen-said max-w-[85%] whitespace-pre-wrap break-words rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg sm:max-w-[70%]">
          <Mentioned text={said} />
        </p>
        {byHand}
        {uploads}
        {/* Its actions show on hover and keep their place when hidden. They share the
            time's line, so a hidden row leaves no empty band under the message. */}
        <div className="flex max-w-full flex-wrap items-center justify-end gap-x-2">
          {row}
          <p className="flex flex-wrap items-baseline justify-end gap-x-2 text-aui-2xs text-aui-muted">
            {notes}
            <MessageTime at={m.at} />
          </p>
        </div>
        {refs && <div className="flex justify-end">{refs}</div>}
      </article>
    );
  return (
    <article
      id={`message-${m.id}`}
      data-turn-id={m.id}
      data-side="theirs"
      aria-label={`Message from ${who}`}
      // A tap focuses the message, which shows its actions (theme.css).
      tabIndex={-1}
      className={`group/message flex gap-3 rounded-aui ${continued ? "-mt-2" : ""} ${focused ? "outline-2 outline-offset-4 outline-aui-accent" : ""}`}
    >
      {continued ? <span aria-hidden className="w-7 shrink-0" /> : <PersonMark name={who} />}
      <div className="min-w-0 flex-1">
        {!continued ? (
          <p className="flex flex-wrap items-baseline gap-x-2 text-aui-xs text-aui-muted">
            <span className="font-medium text-aui-fg">{who}</span>
            <MessageTime at={m.at} />
            {notes}
          </p>
        ) : (
          followup && (
            <p className="flex flex-wrap items-baseline gap-x-2 text-aui-xs text-aui-muted">
              {notes}
            </p>
          )
        )}
        <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">
          <Mentioned
            text={said}
            skills={m.mentions.filter((t) => t.kind === "skill" && !t.refused).map((t) => t.name)}
          />
        </p>
        {byHand}
        {uploads}
        {refs}
        {row}
      </div>
    </article>
  );
}

/**
 * The announcement a thread was started from, ahead of its first message:
 * from the person who posted it, the agent a schedule ran, or humagen itself
 * for a release's notes, never from whoever replied. A withdrawn one says
 * only that. Humagen needs no mark, as in the inbox, so its column stays empty.
 */
/** The names the product posts under as itself: now, and in records from before the rename. */
const PRODUCT_SENDERS = ["humagen", OLD_SYSTEM_SENDER];

function AnnouncementView({ a }: { a: ThreadAnnouncement }) {
  // A release's notes are from the product, whatever name the record keeps.
  const who =
    a.kind === "person"
      ? a.name || a.by
      : a.kind === "system" && PRODUCT_SENDERS.includes(a.by)
        ? defaultCopy.product
        : a.by;
  return (
    <article aria-label={`${defaultCopy.template.announcement} from ${who}`} className="flex gap-3">
      {a.kind === "agent" ? (
        <AgentMark name={a.by} size={28} />
      ) : a.kind === "person" ? (
        <PersonMark name={who} />
      ) : (
        <span aria-hidden className="w-7 shrink-0" />
      )}
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-baseline gap-x-2 text-aui-xs text-aui-muted">
          <span className="font-medium text-aui-fg">{who}</span>
          <span>{defaultCopy.template.announcement}</span>
          <MessageTime at={a.at} />
        </p>
        {a.withdrawn ? (
          <p className="text-aui-sm text-aui-muted">{defaultCopy.announcements.seedWithdrawn}</p>
        ) : (
          <>
            <p className="text-aui-sm font-medium text-aui-fg">{a.subject}</p>
            <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{a.text}</p>
          </>
        )}
      </div>
    </article>
  );
}

/**
 * Why the reader is not offered Retry on a failed answer in the room. A run
 * answering a message acts for the person who posted it, so only they ask it
 * again, from the failure in their inbox, where it answers the same message
 * for them. A turn that acted for nobody, such as a machine's from before
 * threads, is retried for nobody, from the inbox by anyone who may start
 * runs, as the inbox decides (mayRetry). Nobody asks it again here, and
 * nobody regenerates an answer: a message posted in the room is how anyone
 * else asks.
 */
function retryInRoom(run: Run, me: string): string {
  const person = actedFor(run);
  if (!person) return words.retryNobodyFromInbox;
  return person === me ? "" : defaultCopy.retryOnlyTheirs;
}

/**
 * An agent's turn: what it answered, as the conversation view shows it. A
 * question it waits on is offered to the reader only when it is theirs
 * (ask), and an answer the platform refused says why under it, where it was
 * given.
 */
function AnswerView({
  run: originalRun,
  me,
  ask,
  refused,
  onAsk,
  onReply,
  onRedirect,
  actions,
  canStop = false,
  focused = false,
  background = [],
  onBackgroundChanged,
}: {
  run: Run;
  canStop?: boolean;
  focused?: boolean;
  /** The reader's background work from this session, which a step's own Stop ends. */
  background?: BackgroundTask[];
  onBackgroundChanged?: () => void;
  me: string;
  live: boolean;
  ask?: ThreadAsk[];
  refused?: Refusal;
  onAsk?: () => void;
  onReply?: () => void;
  onRedirect?: () => void;
  actions?: ReactNode;
}) {
  const { run, stop } = useRunStop(originalRun, canStop);
  // What a run of someone else's needed, and asking its calls again, are theirs:
  // another reader sees neither the Connect card nor Retry on it.
  const ownsRun = !!me && actedFor(originalRun) === me;
  const retryCalls = useContext(RetryCallContext);
  const itemStops = useItemStops({
    run,
    allowed: canStop,
    background,
    onBackgroundChanged,
    agentStop: stop ? { onStop: stop.onStop, pending: !!stop.pending } : undefined,
  });
  const turn = useRunTurn(run);
  const name = agentShown(run.agent);
  const drawn = useMemo(() => {
    const read = turn ?? turnOf(run);
    // The entry agent answers under the workspace's name.
    const base = read.agent.name === name ? read : { ...read, agent: { ...read.agent, name } };
    return run.citations !== undefined
      ? { ...base, citations: run.citations.map(citationOf) }
      : base;
  }, [turn, run, name]);
  const shown = useMemo(() => {
    const asked = askedOf(drawn, ask);
    return asked.failure
      ? { ...asked, failure: { ...asked.failure, retryNotOffered: retryInRoom(run, me) } }
      : asked;
  }, [drawn, run, me, ask]);
  return (
    <article
      id={`turn-${run.id}`}
      data-turn-id={run.id}
      className={`flex flex-col gap-2 ${focused ? "rounded-aui outline-2 outline-offset-4 outline-aui-accent" : ""}`}
      aria-label={words.answeredBy(name)}
    >
      {/* A run from before threads carries what was asked; one that answers a message does not repeat it. */}
      {!run.message && (
        <p className="humagen-said max-w-[80%] self-end whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">
          <Mentioned text={run.text} />
        </p>
      )}
      {/* The answer names its agent, so where a message was routed is not said again. Why an agent joined is. */}
      {run.picked && <Checkpoint agent={run.agent}>{run.picked}</Checkpoint>}
      <AgentTint name={run.agent}>
        <RunFeedbackHost turn={shown}>
          {(rated) => (
            <RetryCallContext.Provider value={ownsRun ? retryCalls : null}>
              <CallThreads run={run} calls={rated.tasks.some((t) => t.subagent)}>
                <AgentMessage
                  actions={actions}
                  stop={stop}
                  itemStops={itemStops}
                  disclaimer={false}
                  activity={false}
                  timestamp={<MessageTime at={run.created_at} />}
                  turn={ownsRun ? rated : { ...rated, needsConnection: undefined }}
                  copy={copyFor(ask)}
                  itemActions={itemActions(run)}
                  capabilities={{ regenerate: false }}
                  onAskAgent={onAsk}
                  onReply={onReply}
                  onRedirect={onRedirect}
                />
              </CallThreads>
            </RetryCallContext.Provider>
          )}
        </RunFeedbackHost>
      </AgentTint>
      <AnswerRefs run={run} />
      <BrowserCard thread={run.thread} tasks={drawn.tasks} />
      {refused && stillAsks(shown, refused.question) && (
        <div className="pl-11">
          <Problem message={refused.why} />
        </div>
      )}
    </article>
  );
}

/** No agent lives in a project, for a page not told where any does. */
const noHomes: Record<string, string> = {};

/**
 * What an answer in the conversation reads from around it: where a file it
 * wrote opens, what each GitHub number named in the session is, and how a
 * failed call to another agent is asked again.
 */
function TranscriptContexts({
  fileLinks,
  refKinds,
  retryCalls,
  children,
}: {
  fileLinks: FileLinks;
  refKinds: ReadonlyMap<string, "pull" | "issue">;
  retryCalls: RetryCalls | null;
  children: ReactNode;
}) {
  return (
    <FileLinkContext.Provider value={fileLinks}>
      <GitHubRefKindsContext.Provider value={refKinds}>
        <RetryCallContext.Provider value={retryCalls}>{children}</RetryCallContext.Provider>
      </GitHubRefKindsContext.Provider>
    </FileLinkContext.Provider>
  );
}

/** The most characters a message holds (threads.MaxText on the server). */
const MESSAGE_MAX = 8000;

/** What Retry posts: the call's agent, tagged, then its brief with every other tag made a word. */
export function retryText({ agent, text }: RetryCall): string {
  return `@${agent} ${text.replace(/(^|[^\w@])@(?=[\w.-])/g, "$1")}`;
}

/**
 * A shared thread: its people's messages and its agents' answers in the
 * order they were made, who takes part, and a composer that tags with @.
 */
export function ThreadRoom({
  id,
  me,
  handle = "",
  initial,
  tick,
  agents,
  homes = noHomes,
  projects,
  root,
  workspaceName,
  canRun,
  artifactActions,
  canLearn = false,
  alertAccess,
  forkActions,
  personalProject,
  focus,
  sharing = false,
  onSharing,
  view,
  onView,
  onOpenChannel,
  onOpenThread,
  footerStart,
  onChanged,
}: {
  id: string;
  /** The reader's subject: a failed answer is theirs to retry when it acted for them. */
  me: string;
  /** How the platform names the reader (GET /api/me handle), which a thread names its starter by. */
  handle?: string;
  /** The page already read, so the room shows at once. */
  initial: ThreadPage;
  tick: number;
  agents: Revision[];
  /** Where each agent that lives in a project lives, by name. */
  homes?: Record<string, string>;
  projects: ProjectView[];
  root: ProjectView | null;
  workspaceName: string;
  canRun: boolean;
  canLearn?: boolean;
  alertAccess?: { write: boolean };
  artifactActions?: { knowledge: boolean; memory: boolean };
  forkActions?: ConversationForkActions;
  personalProject?: string;
  /** A message to open at, as `?message=` names it. */
  focus?: string;
  /** Whether the Share dialog is open, as `/conversations/{id}/share` says. */
  sharing?: boolean;
  /** Opens or closes the Share dialog, at its address. */
  onSharing?: (open: boolean) => void;
  view?: ConversationView;
  onView?: (view: ConversationView) => void;
  onOpenThread?: (id: string) => void;
  /** The left of the row under the composer: where the session runs (its image). */
  footerStart?: ReactNode;
  onOpenChannel: (project: string, channel: string) => void;
  onChanged: () => void;
}) {
  const [page, setPage] = useState<ThreadPage>(initial);
  const attention = useSessionAlerts(id, !!alertAccess, tick, onChanged);
  // Every turn read so far, oldest first. A refresh reads the newest page,
  // which moves on as the thread grows; the turns it no longer holds stay.
  const [turns, setTurns] = useState<ThreadTurn[]>(initial.turns);
  // Only on first open; subsequent arrivals and manual scrolling keep their usual behavior.
  const [initialApproval] = useState(
    () =>
      [...initial.turns].reverse().flatMap((t) => {
        if (!t.run) return [];
        const asks = asksOf(t);
        const approval = [...turnOf(t.run).interrupts]
          .reverse()
          .find(
            (i) =>
              i.type === "approval" &&
              i.decision.kind === "pending" &&
              (!asks.some((a) => a.question === i.id) ||
                asks.some((a) => a.question === i.id && a.yours)),
          );
        return approval ? [{ run: t.run.id, question: approval.id }] : [];
      })[0],
  );
  // The thread's wave: a tick per message and per answer, previewing what each says.
  const wave = useMemo(() => waveOf(turns), [turns]);
  // A pull request or issue named by its address anywhere in the session reads that way in every answer.
  const refKinds = useMemo(
    () => githubRefKinds(turns.map((t) => t.run?.answer ?? "").join("\n")),
    [turns],
  );
  // The cursor for the turns before the oldest one read.
  const [before, setBefore] = useState<string | null>(initial.next);
  const [problem, setProblem] = useState<string | null>(null);
  const [localView, setLocalView] = useState<ConversationView>({});
  const [seed, setSeed] = useState<ComposerSeed>();
  // The end of the conversation, where messages that wait are shown.
  const [queuePlace, setQueuePlace] = useState<HTMLElement | null>(null);
  const background = useSessionBackground(id);
  // Arriving from Home's first message, the bar slides down to its place.
  const dock = useRef<HTMLDivElement>(null);
  useDockArrival(dock);
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
  // Opens a workbench tab, as a card in the thread does for the work it shows.
  const openWorkbench = (tab: WorkbenchTabId, ref?: string) =>
    openPanel("details", openBench(shownView, tab, ref));
  // The header's views: the panel, a workbench tab, the Workspace pane, the progress.
  const openView = (v: SessionView) => {
    if (v === "details") return openPanel("details", openBench(shownView, "details"));
    if (v === "files" || v === "artifacts")
      return openPanel("workspace", openPane(shownView, { kind: v }));
    if (v === "progress") return openPanel("activity");
    // A session carries its own background work: it is a section of its Details.
    if (v === "background") return openPanel("details", openBench(shownView, "details"));
    openWorkbench(v);
  };
  // A file named in the thread opens at its changes in the workbench beside it,
  // so a changed file can be opened without leaving the session.
  const fileLinks: FileLinks = {
    href: (path) =>
      href({
        page: "conversation",
        id,
        ...openBench(shownView, "changes", path),
        panel: "details",
      }),
    open: (path) => openWorkbench("changes", path),
  };
  // Why an answer to a run's question was not taken, by run: said under it
  // while that question still waits, and gone once anyone settles it.
  const [refusals, setRefusals] = useState<Record<string, Refusal>>({});

  // What the conversation's own stream heard (roomFollow.ts): in a large
  // room, a page every few seconds rather than each message.
  const [heard, setHeard] = useState(0);
  useEffect(() => followRoom(id, () => setHeard((n) => n + 1)), [id]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and heard are the refresh signals
  useEffect(() => {
    let live = true;
    coworking
      .thread(id)
      .then((p) => {
        if (!live) return;
        setPage(p);
        setTurns((t) => mergeTurns(t, p.turns));
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [id, tick, heard]);

  const loadEarlier = () => {
    if (!before) return;
    coworking
      .thread(id, before)
      .then((p) => {
        setTurns((t) => mergeTurns(p.turns, t));
        setBefore(p.next);
      })
      .catch(() => setProblem(defaultCopy.failure.internal()));
  };

  useEffect(() => {
    if (
      !id.startsWith("s_") ||
      !focus ||
      turns.some((turn) => (turn.message?.id ?? turn.run?.id) === focus)
    )
      return;
    let live = true;
    coworking.thread(id, undefined, focus).then(
      (page) => {
        if (!live) return;
        setTurns((current) => mergeTurns(page.turns, current));
        setBefore(page.next);
      },
      (error) => {
        if (live)
          setProblem(
            error instanceof ApiError && error.status === 404
              ? "That message is unavailable."
              : "The linked message could not be loaded.",
          );
      },
    );
    return () => {
      live = false;
    };
  }, [id, focus]);

  const th = page.thread;
  const place = usePlace(th);
  // The root message: the first a person posted. Its sender and origin head the session.
  const rootMessage = turns.find((t) => t.message)?.message;
  const tags = useCompletion(
    th.project,
    agents,
    homes,
    topicsOf(projects, root, th.project),
    me,
    handle,
  );
  // A thread names its starter as the platform names people elsewhere: by handle, which a subject holding an address is not.
  const started = th.created_by === me || (!!handle && th.created_by === handle);
  const posts = canRun && !th.read_only && postsIn(projects, th.project);
  // A private conversation is published from its header by its starter, or
  // an owner of its project, as the server holds them to it.
  const owner =
    (th.project === "" ? root : projects.find((p) => p.name === th.project))?.access?.role ===
    "owner";
  const shareable =
    posts && th.visibility === "private" && !th.surface && !!onSharing && (started || owner);
  const hasRuns = turns.some((t) => t.run);
  const fixedWhy = hasRuns
    ? defaultCopy.threads.shareProjectFixed
    : th.surface
      ? defaultCopy.chat.projectFromSlack
      : th.visibility === "shared"
        ? defaultCopy.chat.projectShared
        : !started && !owner
          ? defaultCopy.chat.projectStarter
          : undefined;
  const shareButton = useRef<HTMLButtonElement>(null);
  // What sharing did: who was told, and who was not added and why.
  const [shareNote, setShareNote] = useState("");
  // The Share address of a conversation that can't be shared is the conversation's.
  useEffect(() => {
    if (sharing && !shareable) onSharing?.(false);
  }, [sharing, shareable, onSharing]);
  const answering = th.participants.filter((p) => p.kind === "agent").map((p) => p.name);
  // Unnamed, a conversation is titled from its first message as the server titles it.
  const title =
    th.title ||
    turns.find((t) => t.announcement)?.announcement?.subject ||
    deriveTitle(turns.find((t) => t.message)?.message?.text || turns[0]?.run?.text || "") ||
    words.channel(th.channel);
  const last = turns[turns.length - 1];
  // A run a message in the room answers asked someone something: its steps
  // and its question are read from its journal, though turns came after it.
  const asked = new Set(turns.flatMap((t) => (t.message?.answers ? [t.message.answers.run] : [])));
  // A run waiting on a person, such as an expert's answer, is shown still.
  const busy = turns.some((t) => t.run && isWorking(t.run.status));
  // A delegated call that failed is asked again as the next message, tagging
  // its own agent alone: other agents its brief names are words, not tags, so
  // Retry wakes nobody the call did not. A refusal is said under the
  // conversation, and Retry can be pressed again.
  const retryCalls: RetryCalls = {
    ask: async (call) => {
      setProblem(null);
      try {
        await coworking.post(id, { text: retryText(call) });
        onChanged();
      } catch (error) {
        setProblem(
          error instanceof Error ? error.message : defaultCopy.conversation.callFailure.retryFailed,
        );
        throw error;
      }
    },
    allows: (call) => [...retryText(call)].length <= MESSAGE_MAX,
  };
  const onAction = (a: UiAction) => {
    if (a.id === ActionId.openRun)
      return openPanel("activity", { run: a.payload.target, step: a.payload.option });
    if (a.id === ActionId.openReview && a.payload.target) {
      openPanel("workspace", reviewView(shownView, a.payload.target));
      return;
    }
    // A review's Ask Follow-up puts its words, tagging the agent, in the box. Nothing is sent.
    if (a.id === ActionId.reviewFollowUp) {
      if (posts && a.payload.text) setSeed({ text: `${a.payload.text} `, mode: "prepend" });
      return;
    }
    if (a.id === ActionId.retry) {
      const run = turns.find((turn) => turn.run?.id === a.payload.turnId)?.run;
      if (!run || !posts) return;
      const denied = retryInRoom(run, me);
      if (denied) return setProblem(denied);
      setProblem(null);
      coworking
        .post(id, { text: run.text })
        .then(() => onChanged())
        .catch((error) => setProblem(error instanceof Error ? error.message : "Retry failed."));
      return;
    }
    if (!isAnswer(a)) return;
    const answer = answerOfAction(a);
    if (!answer) return;
    setProblem(null);
    setRefusals(({ [answer.run]: _, ...rest }) => rest);
    // A reply in words is said in the room, as the person's message, and
    // recorded as the question's answer. A choice or an approval is a click.
    // It answers the question on the card it was given in: the run's
    // stream draws that card, and moves on before the room's record of the
    // run does.
    const text = answer.body.action === "reply" ? answer.body.text : undefined;
    const run = turns.find((t) => t.run && t.run.id === answer.run)?.run;
    const done =
      text && run
        ? coworking.post(id, { text, answers: { run: run.id, question: answer.question } })
        : api.answer(answer);
    // A refusal is said under the question, where the words typed for it
    // are still in its box.
    done.then(onChanged).catch((e) =>
      setRefusals((r) => ({
        ...r,
        [answer.run]: {
          question: answer.question,
          why:
            e instanceof ApiError && e.status === 403
              ? defaultCopy.noPermission
              : e instanceof ApiError
                ? e.message
                : defaultCopy.failure.internal(),
        },
      })),
    );
  };

  useViewShortcuts(openView, (v) => v !== "sidechat" || posts);
  const unavailable = useUnavailableViews(th.id, !!th.session);
  const activeView: SessionView | undefined =
    shownView.panel === "details"
      ? (shownView.bench ?? "details")
      : shownView.panel === "workspace"
        ? "files"
        : shownView.panel === "activity"
          ? "progress"
          : undefined;
  return (
    <SessionDetailsProvider panel={shownView.panel} onPanel={(panel) => openPanel(panel)}>
      <WorkbenchOpen.Provider value={openWorkbench}>
        <TranscriptContexts
          fileLinks={fileLinks}
          refKinds={refKinds}
          retryCalls={posts ? retryCalls : null}
        >
          <section className="flex min-h-0 min-w-0 flex-1">
            <ConversationPage
              views={
                <SessionViews
                  active={activeView}
                  progress={sessionProgress(turns.flatMap((t) => (t.run ? [t.run] : [])))}
                  onOpen={openView}
                  onClose={() => openPanel()}
                  hidden={posts ? [] : ["sidechat"]}
                  disabled={unavailable}
                />
              }
              titleControl={
                <SessionActions
                  shownTitle={title}
                  thread={th}
                  canManage={
                    posts &&
                    (started ||
                      owner ||
                      th.participants.some(
                        (p) => p.kind === "person" && p.owner && [me, handle].includes(p.name),
                      ))
                  }
                  onChanged={onChanged}
                  onUpdated={(next) => setPage((p) => ({ ...p, thread: next }))}
                  onShare={shareable ? () => onSharing?.(!sharing) : undefined}
                  projects={projects}
                  personalProject={personalProject}
                />
              }
              title={title}
              // The reader's own Personal project is not named: a session is there unless it was filed elsewhere.
              project={
                th.project === personalProject
                  ? ""
                  : projectTitle(projects, th.project, workspaceName, personalProject)
              }
              private={th.visibility === "private"}
              agents={answering}
              people={th.participants.filter((p) => p.kind === "person").map((p) => p.name)}
              origin={<OriginButton place={place} />}
              progress={sessionProgress(turns.flatMap((t) => (t.run ? [t.run] : [])))}
              location={
                <>
                  {th.visibility !== "private" && !id.startsWith("s_") && (
                    <button
                      type="button"
                      aria-label={words.sharedIn(
                        projectTitle(projects, th.project, workspaceName, personalProject),
                        th.channel,
                      )}
                      onClick={() => onOpenChannel(th.project, th.channel)}
                      className="min-h-6 rounded-aui hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                    >
                      {words.channel(th.channel)}
                    </button>
                  )}
                  {place && (
                    <SessionPlace place={place} sender={rootMessage?.name || rootMessage?.by} />
                  )}
                </>
              }
              projectRef={shareButton}
              onPanel={(next) => openPanel(next)}
              panel={shownView.panel}
              onClosePanel={() => openPanel()}
              onArtifact={(id) =>
                openPanel("workspace", openPane(shownView, { kind: "artifact", ref: id }))
              }
            >
              <Presence when={!!shareable && !!sharing} ms={220}>
                {(leaving) => (
                  <div data-layer="share" className="relative z-10 px-4 pb-2">
                    <ShareDialog
                      open={!leaving}
                      thread={th}
                      hasRuns={hasRuns}
                      onPublished={
                        onOpenThread
                          ? (next) => {
                              onSharing?.(false);
                              onChanged();
                              onOpenThread(next.id);
                            }
                          : undefined
                      }
                      projects={projects.filter((p) => postsIn(projects, p.name))}
                      workspaceName={workspaceName}
                      me={me}
                      handle={handle}
                      onClose={() => {
                        onSharing?.(false);
                        shareButton.current?.focus();
                      }}
                      onShared={(p, asked) => {
                        // Say who was told, and who was not added and why, as the platform decided.
                        const missed = new Set((p.not_added ?? []).map((n) => n.handle));
                        const told = asked.filter((h) => !missed.has(h));
                        // Why someone was not added, in the console's own words, never the server's.
                        const cannot = (p.not_added ?? [])
                          .filter((n) => /cannot read this project/.test(n.reason))
                          .map((n) => n.handle);
                        const other = (p.not_added ?? [])
                          .filter((n) => !cannot.includes(n.handle))
                          .map((n) => n.handle);
                        const lines = [
                          words.sharedTold(told),
                          words.notAdded(cannot, other),
                        ].filter(Boolean);
                        setShareNote(lines.join(" "));
                        setPage(p);
                        setTurns((t) => mergeTurns(t, p.turns));
                        onSharing?.(false);
                        onChanged();
                      }}
                    />
                  </div>
                )}
              </Presence>
              {shareNote && (
                <p
                  role="status"
                  className="mx-auto w-full max-w-4xl px-4 pb-2 text-aui-xs text-aui-muted"
                >
                  {shareNote}
                </p>
              )}
              {th.fork && (
                <div className="mx-auto w-full max-w-4xl px-4 py-4">
                  <ForkContext thread={th} />
                </div>
              )}
              {th.import && (
                <div className="mx-auto w-full max-w-4xl px-4 py-4">
                  {/* A continuation just started has no turns yet: focus goes to where it came from. */}
                  <ImportContext thread={th} autoFocus={turns.length === 0} />
                </div>
              )}
              {posts && (
                <ConversationForkPanels
                  thread={th}
                  runs={turns.flatMap((turn) => (turn.run ? [turn.run] : []))}
                  actions={forkActions}
                />
              )}
              <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
                <ThreadList
                  className="w-full flex-1"
                  column="mx-auto w-full max-w-4xl px-4"
                  busy={busy}
                  label="Thread"
                  turns={wave}
                  focus={focus}
                  initialApproval={initialApproval}
                  footer={
                    <div
                      ref={dock}
                      className="mx-auto flex w-full max-w-4xl flex-col gap-2 px-4 pb-3"
                    >
                      <Problem message={problem} />
                      {canRun && !posts && (
                        <p className="text-aui-xs text-aui-muted">{words.viewerOnly}</p>
                      )}
                      {posts && (
                        <RoomComposer
                          queuePlace={queuePlace}
                          sessionAgents={page.sessionAgents}
                          sessionRuns={turns.flatMap((turn) => (turn.run ? [turn.run] : []))}
                          background={background.items}
                          onBackgroundChanged={background.refresh}
                          runs={turns.flatMap((turn) =>
                            turn.run && actedFor(turn.run) === me ? [turn.run] : [],
                          )}
                          thread={id}
                          footer={(() => {
                            // Who answers is whoever answered last; the ring reads again as runs end.
                            const runs = turns.flatMap((turn) => (turn.run ? [turn.run] : []));
                            const latest = runs[runs.length - 1];
                            return (
                              <ComposerFooter
                                // Left, in order: project, image (footerStart), then approval.
                                start={
                                  <>
                                    {footerStart}
                                    <ApprovalToggle thread={th} me={handle || me} />
                                  </>
                                }
                                agent={latest ? agentShown(latest.agent) : undefined}
                                model={latest?.usage?.context?.model}
                                usage={
                                  <UsageRing
                                    thread={id}
                                    project={th.project}
                                    agents={answering}
                                    version={`${runs.length}:${runs.filter((r) => isWorking(r.status)).length}`}
                                  />
                                }
                              />
                            );
                          })()}
                          seed={seed}
                          placeholder={words.message}
                          tags={tags}
                          plus={{
                            // The draft's mentions are the + menu's agents.
                            menu: (close, attach, leave, draft) => (
                              <PlusMenu
                                onLeave={leave}
                                agents={tags.agents}
                                {...mentionPicks(draft, tags.agents, setSeed)}
                                skills={tags.skills}
                                skillsFailed={tags.skillsFailed}
                                onSkill={(name) => {
                                  setSeed({ text: `/${name} `, mode: "prepend" });
                                }}
                                onAttach={attach}
                                onClose={close}
                              />
                            ),
                          }}
                          autoFocus={sharing ? false : undefined}
                          onPost={(text, pick, uploads) =>
                            // It follows the newest message, so a run it starts reads the conversation so far.
                            coworking
                              .post(id, { text, ...pick, uploads })
                              .then((p) => {
                                // What was posted shows at once: the message, and any run its tags started.
                                setTurns((t) =>
                                  mergeTurns(t, [
                                    { kind: "message", message: p.message },
                                    ...p.runs.map((run) => ({ kind: "run" as const, run })),
                                  ]),
                                );
                                onChanged();
                                return p;
                              })
                          }
                        />
                      )}
                    </div>
                  }
                >
                  <Problem message={attention.problem ?? null} />
                  {attention.problem && (
                    <button type="button" className={button} onClick={attention.refresh}>
                      Reload alerts
                    </button>
                  )}
                  {attention.undo && (
                    <p role="status" className="text-aui-xs">
                      Alert updated.{" "}
                      <button
                        type="button"
                        className="underline"
                        disabled={attention.busy}
                        onClick={() => void attention.change(attention.undo!, "open")}
                      >
                        Undo
                      </button>
                    </p>
                  )}
                  <SessionAlerts
                    alerts={attention.rows.filter(
                      (a) =>
                        !duplicatesInlineApproval(a, turns) &&
                        (!alertAnchor(a) ||
                          !turns.some((t) => (t.message?.id ?? t.run?.id) === alertAnchor(a))),
                    )}
                    attention={attention}
                    canWrite={!!alertAccess?.write}
                    agents={posts ? agents.map((a) => a.spec.name) : []}
                    onOpen={onOpenThread}
                  />
                  {before && (
                    <button type="button" className={`${button} self-center`} onClick={loadEarlier}>
                      {words.olderTurns}
                    </button>
                  )}
                  {turns.map((t, i) => (
                    <Fragment key={t.announcement?.id ?? t.message?.id ?? t.run?.id}>
                      <SessionAlerts
                        alerts={attention.rows.filter(
                          (a) =>
                            !duplicatesInlineApproval(a, turns) &&
                            !!alertAnchor(a) &&
                            alertAnchor(a) === (t.message?.id ?? t.run?.id),
                        )}
                        attention={attention}
                        canWrite={!!alertAccess?.write}
                        agents={posts ? agents.map((a) => a.spec.name) : []}
                        onOpen={onOpenThread}
                        beside
                      />
                      {t.announcement ? (
                        <AnnouncementView key={`a:${t.announcement.id}`} a={t.announcement} />
                      ) : t.message ? (
                        <MessageView
                          key={t.message.id}
                          m={t.message}
                          mine={t.message.by === me || (!!handle && t.message.by === handle)}
                          continued={turns[i - 1]?.message?.by === t.message.by}
                          focused={focus === t.message.id}
                          fromWeb={place?.kind !== "console" && t.message.via === "console"}
                          actions={
                            posts ? (
                              <>
                                {forkActions?.onFork && (
                                  <button
                                    type="button"
                                    className={messageActionButton}
                                    aria-label={defaultCopy.forking.fromHere}
                                    title={defaultCopy.forking.fromHere}
                                    onClick={() => forkActions.onFork?.(t.message!.id)}
                                  >
                                    <ForkIcon size={16} />
                                  </button>
                                )}
                              </>
                            ) : undefined
                          }
                          onReply={
                            posts
                              ? () =>
                                  setSeed({
                                    quote: {
                                      label: "this message",
                                      text: t.message?.text,
                                      url: href({
                                        page: "conversation",
                                        id,
                                        message: t.message?.id,
                                      }),
                                    },
                                  })
                              : undefined
                          }
                        />
                      ) : t.run ? (
                        <AnswerView
                          focused={focus === t.run.id}
                          key={t.run.id}
                          run={t.run}
                          canStop={posts}
                          background={background.items}
                          onBackgroundChanged={background.refresh}
                          actions={
                            !th.read_only && t.run.status === "completed" ? (
                              <>
                                <OutcomeActions run={t.run} canWrite={canLearn} />
                                {posts && forkActions?.onFork && (
                                  <button
                                    type="button"
                                    className={messageActionButton}
                                    aria-label={defaultCopy.forking.fromHere}
                                    title={defaultCopy.forking.fromHere}
                                    onClick={() => forkActions.onFork?.(t.run!.id)}
                                  >
                                    <ForkIcon size={16} />
                                  </button>
                                )}
                              </>
                            ) : undefined
                          }
                          onAsk={
                            posts
                              ? () => setSeed({ text: `@${t.run?.agent} `, mode: "prepend" })
                              : undefined
                          }
                          onReply={
                            posts
                              ? () =>
                                  setSeed({
                                    quote: {
                                      label: `${agentShown(t.run?.agent ?? "")}'s answer`,
                                      agent: t.run?.agent,
                                      url: href({
                                        page: "conversation",
                                        id,
                                        panel: "activity",
                                        run: t.run?.id,
                                      }),
                                    },
                                  })
                              : undefined
                          }
                          onRedirect={
                            posts
                              ? () =>
                                  setSeed({
                                    text: defaultCopy.conversation.redirectPrompt(
                                      t.run!.agent,
                                      href({
                                        page: "conversation",
                                        id,
                                        panel: "activity",
                                        run: t.run!.id,
                                      }),
                                    ),
                                    mode: "prepend",
                                  })
                              : undefined
                          }
                          me={me}
                          ask={asksOf(t)}
                          refused={refusals[t.run.id]}
                          live={
                            t === last ||
                            asked.has(t.run.id) ||
                            (t.run.status !== "completed" && t.run.status !== "failed")
                          }
                        />
                      ) : null}
                    </Fragment>
                  ))}
                  {/* Messages that wait behind the running turn, drawn by the composer. */}
                  <div ref={setQueuePlace} data-layer="queued-place" className="empty:hidden" />
                </ThreadList>
              </WebSurface>
            </ConversationPage>
            <ConversationInfo
              origin={referenceOfPlace(place)}
              canWrite={canRun}
              runs={turns.flatMap((turn) => (turn.run ? [turn.run] : []))}
              view={shownView}
              panel={shownView.panel}
              contextTab={shownView.contextTab}
              onContextTab={(contextTab) => openPanel("context", { contextTab })}
              thread={th}
              project={projectTitle(projects, th.project, workspaceName, personalProject)}
              onClose={() => openPanel()}
            />
            <Workspace
              extraTab={(tab) =>
                ["review", "pull", "issue"].includes(tab.kind) ? (
                  <GitHubTab
                    tab={tab}
                    review={reviewText(
                      turns.flatMap((t) => (t.run ? [t.run] : [])),
                      tab.ref,
                    )}
                  />
                ) : undefined
              }
              key={id}
              thread={id}
              open={shownView.panel === "workspace"}
              view={shownView}
              onView={(next) => openPanel("workspace", next)}
              onClose={() => openPanel()}
              onRequest={
                posts
                  ? (text) => {
                      setSeed({ text, mode: "prepend" });
                      openPanel();
                    }
                  : undefined
              }
              promotion={
                artifactActions
                  ? { project: th.project, shared: th.visibility === "shared", ...artifactActions }
                  : undefined
              }
              onRevise={
                posts
                  ? (file, slide) => {
                      setSeed({ text: revisionPrompt(file, slide), mode: "prepend" });
                      openPanel();
                    }
                  : undefined
              }
              onRun={(run) => openPanel("activity", { run })}
            />
            <Presence when={shownView.panel === "deliverables"} ms={220}>
              {(leaving) => (
                <Deliverables
                  open={!leaving}
                  promotion={
                    artifactActions
                      ? {
                          project: th.project,
                          shared: th.visibility === "shared",
                          ...artifactActions,
                        }
                      : undefined
                  }
                  thread={id}
                  view={shownView}
                  onView={(next) => openPanel("deliverables", next)}
                  onEditCopy={(file) => openPanel("workspace", { file, workspaceTab: "edit" })}
                  onClose={() => openPanel()}
                  onRevise={
                    posts
                      ? (file, slide) => {
                          setSeed({ text: revisionPrompt(file, slide), mode: "prepend" });
                          openPanel();
                        }
                      : undefined
                  }
                />
              )}
            </Presence>
            <Presence when={shownView.panel === "details"} ms={220}>
              {(leaving) => (
                <SessionWorkbench
                  runs={turns.flatMap((t) => (t.run ? [t.run] : []))}
                  thread={th.id}
                  uploads={turns.flatMap((t) =>
                    (t.message?.attachments ?? []).map((a) => a.name || a.alt || a.media_type),
                  )}
                  origin={referenceOfPlace(place)}
                  canPost={posts}
                  view={shownView}
                  onView={(next) => openPanel("details", next)}
                  files={sessionFiles}
                  leaving={leaving}
                  onClose={() => openPanel()}
                  onOpenFile={(id) =>
                    openPanel("workspace", openPane(shownView, { kind: "artifact", ref: id }))
                  }
                  onOpenWorkspace={(tab) => openPanel("workspace", openPane(shownView, tab))}
                  chosen={th.session}
                  onAsk={posts ? (text) => setSeed({ text, mode: "prepend" }) : undefined}
                />
              )}
            </Presence>
            <Presence when={shownView.panel === "response" && !!shownView.run} ms={220}>
              {(leaving) => {
                const run = turns.find((t) => t.run?.id === shownView.run)?.run;
                return run ? (
                  <ResponseActivityPanel
                    source={traceSource(run)}
                    leaving={leaving}
                    onClose={() => openPanel()}
                  />
                ) : null;
              }}
            </Presence>
            <Presence when={shownView.panel === "activity"} ms={220}>
              {(leaving) => (
                <ConversationActivity
                  step={shownView.step}
                  leaving={leaving}
                  turns={turns.flatMap((turn) => (turn.run ? [turn.run] : []))}
                  focus={shownView.run ?? null}
                  onClose={() => openPanel()}
                  onFocus={(run) => openPanel("activity", run ? { run } : {})}
                />
              )}
            </Presence>
          </section>
        </TranscriptContexts>
      </WorkbenchOpen.Provider>
    </SessionDetailsProvider>
  );
}

/** One thread in a channel's list: its title, who answered and who posted. */
function ThreadRow({ t, onOpen }: { t: ThreadSummary; onOpen: () => void }) {
  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        className="flex w-full items-center gap-3 rounded-aui px-3 py-2.5 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <span className="min-w-0 flex-1">
          <span className="block truncate text-aui-sm text-aui-fg">{t.title}</span>
          <span className="flex flex-wrap gap-x-3 text-aui-xs text-aui-muted">
            {t.visibility === "private" && <span>{words.private}</span>}
            <span>{ago(t.updated_at)}</span>
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-1" aria-label="Agents">
          {t.agents.slice(0, 4).map((a) => (
            <AgentMark key={a} name={a} size={16} />
          ))}
        </span>
        <span className="flex shrink-0 items-center -space-x-1" aria-label="People">
          {t.people.slice(0, 4).map((p) => (
            <PersonMark key={p} name={p} size={20} />
          ))}
        </span>
      </button>
    </li>
  );
}

/**
 * A channel: its threads the reader may read, newest first, and a composer that starts
 * a new one.
 */
export function ChannelPage({
  project,
  channel,
  me = "",
  handle = "",
  tick,
  agents,
  homes = noHomes,
  projects,
  root,
  workspaceName,
  canRun,
  onOpenProject,
  onOpenProjects,
  onOpenThread,
}: {
  project: string;
  channel: string;
  /** The reader's subject, whom the typeahead leaves out. */
  me?: string;
  /** How the platform names the reader, whom completion leaves out. */
  handle?: string;
  tick: number;
  agents: Revision[];
  /** Where each agent that lives in a project lives, by name. */
  homes?: Record<string, string>;
  projects: ProjectView[];
  root: ProjectView | null;
  workspaceName: string;
  canRun: boolean;
  onOpenProject: () => void;
  /** Every project: where a channel of a project that is not there leads back to. */
  onOpenProjects: () => void;
  onOpenThread: (id: string) => void;
}) {
  const [info, setInfo] = useState<Channel | null>(null);
  const [threads, setThreads] = useState<ThreadSummary[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  // What is not there: the project, for the reader (a project they may not
  // see answers as one that does not exist), or the channel in it.
  const [missing, setMissing] = useState<"project" | "channel" | null>(null);
  const tags = useCompletion(project, agents, homes, topicsOf(projects, root, project), me, handle);
  const [seed, setSeed] = useState<ComposerSeed>();

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    let live = true;
    const gone = (e: unknown) => e instanceof ApiError && e.status === 404;
    coworking
      .channels(project)
      .then((cs) => {
        if (!live) return;
        const found = cs.find((c) => c.name === channel) ?? null;
        setInfo(found);
        setMissing(found ? null : "channel");
      })
      .catch((e) => {
        if (live && gone(e)) setMissing("project");
      });
    coworking
      .threads(project, channel)
      .then((p) => {
        if (!live) return;
        setThreads(p.threads);
        setNext(p.next);
      })
      .catch((e) => {
        if (!live) return;
        if (gone(e)) setMissing("project");
        else setProblem(e instanceof ApiError ? e.message : defaultCopy.failure.internal());
      });
    return () => {
      live = false;
    };
  }, [project, channel, tick]);

  const more = () => {
    if (!next) return;
    coworking
      .threads(project, channel, next)
      .then((p) => {
        setThreads((t) => [...(t ?? []), ...p.threads]);
        setNext(p.next);
      })
      .catch(() => {});
  };

  const where = projectTitle(projects, project, workspaceName);
  // Said as the project's own page says it, and nothing more: no channel,
  // no threads, no composer.
  if (missing === "project") {
    const p = defaultCopy.projects;
    return (
      <Page
        title={p.missing(project === "" ? "~" : project)}
        lede={p.missingHint}
        back={{ label: p.allProjects, onClick: onOpenProjects }}
      >
        {null}
      </Page>
    );
  }
  if (missing === "channel") {
    return (
      <Page
        title={words.missingChannel}
        lede={words.missingChannelHint(channel)}
        back={{ label: where, onClick: onOpenProject }}
      >
        {null}
      </Page>
    );
  }
  return (
    <Page
      title={words.channel(channel)}
      meta={
        <span className="inline-flex flex-wrap gap-x-3">
          {info?.title && <span>{info.title}</span>}
          <span>{where}</span>
        </span>
      }
      lede={info?.purpose}
      back={{ label: where, onClick: onOpenProject }}
    >
      <Problem message={problem} />
      {info?.archived && <p className="text-aui-xs text-aui-muted">{words.archived}</p>}
      {threads && threads.length === 0 && (
        <p className="text-aui-sm text-aui-muted">{words.noThreads}</p>
      )}
      {threads && threads.length > 0 && (
        <ul className="flex flex-col" aria-label="Threads">
          {threads.map((t) => (
            <ThreadRow key={t.id} t={t} onOpen={() => onOpenThread(t.id)} />
          ))}
        </ul>
      )}
      {next && (
        <button type="button" className={`${button} self-start`} onClick={more}>
          {words.moreThreads}
        </button>
      )}
      {/* Only a channel known to be there is posted in. */}
      {canRun && info && !info.archived && !postsIn(projects, project) && (
        <p className="text-aui-xs text-aui-muted">{words.viewerOnly}</p>
      )}
      {canRun && info && !info.archived && postsIn(projects, project) && (
        <RoomComposer
          seed={seed}
          placeholder={words.startThread}
          tags={tags}
          plus={{
            // The draft's mentions are the + menu's agents.
            menu: (close, attach, leave, draft) => (
              <PlusMenu
                agents={tags.agents}
                {...mentionPicks(draft, tags.agents, setSeed)}
                skills={tags.skills}
                skillsFailed={tags.skillsFailed}
                onSkill={(name) => {
                  setSeed({ text: `/${name} `, mode: "prepend" });
                }}
                onAttach={attach}
                onClose={close}
                onLeave={leave}
              />
            ),
          }}
          onPost={(text, _pick, uploads) =>
            // Started in a channel, a thread is shared there: the channel is where it was posted.
            coworking
              .start({
                project,
                channel,
                text,
                uploads,
                visibility: "shared",
              })
              .then((p) => {
                onOpenThread(p.thread.id);
                return p;
              })
          }
        />
      )}
    </Page>
  );
}

/** A project's channels, each opening its page, and a form for a new one. */
export function ChannelsTab({
  project,
  canWrite,
  onOpenChannel,
}: {
  project: string;
  canWrite: boolean;
  onOpenChannel: (channel: string) => void;
}) {
  const [channels, setChannels] = useState<Channel[] | null>(null);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ name: "", title: "", purpose: "" });
  const [problem, setProblem] = useState<string | null>(null);
  const [reads, setReads] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: reads is the signal to read again
  useEffect(() => {
    let live = true;
    coworking
      .channels(project)
      .then((cs) => live && setChannels(cs))
      .catch(
        (e) =>
          live && setProblem(e instanceof ApiError ? e.message : defaultCopy.failure.internal()),
      );
    return () => {
      live = false;
    };
  }, [project, reads]);

  const create = () => {
    setProblem(null);
    coworking
      .createChannel(project, {
        name: draft.name.trim(),
        title: draft.title.trim() || undefined,
        purpose: draft.purpose.trim() || undefined,
      })
      .then(() => {
        setAdding(false);
        setDraft({ name: "", title: "", purpose: "" });
        setReads((n) => n + 1);
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.failed.channel));
  };

  return (
    <section className="flex flex-col gap-4" aria-label={words.channels}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-aui-sm text-aui-muted">{words.channelsLede}</p>
        {canWrite && !adding && (
          <button type="button" className={button} onClick={() => setAdding(true)}>
            {words.newChannel}
          </button>
        )}
      </div>
      <Problem message={problem} />
      {adding && (
        <form
          className="flex flex-col gap-3 rounded-aui border border-aui-border p-4"
          aria-label={words.channelForm.heading}
          onSubmit={(e) => {
            e.preventDefault();
            create();
          }}
        >
          <Label text={words.channelForm.name} hint={words.channelForm.nameHint}>
            <input
              className={field}
              value={draft.name}
              required
              maxLength={40}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </Label>
          <Label text={words.channelForm.title}>
            <input
              className={field}
              value={draft.title}
              maxLength={80}
              onChange={(e) => setDraft({ ...draft, title: e.target.value })}
            />
          </Label>
          <Label text={words.channelForm.purpose} hint={words.channelForm.purposeHint}>
            <input
              className={field}
              value={draft.purpose}
              maxLength={200}
              onChange={(e) => setDraft({ ...draft, purpose: e.target.value })}
            />
          </Label>
          <div className="flex gap-2">
            <button type="submit" className={primary} disabled={!draft.name.trim()}>
              {words.createChannel}
            </button>
            <button type="button" className={button} onClick={() => setAdding(false)}>
              {defaultCopy.projects.cancel}
            </button>
          </div>
        </form>
      )}
      {channels && (
        <ul className="flex flex-col" aria-label={words.channels}>
          {channels.map((c) => (
            <li key={c.name} className="flex items-center gap-3">
              <button
                type="button"
                onClick={() => onOpenChannel(c.name)}
                className="flex min-w-0 flex-1 flex-col rounded-aui px-3 py-2.5 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <span className="text-aui-sm font-medium text-aui-fg">
                  {words.channel(c.name)}
                  {c.title && <span className="pl-2 font-normal text-aui-muted">{c.title}</span>}
                  {c.archived && (
                    <span className="pl-2 font-normal text-aui-muted">{words.archived}</span>
                  )}
                </span>
                {c.purpose && (
                  <span className="truncate text-aui-xs text-aui-muted">{c.purpose}</span>
                )}
                <span className="text-aui-xs text-aui-muted">{answersShort(c.answers)}</span>
                {c.slack && (
                  <span className="text-aui-xs text-aui-muted">
                    {words.boundTo(c.slack.client, c.slack.channel)}
                  </span>
                )}
              </button>
              {canWrite && c.name !== "general" && !c.archived && !c.managed && (
                <button
                  type="button"
                  className={button}
                  onClick={() =>
                    coworking
                      .archiveChannel(project, c.name)
                      .then(() => setReads((n) => n + 1))
                      .catch((e) =>
                        setProblem(
                          e instanceof ApiError ? e.message : defaultCopy.failure.internal(),
                        ),
                      )
                  }
                >
                  {words.archiveChannel}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {channels && channels.length <= 1 && (
        <p className="text-aui-xs text-aui-muted">{words.noChannels}</p>
      )}
    </section>
  );
}
