import type { ActionHandler, Turn, TurnEvent } from "@warp/core";
import { NowContext, WebSurface } from "@warp/render-web";
import { ArtifactPreviewProvider } from "@warp/web";
import type { ReactNode } from "react";

/** Story args shared by everything that emits UiActions; `fn()` logs them in the Actions panel. */
export interface ActionArgs {
  onAction: ActionHandler;
}

/** Fixture clock: approvals expire at 15:34, so countdowns read "30m left". */
export const FIXTURE_NOW = Date.parse("2026-10-08T15:04:00Z");

/** The real web UI at chat width. `wide` is for galleries. File tiles open the preview panel. */
export function Canvas({
  children,
  width = "chat",
  onAction,
}: {
  children: ReactNode;
  width?: "chat" | "wide";
  onAction?: ActionHandler;
}) {
  return (
    <div className={`mx-auto w-full ${width === "chat" ? "max-w-3xl" : "max-w-6xl"}`}>
      <NowContext.Provider value={FIXTURE_NOW}>
        <WebSurface onAction={onAction}>
          <ArtifactPreviewProvider>{children}</ArtifactPreviewProvider>
        </WebSurface>
      </NowContext.Provider>
    </div>
  );
}

/** Docs line for stories whose component also renders to Slack. */
export function slack(fixture: string, note?: string) {
  return {
    docs: {
      description: {
        story: `Slack: \`pnpm sandbox post ${fixture}\`${note ? `. ${note}` : ""}`,
      },
    },
  };
}

/** Docs line for web-only stories: what Slack uses natively instead. */
export function webOnly(native: string) {
  return { docs: { description: { story: `Web only. Slack uses ${native}.` } } };
}

export const SLACK_COMPOSER =
  "its native composer, suggested prompts via `assistant.threads.setSuggestedPrompts`, and its native Stop button (agent_session_stopped)";

const REASONING = [
  "The alert is a 5xx spike on **payments-api** starting 14:52. ",
  "Two likely causes: a bad deploy or a downstream dependency. ",
  "Deploy history is cheap to check and the runbook says to rule out deploys first, ",
  "so I'll read the alert, search the runbook, then list recent deploys.",
].join("");

export const reasoningText = REASONING;

function presentTense(title: string) {
  return title
    .replace(/^Read /, "Reading ")
    .replace(/^Searched /, "Searching ")
    .replace(/^Checked /, "Checking ")
    .replace(/^Ran /, "Running ")
    .replace(/^Edited /, "Editing ");
}

function words(text: string, type: "text.delta" | "reasoning.delta"): TurnEvent[] {
  return (text.match(/\S+\s*|\s+/g) ?? []).map((t) => ({ type, text: t }));
}

/**
 * The event stream that would have produced a finished turn: reasoning,
 * each task starting then settling, citations, then tokens.
 */
export function scriptFor(turn: Turn, { reasoning = REASONING } = {}): TurnEvent[] {
  const events: TurnEvent[] = [
    {
      type: "turn.started",
      turnId: turn.id,
      agent: turn.agent,
      sessionUrl: turn.sessionUrl,
      source: turn.source,
      disclaimer: turn.disclaimer,
    },
  ];
  if (reasoning) events.push(...words(reasoning, "reasoning.delta"));
  for (const task of turn.tasks) {
    const running = presentTense(task.title);
    if (turn.planTitle) events.push({ type: "plan.updated", title: running });
    events.push({
      type: "task.upserted",
      task: { id: task.id, title: running, status: "in_progress", details: task.details },
    });
    // Hold a few ticks so the running state is visible before it settles.
    if (turn.planTitle)
      for (let i = 0; i < 6; i++) events.push({ type: "plan.updated", title: running });
    events.push({ type: "task.upserted", task });
  }
  for (const citation of turn.citations) events.push({ type: "citation.added", citation });
  events.push(...words(turn.text, "text.delta"));
  return events;
}

/** Docs line for primitive stories: how render-slack draws the same primitive. */
export function slackPrimitive(how: string) {
  return { docs: { description: { component: `Slack: ${how}.` } } };
}
