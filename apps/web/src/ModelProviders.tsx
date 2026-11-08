import { PlusIcon } from "@humagen/render-web";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type MessageChoices,
  type ModelProvider,
  type ModelProviderCheck,
  type ModelProviderChoice,
  type ModelProviderInput,
  type ModelProviderKind,
} from "./api.ts";
import { Pill } from "./cards.tsx";
import { forgetConfiguredProviders, ProviderMark, useConfiguredProviders } from "./ModelChain.tsx";
import { catalogFor } from "./models.ts";
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

const EFFORTS = ["low", "medium", "high"] as const;

/**
 * What a person may pick for one message, for every agent it starts: models from the
 * providers enabled here, and reasoning efforts. Each agent's own models stay the default.
 */
export function MessageChoicesControl({
  saved,
  canEdit,
  onSave,
}: {
  saved?: MessageChoices;
  canEdit: boolean;
  onSave: (next: MessageChoices | undefined) => void;
}) {
  const configured = useConfiguredProviders();
  const [models, setModels] = useState<string[]>(saved?.models ?? []);
  const [efforts, setEfforts] = useState<string[]>(saved?.efforts ?? []);
  const [adding, setAdding] = useState({ provider: "", model: "" });
  const dirty =
    JSON.stringify([...models].sort()) !== JSON.stringify([...(saved?.models ?? [])].sort()) ||
    JSON.stringify([...efforts].sort()) !== JSON.stringify([...(saved?.efforts ?? [])].sort());
  const toggle = (list: string[], set: (next: string[]) => void, value: string, on: boolean) =>
    set(on ? [...list, value] : list.filter((v) => v !== value));

  const groups = (configured ?? []).map((c) => {
    const kind = c.kind ?? catalogFor(c.name)?.id;
    const offered = (catalogFor(kind)?.models ?? []).map((m) => ({
      ref: `${c.name}/${m.id}`,
      label: m.name,
    }));
    const extra = models
      .filter((m) => m.startsWith(`${c.name}/`) && !offered.some((o) => o.ref === m))
      .map((m) => ({ ref: m, label: m.slice(c.name.length + 1) }));
    return { name: c.name, kind: kind ?? c.name, options: [...offered, ...extra] };
  });
  const stranded = models.filter((m) => !groups.some((g) => m.startsWith(`${g.name}/`)));
  const add = () => {
    const ref = `${adding.provider}/${adding.model.trim()}`;
    if (!adding.provider || !adding.model.trim() || models.includes(ref)) return;
    setModels([...models, ref]);
    setAdding({ ...adding, model: "" });
  };

  return (
    <section className={`${card} flex min-w-0 flex-col gap-3`} aria-label="Message choices">
      <div className="flex flex-col gap-1">
        <h2 className="text-aui-sm font-medium text-aui-fg">Message choices</h2>
        <p className="text-aui-xs text-aui-muted">
          What people may pick for one message. Each agent's own models are the default.
        </p>
      </div>
      {configured !== null && configured.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">Enable a provider above to offer its models.</p>
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          {groups.map((g) => (
            <fieldset key={g.name} className="flex min-w-0 flex-col gap-1.5">
              <legend className="mb-1 flex items-center gap-2 text-aui-xs font-medium text-aui-fg">
                <ProviderMark provider={g.kind} size={14} />
                {g.name}
              </legend>
              <div className="flex min-w-0 flex-wrap gap-x-4 gap-y-1.5">
                {g.options.map((o) => (
                  <label key={o.ref} className="flex items-center gap-2 text-aui-sm">
                    <input
                      type="checkbox"
                      disabled={!canEdit}
                      checked={models.includes(o.ref)}
                      onChange={(e) => toggle(models, setModels, o.ref, e.target.checked)}
                    />
                    {o.label}
                  </label>
                ))}
              </div>
            </fieldset>
          ))}
          {stranded.length > 0 && (
            <fieldset className="flex min-w-0 flex-col gap-1.5">
              <legend className="mb-1 text-aui-xs font-medium text-aui-muted">
                On a provider that is not enabled
              </legend>
              <div className="flex min-w-0 flex-wrap gap-x-4 gap-y-1.5">
                {stranded.map((m) => (
                  <label key={m} className="flex items-center gap-2 text-aui-sm text-aui-muted">
                    <input
                      type="checkbox"
                      disabled={!canEdit}
                      checked
                      onChange={() => setModels(models.filter((v) => v !== m))}
                    />
                    {m}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          {canEdit && groups.length > 0 && (
            <div className="flex min-w-0 flex-wrap items-end gap-2">
              <Label text="Another model">
                <NativeSelect
                  aria-label="Provider of another model"
                  value={adding.provider || groups[0]?.name || ""}
                  onChange={(e) => setAdding({ ...adding, provider: e.target.value })}
                >
                  {groups.map((g) => (
                    <option key={g.name} value={g.name}>
                      {g.name}
                    </option>
                  ))}
                </NativeSelect>
              </Label>
              <input
                className={`${field} min-w-0 flex-1`}
                aria-label="Model id"
                placeholder="Model id"
                value={adding.model}
                onFocus={() =>
                  !adding.provider && setAdding({ ...adding, provider: groups[0]?.name ?? "" })
                }
                onChange={(e) =>
                  setAdding({
                    provider: adding.provider || groups[0]?.name || "",
                    model: e.target.value,
                  })
                }
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    add();
                  }
                }}
              />
              <button
                type="button"
                className={button}
                onClick={add}
                disabled={!adding.model.trim()}
              >
                Add
              </button>
            </div>
          )}
        </div>
      )}
      <fieldset className="flex min-w-0 flex-wrap items-center gap-2">
        <legend className="mb-1.5 text-aui-xs font-medium text-aui-fg">Reasoning effort</legend>
        {EFFORTS.map((e) => (
          <button
            key={e}
            type="button"
            disabled={!canEdit}
            aria-pressed={efforts.includes(e)}
            onClick={() => toggle(efforts, setEfforts, e, !efforts.includes(e))}
            className={`${button} aria-pressed:border-aui-fg aria-pressed:text-aui-fg`}
          >
            {e.charAt(0).toUpperCase() + e.slice(1)}
          </button>
        ))}
      </fieldset>
      {canEdit && dirty && (
        <div className="flex flex-wrap justify-end gap-2">
          <button
            type="button"
            className={button}
            onClick={() => {
              setModels(saved?.models ?? []);
              setEfforts(saved?.efforts ?? []);
            }}
          >
            Discard
          </button>
          <button
            type="button"
            className={primary}
            onClick={() =>
              onSave(models.length || efforts.length ? { models, efforts } : undefined)
            }
          >
            Save Choices
          </button>
        </div>
      )}
    </section>
  );
}
