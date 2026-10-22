import { answerOfAction, answers as isAnswer } from "@warp/adapter/question";
import { defaultCopy } from "@warp/components";
import { ActionId, type UiAction } from "@warp/core";
import { WebSurface } from "@warp/render-web";
import { AgentMessage, Thread as ThreadList, useQueue } from "@warp/web";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Run } from "./adapter.ts";
import {
  ApiError,
  api,
  type Channel,
  coworking,
  type DirectoryPerson,
  type ProjectView,
  type Revision,
  type ThreadAnswers,
  type ThreadMessage,
  type ThreadPage,
  type ThreadPosted,
  type ThreadSummary,
  type ThreadTurn,
} from "./api.ts";
import { Composer } from "./Composer.tsx";
import { itemActions } from "./itemActions.tsx";
import { useRunTurn } from "./Runs.tsx";
import { type TagOption, tagOptions } from "./tags.ts";
import { AgentMark, AgentTint, ago, button, cause, field, Label, Page, Problem, primary } from "./ui.tsx";

/**
 * Channels and shared threads: where people and agents work together in a
 * project. A person's message is posted as them and starts no run of its
 * own; what it starts is the platform's to decide from its tags, and the
 * thread says so.
 */

const words = defaultCopy.threads;

/** A composer control that stays pressed: Post Only, Private. */
const toggle =
  "rounded-aui px-1.5 py-1 text-aui-sm text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-fg aria-pressed:underline";

/** The title of a project, as a thread's meta line names it. */
function projectTitle(projects: ProjectView[], name: string, workspace: string): string {
  if (name === "") return workspace || defaultCopy.projects.workspace;
  const p = projects.find((x) => x.name === name);
  return p?.title || name;
}

/** A person's initial, in a circle: who posted, without a picture to fetch. */
export function PersonMark({ name, size = 28 }: { name: string; size?: number }) {
  return (
    <span
      aria-hidden
      className="inline-flex shrink-0 items-center justify-center rounded-full bg-aui-hover font-medium text-aui-fg"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.42) }}
    >
      {(name.trim()[0] ?? "?").toUpperCase()}
    </span>
  );
}

/** Who answers an untagged message here, in words, under the composer. */
export function whoAnswers(a: ThreadAnswers | undefined): string {
  if (!a || !a.agent) return words.nobody;
  if (a.unavailable) return words.stale(a.agent, a.unavailable);
  return words.answers(a.agent);
}

/** Who answers an untagged message in a channel, as its row in a list says it. */
function answersShort(a: ThreadAnswers | undefined): string {
  if (!a || !a.agent) return defaultCopy.auto.nobody;
  return defaultCopy.auto.answers(a.agent);
}

/** What a posted message did, in one line: who it was sent to, and why a tag started nothing. */
export function postedLine(p: ThreadPosted): string {
  const parts: string[] = [];
  if (p.notified.length > 0) parts.push(words.notified(p.notified));
  for (const r of p.refused) parts.push(words.refused(r.kind === "sme" ? `@sme${r.name ? `:${r.name}` : ""}` : `@${r.name}`, r.refused ?? ""));
  if (p.runs.length === 0 && p.reason && p.refused.length === 0 && p.notified.length === 0) parts.push(p.reason);
  return parts.join(" ");
}

/**
 * The topics someone answers for in a project, along its chain: the
 * project's, then each project's above it, then the workspace's.
 */
export function topicsOf(projects: ProjectView[], root: ProjectView | null, name: string): string[] {
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
 * What a message in a project can tag: @auto, the agents that can answer
 * there, people as their names are typed, and experts by topic.
 */
function useTags(project: string, agents: Revision[], topics: string[]): { label: string; options: TagOption[]; onQuery: (q: string) => void } {
  const [names, setNames] = useState<{ all: boolean; names: string[] } | null>(null);
  const [people, setPeople] = useState<DirectoryPerson[]>([]);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => {
    let live = true;
    coworking
      .available(project)
      .then((a) => live && setNames(a.available))
      .catch(() => live && setNames(null));
    return () => {
      live = false;
    };
  }, [project]);
  const onQuery = useCallback((q: string) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      coworking
        .people(q)
        .then(setPeople)
        .catch(() => setPeople([]));
    }, 150);
  }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const options = useMemo(() => {
    const here = agents
      .filter((a) => !names || names.all || names.names.includes(a.spec.name))
      .map((a) => ({ name: a.spec.name, description: a.spec.description }));
    return tagOptions(here, people, topics, { auto: defaultCopy.auto.description, experts: words.experts });
  }, [agents, names, people, topics]);
  return { label: words.typeahead, options, onQuery };
}

/**
 * The composer of a thread or a channel: the box, Post Only, and who answers.
 * A channel's also offers Private, which starts a thread only the people in
 * it read.
 */
function RoomComposer({
  placeholder,
  answers,
  tags,
  autoFocus,
  canPrivate = false,
  onPost,
}: {
  placeholder: string;
  answers: ThreadAnswers | undefined;
  tags: ReturnType<typeof useTags>;
  autoFocus?: boolean;
  canPrivate?: boolean;
  onPost: (text: string, postOnly: boolean, secret: boolean) => Promise<ThreadPosted>;
}) {
  const [postOnly, setPostOnly] = useState(false);
  const [secret, setSecret] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [said, setSaid] = useState("");
  // People's messages never wait for an agent: each is posted as it is sent.
  const queue = useQueue({
    onStart: (m) => {
      setProblem(null);
      onPost(m.text, postOnly, secret)
        .then((p) => {
          setSaid(postedLine(p));
          queue.turnEnded("done");
        })
        .catch((e) => {
          setProblem(e instanceof ApiError ? e.message : words.failed.post);
          queue.turnEnded("failed");
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
        tags={tags}
        trailing={
          <span className="flex items-center gap-1">
            {canPrivate && (
              <button
                type="button"
                aria-pressed={secret}
                title={words.privateHint}
                onClick={() => setSecret(!secret)}
                className={toggle}
              >
                {words.startPrivate}
              </button>
            )}
            <button
              type="button"
              aria-pressed={postOnly}
              title={words.postOnlyHint}
              onClick={() => setPostOnly(!postOnly)}
              className={toggle}
            >
              {words.postOnly}
            </button>
          </span>
        }
      />
      <p className="px-3 text-aui-xs text-aui-muted" aria-live="polite">
        {postOnly ? words.postOnlyHint : whoAnswers(answers)}
      </p>
      {secret && <p className="px-3 text-aui-xs text-aui-muted">{words.privateHint}</p>}
      {said && (
        <p role="status" className="px-3 text-aui-xs text-aui-muted">
          {said}
        </p>
      )}
    </div>
  );
}

/** A person's message: who posted it, when, and what it says. */
function MessageView({ m, focused }: { m: ThreadMessage; focused: boolean }) {
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
          <time dateTime={m.at}>{ago(m.at)}</time>
          {m.answers && <span>{words.answered}</span>}
        </p>
        <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{m.text}</p>
      </div>
    </article>
  );
}

/** An agent's turn: what it answered, as the conversation view shows it. */
function AnswerView({ run, live }: { run: Run; live: boolean }) {
  const turn = useRunTurn(live ? run : undefined);
  const shown = useMemo(
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
        failure: run.status === "failed" ? { kind: "internal" as const, message: cause(run.error) || "The run failed.", reference: run.id } : undefined,
      },
    [turn, run],
  );
  return (
    <article id={`turn-${run.id}`} className="flex flex-col gap-2" aria-label={`Answered by ${run.agent}`}>
      {/* A run from before threads carries what was asked; one that answers a message does not repeat it. */}
      {!run.message && (
        <p className="warp-glass max-w-[80%] self-end whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">{run.text}</p>
      )}
      {/* The answer names its agent; why @auto chose it is said above it. */}
      {run.picked && <p className="pl-11 text-aui-xs text-aui-muted">{run.picked}</p>}
      <AgentTint name={run.agent}>
        <AgentMessage turn={shown} itemActions={itemActions(run)} />
      </AgentTint>
    </article>
  );
}

/**
 * A shared thread: its people's messages and its agents' answers in the
 * order they were made, who takes part, and a composer that tags with @.
 */
export function ThreadRoom({
  id,
  initial,
  tick,
  agents,
  projects,
  root,
  workspaceName,
  canRun,
  focus,
  onOpenChannel,
  onChanged,
}: {
  id: string;
  /** The page already read, so the room shows at once. */
  initial: ThreadPage;
  tick: number;
  agents: Revision[];
  projects: ProjectView[];
  root: ProjectView | null;
  workspaceName: string;
  canRun: boolean;
  /** A message to open at, as `?message=` names it. */
  focus?: string;
  onOpenChannel: (project: string, channel: string) => void;
  onChanged: () => void;
}) {
  const [page, setPage] = useState<ThreadPage>(initial);
  const [earlier, setEarlier] = useState<ThreadTurn[]>([]);
  const [before, setBefore] = useState<string | null>(initial.next);
  const [problem, setProblem] = useState<string | null>(null);

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    let live = true;
    coworking
      .thread(id)
      .then((p) => {
        if (!live) return;
        setPage(p);
        if (earlier.length === 0) setBefore(p.next);
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
        setEarlier((e) => [...p.turns, ...e]);
        setBefore(p.next);
      })
      .catch(() => setProblem(defaultCopy.failure.internal()));
  };

  const th = page.thread;
  const turns = [...earlier, ...page.turns];
  const tags = useTags(th.project, agents, topicsOf(projects, root, th.project));
  const people = th.participants.filter((p) => p.kind === "person");
  const answering = th.participants.filter((p) => p.kind === "agent").map((p) => p.name);
  const title = th.title || turns.find((t) => t.message)?.message?.text || turns[0]?.run?.text || words.channel(th.channel);
  const last = turns[turns.length - 1];
  const busy = turns.some((t) => t.run && t.run.status !== "completed" && t.run.status !== "failed" && t.run.status !== "stopped");

  const onAction = (a: UiAction) => {
    if (!isAnswer(a)) return;
    const answer = answerOfAction(a);
    if (!answer) return;
    setProblem(null);
    // A reply in words is said in the room, as the person's message, and
    // recorded as the question's answer. A choice or an approval is a click.
    const text = a.id === ActionId.reply ? a.payload.text?.trim() : undefined;
    const run = turns.find((t) => t.run && t.run.id === answer.run)?.run;
    const done =
      text && run?.question
        ? coworking.post(id, { text, answers: { run: run.id, question: run.question.id } })
        : api.answer(answer);
    done
      .then(onChanged)
      .catch((e) => setProblem(e instanceof ApiError && e.status === 403 ? defaultCopy.noPermission : e instanceof ApiError ? e.message : defaultCopy.failure.internal()));
  };

  return (
    <section className="flex min-h-0 flex-1 flex-col">
      <header className="relative z-10 mx-auto flex w-full max-w-4xl flex-wrap items-center gap-x-4 gap-y-1 px-4 pt-4 pb-2">
        <h1 className="min-w-0 flex-1 truncate text-aui-lg font-semibold text-aui-fg">{title}</h1>
        <p className="flex shrink-0 flex-wrap items-center gap-x-3 text-aui-xs text-aui-muted">
          {th.visibility === "private" ? (
            <span>{words.private}</span>
          ) : (
            <button
              type="button"
              onClick={() => onOpenChannel(th.project, th.channel)}
              className="rounded-aui hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              {words.sharedIn(projectTitle(projects, th.project, workspaceName), th.channel)}
            </button>
          )}
          <span title={people.map((p) => p.name).join(", ")}>{words.participants(people.length)}</span>
          {answering.map((a) => (
            <span key={a} className="inline-flex items-center gap-1">
              <AgentMark name={a} size={14} />
              {a}
            </span>
          ))}
        </p>
      </header>
      <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
        <ThreadList className="mx-auto w-full max-w-4xl flex-1 px-4" busy={busy} label="Thread">
          {before && (
            <button type="button" className={`${button} self-center`} onClick={loadEarlier}>
              {words.olderTurns}
            </button>
          )}
          {turns.map((t) =>
            t.message ? (
              <MessageView key={t.message.id} m={t.message} focused={focus === t.message.id} />
            ) : t.run ? (
              <AnswerView key={t.run.id} run={t.run} live={t === last || (t.run.status !== "completed" && t.run.status !== "failed")} />
            ) : null,
          )}
        </ThreadList>
      </WebSurface>
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-2 px-4 pb-4">
        <Problem message={problem} />
        {canRun && (
          <RoomComposer
            placeholder={words.channel(th.channel)}
            answers={page.answers}
            tags={tags}
            onPost={(text, postOnly) =>
              coworking.post(id, { text, post_only: postOnly }).then((p) => {
                onChanged();
                return p;
              })
            }
          />
        )}
      </div>
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
          <span className="block text-aui-xs text-aui-muted">
            {t.visibility === "private" ? `${words.private} · ${ago(t.updated_at)}` : ago(t.updated_at)}
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
  tick,
  agents,
  projects,
  root,
  workspaceName,
  canRun,
  onOpenProject,
  onOpenThread,
}: {
  project: string;
  channel: string;
  tick: number;
  agents: Revision[];
  projects: ProjectView[];
  root: ProjectView | null;
  workspaceName: string;
  canRun: boolean;
  onOpenProject: () => void;
  onOpenThread: (id: string) => void;
}) {
  const [info, setInfo] = useState<Channel | null>(null);
  const [threads, setThreads] = useState<ThreadSummary[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const tags = useTags(project, agents, topicsOf(projects, root, project));

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the refresh signal
  useEffect(() => {
    let live = true;
    coworking
      .channels(project)
      .then((cs) => live && setInfo(cs.find((c) => c.name === channel) ?? null))
      .catch(() => {});
    coworking
      .threads(project, channel)
      .then((p) => {
        if (!live) return;
        setThreads(p.threads);
        setNext(p.next);
      })
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : defaultCopy.failure.internal()));
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
  return (
    <Page
      title={words.channel(channel)}
      meta={<span>{info?.title ? `${info.title} · ${where}` : where}</span>}
      lede={info?.purpose}
      back={{ label: where, onClick: onOpenProject }}
    >
      <Problem message={problem} />
      {info?.archived && <p className="text-aui-xs text-aui-muted">{words.archived}</p>}
      {threads && threads.length === 0 && <p className="text-aui-sm text-aui-muted">{words.noThreads}</p>}
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
      {canRun && !info?.archived && (
        <RoomComposer
          placeholder={words.startThread}
          answers={info?.answers}
          tags={tags}
          canPrivate
          onPost={(text, postOnly, secret) =>
            coworking.start({ project, channel, text, post_only: postOnly, visibility: secret ? "private" : "shared" }).then((p) => {
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
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : defaultCopy.failure.internal()));
    return () => {
      live = false;
    };
  }, [project, reads]);

  const create = () => {
    setProblem(null);
    coworking
      .createChannel(project, { name: draft.name.trim(), title: draft.title.trim() || undefined, purpose: draft.purpose.trim() || undefined })
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
            <input className={field} value={draft.name} required maxLength={40} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
          </Label>
          <Label text={words.channelForm.title}>
            <input className={field} value={draft.title} maxLength={80} onChange={(e) => setDraft({ ...draft, title: e.target.value })} />
          </Label>
          <Label text={words.channelForm.purpose} hint={words.channelForm.purposeHint}>
            <input className={field} value={draft.purpose} maxLength={200} onChange={(e) => setDraft({ ...draft, purpose: e.target.value })} />
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
                  {c.archived && <span className="pl-2 font-normal text-aui-muted">{words.archived}</span>}
                </span>
                {c.purpose && <span className="truncate text-aui-xs text-aui-muted">{c.purpose}</span>}
                <span className="text-aui-xs text-aui-muted">{answersShort(c.answers)}</span>
                {c.slack && <span className="text-aui-xs text-aui-muted">{words.boundTo(c.slack.client, c.slack.channel)}</span>}
              </button>
              {canWrite && c.name !== "general" && !c.archived && !c.managed && (
                <button
                  type="button"
                  className={button}
                  onClick={() =>
                    coworking
                      .archiveChannel(project, c.name)
                      .then(() => setReads((n) => n + 1))
                      .catch((e) => setProblem(e instanceof ApiError ? e.message : defaultCopy.failure.internal()))
                  }
                >
                  {words.archiveChannel}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {channels && channels.length <= 1 && <p className="text-aui-xs text-aui-muted">{words.noChannels}</p>}
    </section>
  );
}
