import { defaultCopy, MessagePartView, messageByline } from "@agent-ui/components";
import {
  type ActionHandler,
  ActionId,
  filterMessages,
  type InboxItemState,
  type InboxMessage,
  type MessageFilter,
  type MessagePart,
  messageKind,
  messagePreview,
  messageState,
  openMessageCount,
  openParts,
  sortMessages,
  type Tone,
} from "@agent-ui/core";
import { Context } from "@agent-ui/primitives";
import { buttonClass, CheckCircleIcon, WebSurface } from "@agent-ui/render-web";
import { type KeyboardEvent, type ReactNode, useEffect, useRef, useState } from "react";

/** Draws one part of a message. Runs inside the reading pane, so primitives and actions work. */
export type PartRenderer = (props: {
  message: InboxMessage;
  part: MessagePart;
  onAction: ActionHandler;
  viewer?: string;
}) => ReactNode;

export type MailboxShow = "open" | "resolved" | "all";

export interface MailboxProps {
  messages: InboxMessage[];
  /** Controlled selection. Leave out to let the mailbox keep its own. */
  selectedId?: string | null;
  onSelect?: (id: string | null) => void;
  /**
   * Every click, in the shared contract: `payload.turnId` is the message id and
   * `payload.target` the part. Opening an unread message sends `ActionId.read`;
   * Dismiss sends `ActionId.dismiss`. Apply them with `resolveMessage` from core.
   */
  onAction?: ActionHandler;
  viewer?: string;
  /**
   * Renderers by part type, or by `name` for custom parts. They replace the
   * built-in ones, so a host can draw its own budget meter or add a part
   * type without forking this component.
   */
  renderers?: Record<string, PartRenderer>;
  /**
   * Narrows the one list, for a host that offers its own filter menu. The
   * mailbox never splits by kind itself: every message carries its kind as a tag.
   */
  filter?: MessageFilter;
  /** Rendered above the list: the host's own filter or search. */
  toolbar?: ReactNode;
  /** Rendered after the last row: a "See more" control, a loading note. */
  listEnd?: ReactNode;
  /** Wrap the reading pane, for example to mark who sent the message. */
  renderFrame?: (message: InboxMessage, children: ReactNode) => ReactNode;
  defaultShow?: MailboxShow;
  /**
   * Controlled state filter, for a host that pages on the server. `null` hides
   * the control and shows whatever `messages` holds.
   */
  show?: MailboxShow | null;
  onShow?: (show: MailboxShow) => void;
  /** The number in the header, when `messages` is only one page of the inbox. */
  openCount?: number;
  /** ISO time used to decide whether a snooze has ended. */
  now?: string;
  className?: string;
}

const SHOW: Record<MailboxShow, InboxItemState[] | null> = {
  open: ["open"],
  resolved: ["resolved", "dismissed", "snoozed"],
  all: null,
};

/** Severity is always said in words (copy.severity); the tint is only a second cue. */
const SEVERITY_TINT: Partial<Record<Tone, string>> = {
  bad: "bg-aui-bad/10 text-aui-bad",
  warn: "bg-aui-warn/10 text-aui-warn",
};

function when(at: string): string {
  if (!at) return "";
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Round for agents and people, square for rules and the system: shape says who is speaking. */
function SenderMark({ from }: { from: InboxMessage["from"] }) {
  const round = from.kind === "agent" || from.kind === "person";
  return (
    <span
      aria-hidden="true"
      className={`inline-flex size-6 shrink-0 items-center justify-center bg-aui-subtle font-semibold text-aui-2xs text-aui-muted ${round ? "rounded-full" : "rounded-aui-sm"}`}
    >
      {from.name.charAt(0).toUpperCase()}
    </span>
  );
}

/**
 * The inbox as a mailbox: a list of messages and a reading pane that shows the
 * selected message's parts in order, each with the controls that resolve it.
 * In a narrow container the list and the pane are separate views.
 *
 * Keys in the list: j/k or the arrows move, Enter opens the message and puts
 * focus on its first control, e dismisses. Nothing here animates: everything
 * in an inbox is waiting on a person.
 */
export function Mailbox({
  messages,
  selectedId,
  onSelect,
  onAction = () => {},
  viewer,
  renderers = {},
  toolbar,
  listEnd,
  renderFrame,
  filter,
  defaultShow = "open",
  show: controlledShow,
  onShow,
  openCount,
  now,
  className,
}: MailboxProps) {
  const copy = defaultCopy;
  const [ownShow, setOwnShow] = useState<MailboxShow>(defaultShow);
  const show = controlledShow === undefined ? ownShow : controlledShow;
  const setShow = (next: MailboxShow) => {
    if (controlledShow === undefined) setOwnShow(next);
    onShow?.(next);
  };
  const [own, setOwn] = useState<string | null>(null);
  const pane = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLUListElement>(null);

  const states = show ? SHOW[show] : null;
  const shown = sortMessages(filter ? filterMessages(messages, filter, now) : messages, now).filter(
    (m) => !states || states.includes(messageState(m, now)),
  );
  const current = selectedId === undefined ? own : selectedId;
  // Keep showing a message that just left this tab, so its record is read where it was resolved.
  const selected = messages.find((m) => m.id === current) ?? null;
  const count = openCount ?? openMessageCount(messages, now);

  // Reading is showing: a row click and a deep link (`selectedId`) both mark it read, once.
  const read = useRef(new Set<string>());
  useEffect(() => {
    if (!selected?.unread || read.current.has(selected.id)) return;
    read.current.add(selected.id);
    onAction({ id: ActionId.read, payload: { turnId: selected.id, target: selected.id } });
  }, [selected, onAction]);

  const select = (id: string | null) => {
    if (selectedId === undefined) setOwn(id);
    onSelect?.(id);
  };
  const dismiss = (message: InboxMessage) =>
    onAction({ id: ActionId.dismiss, payload: { turnId: message.id, target: message.id } });

  const onListKey = (e: KeyboardEvent) => {
    const i = shown.findIndex((m) => m.id === current);
    const move = (to: number) => {
      const next = shown[Math.max(0, Math.min(shown.length - 1, to))];
      if (!next) return;
      e.preventDefault();
      select(next.id);
      list.current?.querySelector<HTMLElement>(`[data-message="${CSS.escape(next.id)}"]`)?.focus();
    };
    if (e.key === "j" || e.key === "ArrowDown") move(i + 1);
    else if (e.key === "k" || e.key === "ArrowUp") move(i - 1);
    else if (e.key === "Enter" && selected) {
      e.preventDefault();
      // Land on the first thing that resolves it, or on the pane itself to read.
      const first = pane.current?.querySelector<HTMLElement>(
        '[data-aui="button"][data-style="primary"], [data-aui="button"], textarea',
      );
      (first ?? pane.current)?.focus();
    } else if (e.key === "e" && selected && messageState(selected, now) === "open") {
      e.preventDefault();
      dismiss(selected);
    }
  };

  const reading = selected ? (
    <article
      data-aui="message"
      data-from-kind={selected.from.kind}
      data-from-name={selected.from.name}
      data-severity={selected.severity ?? "neutral"}
      data-state={messageState(selected, now)}
      className="flex min-h-0 flex-1 flex-col"
    >
      <header className="flex items-start gap-2 border-aui-border border-b pb-2">
        <button
          type="button"
          onClick={() => select(null)}
          className={`${buttonClass("default")} h-6 px-2 @2xl:hidden`}
        >
          {copy.mailbox.back}
        </button>
        <div className="min-w-0 flex-1">
          <h3 className="font-semibold text-aui-base">{selected.subject}</h3>
          <WebSurface>
            <Context items={messageByline(selected, copy)} />
          </WebSurface>
        </div>
        {messageState(selected, now) === "open" && (
          <button
            type="button"
            onClick={() => dismiss(selected)}
            className={`${buttonClass("default")} h-6 px-2`}
          >
            {copy.dismiss}
          </button>
        )}
      </header>
      <div className="aui-scroll min-h-0 flex-1 overflow-y-auto py-2">
        <WebSurface onAction={onAction}>
          {selected.parts.map((part) => {
            const render = renderers[part.type === "custom" ? part.name : part.type];
            return (
              <section
                key={part.id}
                data-aui-part={part.type}
                aria-label={copy.partKind[part.type]}
              >
                {render ? (
                  render({ message: selected, part, onAction, viewer })
                ) : (
                  <MessagePartView message={selected} part={part} viewer={viewer} copy={copy} />
                )}
              </section>
            );
          })}
        </WebSurface>
        {messageState(selected, now) === "open" && openParts(selected).length === 0 && (
          <p className="pt-2 text-aui-muted text-aui-xs">{copy.mailbox.readOnly}</p>
        )}
      </div>
    </article>
  ) : (
    <div className="flex flex-1 flex-col items-center justify-center gap-1.5 py-10 text-center">
      <p className="text-aui-muted text-aui-sm">{copy.mailbox.pick}</p>
    </div>
  );

  return (
    <section
      aria-label={copy.inbox}
      className={`@container flex min-h-0 min-w-0 flex-col ${className ?? ""}`}
    >
      <header className="flex flex-wrap items-center gap-2 border-aui-border border-b pb-1.5">
        <h2 className="font-semibold text-aui-base">{copy.inbox}</h2>
        {count > 0 && (
          <span className="rounded-full bg-aui-accent-soft px-1.5 font-medium text-aui-2xs text-aui-fg tabular-nums">
            {count} open
          </span>
        )}
        {/* Open or resolved is state, so it is a quiet control, not a row of tabs. */}
        {show && (
          <label className="ml-auto flex items-center gap-1.5 text-aui-muted text-aui-xs">
            {copy.show.label}
            <select
              value={show}
              onChange={(e) => setShow(e.target.value as MailboxShow)}
              className="h-6 rounded-aui-sm border border-aui-border bg-aui-bg px-1 text-aui-fg text-aui-xs focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              {(Object.keys(SHOW) as MailboxShow[]).map((id) => (
                <option key={id} value={id}>
                  {copy.show[id]}
                </option>
              ))}
            </select>
          </label>
        )}
      </header>
      <div className="flex min-h-0 min-w-0 flex-1 gap-3 pt-2">
        <div
          className={`min-h-0 w-full min-w-0 flex-col @2xl:flex @2xl:w-80 @2xl:shrink-0 ${selected ? "hidden" : "flex"}`}
        >
          {toolbar}
          {shown.length === 0 ? (
            <div className="flex flex-col items-center gap-1.5 py-10 text-center">
              <CheckCircleIcon size={20} className="text-aui-ok" />
              <p className="font-medium text-aui-sm">{copy.mailbox.empty}</p>
              <p className="text-aui-muted text-aui-xs">{copy.mailbox.emptyHint}</p>
            </div>
          ) : (
            <ul
              ref={list}
              onKeyDown={onListKey}
              className="aui-scroll min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden"
            >
              {shown.map((m) => {
                const severity = m.severity ? copy.severity[m.severity] : undefined;
                const state = messageState(m, now);
                return (
                  <li key={m.id}>
                    <button
                      type="button"
                      data-message={m.id}
                      aria-current={m.id === current ? "true" : undefined}
                      onClick={() => select(m.id)}
                      className="flex w-full min-w-0 items-start gap-2 overflow-hidden rounded-aui px-2 py-1.5 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current]:bg-aui-subtle"
                    >
                      <SenderMark from={m.from} />
                      <span className="min-w-0 flex-1">
                        <span className="flex min-w-0 items-baseline gap-1.5">
                          <span
                            className={`min-w-0 truncate text-aui-sm ${m.unread ? "font-semibold text-aui-fg" : "text-aui-fg"}`}
                          >
                            {m.subject}
                          </span>
                          <span className="ml-auto shrink-0 text-aui-2xs text-aui-muted tabular-nums">
                            {when(m.at)}
                          </span>
                        </span>
                        <span className="block truncate text-aui-muted text-aui-xs">
                          {m.from.name} · {messagePreview(m)}
                        </span>
                        <span className="flex items-center gap-1.5 pt-0.5 text-aui-2xs text-aui-muted">
                          <span
                            data-aui="kind"
                            className={`rounded-aui-sm px-1.5 font-medium ${(m.severity && SEVERITY_TINT[m.severity]) || "bg-aui-subtle text-aui-muted"}`}
                          >
                            {m.tag ?? copy.partKind[messageKind(m)]}
                            {severity && ` · ${severity}`}
                          </span>
                          {state !== "open" && <span className="capitalize">{state}</span>}
                        </span>
                      </span>
                      {m.unread && (
                        <span className="mt-1.5 size-2 shrink-0 rounded-full bg-aui-accent">
                          <span className="sr-only">{copy.mailbox.unread}</span>
                        </span>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
          {listEnd}
          <p className="hidden pt-1 text-aui-2xs text-aui-muted @2xl:block">
            <kbd className="font-medium">j</kbd>/<kbd className="font-medium">k</kbd> to move,{" "}
            <kbd className="font-medium">Enter</kbd> to open, <kbd className="font-medium">e</kbd>{" "}
            to dismiss
          </p>
        </div>
        <div
          ref={pane}
          tabIndex={-1}
          className={`min-h-0 min-w-0 flex-1 flex-col outline-none @2xl:flex ${selected ? "flex" : "hidden"}`}
        >
          {selected && renderFrame ? renderFrame(selected, reading) : reading}
        </div>
      </div>
    </section>
  );
}
