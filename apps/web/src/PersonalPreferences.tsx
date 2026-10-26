import { defaultCopy } from "@humagen/components";
import type { PersonalPreferences as Personal } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api, type Preferences } from "./api.ts";
import { YourConnections } from "./Connections.tsx";
import { card, pageLede, pageTitle, Problem, SkeletonRows } from "./ui.tsx";

const initial: Personal = {
  briefing: "",
  notifications: {},
  quiet_start: "",
  quiet_end: "",
  timezone: "UTC",
  remember: false,
  use_personal_memory_in_projects: false,
};
export function PersonalPreferences() {
  const copy = defaultCopy.personalPreferences;
  const [profile, setProfile] = useState<Preferences | null>(null);
  const [value, setValue] = useState<Personal>(initial);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [saved, setSaved] = useState(false);
  const [connection, setConnection] = useState<string>();
  useEffect(() => {
    let live = true;
    api
      .preferences()
      .then((p) => {
        if (live) {
          setProfile(p);
          setValue(p.personal ?? initial);
        }
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, []);
  const save = async () => {
    if (!profile) return;
    setBusy(true);
    setFailed(false);
    setSaved(false);
    try {
      const p = await api.savePreferences({ ...profile, personal: value });
      setProfile(p);
      setValue(p.personal ?? initial);
      setSaved(true);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="min-w-0 space-y-5">
      <header>
        <h1 className={pageTitle}>{copy.title}</h1>
        <p className={pageLede}>{copy.description}</p>
      </header>
      {failed && <Problem message={copy.failed} />}
      {saved && <p role="status">{copy.saved}</p>}
      {!profile ? (
        <SkeletonRows count={3} />
      ) : (
        <form
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <label className="block">
            {copy.briefing}
            <textarea
              maxLength={500}
              value={value.briefing}
              onChange={(e) => setValue({ ...value, briefing: e.target.value })}
              className="mt-2 min-h-24 w-full rounded border border-aui-border bg-aui-bg p-3"
            />
          </label>
          <fieldset className={`${card} min-w-0 space-y-3 p-4`}>
            <legend>{copy.notifications}</legend>
            {Object.entries(copy.events).map(([kind, label]) => (
              <label key={kind} className="flex flex-wrap items-center justify-between gap-3">
                <span>{label}</span>
                <select
                  className="min-h-11 bg-aui-bg"
                  value={value.notifications[kind] ?? "off"}
                  onChange={(e) =>
                    setValue({
                      ...value,
                      notifications: {
                        ...value.notifications,
                        [kind]: e.target.value as "immediate" | "digest" | "off",
                      },
                    })
                  }
                >
                  {(["immediate", "digest", "off"] as const).map((mode) => (
                    <option key={mode} value={mode}>
                      {copy[mode]}
                    </option>
                  ))}
                </select>
              </label>
            ))}
          </fieldset>
          <fieldset className={`${card} min-w-0 p-4`}>
            <legend>{copy.quiet}</legend>
            <div className="flex flex-wrap gap-4">
              {(
                [
                  ["quiet_start", copy.start],
                  ["quiet_end", copy.end],
                ] as const
              ).map(([key, label]) => (
                <label key={key}>
                  {label}
                  <input
                    type="time"
                    value={value[key]}
                    onChange={(e) => setValue({ ...value, [key]: e.target.value })}
                    className="ml-2 min-h-11 bg-aui-bg"
                  />
                </label>
              ))}
              <label>
                {copy.timezone}
                <input
                  value={value.timezone}
                  onChange={(e) => setValue({ ...value, timezone: e.target.value })}
                  className="ml-2 min-h-11 max-w-full rounded border border-aui-border bg-aui-bg p-2"
                />
              </label>
            </div>
          </fieldset>
          <fieldset className={`${card} space-y-4 p-4`}>
            <legend>{copy.privacy}</legend>
            <p className="text-aui-muted">{copy.privateDefault}</p>
            <label className="flex min-h-11 items-center gap-3">
              <input
                type="checkbox"
                checked={value.remember}
                onChange={(e) => setValue({ ...value, remember: e.target.checked })}
              />
              {copy.remember}
            </label>
            <label className="flex min-h-11 items-center gap-3">
              <input
                type="checkbox"
                checked={value.use_personal_memory_in_projects}
                onChange={(e) =>
                  setValue({ ...value, use_personal_memory_in_projects: e.target.checked })
                }
              />
              {copy.personalMemory}
            </label>
          </fieldset>
          <button
            type="submit"
            disabled={busy}
            className="min-h-11 text-aui-accent disabled:opacity-50"
          >
            {copy.save}
          </button>
        </form>
      )}
      <YourConnections open={connection} onOpen={setConnection} />
    </section>
  );
}
