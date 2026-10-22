import { defaultCopy, MessagePartView } from "@warp/components";
import {
  type ActionHandler,
  ActionId,
  filterMessages,
  type InboxItemState,
  type InboxMessage,
  isAnnouncement,
  isResolvable,
  type MessageFilter,
  type MessagePart,
  messagePreview,
  messageState,
  messageTemplate,
  openMessageCount,
  openParts,
  sortMessages,
  type TemplateId,
  type Tone,
} from "@warp/core";
import {
  AlertOctagonIcon,
  AlertTriangleIcon,
  BrainIcon,
  buttonClass,
  CheckCircleIcon,
  ClockIcon,
  EyeIcon,
  FileIcon,
  GaugeIcon,
  InfoIcon,
  MegaphoneIcon,
  MessageIcon,
  ShieldIcon,
  SparkleIcon,
  WebSurface,
  XCircleIcon,
  XIcon,
} from "@warp/render-web";
import {
  type ComponentType,
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";

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
   * Dismiss sends `ActionId.dismiss`, Snooze `ActionId.snooze` with the time in
   * `option`, and Show for Me, on a message the reader hid for themselves
   * (`hidden`), `ActionId.unhide`. Apply them with `resolveMessage` from core.
   */
  onAction?: ActionHandler;
  viewer?: string;
  /**
   * Renderers by part type, or by `name` for custom parts. They replace the
   * built-in ones, so a host can draw its own budget meter or add a part
   * type without forking this component.
   */
  renderers?: Record<string, PartRenderer>;
  /** Draws who sent a message beside their name, such as an agent's mark. */
  renderSender?: (from: InboxMessage["from"]) => ReactNode;
  /**
   * Narrows the one list, for a host that offers its own filter menu. The
   * mailbox never splits by kind itself: every message carries its kind as a tag.
   */
  filter?: MessageFilter;
  /** Rendered under the title, across the whole mailbox: the host's own filters. */
  toolbar?: ReactNode;
  /** Rendered after the last row: a "See more" control, a loading note. */
  listEnd?: ReactNode;
  /** Wrap the reading pane, for example to mark who sent the message. */
  renderFrame?: (message: InboxMessage, children: ReactNode) => ReactNode;
  /**
   * The host's own controls for the message being read, beside Snooze and
   * Hide: withdrawing what the reader posted, say. Return nothing for none.
   */
  messageActions?: (message: InboxMessage) => ReactNode;
  /** What an empty Open view says, for a host whose inbox holds other things. */
  emptyOpen?: { title: string; hint: string };
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
  /**
   * The level of the mailbox's title: 1 where the mailbox is the page, as the
   * console's Inbox is, so the page has its one title; 2 (the default) inside
   * a page that has its own, such as a project's.
   */
  heading?: 1 | 2;
  className?: string;
}

const SHOW: Record<MailboxShow, InboxItemState[] | null> = {
  open: ["open"],
  resolved: ["resolved", "dismissed", "snoozed"],
  all: null,
};

type Icon = ComponentType<{ size?: number; className?: string }>;

/**
 * Each template's icon and tint. The tint is a second cue only: the
 * template's name and the severity are always said in words beside it.
 */
const TEMPLATE_LOOK: Record<TemplateId, { icon: Icon; tint: string }> = {
  alert: { icon: AlertTriangleIcon, tint: "bg-aui-warn/12 text-aui-warn" },
  approval: { icon: ShieldIcon, tint: "bg-aui-accent-soft text-aui-accent" },
  question: { icon: MessageIcon, tint: "bg-aui-accent-soft text-aui-accent" },
  failure: { icon: XCircleIcon, tint: "bg-aui-bad/12 text-aui-bad" },
  memory: { icon: BrainIcon, tint: "bg-aui-accent-soft text-aui-accent" },
  limit: { icon: GaugeIcon, tint: "bg-aui-warn/12 text-aui-warn" },
  artifact: { icon: FileIcon, tint: "bg-aui-fg/10 text-aui-fg" },
  result: { icon: SparkleIcon, tint: "bg-aui-ok/12 text-aui-ok" },
  note: { icon: InfoIcon, tint: "bg-aui-fg/10 text-aui-muted" },
  mention: { icon: MessageIcon, tint: "bg-aui-accent-soft text-aui-accent" },
  announcement: { icon: MegaphoneIcon, tint: "bg-aui-accent-soft text-aui-accent" },
};

/**
 * What setting a message aside is called. An announcement is everyone's, and
 * putting it away hides only the reader's own copy, so it says so. An agent's
 * draft is its owners' to decide, and dismissing it does so for all of them.
 */
function hideLabel(message: InboxMessage): string {
  return isAnnouncement(message) ? defaultCopy.announcements.hideForMe : defaultCopy.dismiss;
}

/** The template's look, raised to the message's severity: an urgent alert is red. */
function lookOf(message: InboxMessage): { icon: Icon; tint: string } {
  const template = messageTemplate(message);
  const look = TEMPLATE_LOOK[template];
  if (message.severity === "bad" && template === "alert") {
    return { icon: AlertOctagonIcon, tint: "bg-aui-bad/12 text-aui-bad" };
  }
  return look;
}

/** Severity is always said in words (copy.severity); the tint is only a second cue. */
const SEVERITY_TINT: Partial<Record<Tone, string>> = {
  bad: "text-aui-bad",
  warn: "text-aui-warn",
};

const MINUTE = 60_000;
const DAY = 24 * 60 * MINUTE;

/** A list's time: "Now", "12m", "3h", "Yesterday", "Oct 20". Short, so the subject keeps the room. */
export function shortWhen(at: string, now = Date.now()): string {
  const t = Date.parse(at);
  if (Number.isNaN(t)) return "";
  const ago = now - t;
  if (ago < MINUTE) return "Now";
  if (ago < 60 * MINUTE) return `${Math.floor(ago / MINUTE)}m`;
  const then = new Date(t);
  const today = new Date(now);
  const sameDay = then.toDateString() === today.toDateString();
  if (sameDay || ago < 6 * 60 * MINUTE) return `${Math.floor(ago / (60 * MINUTE))}h`;
  if (new Date(now - DAY).toDateString() === then.toDateString()) return "Yesterday";
  return then.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    ...(then.getFullYear() === today.getFullYear() ? {} : { year: "numeric" }),
  });
}

/** The reading pane's time, in full: "Tue, Oct 21, 1:16 AM". */
function fullWhen(at: string): string {
  const d = new Date(at);
  if (!at || Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/** Tomorrow at 9 in the reader's own time: when a snoozed message comes back. */
function tomorrowMorning(): string {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  d.setHours(9, 0, 0, 0);
  return d.toISOString();
}

/** What the message is, as an icon in its template's tint. */
function TemplateMark({ message, size = "sm" }: { message: InboxMessage; size?: "sm" | "lg" }) {
  const { icon: Glyph, tint } = lookOf(message);
  return (
    <span
      aria-hidden="true"
      data-aui="template-mark"
      className={`inline-flex shrink-0 items-center justify-center rounded-full ${tint} ${size === "lg" ? "size-9" : "size-8"}`}
    >
      <Glyph size={size === "lg" ? 18 : 16} />
    </span>
  );
}

/** The template's name and the severity, in words: "Alert · Urgent". */
function TemplateTag({ message }: { message: InboxMessage }) {
  const copy = defaultCopy;
  const severity = message.severity ? copy.severity[message.severity] : undefined;
  return (
    <span
      data-aui="kind"
      className="inline-flex w-fit shrink-0 items-center gap-1 whitespace-nowrap rounded-aui-sm bg-aui-subtle px-1.5 py-px text-aui-2xs text-aui-muted"
    >
      <span>{message.tag ?? copy.template[messageTemplate(message)]}</span>
      {severity && (
        <>
          <span aria-hidden="true">·</span>
          <span
            className={`font-medium ${(message.severity && SEVERITY_TINT[message.severity]) || ""}`}
          >
            {severity}
          </span>
        </>
      )}
    </span>
  );
}

/** Parts whose shared card already draws its own box. */
const OWN_BOX = new Set<MessagePart["type"]>(["failure"]);

/** A small text button for the reading pane's header. Words, not a box of icons. */
const headerButton = `${buttonClass("ghost")} h-7 px-2`;

/**
 * The inbox as a mailbox: a list of messages and a reading pane that shows the
 * selected message's parts in order, each with the controls that resolve it.
 * In a narrow container the list and the pane are separate views.
 *
 * Each message is laid out by its template (see `messageTemplate`): its icon,
 * its name and what it leads with. Messages that ask for something come first,
 * under "Needs you"; updates that are only there to be read follow them.
 *
 * Keys in the list: the arrows move, Enter opens the message and puts focus
 * on its first control, Escape goes back to the list. Nothing here animates:
 * everything in an inbox is waiting on a person.
 */
export function Mailbox({
  messages,
  selectedId,
  onSelect,
  onAction = () => {},
  viewer,
  renderers = {},
  renderSender,
  toolbar,
  listEnd,
  renderFrame,
  messageActions,
  emptyOpen,
  filter,
  defaultShow = "open",
  show: controlledShow,
  onShow,
  openCount,
  now,
  heading = 2,
  className,
}: MailboxProps) {
  const Title = heading === 1 ? "h1" : "h2";
  const copy = defaultCopy;
  const [ownShow, setOwnShow] = useState<MailboxShow>(defaultShow);
  const show = controlledShow === undefined ? ownShow : controlledShow;
  const setShow = (next: MailboxShow) => {
    if (controlledShow === undefined) setOwnShow(next);
    onShow?.(next);
  };
  const [own, setOwn] = useState<string | null>(null);
  const pane = useRef<HTMLElement>(null);
  const list = useRef<HTMLElement>(null);

  const states = show ? SHOW[show] : null;
  const shown = sortMessages(filter ? filterMessages(messages, filter, now) : messages, now).filter(
    (m) => !states || states.includes(messageState(m, now)),
  );
  // Decisions first, then updates. Within each, the order stays most urgent first.
  const asking = shown.filter((m) => messageState(m, now) === "open" && openParts(m).length > 0);
  const rest = shown.filter((m) => !asking.includes(m));
  const grouped = show !== "resolved" && asking.length > 0 && rest.length > 0;
  const ordered = [...asking, ...rest];

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
  const snooze = (message: InboxMessage) =>
    onAction({
      id: ActionId.snooze,
      payload: { turnId: message.id, target: message.id, option: tomorrowMorning() },
    });
  const unhide = (message: InboxMessage) =>
    onAction({ id: ActionId.unhide, payload: { turnId: message.id, target: message.id } });

  const onListKey = (e: KeyboardEvent) => {
    const i = ordered.findIndex((m) => m.id === current);
    const move = (to: number) => {
      const next = ordered[Math.max(0, Math.min(ordered.length - 1, to))];
      if (!next) return;
      e.preventDefault();
      select(next.id);
      list.current?.querySelector<HTMLElement>(`[data-message="${CSS.escape(next.id)}"]`)?.focus();
    };
    if (e.key === "ArrowDown") move(i + 1);
    else if (e.key === "ArrowUp") move(i - 1);
    else if (e.key === "Home") move(0);
    else if (e.key === "End") move(ordered.length - 1);
    else if (e.key === "Enter" && selected) {
      e.preventDefault();
      // Land on the first thing that resolves it, or on the pane itself to read.
      const first = pane.current?.querySelector<HTMLElement>(
        '[data-aui="button"][data-style="primary"], [data-aui="button"], textarea',
      );
      (first ?? pane.current)?.focus();
    }
  };

  const onPaneKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape" || !selected) return;
    e.preventDefault();
    list.current
      ?.querySelector<HTMLElement>(`[data-message="${CSS.escape(selected.id)}"]`)
      ?.focus();
  };

  const row = (m: InboxMessage) => {
    const state = messageState(m, now);
    const open = state === "open";
    const asks = open && openParts(m).length > 0;
    return (
      <li key={m.id} className="group relative">
        <button
          type="button"
          data-message={m.id}
          data-template={messageTemplate(m)}
          aria-current={m.id === current ? "true" : undefined}
          onClick={() => select(m.id)}
          className="flex w-full min-w-0 items-start gap-3 rounded-aui px-3 py-2.5 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent focus-visible:-outline-offset-2 aria-[current]:bg-aui-accent-soft motion-reduce:transition-none"
        >
          {/* The unread dot has its own column, so read and unread rows line up. */}
          <span className="flex w-1.5 shrink-0 justify-center self-stretch pt-3.5">
            {m.unread && (
              <span className="size-1.5 rounded-full bg-aui-accent">
                <span className="sr-only">{copy.mailbox.unread}</span>
              </span>
            )}
          </span>
          <TemplateMark message={m} />
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="flex min-w-0 items-center gap-2">
              {renderSender?.(m.from)}
              <span
                className={`min-w-0 flex-1 truncate text-aui-xs ${m.unread ? "font-semibold text-aui-fg" : "text-aui-muted"}`}
              >
                {m.from.name}
              </span>
              <time
                dateTime={m.at}
                title={fullWhen(m.at)}
                className={`shrink-0 text-aui-2xs tabular-nums ${m.unread ? "text-aui-fg" : "text-aui-muted"} ${open ? "group-focus-within:opacity-0 group-hover:opacity-0" : ""}`}
              >
                {shortWhen(m.at)}
              </time>
            </span>
            <span
              className={`block min-w-0 truncate text-aui-sm ${m.unread ? "font-semibold text-aui-fg" : "text-aui-fg"}`}
            >
              {m.subject}
            </span>
            <span className="flex min-w-0 items-baseline gap-1.5 text-aui-xs">
              <TemplateTag message={m} />
              <span className="min-w-0 flex-1 truncate text-aui-muted">
                {asks && openParts(m).length > 1
                  ? `${openParts(m).length} to decide · ${messagePreview(m)}`
                  : messagePreview(m)}
              </span>
              {!open && (
                <span className="shrink-0 text-aui-2xs text-aui-muted capitalize">
                  {/* Put away by this reader alone: hidden, as Hide for Me says. */}
                  {m.hidden && state === "dismissed" ? copy.announcements.hidden : state}
                </span>
              )}
            </span>
          </span>
        </button>
        {/* Put away without opening it, as an email client's hover action does. */}
        {open && (
          <button
            type="button"
            onClick={() => dismiss(m)}
            title={hideLabel(m)}
            aria-label={`${hideLabel(m)}: ${m.subject}`}
            className="absolute top-2 right-2 inline-flex size-6 items-center justify-center rounded-aui-sm text-aui-muted opacity-0 transition-opacity hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-aui-accent group-focus-within:opacity-100 group-hover:opacity-100 motion-reduce:transition-none"
          >
            <XIcon size={14} />
          </button>
        )}
      </li>
    );
  };

  const group = (label: string, items: InboxMessage[]) => (
    <li key={label} className="flex flex-col">
      <h3 className="px-3 pt-3 pb-1 font-medium text-aui-2xs text-aui-muted uppercase tracking-wider">
        {label}
        <span className="pl-1.5 tabular-nums">{items.length}</span>
      </h3>
      <ul className="flex flex-col gap-px">{items.map(row)}</ul>
    </li>
  );

  const reading = selected ? (
    <ReadingPane
      message={selected}
      now={now}
      viewer={viewer}
      renderers={renderers}
      renderSender={renderSender}
      onAction={onAction}
      onBack={() => select(null)}
      onDismiss={() => dismiss(selected)}
      onSnooze={() => snooze(selected)}
      onUnhide={() => unhide(selected)}
      actions={messageActions?.(selected)}
    />
  ) : (
    <div className="flex flex-1 flex-col items-center justify-center gap-1 py-16 text-center">
      <p className="font-medium text-aui-fg text-aui-sm">{copy.mailbox.pick}</p>
      <p className="text-aui-muted text-aui-xs">{copy.mailbox.pickHint}</p>
    </div>
  );

  const nothingOpen = show === "open" && !filter;

  return (
    <section
      aria-label={copy.inbox}
      className={`@container flex min-h-0 min-w-0 flex-col ${className ?? ""}`}
    >
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2 pb-3">
        <Title className="font-semibold text-aui-lg">{copy.inbox}</Title>
        {count > 0 && (
          <span className="rounded-full bg-aui-accent-soft px-2 py-0.5 font-medium text-aui-2xs text-aui-fg tabular-nums">
            {count} open
          </span>
        )}
        {/* Open, resolved or all: a quiet segmented control, never a native select. */}
        {show && (
          <fieldset className="ml-auto inline-flex shrink-0 rounded-aui border border-aui-border p-0.5">
            <legend className="sr-only">{copy.show.label}</legend>
            {(Object.keys(SHOW) as MailboxShow[]).map((id) => (
              <button
                key={id}
                type="button"
                aria-pressed={show === id}
                onClick={() => setShow(id)}
                className="h-6 rounded-aui-sm px-2.5 text-aui-muted text-aui-xs transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-subtle aria-pressed:text-aui-fg motion-reduce:transition-none"
              >
                {copy.show[id]}
              </button>
            ))}
          </fieldset>
        )}
      </header>
      {toolbar && <div className="min-w-0 pb-2">{toolbar}</div>}
      <div className="flex min-h-0 min-w-0 flex-1 border-aui-border border-t">
        <div
          className={`min-h-0 w-full min-w-0 flex-col @2xl:flex @2xl:w-[22rem] @2xl:shrink-0 @2xl:border-aui-border @2xl:border-r @2xl:pr-2 @5xl:w-[26rem] ${selected ? "hidden" : "flex"}`}
        >
          {ordered.length === 0 ? (
            <div className="flex flex-col items-center gap-1.5 px-4 py-14 text-center">
              <CheckCircleIcon size={22} className="text-aui-ok" />
              <p className="font-medium text-aui-sm">
                {nothingOpen ? (emptyOpen?.title ?? copy.mailbox.emptyOpen) : copy.mailbox.empty}
              </p>
              <p className="max-w-64 text-aui-muted text-aui-xs">
                {nothingOpen
                  ? (emptyOpen?.hint ?? copy.mailbox.emptyOpenHint)
                  : copy.mailbox.emptyHint}
              </p>
            </div>
          ) : (
            <section
              ref={list}
              aria-label={copy.mailbox.messages}
              onKeyDown={onListKey}
              className="aui-scroll min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden pt-1"
            >
              {grouped ? (
                <ul className="flex flex-col">
                  {group(copy.mailbox.needsYou, asking)}
                  {group(copy.mailbox.updates, rest)}
                </ul>
              ) : (
                <ul className="flex flex-col gap-px">{ordered.map(row)}</ul>
              )}
              {listEnd}
            </section>
          )}
        </div>
        <section
          ref={pane}
          aria-label={copy.mailbox.reading}
          tabIndex={-1}
          onKeyDown={onPaneKey}
          className={`min-h-0 min-w-0 flex-1 flex-col outline-none @2xl:flex @2xl:pl-6 ${selected ? "flex" : "hidden"}`}
        >
          {selected && renderFrame ? renderFrame(selected, reading) : reading}
        </section>
      </div>
    </section>
  );
}

/**
 * One message, read: its subject, who sent it and when, what its template
 * leads with, then its parts. Prose reads as the body of an email; each part
 * that asks for something, or holds a file, sits in its own card.
 */
function ReadingPane({
  message,
  now,
  viewer,
  renderers,
  renderSender,
  onAction,
  onBack,
  onDismiss,
  onSnooze,
  onUnhide,
  actions,
}: {
  message: InboxMessage;
  now?: string;
  viewer?: string;
  renderers: Record<string, PartRenderer>;
  renderSender?: (from: InboxMessage["from"]) => ReactNode;
  onAction: ActionHandler;
  onBack: () => void;
  onDismiss: () => void;
  onSnooze: () => void;
  onUnhide: () => void;
  /** The host's own controls for this message. */
  actions?: ReactNode;
}) {
  const copy = defaultCopy;
  const state = messageState(message, now);
  const open = state === "open";
  const template = messageTemplate(message);
  // Posted for everyone, not an agent's draft waiting for an owner.
  const announcement = isAnnouncement(message);
  // Set aside by this reader alone, so it is theirs to bring back.
  const hidden = Boolean(message.hidden) && (state === "dismissed" || state === "snoozed");
  return (
    <article
      data-aui="message"
      data-template={template}
      data-from-kind={message.from.kind}
      data-from-name={message.from.name}
      data-severity={message.severity ?? "neutral"}
      data-state={state}
      className="flex min-h-0 min-w-0 flex-1 flex-col"
    >
      {/* On a narrow pane the controls wrap under Back rather than run off the edge. */}
      <div className="flex min-w-0 flex-wrap items-center gap-1 pt-2 pb-1">
        <button type="button" onClick={onBack} className={`${headerButton} -ml-2 @2xl:hidden`}>
          <span aria-hidden="true">←</span>
          {copy.mailbox.back}
        </button>
        {(open || hidden || actions) && (
          <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-1">
            {open && (
              <>
                <button
                  type="button"
                  onClick={onSnooze}
                  title={copy.mailbox.snoozeTitle}
                  className={headerButton}
                >
                  <ClockIcon size={14} />
                  {copy.mailbox.snooze}
                </button>
                <button type="button" onClick={onDismiss} className={headerButton}>
                  <XIcon size={14} />
                  {hideLabel(message)}
                </button>
              </>
            )}
            {hidden && (
              <button type="button" onClick={onUnhide} className={headerButton}>
                <EyeIcon size={14} />
                {copy.announcements.showForMe}
              </button>
            )}
            {actions}
          </div>
        )}
      </div>
      <div className="aui-scroll min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden pb-4">
        <header className="flex min-w-0 flex-col gap-3 pb-4">
          <h3 className="break-words font-semibold text-[1.25rem] text-aui-fg leading-snug">
            {message.subject}
          </h3>
          <div className="flex min-w-0 items-center gap-3">
            <TemplateMark message={message} size="lg" />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="flex min-w-0 items-center gap-1.5">
                {renderSender?.(message.from)}
                <span className="truncate font-medium text-aui-fg text-aui-sm">
                  {message.from.name}
                </span>
              </span>
              <TemplateTag message={message} />
            </div>
            <time
              dateTime={message.at}
              className="shrink-0 self-start pt-0.5 text-aui-muted text-aui-xs tabular-nums"
            >
              {fullWhen(message.at)}
            </time>
          </div>
        </header>
        <WebSurface onAction={onAction}>
          <div className="flex min-w-0 flex-col gap-3">
            {message.parts.map((part) => {
              const render = renderers[part.type === "custom" ? part.name : part.type];
              // Prose is the body; anything to act on or open is a card of its own,
              // unless it already draws its own box, so no box sits inside another.
              const card =
                (isResolvable(part) || part.type === "artifact") && !OWN_BOX.has(part.type);
              return (
                <section
                  key={part.id}
                  data-aui-part={part.type}
                  aria-label={copy.partKind[part.type]}
                  className={
                    card
                      ? "min-w-0 overflow-hidden rounded-aui-lg border border-aui-border bg-aui-surface p-4"
                      : "min-w-0 max-w-[68ch] break-words"
                  }
                >
                  {render ? (
                    render({ message, part, onAction, viewer })
                  ) : (
                    <MessagePartView message={message} part={part} viewer={viewer} copy={copy} />
                  )}
                </section>
              );
            })}
          </div>
        </WebSurface>
        {open && openParts(message).length === 0 && (
          <p className="pt-4 text-aui-muted text-aui-xs">
            {announcement ? copy.announcements.readOnly : copy.mailbox.readOnly}
          </p>
        )}
        {hidden && (
          <p className="pt-4 text-aui-muted text-aui-xs">
            {state === "snoozed"
              ? copy.announcements.snoozedForYou
              : copy.announcements.hiddenForYou}
          </p>
        )}
        {announcement && state === "dismissed" && !hidden && (
          <p className="pt-4 text-aui-muted text-aui-xs">{copy.announcements.withdrawn}</p>
        )}
      </div>
    </article>
  );
}
