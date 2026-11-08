import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import type { ThreadPage, ThreadPosted, ThreadTurn } from "./api.ts";
import { cacheSessionFeedback } from "./sessionFeedback.ts";

/** Preserve the server's permission metadata while existing controls migrate. */
export function sessionPage(session: AgentSession): ThreadPage {
  if (!session.thread) throw new Error("The session response is missing its access metadata.");
  const turns: ThreadTurn[] = session.messages.flatMap((message): ThreadTurn[] => {
    if (message.run) {
      cacheSessionFeedback(message.run.id, [
        ...(message.feedback ? [message.feedback] : []),
        ...(message.finding_feedback ?? []),
      ]);
      return [{ kind: "run", run: message.run, ask: message.ask }];
    }
    if (message.message) return [{ kind: "message", message: message.message }];
    return [];
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
