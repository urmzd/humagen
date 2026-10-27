import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Preferences } from "./api.ts";

/** Whether the saved preferences were read: until they are, nothing is saved over them. */
export type PrefsRead = "reading" | "read" | "failed";

interface ListChange {
  field: "pinned" | "muted";
  next: (was: string[]) => string[];
  resolve: () => void;
  reject: (error: unknown) => void;
}

/** What the console draws with before the saved preferences are read. */
export const unreadPrefs: Preferences = {
  display_name: "",
  theme: "system",
  accent: "red",
  has_photo: false,
  photo_version: 0,
  pinned: [],
  muted: [],
};

/**
 * The person's own preferences, read once the console opens. A save sends the
 * name, theme and accent with whatever it changes, so a pin or a mute waits
 * for the saved preferences: one made while they are being read is made once
 * they are, and one made after they could not be read reads them again, so a
 * blip does not stop pinning for the rest of the session. Pin and mute show at
 * once and resolve once saved. They reject when the preferences could not be
 * read or the change could not be saved, with nothing changed on screen, so
 * the control can say so.
 */
export function usePreferences(): {
  prefs: Preferences;
  prefsRead: PrefsRead;
  /** Shows preferences just saved elsewhere, as Settings does. */
  setPrefs: (p: Preferences) => void;
  pin: (id: string, on: boolean) => Promise<void>;
  mute: (project: string, on: boolean) => Promise<void>;
} {
  const [prefs, show] = useState<Preferences>(unreadPrefs);
  const [prefsRead, setPrefsRead] = useState<PrefsRead>("reading");
  // Keep acknowledged data separate from the immediate view. Pending list
  // changes replay over it, so a late response cannot erase a newer intent.
  const latest = useRef(prefs);
  const confirmed = useRef(prefs);
  const pending = useRef<ListChange[]>([]);
  const saving = useRef(false);
  const reading = useRef<Promise<void> | null>(null);
  const refresh = useCallback(() => {
    const projected = pending.current.reduce(
      (was, change) => ({ ...was, [change.field]: change.next(was[change.field]) }),
      confirmed.current,
    );
    latest.current = projected;
    show(projected);
  }, []);
  const setPrefs = useCallback((p: Preferences) => {
    confirmed.current = p;
    refresh();
  }, [refresh]);
  // The saved preferences, as on screen. A read under way is shared, a read
  // that failed is tried again, and one that worked is not repeated.
  const read = useCallback((): Promise<Preferences> => {
    if (!reading.current) {
      setPrefsRead("reading");
      reading.current = api.preferences().then(
        (p) => {
          setPrefs(p);
          setPrefsRead("read");
        },
        (e) => {
          reading.current = null;
          setPrefsRead("failed");
          throw e;
        },
      );
    }
    return reading.current.then(() => latest.current);
  }, [setPrefs]);
  useEffect(() => {
    read().catch(() => {});
  }, [read]);

  // Persist list mutations in order. Removing a failed mutation and replaying
  // the rest rolls back only that change, keeping later pins and unpins intact.
  const drain = useCallback(async () => {
    if (saving.current) return;
    saving.current = true;
    while (pending.current.length > 0) {
      const change = pending.current[0]!;
      const was = confirmed.current;
      const changed = { [change.field]: change.next(was[change.field]) };
      let error: unknown;
      let failed = false;
      try {
        confirmed.current = await api.savePreferences({
          display_name: was.display_name,
          theme: was.theme,
          accent: was.accent,
          ...changed,
        });
      } catch (cause) {
        error = cause;
        failed = true;
      }
      pending.current.shift();
      refresh();
      if (failed) change.reject(error);
      else change.resolve();
    }
    saving.current = false;
  }, [refresh]);
  const change = useCallback(
    (field: "pinned" | "muted", next: (was: string[]) => string[]) =>
      read().then(() => new Promise<void>((resolve, reject) => {
        pending.current.push({ field, next, resolve, reject });
        refresh();
        void drain();
      })),
    [read, refresh, drain],
  );
  // Pinned conversations are most recently pinned first.
  const pin = useCallback(
    (id: string, on: boolean) => change("pinned", (was) => (on ? [id, ...was.filter((p) => p !== id)] : was.filter((p) => p !== id))),
    [change],
  );
  const mute = useCallback(
    (project: string, on: boolean) => change("muted", (was) => (on ? [...was.filter((p) => p !== project), project] : was.filter((p) => p !== project))),
    [change],
  );
  return { prefs, prefsRead, setPrefs, pin, mute };
}
