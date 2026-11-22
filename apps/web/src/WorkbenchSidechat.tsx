import { answerOfAction, answers as isAnswer } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import {
  CheckIcon,
  ChevronRightIcon,
  ExternalLinkIcon,
  MessageSquareIcon,
  PlusIcon,
  WebSurface,
} from "@humagen/render-web";
import { AgentMessage, RetryCallContext, Thread as ThreadList, useQueue } from "@humagen/web";
import { useEffect, useMemo, useRef, useState } from "react";
import type { ThreadForkSummary } from "../../../packages/client/src/generated/types.ts";
import { citationOf, type Run, turnOf } from "./adapter.ts";
import { ApiError, api, coworking, type ThreadPage, type ThreadTurn } from "./api.ts";
import { askedOf, asksOf, copyFor } from "./asks.ts";
import { agentShown } from "./branding.ts";
import { Composer } from "./Composer.tsx";
import { useHumanActivity } from "./humanActivity.tsx";
import { itemActions } from "./itemActions.tsx";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { followRoom } from "./roomFollow.ts";
import { href } from "./route.ts";
import {
  conclusionRecords,
  forkPoint,
  isSideChatOf,
  sideChatRouting,
  sideChats,
  sideChatTitle,
} from "./sidechat.ts";
import { mergeTurns } from "./turns.ts";
import {
  AgentTint,
  ago,
  button,
  isWorking,
  Mentioned,
  menuRow,
  Problem,
  SkeletonRows,
} from "./ui.tsx";
import type { WorkbenchTabProps } from "./workbench.ts";

const words = defaultCopy.workbenchSidechat;

// A row of the list, at the workbench's size: nothing in a tab is set under 14px.
const row = menuRow().replace("text-aui-sm", "text-aui-base");
// A text control of the tab's own header.
const quiet =
  "inline-flex min-h-9 pointer-coarse:min-h-11 items-center gap-1.5 rounded-aui px-2 text-aui-base text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/** A side chat as the tab holds it: its page, and every turn read so far. */
interface Chat {
  id: string;
  page?: ThreadPage;
  turns: ThreadTurn[];
  /** It could not be read, or is not a side chat of this session. */
  missing?: boolean;
}

/**
 * The Side Chat tab: a question about the session, asked beside it. A side
 * chat is a private fork of the session, so it is given the session's
 * history as read-only context and answers in a conversation of its own:
 * nothing is posted in the session and a working agent is not interrupted.
 *
 * With nothing selected the tab is the box and the reader's earlier side
 * chats. The first message starts the fork and selects it (`?benchRef=` is
 * its id), so it opens again after a reload, and it is a conversation like
 * any other at `/messages/{id}`. Add to Message on an answer records it as
 * done by hand: it goes with the reader's next message in the session, and
 * nothing is sent before they send that.
 */
export function WorkbenchSidechat({
  thread,
  runs,
  canPost,
  selection,
  onSelection,
  record,
}: WorkbenchTabProps) {
  const [chat, setChat] = useState<Chat>();
  const [earlier, setEarlier] = useState<ThreadForkSummary[]>();
  const [problem, setProblem] = useState<string | null>(null);
  const [heard, setHeard] = useState(0);
  // The side chat this tab started itself, which the address is then told of:
  // its turns are already here and are not read over.
  const held = useRef<string | undefined>(undefined);

  // The reader's earlier side chats of the session, read when none is open.
  useEffect(() => {
    if (selection) return;
    let live = true;
    setProblem(null);
    sideChats(thread).then(
      (found) => {
        if (live) setEarlier(found);
      },
      () => {
        if (!live) return;
        setEarlier([]);
        setProblem(words.failed.list);
      },
    );
    return () => {
      live = false;
    };
  }, [thread, selection]);

  // The open side chat, read when it is selected and again when its room says something happened.
  // biome-ignore lint/correctness/useExhaustiveDependencies: heard is the refresh signal
  useEffect(() => {
    if (!selection) {
      held.current = undefined;
      setChat(undefined);
      return;
    }
    let live = true;
    if (held.current !== selection) {
      held.current = selection;
      setChat({ id: selection, turns: [] });
    }
    coworking.thread(selection).then(
      (page) => {
        if (!live) return;
        setChat((now) =>
          isSideChatOf(page.thread, thread)
            ? {
                id: selection,
                page,
                turns: mergeTurns(now?.id === selection ? now.turns : [], page.turns),
              }
            : { id: selection, turns: [], missing: true },
        );
      },
      () => {
        // A refresh that fails keeps what is on view; a first read that fails has nothing to show.
        if (live)
          setChat((now) =>
            now?.id === selection && now.page ? now : { id: selection, turns: [], missing: true },
          );
      },
    );
    return () => {
      live = false;
    };
  }, [selection, thread, heard]);
  useEffect(
    () => (selection ? followRoom(selection, () => setHeard((n) => n + 1)) : undefined),
    [selection],
  );

  const turns = chat?.turns ?? [];
  const chatRuns = useMemo(() => turns.flatMap((t) => (t.run ? [t.run] : [])), [turns]);
  const through = forkPoint(runs);
  const [waiting, setWaiting] = useState<string[]>([]);
  const waitingNow = useRef<string[]>([]);
  const queue = useQueue({
    onStart: (m) => {
      setProblem(null);
      const send = async () => {
        let id = held.current;
        if (!id) {
          if (!through) throw new Error(words.failed.start);
          // The session's own agents answer, whoever answered in it last.
          const routing = sideChatRouting(
            await coworking.thread(thread).catch(() => undefined),
            runs,
          );
          const forked = await coworking
            .fork(thread, { through, title: sideChatTitle(m.text), ...routing })
            .catch((error: unknown) => {
              throw new Error(error instanceof ApiError ? error.message : words.failed.start);
            });
          // The fork is saved whatever the post does next, so it is selected at once.
          id = forked.id;
          held.current = id;
          setChat({ id, turns: [] });
          onSelection(id);
        }
        const at = id;
        const posted = await coworking.post(at, { text: m.text, ...m.pick, uploads: m.uploads });
        setChat((now) => ({
          id: at,
          page: now?.id === at ? now.page : undefined,
          turns: mergeTurns(now?.id === at ? now.turns : [], [
            { kind: "message", message: posted.message },
            ...posted.runs.map((run) => ({ kind: "run" as const, run })),
          ]),
        }));
        return posted;
      };
      send().then(
        (posted) => {
          const ids = posted.runs.filter((run) => isWorking(run.status)).map((run) => run.id);
          waitingNow.current = ids;
          setWaiting(ids);
          if (!ids.length) queue.turnEnded("done");
        },
        (error: unknown) => {
          setProblem(
            error instanceof ApiError || error instanceof Error ? error.message : words.failed.post,
          );
          queue.reject();
        },
      );
    },
    onInterrupt: () => {
      void Promise.all(waitingNow.current.map((id) => api.stopRun(id))).catch(() =>
        setProblem(words.failed.stop),
      );
    },
  });
  // The box follows the side chat's own answers: a message sent while one is
  // written waits behind it, as in the thread.
  useEffect(() => {
    if (waiting.length) {
      // A run that is no longer on view belongs to a side chat the reader left.
      const current = waiting.map((id) => chatRuns.find((run) => run.id === id));
      if (current.every((run) => !run || !isWorking(run.status))) {
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
      const active = chatRuns.filter((run) => isWorking(run.status));
      if (active.length) {
        waitingNow.current = active.map((run) => run.id);
        setWaiting(waitingNow.current);
        queue.adopt(active[0]?.text || words.thread);
      }
    }
  }, [chatRuns, waiting, queue.running, queue.adopt, queue.turnEnded]);

  // A question or an approval the side chat's agent raised is answered here, in the side chat.
  const onAction = (a: UiAction) => {
    if (!chat) return;
    // Retry asks a failed answer's question again, here.
    if (a.id === ActionId.retry) {
      const failed = chatRuns.find((run) => run.id === a.payload.turnId);
      if (!failed || !canPost) return;
      setProblem(null);
      coworking.post(chat.id, { text: failed.text }).then(
        () => setHeard((n) => n + 1),
        (error: unknown) =>
          setProblem(error instanceof ApiError ? error.message : words.failed.post),
      );
      return;
    }
    if (!isAnswer(a)) return;
    const answer = answerOfAction(a);
    if (!answer) return;
    setProblem(null);
    const text = answer.body.action === "reply" ? answer.body.text : undefined;
    const done = text
      ? coworking.post(chat.id, { text, answers: { run: answer.run, question: answer.question } })
      : api.answer(answer);
    done.then(
      () => setHeard((n) => n + 1),
      (error: unknown) => setProblem(error instanceof ApiError ? error.message : words.failed.post),
    );
  };

  const box = canPost && !chat?.missing && (chat || through) && (
    <div className="flex flex-col gap-2 px-4 pb-4" data-testid="sidechat-composer">
      <Problem message={problem} />
      <Composer placeholder={words.placeholder} emptyText={words.placeholder} queue={queue} />
    </div>
  );

  if (!selection && !chat)
    return (
      <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-testid="sidechat">
        <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-4 py-6">
          <h3 className="humagen-display text-aui-lg font-medium text-aui-fg">
            {!canPost ? words.readOnly : through ? words.empty : words.nothing}
          </h3>
          {!box && <Problem message={problem} />}
          {earlier === undefined ? (
            <SkeletonRows count={3} />
          ) : (
            earlier.length > 0 && (
              <section aria-label={words.earlier} className="flex min-w-0 flex-col gap-1">
                <h4 className="px-2.5 text-aui-base font-medium text-aui-muted">{words.earlier}</h4>
                <ul className="flex flex-col">
                  {earlier.map((side) => (
                    <li key={side.id}>
                      <button type="button" className={row} onClick={() => onSelection(side.id)}>
                        <MessageSquareIcon size={16} aria-hidden />
                        <span className="min-w-0 flex-1 truncate">
                          {side.title || words.untitled}
                        </span>
                        <span className="shrink-0 text-aui-muted">{ago(side.updated_at)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </section>
            )
          )}
        </div>
        {box}
      </div>
    );

  const page = chat?.page;
  const forkedAt = page?.thread.fork?.at;
  const working = chatRuns.some((run) => isWorking(run.status));
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-testid="sidechat">
      {/* The side chat's own row: back to the list, a new one, and the conversation it is. */}
      {!chat?.missing && (
        <div className="flex min-w-0 flex-wrap items-center gap-x-1 gap-y-1 border-aui-border border-b px-2 py-1.5">
          <button type="button" className={quiet} onClick={() => onSelection()}>
            <ChevronRightIcon size={16} aria-hidden className="rotate-180" />
            {words.all}
          </button>
          <span className="min-w-0 flex-1" />
          <button type="button" className={quiet} onClick={() => onSelection()}>
            <PlusIcon size={16} aria-hidden />
            {words.fresh}
          </button>
          {chat && (
            <a className={quiet} href={href({ page: "conversation", id: chat.id })}>
              <ExternalLinkIcon size={16} aria-hidden />
              {words.openFull}
            </a>
          )}
        </div>
      )}
      {chat?.missing ? (
        <div className="flex flex-1 flex-col items-start gap-4 px-4 py-6">
          <h3 className="humagen-display text-aui-lg font-medium text-aui-fg">{words.missing}</h3>
          <button type="button" className={button} onClick={() => onSelection()}>
            {words.all}
          </button>
        </div>
      ) : !page && turns.length === 0 ? (
        <div className="flex-1 px-4 py-6">
          <SkeletonRows count={4} />
        </div>
      ) : (
        <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
          <ThreadList
            className="w-full flex-1"
            column="mx-auto w-full max-w-3xl px-4"
            busy={working}
            label={words.thread}
          >
            <div className="flex min-w-0 flex-col gap-1">
              <h3 className="break-words text-aui-base font-medium text-aui-fg">
                {page?.thread.title || words.untitled}
              </h3>
              {forkedAt && (
                <p className="text-aui-base text-aui-muted">{words.from(ago(forkedAt))}</p>
              )}
            </div>
            {turns.map((turn, i) =>
              turn.message ? (
                <article
                  key={turn.message.id}
                  data-turn-id={turn.message.id}
                  data-side="mine"
                  aria-label={words.saidBy(turn.message.name || turn.message.by)}
                  className="flex flex-col items-end gap-0.5"
                >
                  <p className="humagen-said max-w-[85%] whitespace-pre-wrap break-words rounded-aui-lg px-3.5 py-2 text-aui-base text-aui-fg">
                    <Mentioned text={turn.message.text} />
                  </p>
                </article>
              ) : turn.run ? (
                <SideAnswer
                  key={turn.run.id}
                  run={turn.run}
                  turn={turn}
                  canStop={canPost}
                  question={questionOf(turns, i)}
                  session={thread}
                  record={record}
                />
              ) : null,
            )}
          </ThreadList>
        </WebSurface>
      )}
      {box}
    </div>
  );
}

/** What was asked for the answer at `index`: the message it answers, else the newest one before it. */
function questionOf(turns: ThreadTurn[], index: number): string {
  const run = turns[index]?.run;
  const asked = run?.message
    ? turns.find((t) => t.message?.id === run.message)?.message
    : turns.slice(0, index).findLast((t) => t.message)?.message;
  return asked?.text ?? run?.text ?? "";
}

/**
 * One answer in a side chat, drawn as the thread draws an answer, with the
 * way to carry it back: Add to Message records it as done by hand in the
 * session, to go with the reader's next message there. Pressed again, it is
 * taken back. Removing it from the list above the composer, or sending the
 * message, puts the button back as it was.
 */
function SideAnswer({
  run: original,
  turn: read,
  canStop,
  question,
  session,
  record,
}: {
  run: Run;
  turn: ThreadTurn;
  canStop: boolean;
  question: string;
  session: string;
  record: WorkbenchTabProps["record"];
}) {
  const { run, stop } = useRunStop(original, canStop);
  const live = useRunTurn(run);
  const name = agentShown(run.agent);
  const ask = useMemo(() => asksOf(read), [read]);
  const shown = useMemo(() => {
    const turn = live ?? turnOf(run);
    const named = turn.agent.name === name ? turn : { ...turn, agent: { ...turn.agent, name } };
    return askedOf(
      run.citations !== undefined ? { ...named, citations: run.citations.map(citationOf) } : named,
      ask,
    );
  }, [live, run, name, ask]);
  const { records, remove } = useHumanActivity(session);
  const [carried, setCarried] = useState<string[]>([]);
  const added = carried.length > 0 && carried.some((id) => records.some((r) => r.id === id));
  const conclusion = run.status === "completed" ? conclusionRecords(run, question, name) : [];
  return (
    <article
      data-turn-id={run.id}
      aria-label={words.answeredBy(name)}
      // The answer's own header is set a step up: nothing in the workbench is read under 14px.
      className="flex flex-col gap-2 [&_header]:text-aui-base [&_header_span]:text-aui-base"
    >
      <AgentTint name={run.agent}>
        {/* A side chat is private: Retry here would post its brief into the session, so there is none. */}
        <RetryCallContext.Provider value={null}>
          <AgentMessage
            turn={shown}
            stop={stop}
            disclaimer={false}
            copy={copyFor(ask)}
            itemActions={itemActions(run)}
            capabilities={{ regenerate: false, feedback: false, report: false, followUp: false }}
          />
        </RetryCallContext.Provider>
      </AgentTint>
      {conclusion.length > 0 && (
        <div>
          <button
            type="button"
            className={button}
            aria-pressed={added}
            onClick={() => {
              if (added) {
                for (const id of carried) remove(id);
                setCarried([]);
                return;
              }
              setCarried(conclusion.flatMap((input) => record(input)?.id ?? []));
            }}
          >
            {added ? <CheckIcon size={16} aria-hidden /> : <PlusIcon size={16} aria-hidden />}
            {added ? words.added : words.add}
          </button>
        </div>
      )}
    </article>
  );
}
