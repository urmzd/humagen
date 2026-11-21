import { defaultCopy } from "@humagen/components";
import { PlusIcon } from "@humagen/render-web";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@humagen/render-web/ui/sheet";
import { Switch } from "@humagen/render-web/ui/switch";
import { type FormEvent, type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import {
  ApiError,
  api,
  type ModelProvider,
  type ModelProviderCheck,
  type ModelProviderInput,
  type ModelProviderKind,
  type ModelProviderSetup,
} from "./api.ts";
import { Pill } from "./cards.tsx";
import { forgetConfiguredProviders, ProviderMark } from "./ModelChain.tsx";
import {
  button,
  field,
  Label,
  MoreMenu,
  Problem,
  pageLede,
  pageTitle,
  primary,
  SkeletonRows,
} from "./ui.tsx";

const words = defaultCopy.modelProviders;

/** The providers an administrator enables by name. Agents, the platform's included, name these. */
const KINDS: { kind: ModelProviderKind; name: string }[] = [
  { kind: "anthropic", name: "Anthropic" },
  { kind: "openai", name: "OpenAI" },
  { kind: "google", name: "Google" },
];

const kindName = (kind: ModelProviderKind) => KINDS.find((k) => k.kind === kind)?.name ?? kind;
const providerName = (name: string) => KINDS.find((k) => k.kind === name)?.name ?? name;

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
type Editor = Editing | { remove: ModelProvider };

const providerList = "min-w-0 divide-y divide-aui-border rounded-aui-lg border border-aui-border";

function ProviderGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section className="flex min-w-0 flex-col gap-2">
      <h2 className="font-medium text-aui-sm text-aui-fg">{label}</h2>
      {children}
    </section>
  );
}

function ProviderRow({
  name,
  kind,
  description,
  children,
  status,
  problem,
}: {
  name: string;
  kind: string;
  description: string;
  children?: ReactNode;
  status?: ReactNode;
  problem?: string;
}) {
  return (
    <li className="flex min-w-0 flex-col gap-2 px-3 py-2.5">
      <div className="flex min-h-9 min-w-0 items-center gap-3 pointer-coarse:min-h-11">
        <ProviderMark provider={kind} />
        <div className="flex min-w-0 flex-1 flex-col">
          <span className="truncate font-medium text-aui-sm text-aui-fg" title={name}>
            {name}
          </span>
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-aui-xs text-aui-muted" title={description}>
              {description}
            </span>
            {status}
          </div>
        </div>
        {children}
      </div>
      <Problem message={problem ?? null} />
    </li>
  );
}

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
  const [setup, setSetup] = useState<ModelProviderSetup | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [saving, setSaving] = useState(false);
  const [checks, setChecks] = useState<Record<string, ModelProviderCheck | "checking">>({});
  const [changing, setChanging] = useState<Record<string, boolean>>({});
  const [rowProblems, setRowProblems] = useState<Record<string, string>>({});
  const opener = useRef<HTMLElement | null>(null);
  const add = useRef<HTMLButtonElement | null>(null);

  const load = useCallback(() => {
    setProblem(null);
    api
      .modelProviderSetup()
      .then(setSetup)
      .catch((e) => setProblem(message(e, words.readFailed)));
  }, []);
  useEffect(() => load(), [load]);

  const begin = (next: Editor, trigger = document.activeElement) => {
    opener.current = trigger instanceof HTMLButtonElement ? trigger : add.current;
    setEditor(next);
  };

  const saveWorkspace = (next: (own: ModelProvider[]) => ModelProvider[]) => {
    setSetup((current) => current && { ...current, workspace: next(current.workspace ?? []) });
    forgetConfiguredProviders();
    onChanged?.();
  };
  const update = (m: ModelProvider) =>
    saveWorkspace((own) =>
      own.some((p) => p.name === m.name)
        ? own.map((p) => (p.name === m.name ? m : p))
        : [...own, m],
    );

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
            detail: message(e, words.checkFailed),
            latency_ms: 0,
          },
        })),
      );
  };
  const setEnabled = async (name: string, enabled: boolean) => {
    setChanging((current) => ({ ...current, [name]: true }));
    setRowProblems((current) => ({ ...current, [name]: "" }));
    try {
      update(await api.setModelProviderEnabled(name, enabled));
      setChecks(({ [name]: _old, ...rest }) => rest);
    } catch (error) {
      setRowProblems((current) => ({
        ...current,
        [name]: message(error, words.updateFailed(name)),
      }));
    } finally {
      setChanging((current) => ({ ...current, [name]: false }));
    }
  };
  const saved = (m: ModelProvider) => {
    update(m);
    setEditor(null);
    if (!m.disabled) check(m.name);
  };
  const remove = async (name: string) => {
    await api.deleteModelProvider(name);
    saveWorkspace((own) => own.filter((m) => m.name !== name));
    setChecks(({ [name]: _old, ...rest }) => rest);
    setRowProblems(({ [name]: _old, ...rest }) => rest);
    setEditor(null);
  };

  const own = setup?.workspace ?? [];
  const choices = setup?.providers ?? [];
  const deployed = choices.filter((p) => p.source === "deployment");
  const readOnly = canEdit ? [] : choices.filter((p) => p.source === "workspace" && p.declared);
  const managed = own.filter((m) => !deployed.some((p) => p.name === m.name));
  const configured = [...deployed, ...readOnly, ...managed];
  const available = KINDS.filter(({ kind }) => !configured.some((p) => p.name === kind));

  const workspaceRow = (m: ModelProvider) => {
    const name = providerName(m.name);
    const c = m.disabled ? undefined : checks[m.name];
    const result = c && c !== "checking" ? c : undefined;
    const pending = changing[m.name] || c === "checking";
    const status =
      c === "checking" ? (
        <Pill>{words.status.checking}</Pill>
      ) : result ? (
        <Pill
          tone={
            result.status === "operational" ? "ok" : result.status === "degraded" ? "warn" : "bad"
          }
        >
          {words.status[result.status]}
        </Pill>
      ) : undefined;
    return (
      <ProviderRow
        key={m.name}
        name={name}
        kind={m.kind}
        description={reach(m)}
        status={status}
        problem={
          rowProblems[m.name] || (result?.status !== "operational" ? result?.detail : undefined)
        }
      >
        <div className="flex shrink-0 items-center gap-1">
          <Switch
            type="button"
            aria-label={words.activation(name)}
            checked={!m.disabled}
            aria-busy={changing[m.name] || undefined}
            aria-disabled={pending || undefined}
            disabled={!canEdit}
            className="relative aria-disabled:cursor-wait aria-disabled:opacity-50 pointer-coarse:before:absolute pointer-coarse:before:-inset-y-2.5 pointer-coarse:before:inset-x-0"
            onCheckedChange={(enabled) => {
              if (!pending) void setEnabled(m.name, enabled);
            }}
          />
          {canEdit && (
            <MoreMenu
              label={words.manage(name)}
              items={[
                ...(!m.disabled
                  ? [{ label: words.check, disabled: pending, onSelect: () => check(m.name) }]
                  : []),
                {
                  label: words.edit,
                  disabled: pending,
                  onSelect: () => begin({ provider: m, kind: m.kind, name: m.name }),
                },
                {
                  label: words.remove,
                  destructive: true,
                  disabled: pending,
                  onSelect: () => begin({ remove: m }),
                },
              ]}
            />
          )}
        </div>
      </ProviderRow>
    );
  };

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-x-5 gap-y-3 pb-2">
        <div className="min-w-0 flex-[1_1_18rem]">
          <h1 className={pageTitle}>{words.title}</h1>
          <p className={pageLede}>{words.lede}</p>
        </div>
        {canEdit && (
          <button
            ref={add}
            type="button"
            className={primary}
            disabled={!setup}
            onClick={(event) => begin({ kind: "openai" }, event.currentTarget)}
          >
            <PlusIcon size={14} />
            {words.add}
          </button>
        )}
      </header>
      <Problem
        message={problem}
        action={
          <button type="button" className={button} onClick={load}>
            {defaultCopy.retry}
          </button>
        }
      />
      {!setup && !problem && <SkeletonRows height="3.5rem" />}
      {setup && (
        <>
          <ProviderGroup label={words.configured}>
            {configured.length === 0 ? (
              <p className="text-aui-sm text-aui-muted">{words.empty}</p>
            ) : (
              <ul aria-label={words.configured} className={providerList}>
                {deployed.map((p) => (
                  <ProviderRow
                    key={p.name}
                    name={providerName(p.name)}
                    kind={KINDS.find((k) => k.kind === p.name)?.kind ?? p.kind ?? p.name}
                    description={words.deploymentHint}
                  >
                    <Pill>{words.deployment}</Pill>
                  </ProviderRow>
                ))}
                {readOnly.map((p) => (
                  <ProviderRow
                    key={p.name}
                    name={providerName(p.name)}
                    kind={p.kind ?? p.name}
                    description={words.workspaceHint}
                  >
                    <Pill>{words.enabled}</Pill>
                  </ProviderRow>
                ))}
                {managed.map(workspaceRow)}
              </ul>
            )}
          </ProviderGroup>
          {available.length > 0 && (
            <ProviderGroup label={words.available}>
              <ul aria-label={words.available} className={providerList}>
                {available.map(({ kind, name }) => (
                  <ProviderRow key={kind} name={name} kind={kind} description={words.unconfigured}>
                    {canEdit && (
                      <button
                        type="button"
                        className={button}
                        aria-label={words.setupProvider(name)}
                        onClick={(event) => begin({ kind, name: kind }, event.currentTarget)}
                      >
                        {words.setup}
                      </button>
                    )}
                  </ProviderRow>
                ))}
              </ul>
            </ProviderGroup>
          )}
        </>
      )}
      <Sheet open={editor !== null} onOpenChange={(open) => !open && !saving && setEditor(null)}>
        <SheetContent
          aria-describedby={undefined}
          onCloseAutoFocus={(event) => {
            const target = opener.current?.isConnected ? opener.current : add.current;
            if (target) {
              event.preventDefault();
              target.focus();
            }
          }}
        >
          {editor &&
            ("remove" in editor ? (
              <RemoveProvider
                name={providerName(editor.remove.name)}
                busy={saving}
                setBusy={setSaving}
                onCancel={() => setEditor(null)}
                onRemove={() => remove(editor.remove.name)}
              />
            ) : (
              <ProviderForm
                editing={editor}
                taken={configured.map((m) => m.name)}
                busy={saving}
                setBusy={setSaving}
                onCancel={() => setEditor(null)}
                onSaved={saved}
              />
            ))}
        </SheetContent>
      </Sheet>
    </>
  );
}

function RemoveProvider({
  name,
  busy,
  setBusy,
  onCancel,
  onRemove,
}: {
  name: string;
  busy: boolean;
  setBusy: (busy: boolean) => void;
  onCancel: () => void;
  onRemove: () => Promise<void>;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  const remove = async () => {
    setBusy(true);
    setProblem(null);
    try {
      await onRemove();
    } catch (error) {
      setProblem(message(error, words.removeFailed));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <SheetHeader>
        <SheetTitle>{words.removeProvider(name)}</SheetTitle>
        <SheetDescription>{words.removeHint}</SheetDescription>
      </SheetHeader>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" disabled={busy} className={button} onClick={onCancel}>
          Cancel
        </button>
        <button
          type="button"
          disabled={busy}
          className={`${button} text-aui-bad`}
          onClick={() => void remove()}
        >
          {words.remove}
        </button>
      </div>
    </>
  );
}

/**
 * Enables a provider by its kind's name, adds a server by its address, or changes one. A
 * changed provider keeps its key unless a new one is given.
 */
function ProviderForm({
  editing,
  taken,
  busy,
  setBusy,
  onCancel,
  onSaved,
}: {
  editing: Editing;
  taken: string[];
  busy: boolean;
  setBusy: (busy: boolean) => void;
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
    ? `Edit ${providerName(existing.name)}`
    : server
      ? words.add
      : words.setupProvider(kindName(kind));
  return (
    <>
      <SheetHeader>
        <SheetTitle>{title}</SheetTitle>
      </SheetHeader>
      <form className="flex min-w-0 flex-col gap-3" onSubmit={submit} aria-label="Model provider">
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
          <button type="button" className={button} disabled={busy} onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className={primary} disabled={busy}>
            {busy ? "Saving…" : existing ? "Save" : server ? "Add Server" : words.setup}
          </button>
        </div>
      </form>
    </>
  );
}
