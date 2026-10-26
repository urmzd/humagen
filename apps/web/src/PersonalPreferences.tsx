import { defaultCopy } from "@humagen/components";
import type { PersonalPreferences as Personal } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api, type Preferences } from "./api.ts";
import { NotificationPanel } from "./Notifications.tsx";
import { PersonalAgents } from "./PersonalAgents.tsx";
import { card, field, Page, Problem, SkeletonRows } from "./ui.tsx";

const initial: Personal = {
  briefing: "",
  notifications: {},
  quiet_start: "",
  quiet_end: "",
  timezone: "UTC",
  remember: false,
  use_personal_memory_in_projects: false,
};
export function PersonalPreferences({
  section = "profile",
  onSaved,
}: {
  section?: "profile" | "notifications" | "privacy" | "agents";
  onSaved?: (p: Preferences) => void;
}) {
  const copy = defaultCopy.personalPreferences;
  const [profile, setProfile] = useState<Preferences | null>(null);
  const [roles, setRoles] = useState<string[]>([]);
  const [value, setValue] = useState<Personal>(initial);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    let live = true;
    Promise.all([api.preferences(), api.me()])
      .then(([preferences, me]) => {
        if (live) {
          setProfile(preferences);
          setValue(preferences.personal ?? initial);
          setRoles(me.principal.roles ?? []);
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
      const result = await api.savePreferences({
        display_name: profile.display_name,
        theme: profile.theme,
        accent: profile.accent,
        personal: value,
      });
      setProfile(result);
      onSaved?.(result);
      setValue(result.personal ?? initial);
      setSaved(true);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Page title={copy[section]} lede={copy.description} narrow>
      {section === "agents" ? (
        <PersonalAgents />
      ) : (
        <>
          {failed && <Problem message={copy.failed} />} {saved && <p role="status">{copy.saved}</p>}
          {!profile ? (
            <SkeletonRows count={3} />
          ) : (
            <form
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault();
                void save();
              }}
            >
              {section === "profile" && (
                <>
                  <label className="block">
                    {copy.displayName}
                    <input
                      className={`${field} mt-2 w-full`}
                      value={profile.display_name}
                      maxLength={80}
                      onChange={(event) =>
                        setProfile({ ...profile, display_name: event.target.value })
                      }
                    />
                  </label>
                  <p className="text-aui-sm text-aui-muted">
                    {copy.role}: {roles.join(", ")}
                  </p>
                  <label className="block">
                    {copy.briefing}
                    <input
                      className={`${field} mt-2 w-full`}
                      maxLength={500}
                      value={value.briefing}
                      onChange={(event) => setValue({ ...value, briefing: event.target.value })}
                    />
                  </label>
                  <div className="flex flex-wrap gap-5">
                    <a
                      className="inline-flex min-h-11 items-center text-aui-accent"
                      href="/connections"
                    >
                      {copy.connections}
                    </a>
                    <a
                      className="inline-flex min-h-11 items-center text-aui-accent"
                      href="/library/memory"
                    >
                      {copy.memory}
                    </a>
                  </div>
                </>
              )}
              {section === "notifications" && (
                <>
                  <p className="text-aui-muted">{defaultCopy.notifications.description}</p>
                  <NotificationPanel />
                  <p className="text-aui-muted">{copy.deliveryUnavailable}</p>
                  <label className="flex min-h-11 items-center gap-3">
                    <input type="checkbox" disabled aria-describedby="email-unavailable" />
                    {copy.email}
                  </label>
                  <p id="email-unavailable" className="sr-only">
                    {copy.deliveryUnavailable}
                  </p>
                  <fieldset className={`${card} space-y-3 p-4`}>
                    <legend>{copy.notifications}</legend>
                    {Object.entries(copy.events).map(([kind, label]) => (
                      <label
                        key={kind}
                        className="flex flex-wrap items-center justify-between gap-3"
                      >
                        <span>{label}</span>
                        <select
                          className={`${field} max-w-full`}
                          value={value.notifications[kind] ?? "off"}
                          onChange={(event) =>
                            setValue({
                              ...value,
                              notifications: {
                                ...value.notifications,
                                [kind]: event.target.value as "immediate" | "digest" | "off",
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
                  <fieldset className={`${card} p-4`}>
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
                            className={`${field} ml-2`}
                            value={value[key]}
                            onChange={(event) => setValue({ ...value, [key]: event.target.value })}
                          />
                        </label>
                      ))}
                      <label>
                        {copy.timezone}
                        <input
                          className={`${field} ml-2 max-w-full`}
                          value={value.timezone}
                          onChange={(event) => setValue({ ...value, timezone: event.target.value })}
                        />
                      </label>
                    </div>
                  </fieldset>
                </>
              )}
              {section === "privacy" && (
                <fieldset className={`${card} space-y-4 p-4`}>
                  <legend>{copy.privacy}</legend>
                  <p className="text-aui-muted">{copy.privateDefault}</p>
                  <label className="flex min-h-11 items-center gap-3">
                    <input
                      type="checkbox"
                      checked={value.remember}
                      onChange={(event) => setValue({ ...value, remember: event.target.checked })}
                    />
                    {copy.remember}
                  </label>
                  <label className="flex min-h-11 items-center gap-3">
                    <input
                      type="checkbox"
                      checked={value.use_personal_memory_in_projects}
                      onChange={(event) =>
                        setValue({
                          ...value,
                          use_personal_memory_in_projects: event.target.checked,
                        })
                      }
                    />
                    {copy.personalMemory}
                  </label>
                  <a
                    className="inline-flex min-h-11 items-center text-aui-accent"
                    href="/library/memory"
                  >
                    {copy.memory}
                  </a>
                </fieldset>
              )}
              <button
                type="submit"
                disabled={busy}
                className="min-h-11 text-aui-accent disabled:opacity-50"
              >
                {copy.save}
              </button>
            </form>
          )}
        </>
      )}
    </Page>
  );
}
