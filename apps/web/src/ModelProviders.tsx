import { PlusIcon } from "@humagen/render-web";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type ModelProvider,
  type ModelProviderCheck,
  type ModelProviderChoice,
  type ModelProviderInput,
  type ModelProviderKind,
} from "./api.ts";
import { Pill } from "./cards.tsx";
import { forgetConfiguredProviders, ProviderMark } from "./ModelChain.tsx";
import { button, card, field, Label, Problem, primary } from "./ui.tsx";

/** The providers an administrator enables by name. Agents, the platform's included, name these. */
const KINDS: { kind: ModelProviderKind; name: string }[] = [
  { kind: "anthropic", name: "Anthropic" },
  { kind: "openai", name: "OpenAI" },
  { kind: "google", name: "Google" },
];

const kindName = (kind: ModelProviderKind) => KINDS.find((k) => k.kind === kind)?.name ?? kind;

const message = (error: unknown, fallback: string) =>
  error instanceof ApiError ? error.message : fallback;

/** One line saying how a provider is reached, never its key. */
function reach(m: ModelProvider): string {
  const host = (url: string) => {
    try {
      return new URL(url).host;
    } catch {
      return url;
    }
  };
  const where = m.vertex
    ? `Vertex AI, ${m.vertex.project}, ${m.vertex.location}`
    : m.base_url
      ? host(m.base_url)
      : kindName(m.kind);
  const key = m.key_stored
    ? "key kept"
    : m.key_ref
      ? m.key_ref
      : m.vertex
        ? "server sign-in"
        : "no key";
  return `${where} · ${key}`;
}

type Editing = { provider?: ModelProvider; kind: ModelProviderKind; name?: string };

/**
 * The providers agents and routing run on. Anthropic, OpenAI and Google are enabled by
 * name, which is what agents' models name; any server that speaks the OpenAI API is
 * added by its address. A key is kept as a value in the secret store or named by an
 * environment variable the server gives the workspace, and never shown again. Providers
 * the deployment declares are listed, read only.
 */
export function ModelProviderSettings({
  canEdit,
  onChanged,
}: {
  canEdit: boolean;
  /** A provider was enabled, changed or turned off. */
  onChanged?: () => void;
}) {
  const [own, setOwn] = useState<ModelProvider[] | null>(null);
  const [declared, setDeclared] = useState<ModelProviderChoice[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [checks, setChecks] = useState<Record<string, ModelProviderCheck | "checking">>({});
  const [confirming, setConfirming] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .modelProviderSetup()
      .then((s) => {
        setOwn(s.workspace ?? []);
        setDeclared(s.providers.filter((p) => p.source === "deployment"));
      })
      .catch((e) => setProblem(message(e, "The providers could not be read.")));
  }, []);
  useEffect(load, []);

  const check = (name: string) => {
    setChecks((c) => ({ ...c, [name]: "checking" }));
    api
      .checkModelProvider(name)
      .then((r) => setChecks((c) => ({ ...c, [name]: r })))
      .catch((e) =>
        setChecks((c) => ({
          ...c,
          [name]: {
            id: name,
            name,
            status: "down",
            detail: message(e, "Not checked"),
            latency_ms: 0,
          },
        })),
      );
  };
  const remove = (name: string) => {
    setProblem(null);
    api
      .deleteModelProvider(name)
      .then(() => {
        setConfirming(null);
        forgetConfiguredProviders();
        load();
        onChanged?.();
      })
      .catch((e) => {
        setConfirming(null);
        setProblem(message(e, "The provider could not be removed."));
      });
  };
  const saved = (m: ModelProvider) => {
    setEditing(null);
    forgetConfiguredProviders();
    load();
    onChanged?.();
    check(m.name);
  };

  if (own === null) return <Problem message={problem} />;
  const deployed = (name: string) => declared.find((d) => d.name === name);
  const mine = (name: string) => own.find((m) => m.name === name);
  const servers = own.filter((m) => !KINDS.some((k) => k.kind === m.name));
  const extraDeclared = declared.filter((d) => !KINDS.some((k) => k.kind === d.name));

  const status = (name: string) => {
    const c = checks[name];
    if (c === "checking") return <Pill>Checking</Pill>;
    if (!c) return <Pill tone="ok">Enabled</Pill>;
    return (
      <Pill tone={c.status === "operational" ? "ok" : c.status === "degraded" ? "warn" : "bad"}>
        {c.status === "operational" ? "Answering" : c.status === "degraded" ? "Slow" : "Failing"}
      </Pill>
    );
  };
  const actions = (m: ModelProvider, kind: ModelProviderKind) =>
    canEdit &&
    (confirming === m.name ? (
      <span className="flex items-center gap-2">
        <button type="button" className={button} onClick={() => setConfirming(null)}>
          Keep
        </button>
        <button type="button" className={primary} onClick={() => remove(m.name)}>
          Turn Off {m.name}
        </button>
      </span>
    ) : (
      <span className="flex items-center gap-1">
        <button type="button" className={button} onClick={() => check(m.name)}>
          Check
        </button>
        <button
          type="button"
          className={button}
          onClick={() => setEditing({ provider: m, kind, name: m.name })}
        >
          Edit
        </button>
        <button type="button" className={button} onClick={() => setConfirming(m.name)}>
          Turn Off
        </button>
      </span>
    ));
  const failure = (name: string) => {
    const c = checks[name];
    return c && c !== "checking" && c.status !== "operational" && c.detail ? (
      <span className="text-aui-xs text-aui-bad">{c.detail}</span>
    ) : null;
  };
  const row = "flex min-w-0 flex-wrap items-center gap-3 px-3 py-2.5";

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <Problem message={problem} />
      <ul
        aria-label="Model providers"
        className="flex min-w-0 flex-col divide-y divide-aui-border rounded-aui-lg border border-aui-border"
      >
        {KINDS.map(({ kind, name }) => {
          const d = deployed(kind);
          const m = mine(kind);
          return (
            <li key={kind} className={row}>
              <ProviderMark provider={kind} />
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-aui-sm font-medium text-aui-fg">{name}</span>
                <span className="truncate text-aui-xs text-aui-muted">
                  {d ? "Set in the deployment's spec" : m ? reach(m) : "Not enabled"}
                </span>
                {m && failure(m.name)}
              </div>
              {d ? (
                <Pill tone="ok">Deployment</Pill>
              ) : m ? (
                <>
                  {status(m.name)}
                  {actions(m, kind)}
                </>
              ) : (
                canEdit && (
                  <button
                    type="button"
                    className={primary}
                    onClick={() => setEditing({ kind, name: kind })}
                  >
                    Enable
                  </button>
                )
              )}
            </li>
          );
        })}
        {extraDeclared.map((d) => (
          <li key={`d:${d.name}`} className={row}>
            <ProviderMark provider={d.kind ?? d.name} />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-aui-sm font-medium text-aui-fg">{d.name}</span>
              <span className="truncate text-aui-xs text-aui-muted">
                {d.kind ? kindName(d.kind) : "Provider"} · set in the deployment's spec
              </span>
            </div>
            <Pill tone="ok">Deployment</Pill>
          </li>
        ))}
        {servers.map((m) => (
          <li key={m.name} className={row}>
            <ProviderMark provider={m.kind} />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-aui-sm font-medium text-aui-fg">{m.name}</span>
              <span className="truncate text-aui-xs text-aui-muted" title={reach(m)}>
                {reach(m)}
              </span>
              {failure(m.name)}
            </div>
            {status(m.name)}
            {actions(m, m.kind)}
          </li>
        ))}
      </ul>
      {canEdit && editing === null && (
        <button
          type="button"
          className={`${button} self-start`}
          onClick={() => setEditing({ kind: "openai" })}
        >
          <PlusIcon size={14} />
          Add OpenAI-Compatible Server
        </button>
      )}
      {editing !== null && (
        <ProviderForm
          key={editing.provider?.name ?? editing.name ?? "server"}
          editing={editing}
          taken={[...own.map((m) => m.name), ...declared.map((d) => d.name)]}
          onCancel={() => setEditing(null)}
          onSaved={saved}
        />
      )}
    </div>
  );
}

/**
 * Enables a provider by its kind's name, adds a server by its address, or changes one. A
 * changed provider keeps its key unless a new one is given.
 */
function ProviderForm({
  editing,
  taken,
  onCancel,
  onSaved,
}: {
  editing: Editing;
  taken: string[];
  onCancel: () => void;
  onSaved: (saved: ModelProvider) => void;
}) {
  const kind = editing.kind;
  const server = !editing.name || !KINDS.some((k) => k.kind === editing.name);
  const existing = editing.provider;
  const [name, setName] = useState(editing.name ?? "");
  const [keyWay, setKeyWay] = useState<"value" | "env">(existing?.key_ref ? "env" : "value");
  const [key, setKey] = useState("");
  const [keyRef, setKeyRef] = useState(existing?.key_ref ?? "");
  const [baseURL, setBaseURL] = useState(existing?.base_url ?? "");
  const [vertex, setVertex] = useState(!!existing?.vertex);
  const [project, setProject] = useState(existing?.vertex?.project ?? "");
  const [location, setLocation] = useState(existing?.vertex?.location ?? "us-central1");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const keeps = !!existing && (existing.key_stored || !!existing.key_ref);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    const trimmed = name.trim();
    if (!existing && taken.includes(trimmed)) {
      setProblem(`${trimmed} is already set up.`);
      return;
    }
    const input: ModelProviderInput = { kind };
    if (kind !== "google" && baseURL.trim()) input.base_url = baseURL.trim();
    if (kind === "google" && vertex)
      input.vertex = { project: project.trim(), location: location.trim() };
    if (keyWay === "value" && key.trim()) input.key = key.trim();
    if (keyWay === "env" && keyRef.trim()) input.key_ref = keyRef.trim();
    setBusy(true);
    api
      .setModelProvider(trimmed, input)
      .then(onSaved)
      .catch((err) => setProblem(message(err, "The provider could not be saved.")))
      .finally(() => setBusy(false));
  };

  const keyOptional = keeps || (server && !!baseURL.trim()) || (kind === "google" && vertex);
  const title = existing
    ? `Change ${existing.name}`
    : server
      ? "Add an OpenAI-compatible server"
      : `Enable ${kindName(kind)}`;
  return (
    <form
      className={`${card} flex min-w-0 flex-col gap-3`}
      onSubmit={submit}
      aria-label="Model provider"
    >
      <h2 className="flex items-center gap-2 text-aui-sm font-medium text-aui-fg">
        <ProviderMark provider={kind} />
        {title}
      </h2>
      {server && (
        <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
          <Label text="Name" hint="What agents name it by, such as groq or ollama.">
            <input
              className={field}
              value={name}
              disabled={!!existing}
              maxLength={63}
              pattern="[a-z0-9][a-z0-9-]*"
              required
              onChange={(e) => setName(e.target.value.toLowerCase())}
            />
          </Label>
          <Label text="Base URL" hint="Its /v1 address.">
            <input
              className={field}
              type="url"
              required
              placeholder="http://127.0.0.1:11434/v1"
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
            />
          </Label>
        </div>
      )}
      {kind === "google" && (
        <label className="flex items-center gap-2 text-aui-sm">
          <input type="checkbox" checked={vertex} onChange={(e) => setVertex(e.target.checked)} />
          Use Vertex AI with the server's own sign-in
        </label>
      )}
      {kind === "google" && vertex && (
        <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
          <Label text="Project">
            <input
              className={field}
              required
              value={project}
              onChange={(e) => setProject(e.target.value)}
            />
          </Label>
          <Label text="Location">
            <input
              className={field}
              required
              value={location}
              onChange={(e) => setLocation(e.target.value)}
            />
          </Label>
        </div>
      )}
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-[10rem_minmax(0,1fr)]">
        <Label text="Key">
          <NativeSelect
            aria-label="How the key is given"
            value={keyWay}
            onChange={(e) => setKeyWay(e.target.value as "value" | "env")}
          >
            <option value="value">Paste it</option>
            <option value="env">Environment variable</option>
          </NativeSelect>
        </Label>
        {keyWay === "value" ? (
          <Label
            text="Value"
            hint={
              keeps
                ? "Leave empty to keep the key it has."
                : "Kept in the secret store, never shown again."
            }
          >
            <input
              className={field}
              type="password"
              autoComplete="off"
              required={!keyOptional}
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
          </Label>
        ) : (
          <Label text="Variable" hint="One the server's configuration gives this workspace.">
            <input
              className={field}
              placeholder="env:HUMAGEN_ANTHROPIC_KEY"
              required={!keyOptional}
              value={keyRef}
              onChange={(e) => setKeyRef(e.target.value)}
            />
          </Label>
        )}
      </div>
      <Problem message={problem} />
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className={primary} disabled={busy}>
          {busy ? "Saving…" : existing ? "Save" : server ? "Add Server" : "Enable"}
        </button>
      </div>
    </form>
  );
}
