import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import { ForkIcon, WebSurface } from "@humagen/render-web";
import { AgentMessage, messageActionButton, type railEntry, Thread } from "@humagen/web";
import { type Dispatch, type ReactNode, type SetStateAction, useMemo } from "react";
import { citationOf, type Run, turnOf } from "./adapter.ts";
import type { ProjectView, ThreadAsk, ThreadPage, ThreadSummary } from "./api.ts";
import { askedOf, copyFor } from "./asks.ts";
import { notAskable, siblingsOf } from "./Chat.tsx";
import type { ComposerSeed } from "./Composer.tsx";
import { type ConversationForkActions, ConversationForkPanels } from "./ConversationForkPanels.tsx";
import { ConversationInfo, ConversationPage } from "./ConversationShell.tsx";
import { Deliverables, revisionPrompt } from "./Deliverables.tsx";
import { GitHubTab, reviewText, reviewView } from "./GitHubTab.tsx";
import { itemActions } from "./itemActions.tsx";
import { AnswerRefs, CallThreads, OriginRef } from "./MessageRefs.tsx";
import { MessageTime } from "./MessageTime.tsx";
import { OutcomeActions } from "./OutcomeActions.tsx";
import { openPane } from "./pane.ts";
import { ConversationActivity, ResponseActivityPanel } from "./ResponseActivity.tsx";
import { RunFeedbackHost } from "./RunFeedback.tsx";
import { useRunStop } from "./RunStop.tsx";
import { useRunTurn } from "./Runs.tsx";
import { traceSource } from "./responseSource.ts";
import { type ConversationPanel, type ConversationView, href } from "./route.ts";
import { SessionDetailsProvider } from "./SessionDetails.tsx";
import { SessionDetailsPanel, sessionFiles, sessionProgress } from "./SessionPanel.tsx";
import { OriginButton, referenceOfPlace, SessionPlace, usePlace } from "./SessionPlace.tsx";
import { useItemStops } from "./stops.tsx";
import { AgentTint, Checkpoint, cause, Mentioned, Presence } from "./ui.tsx";
import { Workspace } from "./Workspace.tsx";

/*
 * A conversation of runs alone, from before threads, as Chat shows it: its
 * turns along the branch on screen, and the panels beside them. Chat holds
 * the conversation's state and hands it here; this half is read only when
 * such a conversation is opened, so Home does not carry it.
 */

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
  origin,
}: {
  run: Run;
  canStop?: boolean;
  /** Where the session started, as a chip under the first message. */
  origin?: ReactNode;
  /** The reader's subject. */
  me: string;
  siblings: Run[];
  /** The questions the run waits on, as the reader stands to each: one is offered only when it is theirs. */
  ask?: ThreadAsk[];
  /** Follow the run's events. Off for settled turns, whose record is enough. */
  live: boolean;
  onSwitch: (id: string) => void;
  onAsk?: () => void;
  onReply?: () => void;
  onRedirect?: () => void;
  actions?: ReactNode;
}) {
  const { run, stop } = useRunStop(originalRun, canStop);
  const itemStops = useItemStops({
    run,
    allowed: canStop,
    background: [],
    agentStop: stop ? { onStop: stop.onStop, pending: !!stop.pending } : undefined,
  });
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
      data-turn-id={run.id}
      className="flex flex-col gap-3"
      aria-label={`Turn answered by ${run.agent}`}
    >
      <p className="humagen-glass max-w-[80%] self-end whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-sm text-aui-fg">
        <Mentioned text={run.text} />
      </p>
      {origin && <div className="flex justify-end">{origin}</div>}
      {/* The answer names its agent. Only a router's pick, which it does not show, is said. */}
      {run.picked && <Checkpoint agent={run.agent}>{run.picked}</Checkpoint>}

      <AgentTint name={run.agent}>
        <RunFeedbackHost turn={shown}>
          {(rated) => (
            <CallThreads run={run} calls={rated.tasks.some((t) => t.subagent)}>
              <AgentMessage
                actions={actions}
                stop={stop}
                itemStops={itemStops}
                disclaimer={false}
                activity={false}
                timestamp={<MessageTime at={run.created_at} />}
                turn={rated}
                copy={copyFor(ask)}
                itemActions={itemActions(run)}
                capabilities={{ regenerate: !refused }}
                onAskAgent={onAsk}
                onReply={onReply}
                onRedirect={onRedirect}
              />
            </CallThreads>
          )}
        </RunFeedbackHost>
      </AgentTint>
      <AnswerRefs run={run} />

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

/** The turns of a conversation of runs, its header and its panels, around Chat's own composer. */
export function RunsConversation({
  threadId,
  summary,
  path,
  turns,
  room,
  projects,
  where,
  personalProject,
  workspaceName,
  shownView,
  openPanel,
  canRun,
  canLearn,
  me,
  forkActions,
  artifactActions,
  onAction: act,
  tailWorking,
  wave,
  asks,
  setChosen,
  setSeed,
  composer,
  activity,
  activityOpen,
}: {
  threadId: string;
  summary: ThreadSummary | undefined;
  /** The branch on screen, and every turn read. */
  path: Run[];
  turns: Run[];
  room: ThreadPage | null;
  projects: ProjectView[] | null;
  /** The conversation's project. */
  where: string;
  personalProject?: string;
  workspaceName?: string;
  shownView: ConversationView;
  openPanel: (panel?: ConversationPanel, selection?: Omit<ConversationView, "panel">) => void;
  canRun: boolean;
  canLearn: boolean;
  me: string;
  forkActions?: ConversationForkActions;
  artifactActions?: { knowledge: boolean; memory: boolean };
  /** Every action but opening a review, which opens here. */
  onAction: (action: UiAction) => void;
  tailWorking: boolean;
  wave: (ReturnType<typeof railEntry> & { preview: string })[];
  asks: Record<string, ThreadAsk[]>;
  setChosen: Dispatch<SetStateAction<Record<string, string>>>;
  setSeed: (seed: ComposerSeed) => void;
  composer: ReactNode;
  /** What the activity drawer shows, kept while it leaves, and whether it is open. */
  activity: { focus: string | null } | null;
  activityOpen: boolean;
}) {
  const chatPlace = usePlace(room?.thread);
  const onAction = (a: UiAction) => {
    if (a.id === ActionId.openReview && a.payload.target) {
      openPanel("workspace", reviewView(shownView, a.payload.target));
      return;
    }
    act(a);
  };
  return (
    <SessionDetailsProvider panel={shownView.panel} onPanel={(panel) => openPanel(panel)}>
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
          origin={<OriginButton place={chatPlace} />}
          progress={sessionProgress(turns)}
          location={
            chatPlace && <SessionPlace place={chatPlace} sender={path[0]?.actor?.display} />
          }
          agents={summary?.agents ?? [...new Set(turns.map((t) => t.agent))]}
          people={room?.thread.participants?.filter((p) => p.kind === "person").map((p) => p.name)}
          onPanel={(next) => openPanel(next)}
          panel={shownView.panel}
          onClosePanel={() => openPanel()}
          onArtifact={(id) =>
            openPanel("workspace", openPane(shownView, { kind: "artifact", ref: id }))
          }
        >
          {room && canRun && !room.thread.read_only && (
            <ConversationForkPanels thread={room.thread} runs={turns} actions={forkActions} />
          )}
          <WebSurface onAction={onAction} className="flex min-h-0 flex-1 flex-col">
            <Thread
              className="w-full flex-1"
              column="mx-auto w-full max-w-4xl px-4"
              busy={tailWorking}
              label="Conversation"
              turns={wave}
              footer={composer}
            >
              {path.map((run, i) => (
                <TurnView
                  key={run.id}
                  run={run}
                  origin={i === 0 ? <OriginRef origin={referenceOfPlace(chatPlace)} /> : undefined}
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
                            quote: {
                              label: `${run.agent}'s answer`,
                              agent: run.agent,
                              url: href({
                                page: "conversation",
                                id: threadId,
                                panel: "activity",
                                run: run.id,
                              }),
                            },
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
        </ConversationPage>
        {room && (
          <ConversationInfo
            origin={referenceOfPlace(chatPlace)}
            canWrite={canRun}
            runs={turns}
            view={shownView}
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
        {threadId && (
          <Workspace
            extraTab={(tab) => {
              if (!["review", "pull", "issue"].includes(tab.kind)) return undefined;
              return <GitHubTab tab={tab} review={reviewText(turns, tab.ref)} />;
            }}
            key={threadId}
            thread={threadId}
            open={shownView.panel === "workspace"}
            view={shownView}
            onView={(next) => openPanel("workspace", next)}
            onClose={() => openPanel()}
            onRequest={
              canRun && !room?.thread.read_only
                ? (text) => {
                    setSeed({ text, mode: "prepend" });
                    openPanel();
                  }
                : undefined
            }
            promotion={
              artifactActions
                ? {
                    project: room?.thread.project ?? where ?? "",
                    shared: room?.thread.visibility === "shared",
                    ...artifactActions,
                  }
                : undefined
            }
            onRevise={
              canRun
                ? (file, slide) => {
                    setSeed({ text: revisionPrompt(file, slide), mode: "prepend" });
                    openPanel();
                  }
                : undefined
            }
            onRun={(run) => openPanel("activity", { run })}
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
              onEditCopy={(file) => openPanel("workspace", { file, workspaceTab: "edit" })}
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
        <Presence when={shownView.panel === "details"} ms={220}>
          {(leaving) => (
            <SessionDetailsPanel
              runs={turns}
              thread={room?.thread.id ?? threadId ?? undefined}
              uploads={[]}
              origin={referenceOfPlace(chatPlace)}
              files={sessionFiles}
              leaving={leaving}
              onClose={() => openPanel()}
              onOpenFile={(id) =>
                openPanel("workspace", openPane(shownView, { kind: "artifact", ref: id }))
              }
            />
          )}
        </Presence>
        <Presence when={shownView.panel === "response" && !!shownView.run} ms={220}>
          {(leaving) => {
            const run = turns.find((t) => t.id === shownView.run);
            return run ? (
              <ResponseActivityPanel
                source={traceSource(run)}
                leaving={leaving}
                onClose={() => openPanel()}
              />
            ) : null;
          }}
        </Presence>
        <Presence when={activityOpen} ms={220}>
          {(leaving) => (
            <ConversationActivity
              step={shownView.step}
              leaving={leaving}
              turns={path}
              focus={activity?.focus ?? null}
              onClose={() => openPanel()}
              onFocus={(run) => openPanel("activity", run ? { run } : {})}
            />
          )}
        </Presence>
      </section>
    </SessionDetailsProvider>
  );
}
