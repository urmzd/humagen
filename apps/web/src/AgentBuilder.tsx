import { type ReactNode, useState } from "react";
import type {
  AgentSession,
  AgentSessionSummary,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
import { Page, Problem, primary } from "./ui.tsx";

/** The helper that builds agents with you: the entry agent, which took over from the builder. */
export const BUILDER_AGENT = "humagen";

/** An agent's named builder session stays available for later changes and evals. */
export async function builderSession(name?: string): Promise<string> {
  const label = name ? `manage-${name}` : undefined;
  if (label) {
    let after: string | undefined;
    do {
      const query = new URLSearchParams({ agent: BUILDER_AGENT, mine: "true", limit: "100" });
      if (after) query.set("after", after);
      const page = await call<{ sessions: AgentSessionSummary[]; next?: string }>(
        "GET",
        `/api/sessions?${query}`,
      );
      const existing = page.sessions.find((session) => session.label === label);
      if (existing) return existing.id;
      if (page.next === after) break;
      after = page.next;
    } while (after);
  }
  const session = await call<AgentSession>("POST", "/api/sessions", {
    agent: BUILDER_AGENT,
    ...(label ? { label } : {}),
    text: name
      ? `Help me manage @${name}. Review its current health and eval results. Ask what I want to change before proposing edits; apply changes only after approval.`
      : "Help me create an agent. Ask me what its job should be, one question at a time. Preview the prompt, tools, connections and eval results before asking for approval.",
  });
  return session.id;
}

export function BuilderButton({
  name,
  onOpen,
  children,
  className = primary,
}: {
  name?: string;
  onOpen: (id: string) => void;
  children?: ReactNode;
  className?: string;
}) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const start = async () => {
    if (busy) return;
    setBusy(true);
    setProblem("");
    try {
      onOpen(await builderSession(name));
    } catch (error) {
      setProblem(
        error instanceof Error ? error.message : "The builder session could not be opened.",
      );
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <button type="button" className={className} disabled={busy} onClick={() => void start()}>
        {busy ? "Opening builder…" : (children ?? (name ? "Manage with builder" : "New agent"))}
      </button>
      <Problem message={problem} />
    </div>
  );
}

/** Compatibility addresses remain explicit entry points and never save an agent on navigation. */
export function AgentBuilderStart({
  name,
  onOpen,
}: {
  name?: string;
  onOpen: (id: string) => void;
}) {
  return (
    <Page
      title={name ? `Manage ${name}` : "New agent"}
      lede="Describe what you need in a session with the builder. Review the proposed prompt, tools, connections and eval results before approving a change."
    >
      <BuilderButton name={name} onOpen={onOpen} />
    </Page>
  );
}
