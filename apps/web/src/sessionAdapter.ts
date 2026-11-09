import type { AgentSession, SessionMessage } from "../../../packages/client/src/generated/types.ts";
import type { PickCall, SessionPick, ThreadPage, ThreadPosted, ThreadTurn } from "./api.ts";
import { agentShown } from "./branding.ts";
import { cacheSessionFeedback } from "./sessionFeedback.ts";

const callStatuses: PickCall["status"][] = ["working", "waiting", "done", "failed", "stopped"];

/**
 * What a root session's entry agent picked, from a message with neither a
 * run nor a person's message: its pick part's reason, then each call part,
 * a child session with its answer once it has one. Null for any other.
 * `after` is the person's message read just before it.
 */
export function pickOf(message: SessionMessage, after: string): SessionPick | null {
  if (message.run || message.message) return null;
  const reason = message.parts.find((part) => part.type === "pick");
  if (!reason) return null;
  return {
    id: message.id,
    agent: message.author.name,
    by: message.author.display || agentShown(message.author.name),
    reason: reason.text ?? "",
    message: after,
    at: message.at,
    calls: message.parts.flatMap((part): PickCall[] =>
      part.type === "call" && part.session
        ? [
            {
              session: part.session,
              agent: part.agent ?? "",
              status: callStatuses.find((s) => s === part.status) ?? "working",
              text: part.text ?? "",
            },
          ]
        : [],
    ),
  };
}

/** Preserve the server's permission metadata while existing controls migrate. */
export function sessionPage(session: AgentSession): ThreadPage {
  if (!session.thread) throw new Error("The session response is missing its access metadata.");
  let after = "";
  const turns: ThreadTurn[] = session.messages.flatMap((message): ThreadTurn[] => {
    if (message.run) {
      cacheSessionFeedback(message.run.id, [
        ...(message.feedback ? [message.feedback] : []),
        ...(message.finding_feedback ?? []),
      ]);
      return [{ kind: "run", run: message.run, ask: message.ask }];
    }
    if (message.message) {
      after = message.message.id;
      return [{ kind: "message", message: message.message }];
    }
    const pick = pickOf(message, after);
    return pick ? [{ kind: "pick", pick }] : [];
  });
  return {
    thread: session.thread,
    turns,
    next: session.next || null,
    answers: { agent: session.agent, from: "session" },
    sessionAgents: [...new Set([session.agent, ...session.children.map((child) => child.agent)])],
  };
}

/** Posting returns the newest human message and only the answers to that message. */
export function sessionPosted(session: AgentSession): ThreadPosted {
  const page = sessionPage(session);
  const index = page.turns.findLastIndex((turn) => turn.kind === "message");
  const message = page.turns[index]?.message;
  if (!message) throw new Error("The session response is missing the posted message.");
  return {
    thread: page.thread,
    message,
    runs: page.turns.slice(index + 1).flatMap((turn) => (turn.run ? [turn.run] : [])),
    notified: [],
    refused: message.mentions.filter((mention) => mention.refused),
    rule: "default",
  };
}
