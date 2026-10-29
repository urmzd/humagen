import { answerOfAction, answers as isAnswer } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import { BringBackIcon, ForkIcon, WebSurface } from "@humagen/render-web";
import {
  AgentMessage,
  MessageActionRow,
  messageActionButton,
  Thread as ThreadList,
  useQueue,
} from "@humagen/web";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { Activity } from "./Activity.tsx";
import { actedFor, citationOf, type Run, turnOf } from "./adapter.ts";
import {
  ApiError,
  api,
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
import { askedOf, copyFor, type Refusal, stillAsks } from "./asks.ts";
import { answersLine } from "./availability.ts";
import { Composer, type ComposerSeed } from "./Composer.tsx";
import { type ConversationForkActions, ConversationForkPanels } from "./ConversationForkPanels.tsx";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { type Completion, useCompletion } from "./completion.ts";
import { rememberSaid, saidFor } from "./conflicts.ts";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { ForkContext } from "./ForkConversation.tsx";
import { itemActions } from "./itemActions.tsx";
import { OLD_SYSTEM_SENDER } from "./legacy.ts";
import { MessageTime } from "./MessageTime.tsx";
import { OutcomeActions } from "./OutcomeActions.tsx";
import { PersonMark } from "./PersonMark.tsx";
import {
  emptySelection,
  PlusMenu,
  placeholderFor,
  type Selection,
  SelectionChips,
} from "./Plus.tsx";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { type ConversationPanel, type ConversationView, href } from "./route.ts";
import { ShareDialog } from "./Share.tsx";
import { useThreadSelection } from "./selection.ts";
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
 * The topics someone answers for in a project, along its chain: the
 * project's, then each project's above it, then the workspace's.
 */
export function topicsOf(
  projects: ProjectView[],
  root: ProjectView | null,
  name: string,
): string[] {
  const seen = new Set<string>();
  const add = (p: ProjectView | null | undefined) => {
    for (const e of p?.experts ?? []) for (const t of e.topics) seen.add(t);
  };
  let at = projects.find((p) => p.name === name);
  for (let steps = 0; at && steps < 5; steps++) {
    add(at);
    const parent = at.parent ?? "";
    at = parent === "" ? undefined : projects.find((p) => p.name === parent);
  }
  add(root);
  return [...seen].sort().slice(0, 16);
}

/**
 * The chat bar of a conversation or a channel: the box, with completion of
 * `@` and `$`, and once a message is posted, what it did. Nothing under it
 * says who answers: nobody does until a message tags an agent.
 */
function RoomComposer({
  thread,
  placeholder,
  tags,
  autoFocus,
  seed,
  plus,
  onPost,
}: {
  /** The conversation it posts in, whose first message, posted where it started, may have said something. */
  thread?: string;
  placeholder: string;
  tags: Completion;
  autoFocus?: boolean;
  seed?: ComposerSeed;
  plus?: Parameters<typeof Composer>[0]["plus"];
  onPost: (text: string) => Promise<ThreadPosted>;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  const [said, setSaid] = useState(() => (thread ? saidFor(thread) : ""));
  // People's messages never wait for an agent: each is posted as it is sent.
  const queue = useQueue({
    onStart: (m) => {
      setProblem(null);
      onPost(m.text)
        .then((p) => {
          const line = postedLine(p);
          setSaid(line);
          if (thread) rememberSaid(thread, line);
          queue.turnEnded("done");
        })
        .catch((e) => {
          setProblem(e instanceof ApiError ? e.message : words.failed.post);
          queue.reject();
        });
    },
    onInterrupt: () => {},
  });
  return (
    <div className="flex flex-col gap-2">
      <Problem message={problem} />
      <Composer
        placeholder={placeholder}
        queue={queue}
        autoFocus={autoFocus}
        seed={seed}
        tags={tags}
        plus={plus}
      />
      {said && (
        <p role="status" className="px-3 text-aui-xs text-aui-muted">
          {said}
        </p>
      )}
    </div>
  );
}

/** A person's message: who posted it, when, and what it says. */
function MessageView({
  m,
  focused,
  onReply,
  actions,
}: {
  m: ThreadMessage;
  focused: boolean;
  onReply?: () => void;
  actions?: ReactNode;
}) {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (focused) ref.current?.scrollIntoView({ block: "center" });
  }, [focused]);
  return (
    <article
      ref={ref}
      id={`message-${m.id}`}
      aria-label={`Message from ${m.name || m.by}`}
      className={`flex gap-3 rounded-aui ${focused ? "outline-2 outline-offset-4 outline-aui-accent" : ""}`}
    >
      <PersonMark name={m.name || m.by} />
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-baseline gap-x-2 text-aui-xs text-aui-muted">
          <span className="font-medium text-aui-fg">{m.name || m.by}</span>
          <MessageTime at={m.at} />
          {m.surface_id && <span>{words.fromSlack}</span>}
          {m.answers && <span>{words.answered}</span>}
        </p>
        <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">
          <Mentioned text={m.text} />
        </p>
        <MessageActionRow
          text={m.text}
          copyLabel={defaultCopy.conversation.copyMessage}
          onReply={onReply}
        >
          {actions}
        </MessageActionRow>
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
  return person === me ? words.retryFromInbox : defaultCopy.retryOnlyTheirs;
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
  routed,
  onAsk,
  onReply,
  onRedirect,
  actions,
  canStop = false,
}: {
  run: Run;
  canStop?: boolean;
  me: string;
  live: boolean;
  ask?: ThreadAsk;
  refused?: Refusal;
  /** That it answers because the conversation routes to it, not because it was tagged. */
  routed?: string;
  onAsk?: () => void;
  onReply?: () => void;
  onRedirect?: () => void;
  actions?: ReactNode;
}) {
  const { run, stop } = useRunStop(originalRun, canStop);
  const turn = useRunTurn(run);
  const drawn = useMemo(() => {
    const base = turn ?? turnOf(run);
    return run.citations !== undefined
      ? { ...base, citations: run.citations.map(citationOf) }
      : base;
  }, [turn, run]);
  const shown = useMemo(() => {
    const asked = askedOf(drawn, ask);
    return asked.failure
      ? { ...asked, failure: { ...asked.failure, retryNotOffered: retryInRoom(run, me) } }
      : asked;
  }, [drawn, run, me, ask]);
  return (
    <article
      id={`turn-${run.id}`}
      className="flex flex-col gap-2"
      aria-label={`Answered by ${run.agent}`}
    >
      {/* A run from before threads carries what was asked; one that answers a message does not repeat it. */}
      {!run.message && (
        <p className="humagen-glass max-w-[80%] self-end whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">
          <Mentioned text={run.text} />
        </p>
      )}
      {/* The answer names its agent; why it joined is said above it. */}
      {(routed || run.picked) && (
        <Checkpoint agent={run.agent}>{[routed, run.picked].filter(Boolean).join(". ")}</Checkpoint>
      )}
      <AgentTint name={run.agent}>
        <AgentMessage
          actions={actions}
          stop={stop}
          disclaimer={false}
          timestamp={<MessageTime at={run.created_at} />}
          turn={shown}
          copy={copyFor(ask)}
          itemActions={itemActions(run)}
          capabilities={{ regenerate: false }}
          onAskAgent={onAsk}
          onReply={onReply}
          onRedirect={onRedirect}
        />
      </AgentTint>
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
  forkActions,
  personalProject,
  focus,
  sharing = false,
  onSharing,
  view,
  onView,
  onOpenChannel,
  onOpenThread,
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
  onOpenChannel: (project: string, channel: string) => void;
  onChanged: () => void;
}) {
  const [page, setPage] = useState<ThreadPage>(initial);
  // Every turn read so far, oldest first. A refresh reads the newest page,
  // which moves on as the thread grows; the turns it no longer holds stay.
  const [turns, setTurns] = useState<ThreadTurn[]>(initial.turns);
  // The cursor for the turns before the oldest one read.
  const [before, setBefore] = useState<string | null>(initial.next);
  const [problem, setProblem] = useState<string | null>(null);
  const [localView, setLocalView] = useState<ConversationView>({});
  const [seed, setSeed] = useState<ComposerSeed>();
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
  // Why an answer to a run's question was not taken, by run: said under it
  // while that question still waits, and gone once anyone settles it.
  const [refusals, setRefusals] = useState<Record<string, Refusal>>({});

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
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
  }, [id, tick]);

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

  const th = page.thread;
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
  // What the + menu selected for the conversation, shown at once and saved on the thread.
  const { selection, select } = useThreadSelection(id, th, {
    saved: (t) => {
      setPage((p) => ({ ...p, thread: t }));
      onChanged();
    },
    reread: (p) => {
      setPage(p);
      setTurns((t) => mergeTurns(t, p.turns));
    },
    problem: setProblem,
  });
  const fixedWhy = hasRuns
    ? defaultCopy.threads.shareProjectFixed
    : th.surface
      ? defaultCopy.chat.projectFromSlack
      : th.visibility === "shared"
        ? defaultCopy.chat.projectShared
        : !started && !owner
          ? defaultCopy.chat.projectStarter
          : undefined;
  // A run the conversation's selection started says so, as the run recorded
  // it, so an older answer keeps its line whatever the selection becomes.
  const routedOf = (run: Run): string | undefined => {
    const c = defaultCopy.chat;
    // A message routed to one agent alone says only where it went, whatever the mode was.
    const alone = run.message
      ? turns.filter((t) => t.run?.message === run.message && t.run?.routed).length <= 1
      : false;
    if (run.routed === "all") return alone ? c.routed(run.agent) : c.routedAll(run.agent);
    // Among the selected agents, one that joined says where it went; `picked` says why it joined.
    if (run.routed === "join" || run.routed === "best_fit") return c.routed(run.agent);
    return undefined;
  };
  const shareButton = useRef<HTMLButtonElement>(null);
  // What sharing did: who was told, and who was not added and why.
  const [shareNote, setShareNote] = useState("");
  // The Share address of a conversation that can't be shared is the conversation's.
  useEffect(() => {
    if (sharing && !shareable) onSharing?.(false);
  }, [sharing, shareable, onSharing]);
  const answering = th.participants.filter((p) => p.kind === "agent").map((p) => p.name);
  const title =
    th.title ||
    turns.find((t) => t.announcement)?.announcement?.subject ||
    turns.find((t) => t.message)?.message?.text ||
    turns[0]?.run?.text ||
    words.channel(th.channel);
  const last = turns[turns.length - 1];
  // A run a message in the room answers asked someone something: its steps
  // and its question are read from its journal, though turns came after it.
  const asked = new Set(turns.flatMap((t) => (t.message?.answers ? [t.message.answers.run] : [])));
  // A run waiting on a person, such as an expert's answer, is shown still.
  const busy = turns.some((t) => t.run && isWorking(t.run.status));

  const onAction = (a: UiAction) => {
    if (a.id === ActionId.openRun) return openPanel("activity", { run: a.payload.target });
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

  return (
    <section className="flex min-h-0 min-w-0 flex-1">
      <ConversationPage
        title={title}
        project={projectTitle(projects, th.project, workspaceName, personalProject)}
        private={th.visibility === "private"}
        agents={answering}
        people={th.participants.filter((p) => p.kind === "person").map((p) => p.name)}
        location={
          <>
            {th.visibility !== "private" && (
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
            {th.surface && <span className="ml-3">{words.inSlack}</span>}
          </>
        }
        projectRef={shareButton}
        onProject={
          shareable
            ? () => onSharing?.(!sharing)
            : th.visibility === "shared"
              ? () => onOpenChannel(th.project, th.channel)
              : undefined
        }
        onPanel={(next) => openPanel(next)}
        onArtifact={(id) => openPanel("deliverables", { artifact: id })}
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
                  const lines = [words.sharedTold(told), words.notAdded(cannot, other)].filter(
                    Boolean,
                  );
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
        {posts && (
          <ConversationForkPanels
            thread={th}
            runs={turns.flatMap((turn) => (turn.run ? [turn.run] : []))}
            actions={forkActions}
          />
        )}
        <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
          <ThreadList className="mx-auto w-full max-w-4xl flex-1 px-4" busy={busy} label="Thread">
            {before && (
              <button type="button" className={`${button} self-center`} onClick={loadEarlier}>
                {words.olderTurns}
              </button>
            )}
            {turns.map((t) =>
              t.announcement ? (
                <AnnouncementView key={`a:${t.announcement.id}`} a={t.announcement} />
              ) : t.message ? (
                <MessageView
                  key={t.message.id}
                  m={t.message}
                  focused={focus === t.message.id}
                  actions={
                    posts && forkActions?.onFork ? (
                      <button
                        type="button"
                        className={messageActionButton}
                        aria-label={defaultCopy.forking.fromHere}
                        title={defaultCopy.forking.fromHere}
                        onClick={() => forkActions.onFork?.(t.message!.id)}
                      >
                        <ForkIcon size={16} />
                      </button>
                    ) : undefined
                  }
                  onReply={
                    posts
                      ? () =>
                          setSeed({
                            text: `Replying to [this message](${href({ page: "conversation", id, message: t.message?.id })}): `,
                            mode: "prepend",
                          })
                      : undefined
                  }
                />
              ) : t.run ? (
                <AnswerView
                  key={t.run.id}
                  run={t.run}
                  canStop={posts}
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
                        {posts && th.fork && forkActions?.onBringBack && (
                          <button
                            type="button"
                            className={messageActionButton}
                            aria-label={defaultCopy.forking.bringBack}
                            title={defaultCopy.forking.bringBack}
                            onClick={() => forkActions.onBringBack?.(t.run!.id)}
                          >
                            <BringBackIcon size={16} />
                          </button>
                        )}
                      </>
                    ) : undefined
                  }
                  routed={routedOf(t.run)}
                  onAsk={
                    posts
                      ? () => setSeed({ text: `@${t.run?.agent} `, mode: "prepend" })
                      : undefined
                  }
                  onReply={
                    posts
                      ? () =>
                          setSeed({
                            text: `@${t.run?.agent} following up on [this answer](${href({ page: "conversation", id, panel: "activity", run: t.run?.id })}): `,
                            mode: "prepend",
                          })
                      : undefined
                  }
                  onRedirect={
                    posts
                      ? () =>
                          setSeed({
                            text: defaultCopy.conversation.redirectPrompt(
                              t.run!.agent,
                              href({ page: "conversation", id, panel: "activity", run: t.run!.id }),
                            ),
                            mode: "prepend",
                          })
                      : undefined
                  }
                  me={me}
                  ask={t.ask}
                  refused={refusals[t.run.id]}
                  live={
                    t === last ||
                    asked.has(t.run.id) ||
                    (t.run.status !== "completed" && t.run.status !== "failed")
                  }
                />
              ) : null,
            )}
          </ThreadList>
        </WebSurface>
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-2 px-4 pb-4">
          <Problem message={problem} />
          {canRun && !posts && <p className="text-aui-xs text-aui-muted">{words.viewerOnly}</p>}
          {posts && (
            <RoomComposer
              thread={id}
              seed={seed}
              placeholder={
                selection.agents.length > 0
                  ? placeholderFor(selection)
                  : th.visibility === "private"
                    ? words.message
                    : words.channel(th.channel)
              }
              tags={tags}
              plus={{
                menu: (close, attach, leave) => (
                  <PlusMenu
                    onLeave={leave}
                    agents={tags.agents}
                    skills={tags.skills}
                    skillsFailed={tags.skillsFailed}
                    onSkill={(name) => {
                      setSeed({ text: `$${name} `, mode: "prepend" });
                    }}
                    selection={selection}
                    onChange={select}
                    onAttach={attach}
                    onClose={close}
                    projects={projects.filter((p) => postsIn(projects, p.name))}
                    personalProject={personalProject}
                    kinds={root?.kinds}
                    projectFixed={fixedWhy}
                  />
                ),
                chips: (
                  <SelectionChips
                    personalProject={personalProject}
                    selection={selection}
                    projects={projects}
                    workspace={workspaceName}
                    onChange={select}
                    projectFixed
                  />
                ),
              }}
              autoFocus={sharing ? false : undefined}
              onPost={(text) =>
                // It follows the newest message, so a run it starts reads the conversation so far.
                coworking
                  .post(id, { text })
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
      </ConversationPage>
      <ConversationInfo
        panel={shownView.panel}
        contextTab={shownView.contextTab}
        onContextTab={(contextTab) => openPanel("context", { contextTab })}
        thread={th}
        project={projectTitle(projects, th.project, workspaceName, personalProject)}
        onClose={() => openPanel()}
      />
      <Presence when={shownView.panel === "deliverables"} ms={220}>
        {(leaving) => (
          <Deliverables
            open={!leaving}
            promotion={
              artifactActions
                ? { project: th.project, shared: th.visibility === "shared", ...artifactActions }
                : undefined
            }
            thread={id}
            view={shownView}
            onView={(next) => openPanel("deliverables", next)}
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
      <Presence when={shownView.panel === "activity"} ms={220}>
        {(leaving) => (
          <Activity
            leaving={leaving}
            turns={turns.flatMap((turn) => (turn.run ? [turn.run] : []))}
            stamp={turns.map((turn) => `${turn.run?.id}:${turn.run?.status}`).join("|")}
            focus={shownView.run ?? null}
            onFocus={(run) => openPanel("activity", run ? { run } : {})}
            onClose={() => openPanel()}
          />
        )}
      </Presence>
    </section>
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
  // The agents the + menu picked for the thread this channel's chat bar starts.
  const [picked, setPicked] = useState<Selection>(emptySelection);
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
          placeholder={picked.agents.length > 0 ? placeholderFor(picked) : words.startThread}
          tags={tags}
          plus={{
            menu: (close, attach, leave) => (
              <PlusMenu
                agents={tags.agents}
                skills={tags.skills}
                skillsFailed={tags.skillsFailed}
                onSkill={(name) => {
                  setSeed({ text: `$${name} `, mode: "prepend" });
                }}
                selection={picked}
                onChange={setPicked}
                onAttach={attach}
                onClose={close}
                onLeave={leave}
              />
            ),
            chips: <SelectionChips selection={picked} onChange={setPicked} />,
          }}
          onPost={(text) =>
            // Started in a channel, a thread is shared there: the channel is where it was posted.
            coworking
              .start({
                project,
                channel,
                text,
                visibility: "shared",
                ...(picked.agents.length > 0 ? { agents: picked.agents, mode: picked.mode } : {}),
              })
              .then((p) => {
                rememberSaid(p.thread.id, postedLine(p));
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
