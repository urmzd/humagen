import { defaultCopy } from "@humagen/components";
import { type HumanActivityInput, MAX_HUMAN_DETAIL } from "@humagen/core";
import type { ThreadForkSummary } from "../../../packages/client/src/generated/types.ts";
import type { Run } from "./adapter.ts";
import { call, type Thread } from "./api.ts";

/*
 * What the Side Chat tab knows that is not drawing (WorkbenchSidechat.tsx): a
 * side chat is a private fork of the session (POST /api/threads/{id}/fork),
 * so it carries the session's history as read-only context, answers in a
 * conversation of its own and posts nothing in the session.
 */

const words = defaultCopy.workbenchSidechat;

/** The longest title the platform keeps for a conversation. */
const MAX_TITLE = 200;
/** How much of the question an answer's record repeats. */
const MAX_QUESTION = 100;
/** The most records one answer becomes: three details' worth of its text. */
export const MAX_CONCLUSION_PARTS = 3;

const ended = (run: Run) =>
  run.status === "completed" || run.status === "failed" || run.status === "stopped";

/**
 * The saved turn a new side chat's history runs through: the newest one the
 * platform will fork at. An answer still being written is not saved, so the
 * message it answers stands in for it and the working agent is left alone.
 * Undefined while the session has no saved turn at all.
 */
export function forkPoint(runs: readonly Run[]): string | undefined {
  for (let i = runs.length - 1; i >= 0; i--) {
    const run = runs[i]!;
    if (ended(run)) return run.id;
    if (run.message) return run.message;
  }
  return undefined;
}

/**
 * Who answers a side chat's untagged messages: the session's own agents, as
 * its record routes them, else whoever answers there, else the agent that
 * answered last. `page` is the session as read, undefined when it could not
 * be. Empty when nobody is known, and the platform then asks for a tag.
 */
export function sideChatRouting(
  page: { thread: Thread; answers?: { agent: string } } | undefined,
  runs: readonly Run[],
): { agents?: string[]; mode?: "join" | "all" } {
  const routed = page?.thread.agents ?? [];
  if (routed.length > 1)
    return { agents: routed, ...(page?.thread.mode ? { mode: page.thread.mode } : {}) };
  const agent = routed[0] || page?.answers?.agent || runs[runs.length - 1]?.agent;
  return agent ? { agents: [agent] } : {};
}

const oneLine = (text: string) => text.replace(/\s+/g, " ").trim();
const clip = (text: string, max: number) =>
  text.length > max ? `${text.slice(0, max - 1).trimEnd()}…` : text;

/** The title a side chat is saved under, from its first question. */
export function sideChatTitle(question: string): string {
  const said = oneLine(question);
  return said ? clip(words.title(said), MAX_TITLE) : words.untitled;
}

/** Whether a conversation is a side chat of `session`: a fork of it that still takes messages. */
export function isSideChatOf(thread: Thread, session: string): boolean {
  return thread.fork?.source_thread === session && !thread.read_only;
}

/** The reader's side chats of a session, the most recently active first (at most 50). */
export function sideChats(session: string): Promise<ThreadForkSummary[]> {
  return call<{ forks: ThreadForkSummary[] }>(
    "GET",
    `/api/threads/${encodeURIComponent(session)}/forks`,
  ).then((r) => r.forks);
}

/**
 * Text in pieces of at most `max` characters, at most `parts` of them. A
 * piece ends at a paragraph, a line, a sentence or a word when one falls in
 * its last third, so a cut rarely lands inside a word. What is left after
 * the last piece stays on it, for whoever stores it to cut and mark.
 */
export function inParts(text: string, max: number, parts: number): string[] {
  const out: string[] = [];
  let rest = text.trim();
  while (rest.length > max && out.length < parts - 1) {
    const window = rest.slice(0, max);
    const floor = Math.floor((max * 2) / 3);
    let cut = -1;
    for (const mark of ["\n\n", "\n", ". ", " "]) {
      const at = window.lastIndexOf(mark);
      if (at >= floor) {
        cut = at + (mark === ". " ? 1 : 0);
        break;
      }
    }
    if (cut < 0) cut = max;
    out.push(rest.slice(0, cut).trimEnd());
    rest = rest.slice(cut).trimStart();
  }
  if (rest) out.push(rest);
  return out;
}

/**
 * A side chat's answer as what the person carries back to the session: what
 * the activity record folds into their next message there. A long answer is
 * several records, since one holds 600 characters, and past the third it is
 * cut. Each has a key, so adding the same answer again replaces it.
 */
export function conclusionRecords(
  run: Pick<Run, "id" | "agent"> & { answer?: string },
  question: string,
  agent: string = run.agent,
): HumanActivityInput[] {
  const answer = (run.answer ?? "").trim();
  if (!answer) return [];
  const summary = words.summary(agent, clip(oneLine(question), MAX_QUESTION));
  const parts = inParts(answer, MAX_HUMAN_DETAIL, MAX_CONCLUSION_PARTS);
  return parts.map((detail, i) => ({
    source: "other" as const,
    summary: parts.length > 1 ? words.part(summary, i + 1, parts.length) : summary,
    detail,
    key: `sidechat:${run.id}:${i + 1}`,
  }));
}
