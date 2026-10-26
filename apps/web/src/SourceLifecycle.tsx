import { defaultCopy } from "@humagen/components";
import { type FormEvent, useEffect, useState } from "react";
import type { KnowledgeSourceConfig } from "../../../packages/client/src/generated/types.ts";
import { api, type Source } from "./api.ts";
import { ago, button, Chip, card, field, Label, Problem } from "./ui.tsx";

const words = defaultCopy.sourceLifecycle;

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
  const [config, setConfig] = useState<KnowledgeSourceConfig>({
    mode: lifecycle?.mode ?? "indexed",
    provider: lifecycle?.provider ?? "manual",
    origin: lifecycle?.origin ?? "",
    project: lifecycle ? lifecycle.project || "~" : "",
    stop_after_seconds: lifecycle?.stop_after_seconds ?? 86400,
  });
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
      setProblem(error instanceof Error ? error.message : words.failed);
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
                    mode: event.target.value as KnowledgeSourceConfig["mode"],
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
