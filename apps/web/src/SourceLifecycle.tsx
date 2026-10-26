import { defaultCopy } from "@humagen/components";
import { type FormEvent, useEffect, useState } from "react";

import { api, type Source, type SourceConfiguration } from "./api.ts";
import { ago, button, Chip, card, field, Label, Problem } from "./ui.tsx";

const words = defaultCopy.sourceLifecycle;

export function sourceProblem(error: unknown): string {
  const message = error instanceof Error ? error.message : defaultCopy.sourceOperations.failed;
  if (message.includes("auth_expired")) return defaultCopy.sourceOperations.reconnectRequired;
  for (const [cause, label] of Object.entries(defaultCopy.sourceLifecycle.cause))
    if (message.includes(cause)) return label;
  return message;
}

export function sourceConfiguration(source: Source): SourceConfiguration {
  const lifecycle = source.lifecycle;
  return {
    mode: lifecycle?.mode ?? "indexed",
    provider: lifecycle?.provider ?? "manual",
    origin: lifecycle?.origin ?? "",
    project: lifecycle ? lifecycle.project || "~" : "",
    stop_after_seconds: lifecycle?.stop_after_seconds ?? 86400,
    ...(lifecycle?.resources !== undefined ? { resources: [...lifecycle.resources] } : {}),
    ...(lifecycle?.snapshot !== undefined ? { snapshot: lifecycle.snapshot } : {}),
    ...(lifecycle?.sync_every_seconds !== undefined
      ? { sync_every_seconds: lifecycle.sync_every_seconds }
      : {}),
  };
}

/** A saved reference is deliberately separate from verified provider work. */
export function SourceLifecycle({
  source,
  canWrite,
  onChanged,
}: {
  source: Source;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const lifecycle = source.lifecycle;
  const [config, setConfig] = useState<SourceConfiguration>(() => sourceConfiguration(source));
  const [reference, setReference] = useState("");
  const [dependencies, setDependencies] = useState<
    { agents: string[]; projects: string[] } | null | undefined
  >(undefined);
  useEffect(() => {
    let active = true;
    api
      .sourceDependencies(source.name)
      .then((value) => {
        if (active) setDependencies(value);
      })
      .catch(() => {
        if (active) setDependencies(null);
      });
    return () => {
      active = false;
    };
  }, [source.name]);

  const [problem, setProblem] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const run = async (operation: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    setNotice("");
    try {
      await operation();
      onChanged();
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  const configure = (event: FormEvent) => {
    event.preventDefault();
    void run(() => api.configureSource(source.name, config));
  };
  const connect = (event: FormEvent) => {
    event.preventDefault();
    void run(async () => {
      await api.connectSource(source.name, reference);
      setReference("");
      setNotice(words.referenceSaved);
    });
  };
  return (
    <section className={`${card} flex flex-col gap-3`} aria-label={words.heading}>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="font-medium">{words.heading}</h2>
        <Chip>{words.modes[lifecycle?.mode ?? "indexed"]}</Chip>
        <Chip>
          {lifecycle
            ? words.status[lifecycle.status]
            : source.documents
              ? words.status.available
              : words.status.connected}
        </Chip>
      </div>
      {lifecycle?.cause && <output className="text-aui-sm">{words.cause[lifecycle.cause]}</output>}
      <p className="text-aui-xs text-aui-muted">
        {lifecycle?.last_good_at
          ? `${words.lastGood}: ${ago(lifecycle.last_good_at)}`
          : words.noCopy}
      </p>
      {lifecycle?.origin && (
        <p className="break-all text-aui-sm">
          {lifecycle.provider}: {lifecycle.origin}
        </p>
      )}
      {lifecycle?.owner && (
        <p className="text-aui-xs text-aui-muted">
          {words.owner}: {lifecycle.owner}
        </p>
      )}
      <Problem message={problem} />
      {notice && <output className="text-aui-sm">{notice}</output>}
      {busy && <output className="text-aui-sm">{words.working}</output>}
      {lifecycle && lifecycle.provider !== "manual" && (
        <>
          <form onSubmit={connect} className="flex flex-col gap-2">
            <Label text={words.credential} hint={words.credentialHint}>
              <input
                aria-label={words.credential}
                className={field}
                value={reference}
                autoComplete="off"
                required
                placeholder="op://"
                onChange={(event) => setReference(event.target.value)}
              />
            </Label>
            <button className={`${button} self-start`} type="submit" disabled={busy || !reference}>
              {words.connect}
            </button>
          </form>
          {canWrite && lifecycle.mode !== "live" && (
            <button
              className={`${button} self-start`}
              type="button"
              disabled={busy}
              onClick={() => void run(() => api.syncSource(source.name))}
            >
              {words.sync}
            </button>
          )}
        </>
      )}
      {canWrite && (
        <details>
          <summary className="min-h-8 cursor-pointer py-2 text-aui-sm">
            {words.configuration}
          </summary>
          <p className="mt-2 text-aui-xs text-aui-muted">{words.scopeHint}</p>
          <form onSubmit={configure} className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <Label text={words.provider}>
              <select
                className={field}
                value={config.provider}
                onChange={(event) => setConfig({ ...config, provider: event.target.value })}
              >
                {Object.entries(words.providers).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </Label>
            <Label text={words.mode}>
              <select
                className={field}
                value={config.mode}
                onChange={(event) =>
                  setConfig({
                    ...config,
                    mode: event.target.value as SourceConfiguration["mode"],
                    ...(event.target.value === "live" ? { resources: undefined } : {}),
                  })
                }
              >
                {Object.entries(words.modes).map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
              </select>
            </Label>
            <Label text={words.origin}>
              <input
                className={field}
                value={config.origin}
                maxLength={128}
                required={config.provider !== "manual"}
                onChange={(event) => setConfig({ ...config, origin: event.target.value })}
              />
            </Label>
            <Label text={words.project}>
              <input
                className={field}
                required={config.provider !== "manual"}
                value={config.project}
                onChange={(event) => setConfig({ ...config, project: event.target.value })}
              />
            </Label>
            <Label text={words.stop} hint={words.stopHint}>
              <input
                className={field}
                type="number"
                min={1}
                max={720}
                value={(config.stop_after_seconds ?? 86400) / 3600}
                onChange={(event) =>
                  setConfig({ ...config, stop_after_seconds: Number(event.target.value) * 3600 })
                }
              />
            </Label>
            <button className={`${button} self-start`} type="submit" disabled={busy}>
              {words.save}
            </button>
          </form>
        </details>
      )}
      <div className="flex flex-col gap-2">
        <h3 className="text-aui-sm font-medium">{words.dependents}</h3>
        {dependencies === undefined ? (
          <output className="text-aui-xs text-aui-muted">{words.working}</output>
        ) : dependencies ? (
          dependencies.agents.length + dependencies.projects.length === 0 ? (
            <p className="text-aui-xs text-aui-muted">{words.noDependents}</p>
          ) : (
            <ul className="flex flex-wrap gap-3">
              {dependencies.projects.map((name) => (
                <li key={`project:${name}`}>
                  <a
                    className="inline-flex min-h-8 items-center px-1 text-aui-sm underline"
                    href={`/projects/${encodeURIComponent(name || "~")}`}
                  >
                    {words.projects}: {name || "Workspace"}
                  </a>
                </li>
              ))}
              {dependencies.agents.map((name) => (
                <li key={`agent:${name}`}>
                  <a
                    className="inline-flex min-h-8 items-center px-1 text-aui-sm underline"
                    href={`/agents/${encodeURIComponent(name)}`}
                  >
                    {words.agents}: {name}
                  </a>
                </li>
              ))}
            </ul>
          )
        ) : (
          <p className="text-aui-xs text-aui-muted">{words.dependencyFailed}</p>
        )}
      </div>
      <details>
        <summary className="min-h-8 cursor-pointer py-2 text-aui-sm">{words.attempts}</summary>
        {lifecycle?.attempts.length ? (
          <ol className="mt-2 flex flex-col gap-2">
            {[...lifecycle.attempts].reverse().map((attempt) => (
              <li key={attempt.id} className="text-aui-xs">
                <time dateTime={attempt.started_at}>{ago(attempt.started_at)}</time>:{" "}
                {words.attemptStatus[attempt.status]}
                {attempt.cause
                  ? `: ${words.cause[attempt.cause as keyof typeof words.cause] ?? words.failed}`
                  : ""}
              </li>
            ))}
          </ol>
        ) : (
          <p className="mt-2 text-aui-xs text-aui-muted">{words.emptyAttempts}</p>
        )}
      </details>
    </section>
  );
}

/** A picker imports only explicitly selected resources from a complete provider listing. */
export function SourceResources({
  source,
  canWrite,
  onChanged,
}: {
  source: Source;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const words = defaultCopy.sourceOperations;
  const [resources, setResources] = useState<import("./api.ts").SourceResource[] | null>(null);
  const [complete, setComplete] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const lifecycle = source.lifecycle;
  const load = async () => {
    setBusy(true);
    setProblem(null);
    setResources(null);
    setComplete(false);
    setSelected([]);
    try {
      const result = await api.sourceResources(source.name);
      setResources(result.resources);
      setComplete(result.complete);
      setSelected(
        result.resources
          .filter((r) => r.supported && lifecycle?.resources?.includes(r.id))
          .map((r) => r.id),
      );
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    if (!lifecycle || !complete || selected.length === 0 || busy || !canWrite) return;
    setBusy(true);
    setProblem(null);
    try {
      await api.configureSource(source.name, {
        ...sourceConfiguration(source),
        resources: selected,
      });
      await api.syncSource(source.name);
      onChanged();
    } catch (error) {
      setProblem(sourceProblem(error));
      onChanged();
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={words.choose}>
      <h2 className="text-aui-sm font-semibold">{words.choose}</h2>
      <p className="text-aui-xs text-aui-muted">
        {lifecycle?.mode === "live"
          ? words.liveSelection
          : lifecycle?.mode === "hybrid"
            ? words.pinned
            : words.importHint}
      </p>
      <Problem message={problem} />
      <button
        type="button"
        className={`${button} self-start`}
        disabled={busy || !lifecycle || lifecycle.provider === "manual"}
        onClick={() => void load()}
      >
        {words.loadResources}
      </button>
      {busy && <output>{words.loading}</output>}
      {resources && !complete && (
        <p role="alert" className="text-aui-sm">
          {words.incomplete}
        </p>
      )}
      {resources?.length === 0 && <p className="text-aui-sm text-aui-muted">{words.noResources}</p>}
      <ul className="divide-y divide-aui-border">
        {resources?.map((resource) => (
          <li key={resource.id} className="py-2">
            <label className="flex min-h-11 items-start gap-3 py-2 text-aui-sm">
              <input
                type="checkbox"
                disabled={
                  busy ||
                  !complete ||
                  !resource.supported ||
                  !canWrite ||
                  lifecycle?.mode === "live" ||
                  lifecycle?.snapshot
                }
                checked={selected.includes(resource.id)}
                onChange={(e) =>
                  setSelected(
                    e.target.checked
                      ? [...selected, resource.id]
                      : selected.filter((id) => id !== resource.id),
                  )
                }
              />
              <span className="min-w-0 break-words">
                <span className="font-medium">{resource.name}</span>
                <span className="block text-aui-xs text-aui-muted">
                  {resource.kind}
                  {!resource.supported
                    ? `: ${resource.unsupported_reason || words.unsupported}`
                    : ""}
                </span>
              </span>
            </label>
          </li>
        ))}
      </ul>
      {resources && canWrite && (
        <button
          type="button"
          className={`${button} self-start`}
          disabled={
            busy ||
            !complete ||
            selected.length === 0 ||
            lifecycle?.mode === "live" ||
            lifecycle?.snapshot
          }
          onClick={() => void save()}
        >
          {words.addSelected} ({selected.length})
        </button>
      )}
    </section>
  );
}

/** Lifecycle settings are separate durable requests; errors never claim success. */
export function SourceSyncSettings({
  source,
  canWrite,
  onChanged,
}: {
  source: Source;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const words = defaultCopy.sourceOperations;
  const lifecycle = source.lifecycle;
  const [every, setEvery] = useState((lifecycle?.sync_every_seconds ?? 3600) / 60);
  const [stop, setStop] = useState((lifecycle?.stop_after_seconds ?? 86400) / 3600);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const run = async (fn: () => Promise<unknown>) => {
    if (busy || !canWrite) return;
    setBusy(true);
    setProblem(null);
    try {
      await fn();
      onChanged();
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={words.syncSettings}>
      <h2 className="text-aui-sm font-semibold">{words.syncSettings}</h2>
      <Problem message={problem} />
      {lifecycle?.snapshot && <p className="text-aui-sm text-aui-muted">{words.snapshotHint}</p>}
      <form
        className="grid max-w-lg grid-cols-1 gap-3 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault();
          void run(() =>
            api.sourceSyncSettings(source.name, {
              every_seconds: every * 60,
              stop_after_seconds: stop * 3600,
            }),
          );
        }}
      >
        <Label text={words.every}>
          <input
            className={field}
            type="number"
            min={0}
            max={43200}
            required
            disabled={!canWrite || busy || lifecycle?.snapshot}
            value={every}
            onChange={(e) => setEvery(Number(e.target.value))}
          />
        </Label>
        <Label text={words.stop}>
          <input
            className={field}
            type="number"
            min={1}
            max={720}
            required
            disabled={!canWrite || busy || lifecycle?.snapshot}
            value={stop}
            onChange={(e) => setStop(Number(e.target.value))}
          />
        </Label>
        <button
          type="submit"
          className={`${button} justify-self-start`}
          disabled={!canWrite || busy || !lifecycle || lifecycle.snapshot}
        >
          {words.save}
        </button>
      </form>
      {canWrite && lifecycle && (
        <div className="flex flex-wrap gap-3">
          <button
            type="button"
            className={button}
            disabled={busy || !lifecycle.last_good_at || lifecycle.snapshot}
            onClick={() => void run(() => api.snapshotSource(source.name))}
          >
            {words.keepSnapshot}
          </button>
          <button
            type="button"
            className={button}
            disabled={busy || !lifecycle.snapshot}
            onClick={() => void run(() => api.replaceSource(source.name))}
          >
            {words.replace}
          </button>
        </div>
      )}
    </section>
  );
}

export function SourceAccessMode({
  source,
  canWrite,
  onChanged,
}: {
  source: Source;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const words = defaultCopy.sourceOperations;
  const [config, setConfig] = useState(() => sourceConfiguration(source));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!canWrite || busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await api.configureSource(source.name, config);
      onChanged();
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form aria-label={words.access} className="flex max-w-xl flex-col gap-3" onSubmit={save}>
      <Problem message={problem} />
      <p className="text-aui-sm text-aui-muted">{words.scopeHint}</p>
      <Label text={words.mode}>
        <select
          aria-label={words.mode}
          className={field}
          value={config.mode}
          disabled={!canWrite || busy || source.lifecycle?.snapshot}
          onChange={(event) =>
            setConfig({
              ...config,
              mode: event.target.value as SourceConfiguration["mode"],
              ...(event.target.value === "live" ? { resources: undefined } : {}),
            })
          }
        >
          {Object.entries(defaultCopy.sourceLifecycle.modes).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </Label>
      <p className="text-aui-xs text-aui-muted">
        {config.mode === "live"
          ? words.liveSelection
          : config.mode === "hybrid"
            ? words.pinned
            : words.importHint}
      </p>
      <Label text={words.project}>
        <input
          aria-label={words.project}
          className={field}
          required
          disabled={!canWrite || busy || source.lifecycle?.snapshot}
          value={config.project || "~"}
          onChange={(event) => setConfig({ ...config, project: event.target.value })}
        />
      </Label>
      <button
        type="submit"
        className={`${button} self-start`}
        disabled={!canWrite || busy || !source.lifecycle || source.lifecycle.snapshot}
      >
        {words.saveAccess}
      </button>
    </form>
  );
}
