import { ActionId, type InboxMessage, messageState, openParts, type UiAction } from "@warp/core";
import { Mailbox, type MailboxShow } from "@warp/web";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useHeld, useInbox } from "./inbox.ts";
import { platformParts } from "./MailParts.tsx";
import { kinds, type PlatformMessage, type Source, shownAbout, type Transport, transport as platform } from "./mail.ts";
import { AgentMark, Empty, Problem, SkeletonRows, Stroke, type StrokeState } from "./ui.tsx";

/** The id in the address, so a message can be linked to. */
function linked(): string | null {
  try {
    return new URLSearchParams(window.location.search).get("message");
  } catch {
    return null;
  }
}
function link(id: string | null) {
  try {
    const url = new URL(window.location.href);
    if (id) url.searchParams.set("message", id);
    else url.searchParams.delete("message");
    window.history.replaceState(null, "", url);
  } catch {
    // The address just does not follow the selection.
  }
}

/** The edge a message is drawn with: its agent's color if an agent sent it, and how it stands. */
export function edgeOf(message: InboxMessage): { agent?: string; state: StrokeState } {
  const state = messageState(message);
  const failed = message.parts.some((p) => p.type === "failure" && !p.resolution);
  return {
    agent: message.from.kind === "agent" ? message.from.name : undefined,
    state: state !== "open" ? "done" : failed ? "failed" : openParts(message).length > 0 ? "waiting" : "idle",
  };
}

/**
 * What kind of message to show, as a row of chips like an email client's
 * categories. It scrolls sideways in a narrow pane rather than wrapping, so
 * the row stays one line and nothing spills out of it.
 */
export function KindChips({ value, onChange }: { value: string; onChange: (kind: string) => void }) {
  const options = [{ id: "", label: "Everything" }, ...kinds];
  return (
    <div role="group" aria-label="Kind" className="aui-scroll -mx-1 flex min-w-0 gap-1.5 overflow-x-auto px-1 pb-1">
      {options.map((k) => (
        <button
          key={k.id || "all"}
          type="button"
          aria-pressed={value === k.id}
          onClick={() => onChange(k.id)}
          className="inline-flex h-7 shrink-0 items-center rounded-full border border-aui-border px-3 text-aui-xs whitespace-nowrap text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:border-transparent aria-pressed:bg-aui-fg aria-pressed:text-aui-bg motion-reduce:transition-none"
        >
          {k.label}
        </button>
      ))}
    </div>
  );
}

/** An agent is told apart by its color, beside its name; rules and the system need no mark. */
const senderMark = (from: InboxMessage["from"]) => (from.kind === "agent" ? <AgentMark name={from.name} size={12} /> : null);

/**
 * warp's mailbox with the platform's own pieces on it: the parts it draws
 * itself, the agent's edge on what an agent sent, and everything a message is
 * about as things to open.
 */
export function MailboxView({
  messages,
  selectedId,
  onSelect,
  onAction,
  onOpen,
  viewer,
  toolbar,
  listEnd,
  openCount,
  show,
  onShow,
  messageActions,
  emptyOpen,
}: {
  /** The page's own controls for the message being read, such as Withdraw. */
  messageActions?: (message: PlatformMessage) => ReactNode;
  /** What an empty Open view says, when the default does not fit the page. */
  emptyOpen?: { title: string; hint: string };
  /** Open, resolved or all, when the host filters on the server. Leave out to let the mailbox filter what it is given. */
  show?: MailboxShow;
  onShow?: (show: MailboxShow) => void;
  /** Open messages across the whole inbox, when more exist than are loaded. */
  openCount?: number;
  messages: PlatformMessage[];
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onAction: (action: UiAction) => void;
  onOpen: (source: Source) => void;
  viewer?: string;
  toolbar?: ReactNode;
  listEnd?: ReactNode;
}) {
  const renderers = useMemo(() => platformParts(onOpen), [onOpen]);
  return (
    <Mailbox
      className="min-h-0 flex-1"
      messages={messages}
      selectedId={selectedId}
      onSelect={onSelect}
      onAction={onAction}
      viewer={viewer}
      renderers={renderers}
      renderSender={senderMark}
      toolbar={toolbar}
      listEnd={listEnd}
      openCount={openCount}
      show={show}
      onShow={onShow}
      messageActions={messageActions && ((m) => messageActions(m as PlatformMessage))}
      emptyOpen={emptyOpen}
      renderFrame={(message, children) => {
        const edge = edgeOf(message);
        const sources = shownAbout((message as PlatformMessage).about ?? []);
        return (
          <Stroke agent={edge.agent} state={edge.state} testId="mail-frame" className="flex min-h-0 min-w-0 flex-1 flex-col pl-4">
            {children}
            {sources.length > 0 && (
              <div className="flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1.5 border-[var(--warp-rule)] border-t pt-3 pb-1">
                <span className="text-aui-2xs font-medium tracking-wider text-aui-muted uppercase">About</span>
                <ul className="flex min-w-0 flex-wrap gap-1.5" aria-label="About">
                  {sources.map((s) => (
                    <li key={`${s.kind}:${s.id}`} className="min-w-0">
                      <button
                        type="button"
                        onClick={() => onOpen(s)}
                        className="inline-flex h-6 max-w-56 items-center gap-1 truncate rounded-full border border-aui-border px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
                      >
                        <span className="truncate">{s.label}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </Stroke>
        );
      }}
    />
  );
}

/**
 * The inbox. Alerts, failed runs, memory to review and limits that were
 * reached all arrive here as messages, and are resolved here where they can
 * be. The mailbox is warp's. This page loads it from the platform, keeps
 * it current, and sends each click back.
 */
export function InboxPage({
  signal,
  viewer,
  openId,
  kind: wantedKind,
  onKind,
  onOpen,
  onCounts,
  via = platform,
}: {
  /** The kind shown, when the address says it: "alert" shows only what rules fired. */
  kind?: string;
  onKind?: (kind: string) => void;
  /** Changes whenever the inbox may have. The first page is fetched again. */
  signal: unknown;
  /** Who is reading, shown on what they resolve until the platform confirms it. */
  viewer?: string;
  /** A message to open first, when the page was reached from a link to one. */
  openId?: string | null;
  /** Open what a message is about: a run, a rule, an agent, a project. */
  onOpen: (source: Source) => void;
  /** Reports the open and unread totals, for the sidebar's badge. */
  onCounts?: (counts: { open: number; unread: number }) => void;
  via?: Transport;
}) {
  const [ownKind, setOwnKind] = useState(wantedKind ?? "");
  const kind = onKind ? (wantedKind ?? "") : ownKind;
  const setKind = (next: string) => (onKind ? onKind(next) : setOwnKind(next));
  const [show, setShow] = useState<MailboxShow>("open");
  const [selected, setSelected] = useState<string | null>(() => openId ?? linked());
  const inbox = useInbox(kind, show, signal, via, viewer);
  const { messages, missing, keep } = useHeld(inbox.messages, selected, inbox.loading === "first", via);

  useEffect(() => {
    if (openId) setSelected(openId);
  }, [openId]);

  useEffect(() => link(selected), [selected]);

  useEffect(() => () => link(null), []);

  const { open, unread } = inbox;
  // biome-ignore lint/correctness/useExhaustiveDependencies: report when the totals change, not when the callback does
  useEffect(() => {
    onCounts?.({ open, unread });
  }, [open, unread]);

  function onAction(action: UiAction) {
    const message = messages.find((m) => m.id === action.payload.turnId);
    if (!message) return;
    // The message being read is kept in step even when the list no longer holds it.
    inbox.act(message, action).then(keep);
    if (action.id === ActionId.dismiss && selected === message.id) setSelected(null);
  }

  return (
    <div data-testid="inbox" className="warp-fade flex min-h-0 flex-1 flex-col gap-3 px-5 pt-8 pb-4 md:px-10">
      <p className="sr-only" aria-live="polite">
        {open === 0 ? "Nothing is waiting on you." : `${open} open, ${unread} unread.`}
      </p>
      <Problem message={inbox.problem} />
      {selected && missing && <Problem message="That message is not here. It may have been removed, or you may not have access to it." />}

      {inbox.failed ? (
        <Empty title="The inbox could not be loaded">It will be tried again when something changes. Reload the page to try now.</Empty>
      ) : inbox.loading === "first" && messages.length === 0 ? (
        <SkeletonRows count={5} height="3rem" className="max-w-md" />
      ) : (
        <MailboxView
          messages={messages}
          selectedId={selected}
          onSelect={setSelected}
          onAction={onAction}
          onOpen={onOpen}
          viewer={viewer}
          openCount={open}
          show={show}
          onShow={setShow}
          toolbar={<KindChips value={kind} onChange={setKind} />}
          listEnd={
            (inbox.hasMore || inbox.loading === "more") && (
              <div className="flex justify-center py-3">
                <button
                  type="button"
                  onClick={inbox.more}
                  disabled={inbox.loading === "more"}
                  className="inline-flex h-6 items-center rounded-full border border-[var(--warp-rule)] px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-default"
                >
                  {inbox.loading === "more" ? "Loading" : inbox.moreFailed ? "Could not load. Try again" : "See more"}
                </button>
              </div>
            )
          }
        />
      )}
    </div>
  );
}
