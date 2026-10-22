import { defaultCopy } from "@warp/components";
import type { HomeUpdate } from "@warp/core";
import type { ReactNode } from "react";

/** Before the first message, the prompt is the page. Chat owns its controls and submission. */
export function HomePage({ greeting, canRun, hasAgents, below, children }: {
  greeting: string;
  canRun: boolean;
  hasAgents: boolean;
  /** Quiet lines under the prompt: what needs the person, and what is new. */
  below?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section aria-label="New conversation" className="aui-scroll warp-fade flex min-h-0 flex-1 flex-col justify-center overflow-y-auto py-8">
      <h1 className="warp-display px-6 pb-6 text-center text-[2rem] font-semibold leading-tight text-aui-fg">{greeting}</h1>
      {!canRun ? (
        <p className="px-6 text-center text-aui-sm text-aui-muted">You do not have permission to start conversations in this workspace.</p>
      ) : !hasAgents ? (
        <p className="px-6 text-center text-aui-sm text-aui-muted">An agent needs to be configured before you can start a conversation.</p>
      ) : children}
      {below}
    </section>
  );
}

/**
 * Two quiet lines from the shared Home model: how many things need the person,
 * and What's new, the announcements of their projects, unread first. Each
 * opens where it lives. The headline has the room: on a narrow screen where it
 * was posted goes on a line of its own under it, and beside it only where
 * there is space for both. Nothing here moves: it waits on the person.
 */
export function HomeLines({ needsYou, updates, onInbox, onUpdate }: {
  needsYou: number;
  updates: HomeUpdate[];
  onInbox: () => void;
  onUpdate: (update: HomeUpdate) => void;
}) {
  const copy = defaultCopy;
  if (needsYou === 0 && updates.length === 0) return null;
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-3 px-6 pt-8 text-aui-sm">
      {needsYou > 0 && (
        <button type="button" onClick={onInbox} className="self-start text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent">
          {copy.mailbox.needsYou} · {needsYou}
        </button>
      )}
      {updates.length > 0 && (
        <section aria-label={copy.home.whatsNew} className="flex flex-col gap-1">
          <h2 className="text-aui-xs font-medium text-aui-muted">{copy.home.whatsNew}</h2>
          <ul className="flex flex-col">
            {updates.map((u) => (
              <li key={u.id}>
                <button
                  type="button"
                  onClick={() => onUpdate(u)}
                  className="flex w-full min-w-0 items-baseline gap-2 py-1 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  {/* The dot keeps its own column, so read and unread headlines line up. */}
                  <span className="flex w-1.5 shrink-0 self-start pt-2">
                    {u.unread && <span aria-label="Unread" className="size-1.5 rounded-full bg-aui-accent" />}
                  </span>
                  <span data-update-line className="flex min-w-0 flex-1 flex-col sm:flex-row sm:items-baseline sm:gap-2">
                    <span className={`min-w-0 truncate sm:flex-1 ${u.unread ? "font-medium text-aui-fg" : "text-aui-fg"}`}>{u.subject}</span>
                    <span className="min-w-0 truncate text-aui-xs text-aui-muted sm:max-w-[45%] sm:shrink-0">{copy.announcements.postedIn(u.projectTitle)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
