import { defaultCopy } from "@humagen/components";
import { CheckCircleIcon, XCircleIcon } from "@humagen/render-web";
import { useState } from "react";
import type { ThreadMessage } from "./api.ts";
import { took } from "./format.ts";
import { MessageTime } from "./MessageTime.tsx";

/*
 * A composer message that starts with "!" is a command, run at once in the
 * session's sandbox as the person (`command: true` on the post). The server
 * posts it with how it came out, starts no run, and sends it to the agent
 * with the next message. This module holds what the composer and the
 * conversation need for it, so neither grows: reading a draft, the hint
 * while one is typed, and the command's block in the conversation.
 */

const words = defaultCopy.bangCommand;

/** The command a draft runs: the text after its leading "!", or null when it is no command. */
export function bangCommand(text: string): string | null {
  const t = text.trimStart();
  return t.startsWith("!") ? t.slice(1).trim() : null;
}

/**
 * Why a command may not run where the composer posts, or null when it may.
 * `terminal` is the server's word (GET /api/sessions/options); until it is
 * read the server decides. A command runs only in a session: anywhere else
 * a leading "!" is plain text, so this is undefined there.
 */
export function commandsHere(
  terminal: boolean | undefined,
  session: boolean,
): string | null | undefined {
  if (!session) return undefined;
  if (terminal === false) return words.noSandbox;
  return null;
}

/** Why a "!" draft is kept, not sent: no command, or files that would run with it. */
export function commandHold(
  draft: string,
  files: number,
  commands: string | null | undefined,
): string | null {
  if (commands === undefined) return null;
  const command = bangCommand(draft);
  if (command === null) return null;
  if (files > 0) return words.withFiles;
  if (command === "") return words.empty;
  return null;
}

/**
 * What the composer says while its draft starts with "!": what will happen,
 * or why nothing can. Null for any other draft.
 */
export function commandHint(
  draft: string,
  refused: string | null | undefined,
  home = false,
): string | null {
  const command = bangCommand(draft);
  if (command === null || refused === undefined) return null;
  if (refused) return refused;
  if (command === "") return words.empty;
  return home ? words.hintHome : words.hint;
}

/** Lines past this fold to the last FOLD_TO, with Show All. */
const FOLD_OVER = 40;
const FOLD_TO = 24;

/**
 * A command a person ran, as the conversation shows it: who ran it and
 * when, the command, how it came out and how long it took, and its output,
 * folded when it is long. Their own sits on the right, as their messages do.
 */
export function CommandMessage({
  m,
  mine,
  focused,
}: {
  m: ThreadMessage & { command: NonNullable<ThreadMessage["command"]> };
  mine: boolean;
  focused: boolean;
}) {
  const [all, setAll] = useState(false);
  const c = m.command;
  const who = mine ? words.you : m.name || m.by;
  const failed = c.timed_out || c.exit_code !== 0;
  const state = c.timed_out ? words.timedOut : words.exited(c.exit_code);
  const lines = c.output ? c.output.replace(/\n$/, "").split("\n") : [];
  const folded = lines.length > FOLD_OVER && !all;
  const shown = folded ? lines.slice(-FOLD_TO) : lines;
  return (
    <article
      id={`message-${m.id}`}
      data-turn-id={m.id}
      data-side={mine ? "mine" : "theirs"}
      data-command
      aria-label={words.label(who, c.command)}
      tabIndex={-1}
      className={`flex flex-col gap-1 rounded-aui ${mine ? "items-end" : "items-start"} ${focused ? "outline-2 outline-offset-4 outline-aui-accent" : ""}`}
    >
      <div className="w-full max-w-full min-w-0 rounded-aui-lg border border-aui-border bg-aui-surface sm:max-w-[85%]">
        <p className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 border-aui-border border-b px-3 py-2 text-aui-xs text-aui-muted">
          <span className="font-medium text-aui-fg">{who}</span>
          <span>{words.ran}</span>
          <span className="inline-flex items-center gap-1.5 text-aui-fg">
            {failed ? (
              <XCircleIcon size={14} aria-hidden className="shrink-0 text-aui-bad" />
            ) : (
              <CheckCircleIcon size={14} aria-hidden className="shrink-0 text-aui-ok" />
            )}
            {state}
          </span>
          <span className="tabular-nums">{took(c.duration_ms)}</span>
          <MessageTime at={m.at} />
        </p>
        <div className="flex flex-col gap-1 px-3 py-2">
          <pre className="whitespace-pre-wrap break-words font-aui-mono text-aui-sm text-aui-fg">
            <span aria-hidden className="select-none text-aui-muted">
              ${" "}
            </span>
            <span className="font-semibold">{c.command}</span>
          </pre>
          {(c.truncated || folded) && <p className="text-aui-xs text-aui-muted">{words.cut}</p>}
          {shown.length > 0 ? (
            <pre
              data-testid="command-output"
              className="max-h-96 overflow-auto whitespace-pre-wrap break-words font-aui-mono text-aui-sm text-aui-fg"
            >
              {shown.join("\n")}
            </pre>
          ) : (
            <p className="text-aui-xs text-aui-muted">{words.noOutput}</p>
          )}
          {lines.length > FOLD_OVER && (
            <button
              type="button"
              aria-expanded={all}
              onClick={() => setAll((v) => !v)}
              className="inline-flex min-h-9 items-center self-start rounded-aui-sm text-aui-xs text-aui-muted underline underline-offset-4 hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
            >
              {all ? words.showLess : words.showAll(lines.length)}
            </button>
          )}
        </div>
      </div>
    </article>
  );
}
