import { defaultCopy } from "@humagen/components";
import { ActionId, type InboxMessage, messageState, openParts, type UiAction } from "@humagen/core";
import { Mailbox, type MailboxShow } from "@humagen/web";
import { SlidersIcon } from "@humagen/render-web";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Button } from "@humagen/render-web/ui/button";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { useHeld, useInbox } from "./inbox.ts";
import { platformParts } from "./MailParts.tsx";
import { kinds, type PlatformMessage, type Source, shownAbout, type Transport, transport as platform } from "./mail.ts";
import { AgentMark, Empty, Page, Popover, Problem, SkeletonRows, Stroke, type StrokeState, SearchField, button } from "./ui.tsx";

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

/** One native filter keeps every message kind reachable without a scrolling chip row. */
export function KindFilter({ value, onChange }: { value: string; onChange: (kind: string) => void }) {
  const copy = defaultCopy.inboxControls;
  return <div className="flex min-w-0 items-center gap-2">
    <SlidersIcon size={16} className="shrink-0 text-aui-muted" />
    <NativeSelect aria-label={copy.filter} value={value} onChange={event=>onChange(event.target.value)} className="min-w-36">
      {[{id:"",label:copy.all}, ...kinds].map(kind=><option key={kind.id} value={kind.id}>{kind.label}</option>)}
    </NativeSelect>
  </div>;
}

/** Search and all notification filters share one responsive toolbar. */
export function NotificationFilters({ query, onQuery, kind, onKind, show, onShow }: {
  query: string;
  onQuery: (query: string) => void;
  kind: string;
  onKind: (kind: string) => void;
  show: MailboxShow;
  onShow: (show: MailboxShow) => void;
}) {
  const c = defaultCopy.inboxControls;
  const count = Number(Boolean(kind)) + Number(show !== "all");
  return <div role="group" aria-label={c.filters} className="flex min-w-0 items-center gap-2">
    <SearchField label={c.search} value={query} onChange={onQuery} className="min-w-0 flex-1" />
    <Popover label={c.notificationFilter} align="right" className={`${button} shrink-0 gap-2`} button={<><SlidersIcon size={16} /><span>{c.filterButton}</span>{count > 0 && <span className="rounded-aui-sm bg-aui-subtle px-1.5 text-aui-xs tabular-nums">{count}</span>}</>}>
      {close => <div className="flex flex-col gap-4 p-1">
        <label className="flex flex-col gap-1.5 text-aui-sm">{c.status}
          <NativeSelect aria-label={c.status} value={show} onChange={event => onShow(event.target.value as MailboxShow)}>
            {(["all", "open", "resolved"] as const).map(state => <option key={state} value={state}>{defaultCopy.show[state]}</option>)}
          </NativeSelect>
        </label>
        <label className="flex flex-col gap-1.5 text-aui-sm">{c.type}
          <NativeSelect aria-label={c.type} value={kind} onChange={event => onKind(event.target.value)}>
            {[{id:"",label:c.all}, ...kinds].map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
          </NativeSelect>
        </label>
        <div className="flex items-center justify-between gap-3">
          <Button variant="ghost" size="sm" disabled={count === 0} onClick={() => { onKind(""); onShow("all"); }}>{c.reset}</Button>
          <Button variant="outline" size="sm" onClick={close}>{c.done}</Button>
        </div>
      </div>}
    </Popover>
  </div>;
}

/** An agent is told apart by its color, beside its name; rules and the system need no mark. */
const senderMark = (from: InboxMessage["from"]) => (from.kind === "agent" ? <AgentMark name={from.name} size={12} /> : null);

/**
 * humagen's mailbox with the platform's own pieces on it: the parts it draws
 * itself, the agent's edge on what an agent sent, and everything a message is
 * about as things to open.
 */
export function MailboxView({
  messages,
  query,
  selectedId,
  selectedMessage,
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
  heading,
  groupByAction,
}: {
  /** `false` where the page is the inbox, so its title and count are the page's. */
  heading?: 2 | false;
  groupByAction?: boolean;
  /** The page's own controls for the message being read, such as Withdraw. */
  messageActions?: (message: PlatformMessage) => ReactNode;
  /** What an empty Open view says, when the default does not fit the page. */
  emptyOpen?: { title: string; hint: string };
  /** Open, resolved or all, when the host filters on the server. Leave out to let the mailbox filter what it is given. */
  show?: MailboxShow | null;
  onShow?: (show: MailboxShow) => void;
  /** Open messages across the whole inbox, when more exist than are loaded. */
  openCount?: number;
  query?: string;
  messages: PlatformMessage[];
  selectedId: string | null;
  selectedMessage?: PlatformMessage;
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
      filter={query?.trim() ? { query } : undefined}
      className="min-h-0 flex-1"
      messages={messages}
      selectedId={selectedId}
      selectedMessage={selectedMessage}
      onSelect={onSelect}
      onAction={onAction}
      viewer={viewer}
      renderers={renderers}
      renderSender={senderMark}
      toolbar={toolbar}
      listEnd={listEnd}
      openCount={openCount}
      heading={heading}
      groupByAction={groupByAction}
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
              <div className="mt-5 flex shrink-0 flex-col gap-2 border-[var(--humagen-rule)] border-t pt-4 pb-3">
                <h4 className="text-aui-xs font-medium text-aui-muted">{defaultCopy.mailbox.about}</h4>
                <ul className="flex min-w-0 flex-wrap gap-1.5" aria-label="About">
                  {sources.map((s) => (
                    <li key={`${s.kind}:${s.id}`} className="min-w-0">
                      <button
                        type="button"
                        onClick={() => onOpen(s)}
                        className="inline-flex min-h-10 max-w-full items-center gap-1 rounded-aui border border-aui-border px-3 py-2 text-left text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
                      >
                        <span className="min-w-0 break-words">{s.label}</span>
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
 * be. The mailbox is humagen's. This page loads it from the platform, keeps
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
  onSelection,
  actions,
  notice,
  messageActions,
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
  /** Hosts with a router keep selection and the copied address in one state. */
  onSelection?: (id: string | null) => void;
  actions?: ReactNode;
  notice?: ReactNode;
  messageActions?: (message: PlatformMessage) => ReactNode;
  via?: Transport;
}) {
  const [ownKind, setOwnKind] = useState(wantedKind ?? "");
  const kind = onKind ? (wantedKind ?? "") : ownKind;
  const setKind = (next: string) => (onKind ? onKind(next) : setOwnKind(next));
  const [show, setShow] = useState<MailboxShow>("all");
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(() => openId === undefined ? linked() : openId);
  const inbox = useInbox(kind, show, signal, via, viewer);
  const { messages, missing, keep } = useHeld(inbox.messages, selected, inbox.loading === "first", via);

  useEffect(() => {
    if (openId !== undefined) setSelected(openId);
  }, [openId]);

  useEffect(() => { if (!onSelection) link(selected); }, [selected, onSelection]);

  useEffect(() => () => { if (!onSelection) link(null); }, [onSelection]);

  const select = (id: string | null) => {
    setSelected(id);
    onSelection?.(id);
  };

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
    if (action.id === ActionId.dismiss && selected === message.id) select(null);
  }

  return (
    <Page title={defaultCopy.notifications.title} actions={actions} meta={open > 0 ? <span>{defaultCopy.mailbox.openCount(open)}</span> : undefined}>
      <div data-testid="inbox" className="flex min-h-0 flex-1 flex-col gap-3">
      <p className="sr-only" aria-live="polite">
        {open === 0 ? "Nothing is waiting on you." : `${open} open, ${unread} unread.`}
      </p>
      <Problem message={inbox.problem} />
      {notice}
      {selected && missing && <Problem message="That message is not here. It may have been removed, or you may not have access to it." />}

      <NotificationFilters query={query} onQuery={setQuery} kind={kind} onKind={setKind} show={show} onShow={setShow} />
      {query.trim() && inbox.hasMore && <p className="text-aui-xs text-aui-muted">{defaultCopy.inboxControls.loadedHint}</p>}
      {inbox.failed ? (
        <Empty title={defaultCopy.notifications.failed}>It will be tried again when something changes. Reload the page to try now.</Empty>
      ) : inbox.loading === "first" && messages.length === 0 ? (
        <SkeletonRows count={5} height="3rem" className="max-w-md" />
      ) : (
        <MailboxView
          heading={false}
          groupByAction={false}
          query={query}
          messages={inbox.messages}
          selectedId={selected}
          selectedMessage={messages.find(message => message.id === selected)}
          onSelect={select}
          onAction={onAction}
          onOpen={onOpen}
          viewer={viewer}
          openCount={open}
          show={null}
          messageActions={messageActions}
          listEnd={
            (inbox.hasMore || inbox.loading === "more") && (
              <div className="flex justify-center py-3">
                <button
                  type="button"
                  onClick={inbox.more}
                  disabled={inbox.loading === "more"}
                  className="inline-flex h-6 items-center rounded-full border border-[var(--humagen-rule)] px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-default"
                >
                  {inbox.loading === "more" ? "Loading" : inbox.moreFailed ? "Could not load. Try again" : "See more"}
                </button>
              </div>
            )
          }
        />
      )}
      </div>
    </Page>
  );
}
