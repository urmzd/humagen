import { useCallback, useEffect, useRef, useState } from "react";
import type { NativeNotification } from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";

/*
 * The hook lives apart from the Notifications page so the console, which
 * holds one projection for the navigation, does not load the page with it.
 */

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
