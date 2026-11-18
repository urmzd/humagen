import { defaultCopy } from "@humagen/components";
import { XIcon } from "@humagen/render-web";
import type { ComponentProps, MouseEvent, ReactNode } from "react";
import { InboxPage } from "./InboxPage.tsx";
import { useNotifications } from "./notificationDelivery.ts";
import { href, type Route } from "./route.ts";
import { textAction } from "./ui.tsx";

// Kept here for its callers; the console reads it from notificationDelivery.ts.
export { useNotifications };

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
        <>
          <NotificationLink
            to={{ page: "monitor", tab: "rules" }}
            onGo={onGo}
            className={`${textAction} inline-flex min-h-11 items-center`}
          >
            {c.alertRules}
          </NotificationLink>
          <NotificationLink
            to={{ page: "you", section: "notifications" }}
            onGo={onGo}
            className={`${textAction} inline-flex min-h-11 items-center`}
          >
            {c.settings}
          </NotificationLink>
        </>
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
