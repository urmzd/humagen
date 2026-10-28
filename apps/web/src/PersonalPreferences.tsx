import { defaultCopy } from "@humagen/components";
import {
  BellIcon,
  BrainIcon,
  ChevronRightIcon,
  ClockIcon,
  MessageIcon,
  PlugIcon,
  ProjectIcon,
  ShieldIcon,
  UserIcon,
} from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { Input } from "@humagen/render-web/ui/input";
import { Label as FieldLabel } from "@humagen/render-web/ui/label";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Switch } from "@humagen/render-web/ui/switch";
import { Textarea } from "@humagen/render-web/ui/textarea";
import { type ReactNode, useEffect, useId, useState } from "react";
import type { PersonalPreferences as Personal } from "../../../packages/client/src/generated/types.ts";
import { ApiError, api, type Preferences } from "./api.ts";
import { PersonalAgents } from "./PersonalAgents.tsx";
import { SettingsFormRow } from "./SettingsFormRow.tsx";
import { Page, Problem, SkeletonRows } from "./ui.tsx";

function PreferenceRow({
  icon,
  label,
  description,
  checked,
  onChange,
  disabled,
}: {
  icon: ReactNode;
  label: string;
  description: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}) {
  const id = useId();
  return (
    <SettingsFormRow id={id} label={label} description={description} icon={icon} compact>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
        aria-describedby={`${id}-hint`}
        className="mt-1"
      />
    </SettingsFormRow>
  );
}

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
  const formID = useId();
  const [profile, setProfile] = useState<Preferences | null>(null);
  const [emailAvailable, setEmailAvailable] = useState(false);
  const [roles, setRoles] = useState<string[]>([]);
  const [value, setValue] = useState<Personal>(initial);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [saved, setSaved] = useState(false);
  const [emailConflict, setEmailConflict] = useState(false);
  useEffect(() => {
    let live = true;
    Promise.all([api.preferences(), api.me()])
      .then(([preferences, me]) => {
        if (live) {
          setProfile(preferences);
          setValue(preferences.personal ?? initial);
          setRoles(me.principal.roles ?? []);
          setEmailAvailable(me.email_notifications_available === true);
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
    setEmailConflict(false);
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
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        try {
          const latest = await api.preferences();
          setValue((current) => ({
            ...current,
            email: latest.personal?.email ?? false,
            email_version: latest.personal?.email_version ?? 0,
          }));
          setEmailConflict(true);
        } catch {
          setFailed(true);
        }
      } else setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Page title={copy[section]} lede={copy.descriptions[section]} narrow>
      {section === "agents" ? (
        <PersonalAgents />
      ) : (
        <>
          {failed && <Problem message={copy.failed} />}{" "}
          {emailConflict && <Problem message={copy.emailChanged} />}{" "}
          {saved && <p role="status">{copy.saved}</p>}
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
                  <div>
                    <SettingsFormRow
                      id={`${formID}-name`}
                      label={copy.displayName}
                      description={copy.displayNameHint}
                      icon={<UserIcon size={18} />}
                    >
                      <Input
                        id={`${formID}-name`}
                        aria-describedby={`${formID}-name-hint`}
                        autoComplete="name"
                        value={profile.display_name}
                        maxLength={80}
                        disabled={busy}
                        onChange={(event) => {
                          setProfile({ ...profile, display_name: event.target.value });
                          setSaved(false);
                        }}
                      />
                    </SettingsFormRow>
                    <SettingsFormRow
                      id={`${formID}-briefing`}
                      label={copy.briefing}
                      description={copy.briefingHint}
                      icon={<MessageIcon size={18} />}
                    >
                      <Textarea
                        id={`${formID}-briefing`}
                        aria-describedby={`${formID}-briefing-hint`}
                        rows={4}
                        className="min-h-28 resize-y"
                        maxLength={500}
                        value={value.briefing}
                        disabled={busy}
                        onChange={(event) => {
                          setValue({ ...value, briefing: event.target.value });
                          setSaved(false);
                        }}
                      />
                    </SettingsFormRow>
                  </div>
                  <p className="text-aui-xs text-aui-muted">
                    {copy.role}: {roles.join(", ")}
                  </p>
                  <div className="flex flex-col gap-2 sm:flex-row">
                    <Button asChild variant="outline" className="justify-start">
                      <a href="/connections">
                        <PlugIcon aria-hidden="true" />
                        {copy.connections}
                        <ChevronRightIcon aria-hidden="true" className="ml-auto" />
                      </a>
                    </Button>
                    <Button asChild variant="outline" className="justify-start">
                      <a href="/library/memory">
                        <BrainIcon aria-hidden="true" />
                        {copy.memory}
                        <ChevronRightIcon aria-hidden="true" className="ml-auto" />
                      </a>
                    </Button>
                  </div>
                </>
              )}
              {section === "notifications" && (
                <>
                  <section aria-label={copy.notifications}>
                    <p className="pb-2 text-sm leading-6 text-aui-muted">
                      {defaultCopy.notifications.description}
                    </p>
                    <PreferenceRow
                      icon={<BellIcon size={18} />}
                      label={copy.email}
                      description={
                        emailAvailable ? copy.emailDescription : copy.deliveryUnavailable
                      }
                      checked={value.email ?? false}
                      disabled={busy || (!emailAvailable && !value.email)}
                      onChange={(email) => {
                        setValue({ ...value, email });
                        setSaved(false);
                      }}
                    />
                    {Object.entries(copy.events).map(([kind, label]) => (
                      <SettingsFormRow key={kind} id={`${formID}-${kind}`} label={label}>
                        <NativeSelect
                          id={`${formID}-${kind}`}
                          value={value.notifications[kind] ?? "off"}
                          disabled={busy}
                          onChange={(event) => {
                            setValue({
                              ...value,
                              notifications: {
                                ...value.notifications,
                                [kind]: event.target.value as "immediate" | "digest" | "off",
                              },
                            });
                            setSaved(false);
                          }}
                        >
                          {(["immediate", "digest", "off"] as const).map((mode) => (
                            <option key={mode} value={mode}>
                              {copy[mode]}
                            </option>
                          ))}
                        </NativeSelect>
                      </SettingsFormRow>
                    ))}
                  </section>
                  <section aria-labelledby={`${formID}-quiet`}>
                    <h2
                      id={`${formID}-quiet`}
                      className="flex items-center gap-2 text-sm font-medium"
                    >
                      <ClockIcon size={18} className="text-aui-muted" />
                      {copy.quiet}
                    </h2>
                    <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-[1fr_1fr_2fr]">
                      {(
                        [
                          ["quiet_start", copy.start],
                          ["quiet_end", copy.end],
                        ] as const
                      ).map(([key, label]) => (
                        <div key={key} className="space-y-2">
                          <FieldLabel htmlFor={`${formID}-${key}`}>{label}</FieldLabel>
                          <Input
                            id={`${formID}-${key}`}
                            type="time"
                            value={value[key]}
                            disabled={busy}
                            onChange={(event) => {
                              setValue({ ...value, [key]: event.target.value });
                              setSaved(false);
                            }}
                          />
                        </div>
                      ))}
                      <div className="col-span-2 space-y-2 sm:col-span-1">
                        <FieldLabel htmlFor={`${formID}-timezone`}>{copy.timezone}</FieldLabel>
                        <Input
                          id={`${formID}-timezone`}
                          value={value.timezone}
                          disabled={busy}
                          onChange={(event) => {
                            setValue({ ...value, timezone: event.target.value });
                            setSaved(false);
                          }}
                        />
                      </div>
                    </div>
                  </section>
                </>
              )}
              {section === "privacy" && (
                <div className="space-y-5">
                  <section className="flex items-start gap-3 border-b border-aui-border pb-5">
                    <ShieldIcon size={20} className="mt-1 shrink-0 text-aui-muted" />
                    <div>
                      <h2 className="text-sm font-medium">{copy.privateTitle}</h2>
                      <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.privateDefault}</p>
                    </div>
                  </section>
                  <section aria-label={copy.memorySettings}>
                    <PreferenceRow
                      icon={<BrainIcon size={18} />}
                      label={copy.remember}
                      description={copy.rememberHint}
                      checked={value.remember}
                      disabled={busy}
                      onChange={(remember) => {
                        setValue({ ...value, remember });
                        setSaved(false);
                      }}
                    />
                    <PreferenceRow
                      icon={<ProjectIcon size={18} />}
                      label={copy.personalMemory}
                      description={copy.personalMemoryHint}
                      checked={value.use_personal_memory_in_projects}
                      disabled={busy}
                      onChange={(use_personal_memory_in_projects) => {
                        setValue({ ...value, use_personal_memory_in_projects });
                        setSaved(false);
                      }}
                    />
                  </section>
                  <Button asChild variant="outline">
                    <a href="/library/memory">
                      <BrainIcon aria-hidden="true" />
                      {copy.memory}
                      <ChevronRightIcon aria-hidden="true" />
                    </a>
                  </Button>
                  <section className="rounded-aui-lg border border-aui-border p-4">
                    <h2 className="text-sm font-medium">
                      {defaultCopy.preferences.ownershipTitle}
                    </h2>
                    <p className="mt-1 text-sm leading-6 text-aui-muted">
                      {defaultCopy.preferences.ownershipDescription}
                    </p>
                  </section>
                </div>
              )}
              <div className="flex justify-end border-t border-aui-border pt-4">
                <Button type="submit" disabled={busy}>
                  {copy.save}
                </Button>
              </div>
            </form>
          )}
        </>
      )}
    </Page>
  );
}
