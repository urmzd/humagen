import { defaultCopy } from "@humagen/components";
import type { NativeNotification } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api } from "./api.ts";
import { href } from "./route.ts";

/** Native delivery projection; dismissing never resolves the operational inbox. */
export function NotificationPanel() {
  const copy = defaultCopy.notifications;
  const [rows, setRows] = useState<NativeNotification[]>([]);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState<string>();
  useEffect(() => {
    let live = true;
    let active = false;
    let timer: ReturnType<typeof setInterval> | undefined;
    const refresh = async () => {
      if (active) return;
      active = true;
      try {
        const delivered = await api.notifications();
        if (live) {
          setRows(delivered);
          setFailed(false);
        }
      } catch {
        if (live) setFailed(true);
      } finally {
        active = false;
      }
    };
    api
      .me()
      .then((me) => {
        if (live && me.principal.kind === "user") {
          void refresh();
          timer = setInterval(() => void refresh(), 5000);
        }
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
      if (timer) clearInterval(timer);
    };
  }, []);
  const dismiss = async (id: string) => {
    setBusy(id);
    try {
      await api.acknowledgeNotification(id);
      setRows((current) => current.filter((row) => row.id !== id));
    } catch {
      setFailed(true);
    } finally {
      setBusy(undefined);
    }
  };
  return (
    <details className="min-w-0 rounded border border-aui-border p-3">
      <summary className="min-h-11 cursor-pointer text-aui-sm">
        {copy.title}
        {rows.length > 0 && <span className="ml-2 tabular-nums">{rows.length}</span>}
      </summary>
      <div className="max-h-96 space-y-4 overflow-y-auto pt-3">
        <a
          className="inline-flex min-h-11 items-center text-aui-sm text-aui-accent"
          href="/you/notifications"
        >
          {copy.settings}
        </a>
        {failed && (
          <p role="status" className="text-aui-sm text-aui-muted">
            {copy.failed}
          </p>
        )}
        {rows.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{copy.empty}</p>
        ) : (
          <ol className="space-y-4">
            {rows.map((row) => (
              <li key={row.id} className="min-w-0 break-words">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <span className="text-aui-sm font-medium">
                    {row.mode === "digest" ? copy.digest : copy.immediate}
                    {row.items.length > 1 ? ` ${copy.items(row.items.length)}` : ""}
                  </span>
                  <button
                    type="button"
                    disabled={busy === row.id}
                    onClick={() => void dismiss(row.id)}
                    className="min-h-11 text-aui-sm text-aui-accent disabled:opacity-50"
                  >
                    {copy.dismiss}
                  </button>
                </div>
                <ul className="space-y-2">
                  {row.items.map((item) => (
                    <li key={item.inbox}>
                      <a
                        className="inline-block min-h-11 break-words py-2 text-aui-sm hover:underline"
                        href={href({ page: "inbox", message: item.inbox })}
                      >
                        {item.subject}
                      </a>
                    </li>
                  ))}
                </ul>
                <time className="text-aui-xs text-aui-muted" dateTime={row.at}>
                  {new Date(row.at).toLocaleString()}
                </time>
              </li>
            ))}
          </ol>
        )}
      </div>
    </details>
  );
}
