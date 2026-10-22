import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { words } from "./host.ts";
import { JOURNAL_DIR } from "./legacy.ts";
import { type SlackApi, SlackError } from "./slack.ts";

/**
 * Streams this process opened in Slack and has not closed yet, kept in a
 * small file so the next process can close them. A stream left open after a
 * crash, a kill or a lost machine shows as still typing in its thread until
 * Slack gives up on it. Humagen's API has nowhere to keep this, so the app
 * keeps it beside itself.
 *
 * The file holds only Slack coordinates: a channel, a message and its
 * thread. It is rewritten whole on every change, through a rename, so a
 * crash mid-write leaves the previous version. One file serves one process.
 */

export interface OpenStream {
  channel: string;
  /** The streamed message. */
  ts: string;
  /** The thread it answers in, so the session can be settled. */
  threadTs?: string;
  /** When it was opened, for the log. */
  at: string;
}

export interface Journal {
  open(stream: OpenStream): void;
  close(channel: string, ts: string): void;
  list(): OpenStream[];
}

const keyOf = (channel: string, ts: string) => `${channel}/${ts}`;

/** Where the journal lives by default: the user's state directory, one file per team. */
export function defaultJournalPath(team: string, env = process.env): string {
  const base = env.XDG_STATE_HOME?.trim() || join(homedir(), ".local", "state");
  return join(base, JOURNAL_DIR, `streams-${team}.json`);
}

/** A journal in memory, for tests and for when no file may be written. */
export function memoryJournal(): Journal {
  const open = new Map<string, OpenStream>();
  return {
    open: (s) => void open.set(keyOf(s.channel, s.ts), s),
    close: (channel, ts) => void open.delete(keyOf(channel, ts)),
    list: () => [...open.values()],
  };
}

/**
 * A journal kept in a file. An unreadable or malformed file is reported
 * and treated as empty: the worst it costs is a stream left to Slack's own
 * timeout, which is what happens without a journal at all.
 */
export function fileJournal(path: string, log: (line: string) => void = () => {}): Journal {
  const open = new Map<string, OpenStream>();
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as { streams?: OpenStream[] };
    for (const s of raw.streams ?? []) {
      if (typeof s?.channel === "string" && typeof s?.ts === "string") {
        open.set(keyOf(s.channel, s.ts), s);
      }
    }
  } catch (e) {
    if ((e as NodeJS.ErrnoException).code !== "ENOENT") {
      log(`stream journal ${path} could not be read, starting empty: ${(e as Error).message}`);
    }
  }
  const save = () => {
    try {
      mkdirSync(dirname(path), { recursive: true });
      const tmp = `${path}.${process.pid}.tmp`;
      writeFileSync(tmp, JSON.stringify({ streams: [...open.values()] }), { mode: 0o600 });
      renameSync(tmp, path);
    } catch (e) {
      // A journal that cannot be written never costs a turn.
      log(`stream journal ${path} could not be written: ${(e as Error).message}`);
    }
  };
  return {
    open(s) {
      open.set(keyOf(s.channel, s.ts), s);
      save();
    },
    close(channel, ts) {
      if (open.delete(keyOf(channel, ts))) save();
    },
    list: () => [...open.values()],
  };
}

/**
 * Slack, with every stream it opens recorded and every stream it closes
 * forgotten. A stop Slack refuses because the stream is already gone also
 * forgets it. Any other failed stop leaves it for the next start.
 */
export function journaled(slack: SlackApi, journal: Journal): SlackApi {
  return {
    respond: (url, body) => slack.respond(url, body),
    async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
      if (method === "chat.stopStream") {
        try {
          const done = await slack.call<T>(method, args);
          journal.close(String(args.channel), String(args.ts));
          return done;
        } catch (e) {
          if (gone(e)) journal.close(String(args.channel), String(args.ts));
          throw e;
        }
      }
      const done = await slack.call<T>(method, args);
      if (method === "chat.startStream") {
        const ts = (done as { ts?: string }).ts;
        if (ts) {
          journal.open({
            channel: String(args.channel),
            ts,
            ...(typeof args.thread_ts === "string" ? { threadTs: args.thread_ts } : {}),
            at: new Date().toISOString(),
          });
        }
      }
      return done;
    },
  };
}

/** Slack says the stream cannot be stopped because there is nothing left to stop. */
const GONE =
  /^(message_not_found|message_not_in_streaming_state|not_in_streaming_state|stream_not_found|cant_update_message|channel_not_found|is_archived|not_in_channel)$/;

function gone(e: unknown): boolean {
  return e instanceof SlackError && GONE.test(e.code);
}

/**
 * Close every stream a previous process left open, with a short note that
 * the answer was cut off, and settle each thread's session. A stream whose
 * close fails for want of the network is kept for the next start; one Slack
 * refuses outright is let go, since asking again would fail the same way.
 */
export async function recoverStreams(
  slack: SlackApi,
  journal: Journal,
  log: (line: string) => void = () => {},
): Promise<number> {
  let closed = 0;
  for (const s of journal.list()) {
    const at = { channel: s.channel, ts: s.ts };
    try {
      try {
        await slack.call("chat.stopStream", {
          ...at,
          chunks: [{ type: "markdown_text", text: words.interrupted }],
        });
      } catch (e) {
        if (gone(e)) throw e;
        if (!(e instanceof SlackError)) throw e;
        // The workspace would not take the note on the stream: stop it bare, and say so beside it.
        await slack.call("chat.stopStream", at);
        if (s.threadTs) {
          await slack
            .call("chat.postMessage", {
              channel: s.channel,
              thread_ts: s.threadTs,
              text: words.interrupted,
            })
            .catch(() => {});
        }
      }
      closed += 1;
      log(`closed a stream left open in ${s.channel} at ${s.ts} (opened ${s.at})`);
    } catch (e) {
      if (!(e instanceof SlackError)) {
        log(
          `a stream left open in ${s.channel} at ${s.ts} could not be closed yet: ${(e as Error).message}`,
        );
        continue;
      }
      log(`a stream left open in ${s.channel} at ${s.ts} is gone: ${e.code}`);
    }
    journal.close(s.channel, s.ts);
    if (s.threadTs) {
      await slack
        .call("agents.sessions.setStatus", {
          channel_id: s.channel,
          thread_ts: s.threadTs,
          status: "active",
        })
        .catch((e: Error) => log(`session status skipped: ${e.message}`));
    }
  }
  return closed;
}
