import { defaultCopy } from "@humagen/components";
import { BellIcon, SettingsIcon, XIcon } from "@humagen/render-web";
import { useCallback, useEffect, useRef, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import type { NativeNotification } from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { href, type Route } from "./route.ts";
import { Empty, Page, SkeletonRows, textAction } from "./ui.tsx";

/** One reader's delivery projection, shared by navigation and its page. */
export function useNotifications(enabled: boolean, subject?: string) {
  const [owner, setOwner] = useState(subject);
  const [rows, setRows] = useState<NativeNotification[]>([]);
  const [loading, setLoading] = useState(enabled);
  const [failed, setFailed] = useState(false);
  const [dismissFailed, setDismissFailed] = useState(false);
  const [busy, setBusy] = useState<string>();
  const [tick, setTick] = useState(0);
  const epoch = useRef(0);
  const serial = useRef(0);
  useEffect(() => {
    const reader = ++epoch.current;
    let active = false;
    setRows([]);
    setOwner(subject);
    setLoading(enabled);
    setFailed(false);
    setDismissFailed(false);
    setBusy(undefined);
    if (!enabled) return;
    const refresh = async () => {
      if (active) return;
      active = true;
      const request = ++serial.current;
      try {
        const delivered = await api.notifications();
        if (epoch.current === reader && request === serial.current) {
          setRows(delivered);
          setFailed(false);
          setDismissFailed(false);
        }
      } catch {
        if (epoch.current === reader && request === serial.current) {
          setRows([]);
          setFailed(true);
        }
      } finally {
        active = false;
        if (epoch.current === reader) setLoading(false);
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 5000);
    return () => {
      epoch.current++;
      clearInterval(timer);
    };
  }, [enabled, subject, tick]);
  const dismiss = useCallback(async (id: string) => {
    if (!enabled || busy) return;
    const reader = epoch.current;
    serial.current++;
    setBusy(id);
    try {
      await api.acknowledgeNotification(id);
      if (epoch.current === reader) {
        serial.current++;
        setRows((current) => current.filter((row) => row.id !== id));
        setFailed(false);
        setDismissFailed(false);
      }
    } catch {
      if (epoch.current === reader) { setFailed(true); setDismissFailed(true); }
    } finally {
      if (epoch.current === reader) setBusy(undefined);
    }
  }, [enabled, busy]);
  return { rows: enabled && owner === subject ? rows : [], loading: enabled && (loading || owner !== subject), failed: enabled && owner === subject && failed, dismissFailed: enabled && owner === subject && dismissFailed, busy, dismiss, retry: () => setTick((value) => value + 1) };
}

function NotificationLink({ to, onGo, children, className = "" }: { to: Route; onGo: (route: Route) => void; children: ReactNode; className?: string }) {
  const open = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onGo(to);
  };
  return <a href={href(to)} onClick={open} className={className}>{children}</a>;
}

/** Dismissing a delivery never resolves the operational inbox item. */
export function NotificationsPage({ notifications, onGo }: { notifications: ReturnType<typeof useNotifications>; onGo: (route: Route) => void }) {
  const c = defaultCopy.notifications;
  const { rows, loading, failed, dismissFailed, busy, dismiss, retry } = notifications;
  return (
    <Page title={c.title} lede={c.pageDescription} actions={
      <NotificationLink to={{ page: "you", section: "notifications" }} onGo={onGo} className={`${textAction} inline-flex min-h-11 items-center gap-2`}>
        <SettingsIcon />{c.settings}
      </NotificationLink>
    }>
      <section aria-label={c.deliveries} className="w-full max-w-3xl">
        {failed && <div role="alert" className="mb-6 flex flex-wrap items-center gap-x-4 gap-y-2 text-aui-sm">
          <p>{dismissFailed ? c.dismissFailed : c.failed}</p><button type="button" onClick={retry} className={`${textAction} min-h-11`}>{c.retry}</button>
        </div>}
        {loading ? <SkeletonRows count={3} height="5rem" /> : rows.length === 0 ? (
          !failed && <Empty title={c.empty}>{c.emptyHint}</Empty>
        ) : (
          <ol className="divide-y divide-aui-border">
            {rows.map((row) => (
              <li key={row.id} className="flex min-w-0 gap-3 py-6 first:pt-0 sm:gap-4">
                <span className="mt-1 flex size-9 shrink-0 items-center justify-center rounded-full bg-aui-accent-soft text-aui-accent"><BellIcon size={18} /></span>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
                    <span className="text-aui-sm font-medium">{row.mode === "digest" ? c.digest : c.immediate}{row.items.length > 1 ? ` ${c.items(row.items.length)}` : ""}</span>
                    <time className="text-aui-xs text-aui-muted" dateTime={row.at}>{new Date(row.at).toLocaleString()}</time>
                  </div>
                  <ul className="mt-2 space-y-1">
                    {row.items.map((item) => <li key={item.inbox}>
                      <NotificationLink to={{ page: "inbox", message: item.inbox }} onGo={onGo} className="inline-flex min-h-11 items-center break-words text-aui-base text-aui-fg underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent [overflow-wrap:anywhere]">{item.subject}</NotificationLink>
                    </li>)}
                  </ul>
                  <button type="button" disabled={!!busy} onClick={() => void dismiss(row.id)} className={`${textAction} mt-2 inline-flex min-h-11 items-center gap-2 disabled:opacity-50`}>
                    <XIcon />{c.dismiss}
                  </button>
                </div>
              </li>
            ))}
          </ol>
        )}
      </section>
    </Page>
  );
}
