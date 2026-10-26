import { defaultCopy } from "@humagen/components";
import { HOME_NEEDS, type HomeModel, type HomeNotification, type HomeUpdate, orderNotifications, orderUpdates } from "@humagen/core";
import type { ReactNode } from "react";
import { ago, RowButton, Section, textAction } from "./ui.tsx";

/** Before the first message, the prompt is the page. Chat owns its controls and submission. */
export function HomePage({ greeting, canRun, hasAgents, banner, below, children }: {
  banner?: HomeAnnouncement;
  greeting: string;
  canRun: boolean;
  hasAgents: boolean;
  /** Under the prompt: what needs the person, and what is new (HomeBoxes). */
  below?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section aria-label="New conversation" className="aui-scroll humagen-fade flex min-h-0 flex-1 flex-col overflow-y-auto py-8">
      <div className="my-auto w-full min-w-0">
      {banner && <AnnouncementBanner announcement={banner} />}
      <h1 className="humagen-display px-6 pb-6 text-center text-[2rem] font-semibold leading-tight text-aui-fg">{greeting}</h1>
      {!canRun ? (
        <p className="px-6 text-center text-aui-sm text-aui-muted">You do not have permission to start conversations in this workspace.</p>
      ) : !hasAgents ? (
        <p className="px-6 text-center text-aui-sm text-aui-muted">An agent needs to be configured before you can start a conversation.</p>
      ) : children}
      {below}
      </div>
    </section>
  );
}

/** Something Home reads from the server: not read yet, could not be read, or read. */
export type Loaded<T> = { state: "loading" } | { state: "failed" } | { state: "ready"; value: T };

/**
 * Under the chat bar, two boxes from the shared Home model: Needs you (the
 * newest few things waiting on the person, from whom, with how many there are
 * in all) and What's new (announcements in their projects, unread marked,
 * where each was posted). Every row opens where it lives. A box with nothing
 * in it says so; a person with nothing in either is told what Home is for, in
 * one box. A box left out is one the person may not read. Nothing here moves:
 * it waits on the person.
 */
export function HomeBoxes({ notifications, updates, onOpen, onInbox, onUpdate, onUpdates }: {
  /** Whether the person can start a conversation above, which the welcome tells them to. */
  canStart?: boolean;
  notifications?: Loaded<HomeModel["notifications"]>;
  updates?: Loaded<NonNullable<HomeModel["updates"]>>;
  onOpen: (item: HomeNotification) => void;
  onInbox: () => void;
  onUpdate: (update: HomeUpdate) => void;
  onUpdates: () => void;
}) {
  const copy = defaultCopy;
  const c = copy.home;
  if (!notifications && !updates) return null;
  const items = notifications?.state === "ready" ? orderNotifications(notifications.value.items).slice(0, HOME_NEEDS) : [];
  const total = notifications?.state === "ready" ? Math.max(notifications.value.inbox?.total ?? 0, notifications.value.items.length) : 0;
  const news = updates?.state === "ready" ? orderUpdates(updates.value) : [];
  const marked = news.some((u) => u.unread);
  const ready = (l?: Loaded<unknown>) => l === undefined || l.state === "ready";
  const column = "mx-auto w-full max-w-4xl px-4 pt-8";
  if (ready(notifications) && ready(updates) && total === 0 && news.length === 0) return null;

  const quiet = "px-2 text-aui-sm text-aui-muted";
  const state = (l: Loaded<unknown>, failed: string, empty: string, shown: number) =>
    l.state === "loading" ? (
      <p role="status" className={quiet}>{c.loading}</p>
    ) : l.state === "failed" ? (
      <p className={quiet}>{failed}</p>
    ) : shown === 0 ? (
      <p className={quiet}>{empty}</p>
    ) : null;
  return (
    <div className={`${column} grid items-start gap-4 md:grid-cols-2`}>
      {notifications && (
        <Section
          box
          title={c.needsYou}
          count={total}
          actions={<button type="button" onClick={onInbox} className={textAction}>{c.openInbox}</button>}
        >
          {state(notifications, c.needsFailed, c.nothingWaiting, items.length) ?? (
            <ul className="-mx-2 flex flex-col">
              {items.map((n) => (
                <li key={`${n.kind}:${n.id}`}>
                  <RowButton
                    onClick={() => onOpen(n)}
                    title={
                      <>
                        <span className="font-medium">{n.from}</span> {n.summary}
                      </>
                    }
                    detail={[n.severity ? c.urgency[n.severity] : null, n.at ? ago(n.at) : null].filter(Boolean).join(" ") || undefined}
                  />
                </li>
              ))}
            </ul>
          )}
        </Section>
      )}
      {updates && (
        <Section
          box
          title={c.whatsNew}
          actions={<button type="button" onClick={onUpdates} className={textAction}>{c.seeAll}</button>}
        >
          {state(updates, c.newsFailed, c.nothingNew, news.length) ?? (
            <ul className="-mx-2 flex flex-col">
              {news.map((u) => (
                <li key={u.id}>
                  <RowButton
                    onClick={() => onUpdate(u)}
                    // The mark's column is kept only when some row is marked, so the rows line up either way.
                    lead={marked ? u.unread ? <span aria-hidden className="size-1.5 rounded-full bg-aui-accent" /> : null : undefined}
                    title={
                      <>
                        {u.unread && <span className="sr-only">{copy.mailbox.unread}: </span>}
                        <span className={u.unread ? "font-medium" : undefined}>{u.subject}</span>
                      </>
                    }
                    detail={[copy.announcements.postedIn(u.projectTitle), u.at ? ago(u.at) : null].filter(Boolean).join(" ")}
                  />
                </li>
              ))}
            </ul>
          )}
        </Section>
      )}
    </div>
  );
}

/** A retained inbox announcement with that reader's own read/hide actions. */
export interface HomeAnnouncement {
  id: string;
  source: string;
  title: string;
  text: string;
  unread: boolean;
  busy?: boolean;
  onRead: () => void;
  onHide: () => void;
  onOpen?: () => void;
}
export function AnnouncementBanner({ announcement: a }: { announcement: HomeAnnouncement }) {
  const c = defaultCopy.home.banner;
  return <section aria-label={c.label} className="mx-auto mb-8 w-[calc(100%_-_2rem)] max-w-3xl border-l-2 border-aui-fg/30 bg-aui-muted/5 px-4 py-3 text-aui-sm">
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-aui-xs text-aui-muted"><span>{a.source}</span>{a.unread && <span>{defaultCopy.mailbox.unread}</span>}</div>
    <p className="mt-1 font-medium text-aui-fg">{a.title}</p>
    <p className="mt-1 line-clamp-3 max-w-[70ch] whitespace-pre-wrap text-aui-muted">{a.text}</p>
    <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
      {a.onOpen && <button type="button" className={textAction} onClick={a.onOpen}>{c.open}</button>}
      {a.unread && <button type="button" className={textAction} disabled={a.busy} onClick={a.onRead}>{c.markRead}</button>}
      <button type="button" className={textAction} disabled={a.busy} onClick={a.onHide}>{c.hide}</button>
    </div>
  </section>;
}
