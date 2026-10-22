import { words } from "./host.ts";
import { type SlackApi, SlackError } from "./slack.ts";

/**
 * What people said in a thread between two turns, without asking the app.
 * The agent sees only the messages addressed to it; a channel thread is
 * mostly people talking to each other, and a mention deep in one makes
 * little sense without what came before it.
 *
 * The messages since the app was last asked in the thread are read from
 * Slack (`conversations.replies`) when it is asked again, and put ahead of
 * the question with who said each. The run takes nothing but text, so they
 * travel in the text. Nothing is kept here: the last time the app was asked
 * is the last message in the thread that mentions it, or its own post that
 * opened the thread.
 *
 * This follows fiber's thread reconciliation (`pkg/thread`) in intent: only
 * what the agent has not been shown, oldest first, never its own messages,
 * and never the message that asks, which is the run's own request.
 */

export interface SlackMessage {
  ts: string;
  user?: string;
  bot_id?: string;
  subtype?: string;
  text?: string;
}

/** How much history one turn carries: the newest messages, within both caps. */
export const HISTORY = { messages: 30, chars: 6_000, perMessage: 1_000, pages: 5 };

/** Subtypes that are still a person saying something. Joins, edits and bots are not. */
const SPOKEN = new Set(["thread_broadcast", "file_share", "me_message"]);

const before = (a: string, b: string) => Number(a) < Number(b);

/**
 * The messages to show the agent, oldest first: people's messages after the
 * app was last asked in the thread and before the one asking now.
 */
export function sinceLastTurn(
  messages: SlackMessage[],
  opts: { botUser: string; threadTs: string; triggerTs: string },
): SlackMessage[] {
  const mention = `<@${opts.botUser}>`;
  const ours = (m: SlackMessage) => m.user === opts.botUser;
  const earlier = messages
    .filter((m) => before(m.ts, opts.triggerTs))
    .sort((a, b) => Number(a.ts) - Number(b.ts));
  // The last time the app was asked here: a mention of it, or its own post that opened the thread.
  let from = -1;
  for (const [i, m] of earlier.entries()) {
    const asked = !ours(m) && !m.bot_id && (m.text ?? "").includes(mention);
    if (asked || (ours(m) && m.ts === opts.threadTs)) from = i;
  }
  return earlier
    .slice(from + 1)
    .filter(
      (m) =>
        m.user &&
        !ours(m) &&
        !m.bot_id &&
        (!m.subtype || SPOKEN.has(m.subtype)) &&
        (m.text ?? "").trim() !== "",
    );
}

/**
 * The question with the thread's messages ahead of it. Each says who wrote
 * it. The newest are kept when there are more than the caps allow.
 */
export function withHistory(
  text: string,
  messages: SlackMessage[],
  names: Map<string, string> = new Map(),
): string {
  const lines: string[] = [];
  let used = 0;
  for (const m of [...messages].reverse()) {
    if (lines.length >= HISTORY.messages) break;
    const who = names.get(m.user ?? "");
    const by = who ? `${who} (<@${m.user}>)` : `<@${m.user}>`;
    let said = (m.text ?? "").trim().replace(/\s*\n\s*/g, " ");
    if (said.length > HISTORY.perMessage) said = `${said.slice(0, HISTORY.perMessage)}…`;
    const line = `${by}: ${said}`;
    if (used + line.length > HISTORY.chars) break;
    used += line.length;
    lines.unshift(line);
  }
  if (lines.length === 0) return text;
  return [words.historyOpen, ...lines, words.historyClose, "", text].join("\n");
}

/**
 * Read a thread up to the message asking now, or to its end when nothing
 * asks, a bounded number of pages. The oldest pages come first from Slack,
 * so a very long thread is read from its start and its newest messages are
 * the ones the caps keep.
 */
export async function readThread(
  slack: SlackApi,
  at: { channel: string; threadTs: string; triggerTs?: string },
): Promise<SlackMessage[]> {
  const out: SlackMessage[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < HISTORY.pages; page++) {
    const got = await slack.call<{
      messages?: SlackMessage[];
      response_metadata?: { next_cursor?: string };
    }>("conversations.replies", {
      channel: at.channel,
      ts: at.threadTs,
      ...(at.triggerTs ? { latest: at.triggerTs, inclusive: false } : {}),
      limit: 200,
      ...(cursor ? { cursor } : {}),
    });
    out.push(...(got.messages ?? []));
    cursor = got.response_metadata?.next_cursor || undefined;
    if (!cursor) break;
  }
  return out;
}

/**
 * Who people are, by display name, for the attribution. Optional: without
 * `users:read` the attribution is the Slack mention alone, and Slack is not
 * asked again.
 */
export class Names {
  private readonly known = new Map<string, string>();
  private refused = false;
  constructor(
    private readonly slack: SlackApi,
    private readonly log: (line: string) => void = () => {},
  ) {}

  async of(users: string[]): Promise<Map<string, string>> {
    const out = new Map<string, string>();
    for (const user of new Set(users)) {
      const name = this.known.get(user) ?? (await this.look(user));
      if (name) out.set(user, name);
    }
    return out;
  }

  private async look(user: string): Promise<string | undefined> {
    if (this.refused) return undefined;
    try {
      const got = await this.slack.call<{
        user?: { name?: string; real_name?: string; profile?: { display_name?: string } };
      }>("users.info", { user });
      const name =
        got.user?.profile?.display_name || got.user?.real_name || got.user?.name || undefined;
      if (name) this.known.set(user, name);
      return name;
    } catch (e) {
      if (e instanceof SlackError && e.code === "missing_scope") {
        this.refused = true;
        this.log("users.info needs users:read; thread history names people by mention only");
      }
      return undefined;
    }
  }
}

/**
 * Whether a Slack person is already in a thread: they wrote in it, or were
 * mentioned in it the Slack way (`<@U…>`), so Slack told them there.
 */
export function inThread(messages: SlackMessage[], user: string): boolean {
  const mentioned = (text: string) => text.includes(`<@${user}>`) || text.includes(`<@${user}|`);
  return messages.some((m) => m.user === user || mentioned(m.text ?? ""));
}
