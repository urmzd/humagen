import { defaultCopy } from "@humagen/components";
import { CheckIcon, ChevronDownIcon, CloudIcon, GitHubIcon } from "@humagen/render-web";
import { type KeyboardEvent, useEffect, useState } from "react";
import { call } from "./api.ts";
import { href } from "./route.ts";
import { Popover } from "./ui.tsx";

const words = defaultCopy.sessionSetup;

/** One environment a new session may run in, as the deployment offers it. */
export interface SessionEnvironment {
  /** "auto" is the deployment's default. */
  name: string;
  label: string;
  /** Runs lease an instance of their own. */
  isolated: boolean;
  /** A person may open a terminal in it. */
  terminal: boolean;
}

/** What a new session may be set up with (`GET /api/sessions/options`). */
export interface SessionOptions {
  environments: SessionEnvironment[];
  repos: string[];
}

/** A new session's choices, sent with its first message and kept on it. */
export interface SessionSetup {
  environment: string;
  repos: string[];
}

export const DEFAULT_SETUP: SessionSetup = { environment: "auto", repos: [] };

/** The deployment's environments and the repositories its GitHub apps may read, read once per page. */
let optionsRead: Promise<SessionOptions> | undefined;
export function useSessionOptions(
  enabled = true,
  read: () => Promise<SessionOptions> = () =>
    (optionsRead ??= call<SessionOptions>("GET", "/api/sessions/options").catch((e) => {
      optionsRead = undefined;
      throw e;
    })),
): SessionOptions | null | undefined {
  const [options, setOptions] = useState<SessionOptions | null>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: read once.
  useEffect(() => {
    if (!enabled) return;
    let on = true;
    read().then(
      (o) => on && setOptions(o),
      () => on && setOptions(null),
    );
    return () => {
      on = false;
    };
  }, []);
  return options;
}

/** Text controls under the composer, as the approval mode is: no boxes. */
const control =
  "inline-flex h-8 min-w-8 pointer-coarse:h-11 pointer-coarse:min-w-11 shrink-0 items-center justify-center gap-1.5 rounded-aui px-2 text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:bg-aui-hover aria-expanded:text-aui-fg";
const row =
  "flex min-h-11 w-full items-start gap-2.5 rounded-aui px-2.5 py-2 text-left outline-none hover:bg-aui-hover focus-visible:bg-aui-hover";

/** Arrow keys, Home and End move through a menu's rows; Escape or Tab closes it. */
const keys =
  (close: () => void, role: string) =>
  (e: KeyboardEvent<HTMLDivElement>): void => {
    if (e.key === "Escape" || e.key === "Tab") {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    const rows = [...e.currentTarget.querySelectorAll<HTMLElement>(`[role="${role}"]`)];
    const at = rows.indexOf(document.activeElement as HTMLElement);
    const to =
      e.key === "ArrowDown"
        ? (at + 1) % rows.length
        : e.key === "ArrowUp"
          ? (at - 1 + rows.length) % rows.length
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? rows.length - 1
              : -1;
    if (to < 0) return;
    e.preventDefault();
    rows[to]?.focus();
  };

/** What an environment offers, in a few words. */
export function environmentHint(e: SessionEnvironment): string {
  return [e.isolated ? words.isolated : words.onServer, e.terminal ? words.terminal : ""]
    .filter(Boolean)
    .join(" · ");
}

/**
 * Where a new session's tools run: Auto, the deployment's default, or one
 * of the environments it offers. Chosen before the first message.
 */
export function EnvironmentPicker({
  environments,
  value,
  onChange,
}: {
  environments: readonly SessionEnvironment[];
  value: string;
  onChange: (name: string) => void;
}) {
  const current = environments.find((e) => e.name === value) ?? environments[0];
  const name = current?.name === "auto" || !current ? words.auto : current.label;
  return (
    <Popover
      up
      popup="menu"
      label={`${words.environment}: ${name}`}
      tooltip={`${words.environment}: ${name}`}
      className={control}
      button={
        <>
          <CloudIcon size={16} />
          <span aria-hidden className="inline-flex items-center gap-1 text-aui-xs" data-label>
            <span className="max-w-32 truncate">{name}</span>
            <ChevronDownIcon size={12} />
          </span>
        </>
      }
    >
      {(close) => (
        <div
          role="menu"
          aria-label={words.environment}
          onKeyDown={keys(close, "menuitemradio")}
          className="flex min-w-60 flex-col"
          data-testid="environment-menu"
        >
          {environments.map((e) => {
            const on = e.name === current?.name;
            return (
              <button
                key={e.name}
                type="button"
                role="menuitemradio"
                aria-checked={on}
                // biome-ignore lint/a11y/noAutofocus: an opened menu takes focus on its current row.
                autoFocus={on}
                data-environment={e.name}
                onClick={() => {
                  close();
                  onChange(e.name);
                }}
                className={row}
              >
                <CloudIcon size={16} className="mt-0.5 shrink-0 text-aui-muted" />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="text-aui-sm text-aui-fg">
                    {e.name === "auto" ? words.auto : e.label}
                  </span>
                  <span className="text-aui-xs text-aui-muted">
                    {e.name === "auto" ? words.autoHint : environmentHint(e)}
                  </span>
                </span>
                <CheckIcon
                  size={16}
                  aria-hidden
                  className={`mt-0.5 shrink-0 text-aui-fg ${on ? "" : "invisible"}`}
                />
              </button>
            );
          })}
        </div>
      )}
    </Popover>
  );
}

/**
 * The repositories a new session works in, from those the workspace's
 * GitHub apps may read. Any number up to ten; none is fine.
 */
export function RepoPicker({
  repos,
  value,
  onChange,
  max = 10,
}: {
  repos: readonly string[];
  value: readonly string[];
  onChange: (repos: string[]) => void;
  max?: number;
}) {
  const name =
    value.length === 0
      ? words.repo
      : value.length === 1
        ? (value[0] ?? words.repo)
        : words.repos(value.length);
  return (
    <Popover
      up
      popup="menu"
      label={`${words.repoLabel}: ${value.length ? value.join(", ") : words.none}`}
      tooltip={`${words.repoLabel}: ${value.length ? value.join(", ") : words.none}`}
      className={control}
      button={
        <>
          <GitHubIcon size={16} />
          <span aria-hidden className="inline-flex items-center gap-1 text-aui-xs" data-label>
            <span className="max-w-40 truncate">{name}</span>
            <ChevronDownIcon size={12} />
          </span>
        </>
      }
    >
      {(close) => (
        <div
          role="menu"
          aria-label={words.repoLabel}
          onKeyDown={keys(close, "menuitemcheckbox")}
          className="flex min-w-64 flex-col"
          data-testid="repo-menu"
        >
          {repos.length === 0 ? (
            <div className="flex flex-col items-start gap-2 px-2.5 py-2">
              <p className="text-aui-sm text-aui-fg">{words.noRepos}</p>
              <a
                href={href({ page: "connections" })}
                className="inline-flex min-h-9 items-center rounded-aui text-aui-sm text-aui-fg underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                {words.connectGitHub}
              </a>
            </div>
          ) : (
            repos.map((r, i) => {
              const on = value.includes(r);
              const full = !on && value.length >= max;
              return (
                <button
                  key={r}
                  type="button"
                  role="menuitemcheckbox"
                  aria-checked={on}
                  aria-disabled={full || undefined}
                  // biome-ignore lint/a11y/noAutofocus: an opened menu takes focus on its first row.
                  autoFocus={i === 0}
                  data-repo={r}
                  onClick={() => {
                    if (full) return;
                    onChange(on ? value.filter((x) => x !== r) : [...value, r]);
                  }}
                  className={`${row} items-center aria-disabled:opacity-50`}
                >
                  <GitHubIcon size={16} className="shrink-0 text-aui-muted" />
                  <span className="min-w-0 flex-1 truncate text-aui-sm text-aui-fg">{r}</span>
                  <CheckIcon
                    size={16}
                    aria-hidden
                    className={`shrink-0 text-aui-fg ${on ? "" : "invisible"}`}
                  />
                </button>
              );
            })
          )}
        </div>
      )}
    </Popover>
  );
}

/**
 * A new session's setup, beside the composer's plus: where it runs and the
 * repositories it works in. Nothing shows until the options are read, and
 * nothing shows when they cannot be.
 */
export function SessionSetupPickers({
  value,
  onChange,
  options: given,
}: {
  value: SessionSetup;
  onChange: (next: SessionSetup) => void;
  /** For a test to stand in for the server. */
  options?: SessionOptions | null;
}) {
  const read = useSessionOptions(given === undefined);
  const options = given !== undefined ? given : read;
  if (!options) return null;
  const environments: SessionEnvironment[] = options.environments.some((e) => e.name === "auto")
    ? options.environments
    : [
        { name: "auto", label: words.auto, isolated: false, terminal: false },
        ...options.environments,
      ];
  return (
    <span className="inline-flex min-w-0 items-center gap-0.5" data-testid="session-setup">
      <EnvironmentPicker
        environments={environments}
        value={value.environment}
        onChange={(environment) => onChange({ ...value, environment })}
      />
      <RepoPicker
        repos={options.repos}
        value={value.repos}
        onChange={(repos) => onChange({ ...value, repos })}
      />
    </span>
  );
}
