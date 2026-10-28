import { defaultCopy } from "@humagen/components";
import { SettingsIcon, XIcon } from "@humagen/render-web";
import type { ComponentProps, MouseEvent, ReactNode } from "react";
import { useCallback, useEffect, useRef, useState } from "react";
import type { NativeNotification } from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { InboxPage } from "./InboxPage.tsx";
import { href, type Route } from "./route.ts";
import { textAction } from "./ui.tsx";

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
  const dismiss = useCallback(
    async (id: string) => {
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
        if (epoch.current === reader) {
          setFailed(true);
          setDismissFailed(true);
        }
      } finally {
        if (epoch.current === reader) setBusy(undefined);
      }
    },
    [enabled, busy],
  );
  return {
    rows: enabled && owner === subject ? rows : [],
    loading: enabled && (loading || owner !== subject),
    failed: enabled && owner === subject && failed,
    dismissFailed: enabled && owner === subject && dismissFailed,
    busy,
    dismiss,
    retry: () => setTick((value) => value + 1),
  };
}

function NotificationLink({
  to,
  onGo,
  children,
  className = "",
}: {
  to: Route;
  onGo: (route: Route) => void;
  children: ReactNode;
  className?: string;
}) {
  const open = (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    onGo(to);
  };
  return (
    <a href={href(to)} onClick={open} className={className}>
      {children}
    </a>
  );
}

/** One feed renders the original messages; delivery acknowledgements remain separate actions. */
export function NotificationsPage({
  notifications,
  onGo,
  ...inbox
}: Omit<ComponentProps<typeof InboxPage>, "actions" | "notice" | "messageActions"> & {
  notifications?: ReturnType<typeof useNotifications>;
  onGo: (route: Route) => void;
}) {
  const c = defaultCopy.notifications;
  return (
    <InboxPage
      {...inbox}
      actions={
        <NotificationLink
          to={{ page: "you", section: "notifications" }}
          onGo={onGo}
          className={`${textAction} inline-flex min-h-11 items-center gap-2`}
        >
          <SettingsIcon />
          {c.settings}
        </NotificationLink>
      }
      notice={
        notifications?.failed && (
          <div role="alert" className="flex flex-wrap items-center gap-x-4 gap-y-2 text-aui-sm">
            <p>{notifications.dismissFailed ? c.dismissFailed : c.deliveryFailed}</p>
            {!notifications.dismissFailed && (
              <button
                type="button"
                onClick={notifications.retry}
                className={`${textAction} min-h-11`}
              >
                {c.retry}
              </button>
            )}
          </div>
        )
      }
      messageActions={(message) =>
        notifications?.rows
          .filter((row) => row.items.some((item) => item.inbox === message.id))
          .map((row) => (
            <button
              key={row.id}
              type="button"
              disabled={!!notifications.busy}
              onClick={() => void notifications.dismiss(row.id)}
              className={`${textAction} inline-flex min-h-11 items-center gap-2 disabled:opacity-50`}
            >
              <XIcon />
              {row.items.length > 1 ? c.dismissDigest(row.items.length) : c.dismiss}
            </button>
          ))
      }
    />
  );
}
