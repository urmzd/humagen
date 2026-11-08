import { PlusIcon } from "@humagen/render-web";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import {
  type AgentPackageListing,
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
import { catalogFor, defaultModel } from "./models.ts";
import { button, card, field, Label, Problem, primary } from "./ui.tsx";

const kindName: Record<ModelProviderKind, string> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  google: "Google",
};

const message = (error: unknown, fallback: string) =>
  error instanceof ApiError ? error.message : fallback;

/** One line saying how a provider is reached, never its key. */
function reach(m: ModelProvider): string {
  const where = m.vertex
    ? `Vertex AI, ${m.vertex.project}, ${m.vertex.location}`
    : m.base_url
      ? (() => {
          try {
            return new URL(m.base_url).host;
          } catch {
            return m.base_url;
          }
        })()
      : kindName[m.kind];
  const key = m.key_stored
    ? "key kept"
    : m.key_ref
      ? m.key_ref
      : m.vertex
        ? "server sign-in"
        : "no key";
  return `${where} · ${key}`;
}

/**
 * The workspace's model providers: what agents and routing may run on. An administrator
 * adds each with its key, kept as a value in the secret store or named by an environment
 * variable the server gives the workspace. Providers the deployment declares are listed
 * read only.
 */
export function ModelProviderSettings({
  canEdit,
  onAgents,
}: {
  canEdit: boolean;
  /** Opens Agents, where the built-in agents are installed. */
  onAgents?: () => void;
}) {
  const [own, setOwn] = useState<ModelProvider[] | null>(null);
  const [declared, setDeclared] = useState<ModelProviderChoice[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [editing, setEditing] = useState<ModelProvider | "new" | null>(null);
  const [checks, setChecks] = useState<Record<string, ModelProviderCheck | "checking">>({});
  const [confirming, setConfirming] = useState<string | null>(null);
  const [uninstalled, setUninstalled] = useState(0);

  const load = useCallback(() => {
    api
      .modelProviderSetup()
      .then((s) => {
        setOwn(s.workspace ?? []);
        setDeclared(s.providers.filter((p) => p.source === "deployment"));
      })
      .catch((e) => setProblem(message(e, "The providers could not be read.")));
    api
      .agentPackages()
      .then((all) => setUninstalled(all.filter((p) => p.installed < p.agents.length).length))
      .catch(() => setUninstalled(0));
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
      })
      .catch((e) => {
        setConfirming(null);
        setProblem(message(e, "The provider could not be removed."));
      });
  };

  const none = own !== null && own.length === 0 && declared.length === 0;
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <Problem message={problem} />
      {none && editing === null && (
        <div className={`${card} flex flex-wrap items-center justify-between gap-3`}>
          <p className="text-aui-sm text-aui-muted">
            No model provider yet. Agents can't answer until one is added.
          </p>
          {canEdit && (
            <button type="button" className={primary} onClick={() => setEditing("new")}>
              <PlusIcon size={14} />
              Add Provider
            </button>
          )}
        </div>
      )}
      {((own?.length ?? 0) > 0 || declared.length > 0) && (
        <ul className="flex min-w-0 flex-col divide-y divide-aui-border rounded-aui-lg border border-aui-border">
          {declared.map((d) => (
            <li key={`d:${d.name}`} className="flex min-w-0 items-center gap-3 px-3 py-2.5">
              <ProviderMark provider={d.kind ?? d.name} />
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-aui-sm font-medium text-aui-fg">{d.name}</span>
                <span className="truncate text-aui-xs text-aui-muted">
                  {d.kind ? kindName[d.kind] : "Provider"} · set in the deployment's spec
                </span>
              </div>
              <Pill>Deployment</Pill>
            </li>
          ))}
          {(own ?? []).map((m) => {
            const c = checks[m.name];
            return (
              <li key={m.name} className="flex min-w-0 flex-wrap items-center gap-3 px-3 py-2.5">
                <ProviderMark provider={m.kind} />
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-aui-sm font-medium text-aui-fg">{m.name}</span>
                  <span className="truncate text-aui-xs text-aui-muted" title={reach(m)}>
                    {reach(m)}
                  </span>
                  {c && c !== "checking" && c.status !== "operational" && c.detail && (
                    <span className="text-aui-xs text-aui-bad">{c.detail}</span>
                  )}
                </div>
                {c === "checking" ? (
                  <Pill>Checking</Pill>
                ) : c ? (
                  <Pill
                    tone={
                      c.status === "operational" ? "ok" : c.status === "degraded" ? "warn" : "bad"
                    }
                  >
                    {c.status === "operational"
                      ? "Answering"
                      : c.status === "degraded"
                        ? "Slow"
                        : "Failing"}
                  </Pill>
                ) : null}
                {canEdit &&
                  (confirming === m.name ? (
                    <span className="flex items-center gap-2">
                      <button type="button" className={button} onClick={() => setConfirming(null)}>
                        Keep
                      </button>
                      <button type="button" className={primary} onClick={() => remove(m.name)}>
                        Remove {m.name}
                      </button>
                    </span>
                  ) : (
                    <span className="flex items-center gap-1">
                      <button type="button" className={button} onClick={() => check(m.name)}>
                        Check
                      </button>
                      <button type="button" className={button} onClick={() => setEditing(m)}>
                        Edit
                      </button>
                      <button
                        type="button"
                        className={button}
                        onClick={() => setConfirming(m.name)}
                      >
                        Remove
                      </button>
                    </span>
                  ))}
              </li>
            );
          })}
        </ul>
      )}
      {canEdit && editing === null && own !== null && !none && (
        <button type="button" className={`${button} self-start`} onClick={() => setEditing("new")}>
          <PlusIcon size={14} />
          Add Provider
        </button>
      )}
      {editing !== null && (
        <ProviderForm
          key={editing === "new" ? "new" : editing.name}
          editing={editing === "new" ? undefined : editing}
          taken={[...(own ?? []).map((m) => m.name), ...declared.map((d) => d.name)]}
          onCancel={() => setEditing(null)}
          onSaved={(saved) => {
            setEditing(null);
            forgetConfiguredProviders();
            load();
            check(saved.name);
          }}
        />
      )}
      {uninstalled > 0 && !none && onAgents && (
        <p className="text-aui-sm text-aui-muted">
          The built-in agents are not installed yet.{" "}
          <button
            type="button"
            className="rounded-aui text-aui-fg underline focus-visible:outline-2 focus-visible:outline-aui-accent"
            onClick={onAgents}
          >
            Install in Agents
          </button>
        </p>
      )}
    </div>
  );
}

/** Adds a provider, or replaces one. A replaced provider keeps its key unless a new one is given. */
function ProviderForm({
  editing,
  taken,
  onCancel,
  onSaved,
}: {
  editing?: ModelProvider;
  taken: string[];
  onCancel: () => void;
  onSaved: (saved: ModelProvider) => void;
}) {
  const [kind, setKind] = useState<ModelProviderKind>(editing?.kind ?? "anthropic");
  const free = (k: string) => (taken.includes(k) ? `${k}-2` : k);
  const [name, setName] = useState(editing?.name ?? free("anthropic"));
  const [named, setNamed] = useState(!!editing);
  const [keyWay, setKeyWay] = useState<"value" | "env">(editing?.key_ref ? "env" : "value");
  const [key, setKey] = useState("");
  const [keyRef, setKeyRef] = useState(editing?.key_ref ?? "");
  const [baseURL, setBaseURL] = useState(editing?.base_url ?? "");
  const [vertex, setVertex] = useState(!!editing?.vertex);
  const [project, setProject] = useState(editing?.vertex?.project ?? "");
  const [location, setLocation] = useState(editing?.vertex?.location ?? "us-central1");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const keeps = !!editing && editing.kind === kind && (editing.key_stored || !!editing.key_ref);

  const pick = (next: ModelProviderKind) => {
    setKind(next);
    if (!named) setName(free(next));
    if (next !== "google") setVertex(false);
    if (next === "google") setBaseURL("");
  };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    const input: ModelProviderInput = { kind };
    if (kind !== "google" && baseURL.trim()) input.base_url = baseURL.trim();
    if (kind === "google" && vertex)
      input.vertex = { project: project.trim(), location: location.trim() };
    if (keyWay === "value" && key.trim()) input.key = key.trim();
    if (keyWay === "env" && keyRef.trim()) input.key_ref = keyRef.trim();
    setBusy(true);
    api
      .setModelProvider(name.trim(), input)
      .then(onSaved)
      .catch((err) => setProblem(message(err, "The provider could not be saved.")))
      .finally(() => setBusy(false));
  };

  const keyOptional =
    keeps || (kind === "openai" && !!baseURL.trim()) || (kind === "google" && vertex);
  return (
    <form
      className={`${card} flex min-w-0 flex-col gap-3`}
      onSubmit={submit}
      aria-label="Model provider"
    >
      <fieldset className="flex min-w-0 flex-wrap gap-2" disabled={!!editing}>
        <legend className="sr-only">Kind</legend>
        {(["anthropic", "openai", "google"] as const).map((k) => (
          <button
            key={k}
            type="button"
            aria-pressed={kind === k}
            onClick={() => pick(k)}
            className={`${button} gap-2 aria-pressed:border-aui-fg`}
          >
            <ProviderMark provider={k} />
            {kindName[k]}
          </button>
        ))}
      </fieldset>
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
        <Label text="Name" hint="What agents name it by.">
          <input
            className={field}
            value={name}
            disabled={!!editing}
            maxLength={63}
            pattern="[a-z0-9][a-z0-9-]*"
            required
            onChange={(e) => {
              setName(e.target.value.toLowerCase());
              setNamed(true);
            }}
          />
        </Label>
        {kind === "openai" && (
          <Label
            text="Base URL"
            hint="Leave empty for OpenAI. For Ollama, vLLM or Groq, their /v1 address."
          >
            <input
              className={field}
              type="url"
              placeholder="https://api.openai.com/v1"
              value={baseURL}
              onChange={(e) => setBaseURL(e.target.value)}
            />
          </Label>
        )}
      </div>
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
          {busy ? "Saving…" : editing ? "Save Provider" : "Add Provider"}
        </button>
      </div>
    </form>
  );
}

/**
 * The platform's agents, installed on a model an administrator picks. Nothing of the
 * platform's runs until then: there is no built-in model. Shows only packages not yet
 * fully installed.
 */
export function AgentPackages({
  canInstall,
  onProviders,
  onInstalled,
}: {
  canInstall: boolean;
  /** Opens Model providers, where a provider is set up. */
  onProviders?: () => void;
  onInstalled?: () => void;
}) {
  const [packages, setPackages] = useState<AgentPackageListing[] | null>(null);
  const [providers, setProviders] = useState<ModelProviderChoice[] | null>(null);
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const load = useCallback(() => {
    api
      .agentPackages()
      .then(setPackages)
      .catch(() => setPackages([]));
    api
      .modelProviders()
      .then((all) => {
        const set = all.filter((p) => p.declared);
        setProviders(set);
        const first = set[0];
        if (first) {
          setProvider((p) => p || first.name);
          setModel((m) => m || defaultModel[first.kind ?? first.name] || "");
        }
      })
      .catch(() => setProviders([]));
  }, []);
  useEffect(load, []);

  const open = (packages ?? []).filter((p) => p.installed < p.agents.length);
  if (!canInstall || packages === null || open.length === 0) return null;
  const kind = providers?.find((p) => p.name === provider)?.kind ?? provider;
  const suggested = catalogFor(kind)?.models ?? [];
  const install = (name: string) => {
    setProblem(null);
    setBusy(name);
    api
      .installAgentPackage(name, { provider, model: model.trim() })
      .then(() => {
        load();
        onInstalled?.();
      })
      .catch((e) => setProblem(message(e, "The package could not be installed.")))
      .finally(() => setBusy(null));
  };
  return (
    <section className={`${card} flex min-w-0 flex-col gap-3`} aria-label="Built-in agents">
      <h2 className="text-aui-sm font-medium text-aui-fg">Built-in agents</h2>
      {providers !== null && providers.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">
          They need a model.{" "}
          {onProviders && (
            <button
              type="button"
              className="rounded-aui text-aui-fg underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              onClick={onProviders}
            >
              Set Up a Provider
            </button>
          )}
        </p>
      ) : (
        <>
          <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
            <Label text="Provider">
              <NativeSelect
                aria-label="Install on provider"
                value={provider}
                onChange={(e) => {
                  setProvider(e.target.value);
                  const k =
                    providers?.find((p) => p.name === e.target.value)?.kind ?? e.target.value;
                  setModel(defaultModel[k] ?? "");
                }}
              >
                {(providers ?? []).map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </NativeSelect>
            </Label>
            <Label text="Model">
              <input
                className={field}
                list="package-models"
                aria-label="Install on model"
                value={model}
                onChange={(e) => setModel(e.target.value)}
              />
              <datalist id="package-models">
                {suggested.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name}
                  </option>
                ))}
              </datalist>
            </Label>
          </div>
          <ul className="flex min-w-0 flex-col divide-y divide-aui-border">
            {open.map((p) => (
              <li key={p.name} className="flex min-w-0 flex-wrap items-center gap-3 py-2">
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="text-aui-sm font-medium text-aui-fg">{p.title}</span>
                  <span className="text-aui-xs text-aui-muted">{p.description}</span>
                </div>
                <button
                  type="button"
                  className={primary}
                  disabled={busy !== null || !provider || !model.trim()}
                  onClick={() => install(p.name)}
                >
                  {busy === p.name ? "Installing…" : `Install ${p.title}`}
                </button>
              </li>
            ))}
          </ul>
          <Problem message={problem} />
        </>
      )}
    </section>
  );
}
