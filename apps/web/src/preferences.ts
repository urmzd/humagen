import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Preferences } from "./api.ts";

/** Whether the saved preferences were read: until they are, nothing is saved over them. */
export type PrefsRead = "reading" | "read" | "failed";

/** What the console draws with before the saved preferences are read. */
export const unreadPrefs: Preferences = {
  display_name: "",
  theme: "system",
  accent: "purple",
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
  // What is on screen, for a change made before the next render: two pins in
  // a row each build on the one before.
  const latest = useRef(prefs);
  const reading = useRef<Promise<void> | null>(null);
  const setPrefs = useCallback((p: Preferences) => {
    latest.current = p;
    show(p);
  }, []);
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

  // A change to one list, shown at once and saved with the name, theme and
  // accent as read. If the save fails, the list goes back to what it was.
  const change = useCallback(
    (field: "pinned" | "muted", next: (was: string[]) => string[]) =>
      read().then((was) => {
        const list = next(was[field]);
        const changed = field === "pinned" ? { pinned: list } : { muted: list };
        setPrefs({ ...was, ...changed });
        return api.savePreferences({ display_name: was.display_name, theme: was.theme, accent: was.accent, ...changed }).then(setPrefs, (e) => {
          setPrefs({ ...latest.current, [field]: was[field] });
          throw e;
        });
      }),
    [read, setPrefs],
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
