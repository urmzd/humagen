import { defaultCopy } from "@humagen/components";
import { type FormEvent, useCallback, useEffect, useRef, useState } from "react";
import {
  api,
  coworking,
  type BridgeDirection,
  type ClientCapabilities,
  type ActionPolicy,
  type Client,
  type Entry,
  type Source,
  type SourceLinkRule,
  type SourceLinkSuggestion,
  type SourceRuleMatch,
  type SourceRuleResource,
} from "./api.ts";
import { ClientConnect } from "./ClientConnect.tsx";
import {
  SourceLifecycle,
  SourceResources,
  SourceSyncSettings,
  SourceAccessMode,
  SourceAttachments,
  sourceProblem,
} from "./SourceLifecycle.tsx";
import { ago, button, Chip, field, Label, Page, primary, Problem } from "./ui.tsx";

export type IntegrationTab = "connect" | "resources" | "links" | "access" | "events" | "actions";
const words = defaultCopy.sourceOperations;
const tabs: IntegrationTab[] = ["connect", "resources", "links", "access", "events", "actions"];
function ruleInput(rule: SourceLinkRule) {
  const { id, copied_from, ...input } = rule;
  return input;
}
const modes = { suggest: words.suggest, auto_link: words.auto, off: words.off };

export function Integrations({
  canWrite,
  canKnowledgeWrite,
  selected,
  tab = "connect",
  onSelect,
  onOpenSource,
  onOpenProject,
}: {
  canWrite: boolean;
  canKnowledgeWrite: boolean;
  selected?: string;
  tab?: IntegrationTab;
  onSelect?: (id: string | undefined, tab?: IntegrationTab) => void;
  onOpenSource?: (name: string) => void;
  onOpenProject?: (name: string) => void;
}) {
  const [clients, setClients] = useState<Client[]>([]);
  const [sources, setSources] = useState<Source[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [local, setLocal] = useState(selected);
  const [localTab, setLocalTab] = useState<IntegrationTab>(tab);
  const [creating, setCreating] = useState(false);
  const [newKind, setNewKind] = useState(canKnowledgeWrite ? "source" : "client");
  const load = useCallback(() => {
    setProblem(null);
    setLoading(true);
    void Promise.allSettled([api.clients(), api.sources()]).then(([clientResult, sourceResult]) => {
      if (clientResult.status === "fulfilled") setClients(clientResult.value.clients);
      else setClients([]);
      if (sourceResult.status === "fulfilled") setSources(sourceResult.value);
      else setSources([]);
      if (clientResult.status === "rejected" || sourceResult.status === "rejected")
        setProblem(words.readFailed);
      setLoading(false);
    });
  }, []);
  useEffect(load, [load]);
  useEffect(() => setLocal(selected), [selected]);
  useEffect(() => setLocalTab(tab), [tab]);
  const id = selected ?? local;
  const active = onSelect ? tab : localTab;
  const source = sources.find((s) => `source:${s.name}` === id);
  const client = clients.find((c) => `client:${c.id}` === id);
  const pick = (next: string | undefined, nextTab: IntegrationTab = active) => {
    setLocal(next);
    setLocalTab(nextTab);
    onSelect?.(next, nextTab);
  };
  const mutate = async (fn: () => Promise<unknown>) => {
    setProblem(null);
    try {
      await fn();
      load();
    } catch (error) {
      setProblem(sourceProblem(error));
    }
  };
  return (
    <Page
      title={words.integrations}
      lede={words.integrationsHint}
      actions={
        <div className="flex flex-wrap gap-2">
          <button type="button" className={button} onClick={load}>
            {words.refresh}
          </button>
          {(canWrite || canKnowledgeWrite) && (
            <button type="button" className={primary} onClick={() => setCreating(true)}>
              {words.connectSystem}
            </button>
          )}
        </div>
      }
    >
      <Problem message={problem} />
      {loading && <output>{words.loading}</output>}
      {creating && (
        <section className="mb-6 flex min-w-0 flex-col gap-4 rounded-aui border border-aui-border p-4">
          <p className="text-aui-sm text-aui-muted">{words.connectHint}</p>
          <Label text={words.connectionType}>
            <select
              aria-label={words.connectionType}
              className={field}
              value={newKind}
              onChange={(e) => setNewKind(e.target.value)}
            >
              {canKnowledgeWrite && <option value="source">{words.externalSource}</option>}
              {canWrite && <option value="client">{words.messaging}</option>}
            </select>
          </Label>
          {newKind === "source" && canKnowledgeWrite ? (
            <ConnectSource
              onCreated={(name) => {
                setCreating(false);
                load();
                pick(`source:${name}`, "connect");
              }}
              onCancel={() => setCreating(false)}
            />
          ) : canWrite ? (
            <ClientConnect
              onCreated={() => {
                setCreating(false);
                load();
              }}
              onCancel={() => setCreating(false)}
            />
          ) : null}
        </section>
      )}
      <div className="grid min-w-0 grid-cols-1 items-start gap-6 @4xl:grid-cols-[15rem_minmax(0,1fr)]">
        <nav aria-label={words.systems} className="min-w-0">
          <h2 className="mb-2 text-aui-sm font-semibold">{words.systems}</h2>
          <ul className="flex min-w-0 flex-col gap-1">
            {clients.map((c) => (
              <li key={`client:${c.id}`}>
                <button
                  type="button"
                  className="flex min-h-12 w-full min-w-0 flex-col gap-1 rounded-aui px-2 py-3 text-left hover:bg-aui-hover"
                  aria-pressed={client?.id === c.id}
                  onClick={() => pick(`client:${c.id}`)}
                >
                  <span className="break-words text-aui-sm font-medium">{c.name}</span>
                  <span className="text-aui-xs text-aui-muted">
                    {c.surface === "slack"
                      ? defaultCopy.clientConnect.slack
                      : defaultCopy.clientConnect.github}
                    : {c.enabled ? words.enabled : words.disabled}
                  </span>
                </button>
              </li>
            ))}
            {sources.map((s) => (
              <li key={`source:${s.name}`}>
                <button
                  type="button"
                  className="flex min-h-12 w-full min-w-0 flex-col gap-1 rounded-aui px-2 py-3 text-left hover:bg-aui-hover"
                  aria-pressed={source?.name === s.name}
                  onClick={() => pick(`source:${s.name}`)}
                >
                  <span className="break-words text-aui-sm font-medium">{s.name}</span>
                  <span className="text-aui-xs text-aui-muted">
                    {s.lifecycle?.provider ?? defaultCopy.sourceLifecycle.providers.manual}
                  </span>
                </button>
              </li>
            ))}
          </ul>
          {!loading && clients.length + sources.length === 0 && (
            <p className="text-aui-sm text-aui-muted">{words.empty}</p>
          )}
        </nav>
        <section className="flex min-w-0 flex-col gap-5">
          {source || client ? (
            <>
              <h2 className="break-words text-aui-lg font-semibold">
                {source?.name ?? client?.name}
              </h2>
              <nav
                className="flex flex-wrap gap-1 border-b border-aui-border"
                aria-label={words.integrations}
              >
                {tabs.map((name) => (
                  <button
                    type="button"
                    key={name}
                    aria-current={active === name ? "page" : undefined}
                    className={`min-h-11 px-3 text-aui-sm ${active === name ? "border-b-2 border-aui-accent font-semibold" : "text-aui-muted"}`}
                    onClick={() => pick(id, name)}
                  >
                    {words[name]}
                  </button>
                ))}
              </nav>
              {active === "connect" &&
                (source ? (
                  <SourceLifecycle
                    key={`${source.name}:${source.updated_at}`}
                    source={source}
                    canWrite={canKnowledgeWrite}
                    onChanged={load}
                  />
                ) : (
                  client && (
                    <section className="flex flex-col gap-3">
                      <Chip>{words.configured}</Chip>
                      <p className="text-aui-sm text-aui-muted">{words.noLiveTest}</p>
                      <dl className="grid grid-cols-1 gap-2 text-aui-sm">
                        <dt className="text-aui-muted">{words.routing}</dt>
                        <dd>
                          {client.home?.project || words.workspace}
                          {client.home?.channel ? ` / #${client.home.channel}` : ""}
                        </dd>
                        <dt className="text-aui-muted">{words.defaultAgent}</dt>
                        <dd>{client.default_agent || words.noDefault}</dd>
                        <dt className="text-aui-muted">{words.allowed}</dt>
                        <dd className="break-words">
                          {client.agents?.join(", ") || words.noDefault}
                        </dd>
                      </dl>
                      {canWrite && !client.managed && (
                        <button
                          type="button"
                          className={`${button} self-start`}
                          onClick={() => void mutate(() => api.saveClient(client, !client.enabled))}
                        >
                          {client.enabled ? words.disabled : words.enabled}
                        </button>
                      )}
                    </section>
                  )
                ))}
              {active === "resources" &&
                (source ? (
                  <>
                    <SourceResources
                      key={`${source.name}:${source.updated_at}`}
                      source={source}
                      canWrite={canKnowledgeWrite}
                      onChanged={load}
                    />
                    <SourceAttachments
                      key={`${source.name}:${source.updated_at}:attachments`}
                      source={source}
                      canWrite={canKnowledgeWrite}
                      onChanged={load}
                    />
                  </>
                ) : (
                  client && (
                    <ClientResources
                      client={client}
                      canWrite={canWrite}
                      onOpenProject={onOpenProject}
                    />
                  )
                ))}
              {active === "links" && (
                <SourceRules
                  key={source?.name ?? client?.id ?? ""}
                  client={client?.id}
                  source={source?.name}
                  provider={source?.lifecycle?.provider ?? client?.surface ?? ""}
                  canWrite={canWrite}
                  onOpenProject={onOpenProject}
                />
              )}
              {active === "access" &&
                (source ? (
                  <>
                    <p className="text-aui-sm text-aui-muted">{words.scopeHint}</p>
                    <SourceAccessMode
                      key={`${source.name}:${source.updated_at}:access`}
                      source={source}
                      canWrite={canKnowledgeWrite}
                      onChanged={load}
                    />
                    <SourceSyncSettings
                      key={`${source.name}:${source.updated_at}`}
                      source={source}
                      canWrite={canKnowledgeWrite}
                      onChanged={load}
                    />
                    {onOpenSource && (
                      <button
                        type="button"
                        className={`${button} self-start`}
                        onClick={() => onOpenSource(source.name)}
                      >
                        {words.sourceDetail}
                      </button>
                    )}
                  </>
                ) : (
                  client && (
                    <ClientResources
                      client={client}
                      canWrite={canWrite}
                      onOpenProject={onOpenProject}
                    />
                  )
                ))}
              {active === "events" &&
                (client ? (
                  <>
                    <ClientCapabilitiesPanel client={client} mode="events" canWrite={canWrite} />
                    <ClientEvents client={client} />
                  </>
                ) : (
                  source && <SourceEvents source={source} />
                ))}
              {active === "actions" && (
                <>
                  {client && (
                    <ClientCapabilitiesPanel client={client} mode="actions" canWrite={canWrite} />
                  )}
                  <IntegrationActions />
                </>
              )}
            </>
          ) : (
            <p className="text-aui-sm text-aui-muted">{words.noSelection}</p>
          )}
        </section>
      </div>
    </Page>
  );
}

function ConnectSource({
  onCreated,
  onCancel,
}: {
  onCreated: (name: string) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [provider, setProvider] = useState("drive");
  const [origin, setOrigin] = useState("");
  const [project, setProject] = useState("~");
  const [mode, setMode] = useState<"indexed" | "live" | "hybrid">("indexed");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await api.createSource(name, description, { provider, origin, project, mode });
      onCreated(name);
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      aria-label={words.newSource}
      className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2"
      onSubmit={submit}
    >
      <Problem message={problem} />
      <Label text={words.name}>
        <input
          className={field}
          required
          disabled={busy}
          maxLength={40}
          pattern="[a-z0-9][a-z0-9-]{0,39}"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </Label>
      <Label text={words.description}>
        <input
          className={field}
          maxLength={500}
          disabled={busy}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Label>
      <Label text={words.provider}>
        <select
          aria-label={words.provider}
          className={field}
          value={provider}
          onChange={(e) => setProvider(e.target.value)}
        >
          <option value="drive">{defaultCopy.sourceLifecycle.providers.drive}</option>
          <option value="notion">{defaultCopy.sourceLifecycle.providers.notion}</option>
        </select>
      </Label>
      <Label text={words.mode}>
        <select
          aria-label={words.mode}
          className={field}
          value={mode}
          onChange={(e) => setMode(e.target.value as typeof mode)}
        >
          {Object.entries(defaultCopy.sourceLifecycle.modes).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </Label>
      <Label text={words.origin}>
        <input
          className={field}
          required
          maxLength={128}
          value={origin}
          onChange={(e) => setOrigin(e.target.value)}
        />
      </Label>
      <Label text={words.project} hint={words.scopeHint}>
        <input
          aria-label={words.project}
          className={field}
          required
          value={project}
          onChange={(e) => setProject(e.target.value)}
        />
      </Label>
      <div className="flex flex-wrap gap-2">
        <button type="submit" className={primary} disabled={busy}>
          {words.create}
        </button>
        <button type="button" className={button} onClick={onCancel}>
          {words.cancel}
        </button>
      </div>
    </form>
  );
}

function ClientResources({
  client,
  canWrite,
  onOpenProject,
}: {
  client: Client;
  canWrite: boolean;
  onOpenProject?: (name: string) => void;
}) {
  const [bindings, setBindings] = useState<Awaited<ReturnType<typeof api.clientBindings>> | null>(
    null,
  );
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    setBindings(null);
    setProblem(null);
    if (client.surface !== "slack") return;
    api
      .clientBindings(client.id)
      .then((r) => {
        if (active) setBindings(r);
      })
      .catch((error) => {
        if (active) setProblem(sourceProblem(error));
      });
    return () => {
      active = false;
    };
  }, [client.id, client.surface]);
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <Problem message={problem} />
      <h3 className="text-aui-sm font-semibold">
        {client.surface === "slack" ? words.bindings : words.resources}
      </h3>
      {client.surface === "slack" ? (
        <>
          {!bindings && !problem && <output>{words.loading}</output>}
          {bindings?.length === 0 && (
            <p className="text-aui-sm text-aui-muted">{words.noBindings}</p>
          )}
          <ul className="divide-y divide-aui-border">
            {bindings?.map((b) => (
              <li key={b.slack_channel} className="flex flex-wrap items-center gap-3 py-3">
                <span className="break-all text-aui-sm">{b.slack_channel}</span>
                <a
                  className="inline-flex min-h-11 items-center break-words text-aui-sm underline"
                  href={`/projects/${encodeURIComponent(b.project || "~")}/channels/${encodeURIComponent(b.channel)}`}
                >
                  {b.project || words.workspace} / #{b.channel}
                </a>
                <BridgeDirectionControl
                  key={JSON.stringify([client.id, b.project, b.channel, b.slack_channel, b.direction ?? "two_way"])}
                  client={client}
                  binding={b}
                  canWrite={canWrite}
                  onChanged={() =>
                    api
                      .clientBindings(client.id)
                      .then(setBindings)
                      .catch((error) => setProblem(sourceProblem(error)))
                  }
                />
              </li>
            ))}
          </ul>
        </>
      ) : (
        <ul className="flex flex-col gap-2">
          {(client.github?.read_repositories ?? client.identity.owners ?? []).map((repo) => (
            <li key={repo} className="break-all text-aui-sm">
              {repo}
            </li>
          ))}
        </ul>
      )}
      {client.home && (
        <a
          className="inline-flex min-h-11 items-center text-aui-sm underline"
          href={`/projects/${encodeURIComponent(client.home.project || "~")}`}
          onClick={(e) => {
            if (onOpenProject && !e.metaKey && !e.ctrlKey) {
              e.preventDefault();
              onOpenProject(client.home?.project ?? "");
            }
          }}
        >
          {words.destination}: {client.home.project || words.workspace}
        </a>
      )}
    </section>
  );
}
function ClientEvents({ client }: { client: Client }) {
  const [entries, setEntries] = useState<Entry[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    setEntries(null);
    setProblem(null);
    api
      .journal({ origin: client.surface, window: "24h", limit: "30" })
      .then((r) => {
        if (active) setEntries(r);
      })
      .catch((error) => {
        if (active) setProblem(sourceProblem(error));
      });
    return () => {
      active = false;
    };
  }, [client.id, client.surface]);
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <p className="text-aui-sm text-aui-muted">{words.eventsHint}</p>
      <p className="text-aui-xs text-aui-muted">{words.workspaceEvents}</p>
      <Problem message={problem} />
      {!entries && !problem && <output>{words.loading}</output>}
      {entries?.length === 0 && <p className="text-aui-sm text-aui-muted">{words.emptyEvents}</p>}
      <ul className="divide-y divide-aui-border">
        {entries?.map((entry) => (
          <li key={entry.seq} className="break-words py-3 text-aui-sm">
            {entry.kind}
            <span className="block text-aui-xs text-aui-muted">{ago(entry.at)}</span>
          </li>
        ))}
      </ul>
      <p className="text-aui-xs text-aui-muted">{words.eventHint}</p>
    </section>
  );
}
function SourceEvents({ source }: { source: Source }) {
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-aui-sm font-semibold">{words.attempts}</h3>
      {!source.lifecycle?.attempts.length && (
        <p className="text-aui-sm text-aui-muted">{words.noAttempts}</p>
      )}
      <ol className="divide-y divide-aui-border">
        {[...(source.lifecycle?.attempts ?? [])].reverse().map((attempt) => (
          <li className="py-3 text-aui-sm" key={attempt.id}>
            {defaultCopy.sourceLifecycle.attemptStatus[attempt.status]}
            {attempt.cause ? `: ${sourceProblem(new Error(attempt.cause))}` : ""}
            <span className="block text-aui-xs text-aui-muted">
              {ago(attempt.finished_at ?? attempt.started_at)}
            </span>
          </li>
        ))}
      </ol>
    </section>
  );
}
function IntegrationActions() {
  const [policy, setPolicy] = useState<ActionPolicy | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    api
      .actionPolicy("workspace")
      .then((r) => {
        if (active) setPolicy(r);
      })
      .catch(() => {
        if (active) setProblem(words.policyUnavailable);
      });
    return () => {
      active = false;
    };
  }, []);
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-aui-sm font-semibold">{words.policy}</h3>
      <p className="text-aui-sm text-aui-muted">{words.policyHint}</p>
      <Problem message={problem} />
      {!policy && !problem && <output>{words.loading}</output>}
      {policy && (
        <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {Object.entries(policy.rules).map(([category, level]) => (
            <div key={category}>
              <dt className="break-words text-aui-sm">
                {defaultCopy.operations.categories[
                  category as keyof typeof defaultCopy.operations.categories
                ] ?? category}
              </dt>
              <dd className="text-aui-xs text-aui-muted">
                {level === "allow" ? words.allow : level === "ask" ? words.ask : words.neverAction}
              </dd>
            </div>
          ))}
        </dl>
      )}
      {policy && Object.keys(policy.rules).length === 0 && (
        <p className="text-aui-sm text-aui-muted">{words.noPolicy}</p>
      )}
      <a
        href="/operations/policies"
        className="inline-flex min-h-11 items-center text-aui-sm underline"
      >
        {words.policies}
      </a>
    </section>
  );
}

function SourceRules({
  client,
  source,
  provider,
  canWrite,
  onOpenProject,
}: {
  client?: string;
  source?: string;
  provider: string;
  canWrite: boolean;
  onOpenProject?: (name: string) => void;
}) {
  const empty = (): SourceLinkRule => ({
    id: "",
    provider: provider as SourceLinkRule["provider"],
    attribute: "name",
    pattern: "",
    project: "",
    channel: "",
    mode: "suggest",
  });
  const [rules, setRules] = useState<SourceLinkRule[] | null>(null);
  const [editing, setEditing] = useState<SourceLinkRule | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [resource, setResource] = useState("");
  const [attributes, setAttributes] = useState("{}");
  const [matches, setMatches] = useState<SourceRuleMatch[] | null>(null);
  const [previewed, setPreviewed] = useState<string | null>(null);
  const signature = editing ? JSON.stringify(ruleInput(editing)) : null;
  const [suggestions, setSuggestions] = useState<SourceLinkSuggestion[] | null>(null);
  const [suggestionProblem, setSuggestionProblem] = useState<string | null>(null);
  const [suggestionsTick, setSuggestionsTick] = useState(0);
  const [suggestionState, setSuggestionState] =
    useState<SourceLinkSuggestion["state"]>("suggested");
  useEffect(() => {
    let active = true;
    setSuggestions(null);
    setSuggestionProblem(null);
    api
      .sourceLinkSuggestions(client, source, suggestionState)
      .then((items) => {
        if (active)
          setSuggestions(
            items.filter(
              (item) =>
                item.resource.provider === provider && (!source || item.knowledge === source),
            ),
          );
      })
      .catch((error) => {
        if (active) setSuggestionProblem(sourceProblem(error));
      });
    return () => {
      active = false;
    };
  }, [client, source, provider, suggestionsTick, suggestionState]);
  const decide = async (item: SourceLinkSuggestion, action: "link" | "ignore") => {
    if (!canWrite || busy) return;
    setBusy(true);
    setSuggestionProblem(null);
    try {
      const changed = await api.decideSourceLink(item.id, action, item.version);
      setSuggestions(
        (previous) => previous?.map((row) => (row.id === changed.id ? changed : row)) ?? [changed],
      );
    } catch (error) {
      setSuggestionProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  const turnRule = (item: SourceLinkSuggestion) => {
    const pair = Object.entries(item.resource.attributes)
      .sort(([a], [b]) => a.localeCompare(b))
      .find(([, values]) => values.length > 0);
    setEditing({
      id: "",
      provider: item.resource.provider as SourceLinkRule["provider"],
      attribute: pair?.[0] ?? "name",
      pattern: pair?.[1][0] ?? item.resource.id,
      project: item.project,
      channel: item.channel ?? "",
      mode: "suggest",
    });
    setResource(item.resource.id);
    setAttributes(JSON.stringify(item.resource.attributes, null, 2));
    setMatches(null);
    setPreviewed(null);
  };

  const load = useCallback(() => {
    setProblem(null);
    api
      .sourceLinkRules()
      .then(setRules)
      .catch((error) => setProblem(sourceProblem(error)));
  }, []);
  useEffect(load, [load]);
  const previousProvider = useRef(provider);
  useEffect(() => {
    if (previousProvider.current === provider) return;
    previousProvider.current = provider;
    setEditing(null);
    setMatches(null);
  }, [provider]);
  const run = async (fn: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await fn();
      load();
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  const preview = async (e: FormEvent) => {
    e.preventDefault();
    setMatches(null);
    let value: Record<string, string[]>;
    try {
      const parsed = JSON.parse(attributes);
      if (
        !parsed ||
        Array.isArray(parsed) ||
        typeof parsed !== "object" ||
        Object.values(parsed).some((v) => !Array.isArray(v) || v.some((i) => typeof i !== "string"))
      )
        throw new Error(words.invalidJSON);
      value = parsed;
    } catch {
      setProblem(words.invalidJSON);
      return;
    }
    setBusy(true);
    setProblem(null);
    try {
      const result = await api.previewSourceLinkRules({
        provider: provider as SourceRuleResource["provider"],
        id: resource,
        attributes: value,
        ...(editing ? { candidate: ruleInput(editing) } : {}),
      });
      setMatches(result.matches);
      setPreviewed(signature);
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  if (!["drive", "notion", "github", "slack"].includes(provider))
    return <p className="text-aui-sm text-aui-muted">{words.rulesUnsupported}</p>;
  return (
    <section className="flex min-w-0 flex-col gap-4">
      <h3 className="text-aui-sm font-semibold">{words.ruleTitle}</h3>
      <p className="text-aui-sm text-aui-muted">{words.ruleHint}</p>
      <Problem message={problem} />
      {!rules && !problem && <output>{words.loading}</output>}
      {rules?.filter((rule) => rule.provider === provider).length === 0 && (
        <p className="text-aui-sm text-aui-muted">{words.noRules}</p>
      )}
      <ul className="divide-y divide-aui-border">
        {rules
          ?.filter((rule) => rule.provider === provider)
          .map((rule) => (
            <li className="flex min-w-0 flex-col gap-2 py-3" key={rule.id}>
              <p className="break-words text-aui-sm">
                {rule.attribute}: {rule.pattern}
              </p>
              <a
                href={`/projects/${encodeURIComponent(rule.project || "~")}`}
                className="inline-flex min-h-11 items-center text-aui-sm underline"
                onClick={(e) => {
                  if (onOpenProject && !e.metaKey && !e.ctrlKey) {
                    e.preventDefault();
                    onOpenProject(rule.project);
                  }
                }}
              >
                {rule.project || words.workspace}
                {rule.channel ? ` / #${rule.channel}` : ""}
              </a>
              <Chip>{modes[rule.mode]}</Chip>
              {canWrite && (
                <div className="flex flex-wrap gap-2">
                  <button
                    type="button"
                    className={button}
                    disabled={busy}
                    onClick={() => setEditing({ ...rule })}
                  >
                    {words.edit}
                  </button>
                  <button
                    type="button"
                    className={button}
                    disabled={busy}
                    onClick={() => void run(() => api.deleteSourceLinkRule(rule.id))}
                  >
                    {words.remove}
                  </button>
                </div>
              )}
            </li>
          ))}
      </ul>
      {canWrite && !editing && (
        <button
          type="button"
          className={`${button} self-start`}
          onClick={() => setEditing(empty())}
        >
          {words.newRule}
        </button>
      )}
      {editing && (
        <form
          className="grid max-w-xl grid-cols-1 gap-3 sm:grid-cols-2"
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              if (editing.mode !== "off" && previewed !== signature) return;
              await api.saveSourceLinkRule(editing, !editing.id);
              setEditing(null);
            });
          }}
        >
          <Label text={words.attribute}>
            <input
              className={field}
              required
              maxLength={64}
              value={editing.attribute}
              onChange={(e) => setEditing({ ...editing, attribute: e.target.value })}
            />
          </Label>
          <Label text={words.pattern}>
            <input
              className={field}
              required
              maxLength={256}
              value={editing.pattern}
              onChange={(e) => setEditing({ ...editing, pattern: e.target.value })}
            />
          </Label>
          <Label text={words.destination}>
            <input
              className={field}
              required
              value={editing.project || "~"}
              onChange={(e) =>
                setEditing({ ...editing, project: e.target.value === "~" ? "" : e.target.value })
              }
            />
          </Label>
          <Label text={words.channel}>
            <input
              className={field}
              maxLength={40}
              value={editing.channel ?? ""}
              onChange={(e) => setEditing({ ...editing, channel: e.target.value })}
            />
          </Label>
          <Label text={words.ruleMode}>
            <select
              aria-label={words.ruleMode}
              className={field}
              value={editing.mode}
              onChange={(e) =>
                setEditing({ ...editing, mode: e.target.value as SourceLinkRule["mode"] })
              }
            >
              {Object.entries(modes).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </select>
          </Label>
          <div className="flex items-end gap-2">
            <button
              type="submit"
              className={primary}
              disabled={busy || (editing.mode !== "off" && previewed !== signature)}
            >
              {words.save}
            </button>
            <button type="button" className={button} onClick={() => setEditing(null)}>
              {words.cancel}
            </button>
          </div>
        </form>
      )}
      {editing && <p className="text-aui-xs text-aui-muted">{words.previewBeforeSave}</p>}
      <form onSubmit={preview} className="flex max-w-xl flex-col gap-3">
        <h3 className="text-aui-sm font-semibold">{words.preview}</h3>
        <p className="text-aui-xs text-aui-muted">{words.previewHint}</p>
        <Label text={words.resourceID}>
          <input
            className={field}
            required
            maxLength={256}
            value={resource}
            onChange={(e) => setResource(e.target.value)}
          />
        </Label>
        <Label text={words.metadata}>
          <textarea
            className={field}
            required
            rows={4}
            maxLength={16000}
            value={attributes}
            onChange={(e) => setAttributes(e.target.value)}
          />
        </Label>
        <button type="submit" className={`${button} self-start`} disabled={busy}>
          {words.preview}
        </button>
      </form>
      {matches?.length === 0 && <p className="text-aui-sm text-aui-muted">{words.noMatches}</p>}
      <ul>
        {matches?.map((match) => (
          <li
            key={`${match.project}:${match.channel ?? ""}`}
            className="break-words py-2 text-aui-sm"
          >
            {match.project || words.workspace}
            {match.channel ? ` / #${match.channel}` : ""}: {modes[match.mode]}
            <span className="block text-aui-xs text-aui-muted">
              {match.rules
                ?.map((id) => (id === "preview-candidate" ? words.previewCandidate : id))
                .join(", ") ?? ""}
            </span>
          </li>
        ))}
      </ul>
      <h3 className="text-aui-sm font-semibold">{words.suggestions}</h3>
      <button
        type="button"
        className={`${button} self-start`}
        disabled={busy}
        onClick={() => setSuggestionsTick((value) => value + 1)}
      >
        {words.refreshSuggestions}
      </button>
      <Label text={words.linkHistory}>
        <select
          aria-label={words.linkHistory}
          className={field}
          value={suggestionState}
          onChange={(event) =>
            setSuggestionState(event.target.value as SourceLinkSuggestion["state"])
          }
        >
          {Object.entries(words.linkStates).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </Label>
      <Problem message={suggestionProblem} />
      {!suggestions && !suggestionProblem && <output>{words.loading}</output>}
      {suggestions?.length === 0 && (
        <p className="text-aui-sm text-aui-muted">{words.noSuggestions}</p>
      )}
      <ul className="divide-y divide-aui-border">
        {suggestions?.map((item) => (
          <li key={item.id} className="flex min-w-0 flex-col gap-3 py-4">
            <p className="break-words text-aui-sm font-medium">{item.resource.id}</p>
            <a
              className="inline-flex min-h-11 items-center text-aui-sm underline"
              href={`/projects/${encodeURIComponent(item.project || "~")}`}
            >
              {item.project || words.workspace}
              {item.channel ? ` / #${item.channel}` : ""}
            </a>
            <p className="text-aui-xs text-aui-muted">
              {words.matchedRules}: {item.rules.join(", ")}
            </p>
            {item.state === "suggested" ? (
              canWrite && (
                <div className="flex flex-wrap gap-2">
                  <button
                    type="button"
                    className={button}
                    disabled={busy}
                    onClick={() => void decide(item, "link")}
                  >
                    {words.link}
                  </button>
                  <button
                    type="button"
                    className={button}
                    disabled={busy}
                    onClick={() => void decide(item, "ignore")}
                  >
                    {words.ignore}
                  </button>
                  <button
                    type="button"
                    className={button}
                    disabled={busy}
                    onClick={() => turnRule(item)}
                  >
                    {words.turnRule}
                  </button>
                </div>
              )
            ) : (
              <output className="text-aui-sm">
                {item.state === "linked"
                  ? item.automatic && "active" in item && item.active === false
                    ? words.inactive
                    : words.linked
                  : words.ignored}
              </output>
            )}
          </li>
        ))}
      </ul>
      {editing && <p className="text-aui-xs text-aui-muted">{words.ruleDraftHint}</p>}
    </section>
  );
}

function BridgeDirectionControl({
  client,
  binding,
  canWrite,
  onChanged,
}: {
  client: Client;
  binding: Awaited<ReturnType<typeof api.clientBindings>>[number];
  canWrite: boolean;
  onChanged: () => void;
}) {
  const [direction, setDirection] = useState<BridgeDirection>(binding.direction ?? "two_way");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (busy || !canWrite) return;
    setBusy(true);
    setProblem(null);
    try {
      const channels = await coworking.channels(binding.project);
      const channel = channels.find((c) => c.name === binding.channel);
      if (
        !channel ||
        channel.slack?.client !== client.id ||
        channel.slack.channel !== binding.slack_channel ||
        (channel.slack.direction ?? "two_way") !== (binding.direction ?? "two_way")
      )
        throw new Error(words.bindingChanged);
      await api.setSlackDirection(binding.project, channel, direction);
      onChanged();
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      aria-label={`${words.direction}: ${binding.slack_channel}`}
      className="flex w-full min-w-0 flex-col gap-3"
      onSubmit={save}
    >
      <Problem message={problem} />
      <p className="text-aui-xs text-aui-muted">{words.bridgeSemantics}</p>
      <Label text={words.direction} hint={words.directionHint}>
        <select
          aria-label={`${words.direction}: ${binding.slack_channel}`}
          className={field}
          value={direction}
          disabled={!canWrite || busy}
          onChange={(event) => setDirection(event.target.value as BridgeDirection)}
        >
          {Object.entries(words.directions).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </Label>
      {canWrite && (
        <button
          type="submit"
          className={`${button} self-start`}
          disabled={busy || direction === (binding.direction ?? "two_way")}
        >
          {words.saveDirection}
        </button>
      )}
    </form>
  );
}

function ClientCapabilitiesPanel({
  client,
  mode,
  canWrite,
}: {
  client: Client;
  mode: "events" | "actions";
  canWrite: boolean;
}) {
  const [record, setRecord] = useState<ClientCapabilities | null>(null);
  const [events, setEvents] = useState<string[]>([]);
  const [actions, setActions] = useState<string[]>([]);
  const [outcomes, setOutcomes] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const accept = (result: ClientCapabilities) => {
    setRecord(result);
    setEvents(result.events.filter((event) => event.enabled).map((event) => event.id));
    setActions(result.actions.filter((action) => action.enabled).map((action) => action.id));
    setOutcomes(result.deliver_outcomes);
  };
  useEffect(() => {
    let active = true;
    setRecord(null);
    setProblem(null);
    api
      .clientCapabilities(client.id)
      .then((result) => {
        if (active) accept(result);
      })
      .catch((error) => {
        if (active) setProblem(sourceProblem(error));
      });
    return () => {
      active = false;
    };
  }, [client.id, client.version]);
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!record || busy || !canWrite || record.managed) return;
    setBusy(true);
    setProblem(null);
    try {
      accept(
        await api.saveClientCapabilities(client.id, {
          version: record.version,
          events,
          actions,
          deliver_outcomes: outcomes,
        }),
      );
    } catch (error) {
      setProblem(sourceProblem(error));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="flex min-w-0 flex-col gap-3" aria-label={words.capabilities}>
      <h3 className="text-aui-sm font-semibold">
        {mode === "events" ? words.subscriptions : words.registeredActions}
      </h3>
      <button
        type="button"
        className={`${button} self-start`}
        disabled={busy}
        onClick={() => {
          setBusy(true);
          setProblem(null);
          api
            .clientCapabilities(client.id)
            .then(accept)
            .catch((error) => setProblem(sourceProblem(error)))
            .finally(() => setBusy(false));
        }}
      >
        {words.refreshCapabilities}
      </button>
      <p className="text-aui-sm text-aui-muted">{words.capabilitiesHint}</p>
      <Problem message={problem} />
      {!record && !problem && <output>{words.loading}</output>}
      {record && (
        <form onSubmit={save} className="flex flex-col gap-3">
          {record.managed && (
            <p className="text-aui-xs text-aui-muted">{words.managedCapabilities}</p>
          )}
          {record.legacy && (
            <p className="text-aui-xs text-aui-muted">{words.legacyCapabilities}</p>
          )}
          <fieldset
            disabled={!canWrite || record.managed || busy}
            className="flex min-w-0 flex-col gap-2"
          >
            {mode === "events" ? (
              record.events.map((event) => (
                <label key={event.id} className="flex min-h-11 items-center gap-3 text-aui-sm">
                  <input
                    type="checkbox"
                    checked={events.includes(event.id)}
                    onChange={(change) =>
                      setEvents(
                        change.target.checked
                          ? [...events, event.id]
                          : events.filter((id) => id !== event.id),
                      )
                    }
                  />
                  {words.eventNames[event.id as keyof typeof words.eventNames] ?? event.id}
                </label>
              ))
            ) : (
              <>
                {record.actions.length === 0 && (
                  <p className="text-aui-sm text-aui-muted">{words.noRegisteredActions}</p>
                )}
                {record.actions.map((action) => (
                  <label
                    key={action.id}
                    className="flex min-h-11 items-start gap-3 py-2 text-aui-sm"
                  >
                    <input
                      type="checkbox"
                      checked={actions.includes(action.id)}
                      onChange={(change) =>
                        setActions(
                          change.target.checked
                            ? [...actions, action.id]
                            : actions.filter((id) => id !== action.id),
                        )
                      }
                    />
                    <span className="min-w-0 break-words">
                      <span className="font-medium">{action.description || action.id}</span>
                      <span className="block text-aui-xs text-aui-muted">
                        {defaultCopy.operations.categories[
                          action.category as keyof typeof defaultCopy.operations.categories
                        ] ?? action.category}
                      </span>
                    </span>
                  </label>
                ))}
              </>
            )}
            <label className="flex min-h-11 items-center gap-3 text-aui-sm">
              <input
                type="checkbox"
                checked={outcomes}
                onChange={(event) => setOutcomes(event.target.checked)}
              />
              {words.deliverOutcomes}
            </label>
          </fieldset>
          {canWrite && !record.managed && (
            <button className={`${button} self-start`} type="submit" disabled={busy}>
              {words.saveCapabilities}
            </button>
          )}
        </form>
      )}
    </section>
  );
}
