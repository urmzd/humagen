import { defaultCopy } from "@humagen/components";
import { type FormEvent, type MouseEvent, useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api, type ToolCredential, type ToolCredentials } from "./api.ts";
import { BigQueryConnection } from "./BigQueryConnection.tsx";
import { ClientConnect } from "./ClientConnect.tsx";
import { ConnectionDetail } from "./ConnectionDetail.tsx";
import { ConnectionHealth } from "./ConnectionHealth.tsx";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { ConnectionSection } from "./ConnectionSection.tsx";
import { type ConnectionPreset, connectionApi } from "./connectionApi.ts";
import {
  CONNECT_PERMISSION,
  cannotConnect,
  destination,
  offers,
  standing,
  startsAllowed,
  type Way,
  ways,
  whereReferences,
} from "./connections.ts";
import { ConnectSource, Integrations } from "./Integrations.tsx";
import { href } from "./route.ts";
import { ToolList, YourToolServers } from "./ToolServers.tsx";
import {
  button,
  card,
  Empty,
  field,
  Label,
  Page,
  Problem,
  primary,
  Segmented,
  Tabs,
} from "./ui.tsx";

/** A plain click goes there in place; a modified one is the browser's. */
const follow = (go: () => void) => (e: MouseEvent<HTMLAnchorElement>) => {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go();
};

/**
 * Your connections: the tools in this workspace that act as you, reached with
 * your own credential, and whether you have given one. Everything here is
 * yours alone; nobody else's credential is shown or used for you.
 */
export function YourConnections({
  open,
  section,
  onOpen,
  canConnect = true,
  onWhy,
  embedded = false,
}: {
  embedded?: boolean;
  open?: string;
  section?: "connections" | "tools";
  onOpen: (provider?: string) => void;
  /** May give or remove a credential, which the server holds to CONNECT_PERMISSION. */
  canConnect?: boolean;
  /** Opens why the person may not connect, in Settings > Access. */
  onWhy?: () => void;
}) {
  const [state, setState] = useState<ToolCredentials | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const fail = (fallback: string) => (e: unknown) =>
    setProblem(e instanceof ApiError ? e.message : fallback);
  const reload = useCallback(() => {
    api.toolCredentials().then(setState).catch(fail("Your connections could not be loaded."));
  }, []);
  useEffect(reload, [reload]);

  const save = (provider: string, way: Way, text: string) => {
    setProblem(null);
    setBusy(true);
    api
      .connectTool(provider, way === "value" ? { value: text } : { ref: text.trim() })
      .then(() => {
        onOpen(undefined);
        reload();
      })
      .catch(fail("The credential could not be saved."))
      .finally(() => setBusy(false));
  };
  const remove = (provider: string) => {
    setProblem(null);
    api.disconnectTool(provider).then(reload).catch(fail("The credential could not be removed."));
  };

  return (
    <ConnectionSection
      embedded={embedded}
      title={section === "tools" ? "Your tool servers" : "Your connections"}
      lede="Some tools act as you: they reach their service with your own credential. Without one, an agent cannot use them for you, and nobody else's is used instead."
      narrow
    >
      <Problem message={problem} />
      {section !== "tools" && !canConnect && state && state.providers.length > 0 && (
        <p className="text-aui-sm text-aui-muted">
          {cannotConnect}
          {onWhy && (
            <>
              {" "}
              <a
                href={href({ page: "settings", section: "access", permission: CONNECT_PERMISSION })}
                onClick={follow(onWhy)}
                className="rounded-aui text-aui-fg underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                Why can't I?
              </a>
            </>
          )}
        </p>
      )}
      {section !== "tools" && state && state.providers.length === 0 && (
        <Empty title="No tools here act as you">
          Connect a service below to give agents access through your own account.
        </Empty>
      )}
      {section !== "tools" &&
        state?.providers.map((c) => (
          <ConnectionCard
            key={c.provider}
            connection={c}
            ways={ways(state)}
            references={state.references}
            canConnect={canConnect}
            open={canConnect && open === c.provider}
            busy={busy}
            onOpen={() => onOpen(c.provider)}
            onCancel={() => onOpen(undefined)}
            onSave={(way, text) => save(c.provider, way, text)}
            onRemove={() => remove(c.provider)}
          />
        ))}
      {section !== "connections" && state && (
        <YourToolServers
          references={state.references}
          allowedHosts={state.allowed_hosts}
          canConnect={canConnect}
        />
      )}
    </ConnectionSection>
  );
}

/** One tool that acts as you, and what you gave for it. */
export function ConnectionCard({
  connection: c,
  ways,
  references,
  canConnect = true,
  open,
  busy = false,
  onOpen,
  onCancel,
  onSave,
  onRemove,
}: {
  connection: ToolCredential;
  ways: Way[];
  references: string[];
  /** May give or remove a credential. One who may not is shown where they stand, and nothing to press. */
  canConnect?: boolean;
  open: boolean;
  busy?: boolean;
  onOpen: () => void;
  onCancel: () => void;
  onSave: (way: Way, text: string) => void;
  onRemove: () => void;
}) {
  return (
    <section className={`${card} flex flex-col gap-3`} aria-label={c.provider}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-aui-sm font-medium text-aui-fg">
          {c.provider} <span className="font-normal text-aui-muted">{offers(c)}</span>
        </h2>
        <span className={`text-aui-xs ${c.connected ? "text-aui-fg" : "text-aui-muted"}`}>
          {c.connected ? "Connected" : "Not connected"}
        </span>
      </div>
      <p className="text-aui-sm text-aui-muted">{standing(c)}</p>
      <p className="text-aui-xs text-aui-muted">{destination(c)}</p>
      {c.connected && (
        <dl className="text-aui-xs text-aui-muted">
          <div>
            <dt>{defaultCopy.personalPreferences.lastAgentUse}</dt>
            <dd>
              {c.last_used_at
                ? new Date(c.last_used_at).toLocaleString()
                : defaultCopy.personalPreferences.neverUsed}
            </dd>
          </div>
          <div className="mt-2">
            <dt>{defaultCopy.personalPreferences.grantedScopes}</dt>
            <dd>{defaultCopy.personalPreferences.scopesUnknown}</dd>
          </div>
        </dl>
      )}
      {!canConnect ? null : open ? (
        <ConnectForm
          ways={ways}
          references={references}
          replacing={c.connected}
          busy={busy}
          onSave={onSave}
          onCancel={onCancel}
        />
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          {ways.length > 0 && (
            <button type="button" className={c.connected ? button : primary} onClick={onOpen}>
              {c.connected ? "Replace" : "Connect"}
            </button>
          )}
          {c.connected && (
            <button type="button" className={button} onClick={onRemove}>
              Remove
            </button>
          )}
          {ways.length === 0 && !c.connected && (
            <p className="text-aui-xs text-aui-muted">
              This workspace lets nobody connect a credential yet: the operator has listed nowhere
              references may live, and stored values are off.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/**
 * Give a credential: a reference to where it is kept, or, where the workspace
 * keeps them, the value. A value is typed once and never shown again.
 */
export function ConnectForm({
  ways,
  references,
  replacing,
  busy = false,
  onSave,
  onCancel,
}: {
  ways: Way[];
  references: string[];
  replacing: boolean;
  busy?: boolean;
  onSave: (way: Way, text: string) => void;
  onCancel: () => void;
}) {
  const [way, setWay] = useState<Way>(ways[0] ?? "reference");
  const [text, setText] = useState("");
  const outside = way === "reference" && text.trim() !== "" && !startsAllowed(text, references);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (text.trim() === "" || outside) return;
    onSave(way, text);
  };
  return (
    <form className="flex flex-col gap-3" onSubmit={submit}>
      {ways.length > 1 && (
        <Segmented<Way>
          label="How to give it"
          value={way}
          options={[
            { id: "reference", label: "Reference" },
            { id: "value", label: "Value" },
          ]}
          onChange={(next) => {
            setWay(next);
            setText("");
          }}
        />
      )}
      {way === "reference" ? (
        <Label
          text="Reference"
          hint={
            outside
              ? "That is not somewhere this workspace lets references live."
              : whereReferences(references)
          }
        >
          <input
            className={field}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={references[0] ? `${references[0]}…` : "op://VAULT/ITEM/FIELD"}
            autoComplete="off"
            spellCheck={false}
          />
        </Label>
      ) : (
        <Label
          text="Value"
          hint="Stored with Humagen, in the store client secrets are kept in. It is never shown again."
        >
          <input
            className={field}
            type="password"
            value={text}
            onChange={(e) => setText(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </Label>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className={primary} disabled={busy || text.trim() === "" || outside}>
          {replacing ? "Replace" : "Connect"}
        </button>
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

/** One address for systems, personal accounts, and approved tool connections. */
export function Connections({
  provider,
  canConnect = true,
  canWrite = false,
  canKnowledgeWrite = false,
  onOpen,
}: {
  provider?: string;
  canConnect?: boolean;
  canWrite?: boolean;
  canKnowledgeWrite?: boolean;
  onOpen?: (id?: string) => void;
}) {
  const [presets, setPresets] = useState<ConnectionPreset[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | undefined>(
    provider === "callback" ? undefined : provider,
  );
  useEffect(() => {
    if (provider !== "callback") setSelected(provider);
  }, [provider]);
  // Every connection belongs to one person; the workspace only decides which services are allowed.
  const scope = "person" as "person" | "workspace";
  const [tab, setTab] = useState<ConnectionTab>(() => tabFor(provider));
  useEffect(() => {
    if (provider && provider !== "callback") setTab(tabFor(provider));
  }, [provider]);
  const [endpoint, setEndpoint] = useState("");
  const [registered, setRegistered] = useState(false);
  const [issuer, setIssuer] = useState("");
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [review, setReview] = useState<Awaited<ReturnType<typeof api.reviewPersonTools>> | null>(
    null,
  );
  const callbackStarted = useRef(false);
  const reload = useCallback(() => {
    connectionApi
      .presets()
      .then(setPresets)
      .catch((e: unknown) =>
        setProblem(e instanceof Error ? e.message : "Connections could not be loaded."),
      );
  }, []);
  useEffect(reload, [reload]);
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (!params.has("state") || callbackStarted.current) return;
    callbackStarted.current = true;
    const provider = sessionStorage.getItem("humagen.connection.provider");
    const nonce = params.get("state");
    const code = params.get("code");
    const installation = params.get("installation_id") ?? undefined;
    // Remove authorization material from browser history before any request.
    window.history.replaceState(window.history.state, "", "/connections");
    sessionStorage.removeItem("humagen.connection.provider");
    if (!provider || !nonce || !code) {
      setProblem("Connection was not authorized. Choose Connect to try again.");
      return;
    }
    setBusy(true);
    connectionApi
      .complete(provider, nonce, code, installation)
      .then(() => {
        reload();
        setSelected(provider);
      })
      .catch((e: unknown) =>
        setProblem(e instanceof Error ? e.message : "Connection could not be completed."),
      )
      .finally(() => setBusy(false));
  }, [reload]);
  const act = async (fn: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await fn();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : "The connection could not be changed.");
    } finally {
      setBusy(false);
    }
  };
  const connect = (
    preset: ConnectionPreset,
    requestedScope: "person" | "workspace" = preset.connected
      ? preset.scope === "workspace"
        ? "workspace"
        : "person"
      : scope,
  ) =>
    void act(async () => {
      const auth = await connectionApi.authorize(preset.id, {
        scope: requestedScope,
        ...(preset.endpoint ? { endpoint: preset.endpoint } : {}),
      });
      sessionStorage.setItem("humagen.connection.provider", auth.provider ?? preset.id);
      window.location.assign(auth.url);
    });
  const reviewTools = (preset: ConnectionPreset) =>
    void act(async () => {
      if (!preset.endpoint) return;
      setReview(
        await api.reviewPersonTools({ name: preset.id, url: preset.endpoint, oauth: preset.id }),
      );
    });
  const approve = () =>
    void act(async () => {
      if (!review) return;
      const current = await api.personContext();
      await api.setPersonContext({
        run: current.run,
        version: current.version,
        tool_servers: [...current.tool_servers.filter((s) => s.name !== review.name), review],
      });
      setReview(null);
      setRevision((v) => v + 1);
    });
  return (
    <Page
      title="Connections"
      lede="Connect your own accounts. Agents act as you with them, nobody else uses your credentials, and you can revoke one at any time."
    >
      <Problem message={selected === "other" ? null : problem} />
      <Tabs
        label="Connection kinds"
        value={tab}
        onChange={(next) => {
          setTab(next);
          setSelected(undefined);
        }}
        tabs={[
          { id: "tools", label: "Agent tools" },
          { id: "knowledge", label: "Knowledge" },
          ...(canWrite ? [{ id: "workspace" as const, label: "Workspace apps" }] : []),
        ]}
      />
      <p className="text-aui-sm text-aui-muted">{tabLede[tab]}</p>
      {busy && <output>Connecting…</output>}
      <ul
        className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
        aria-label="Connect a service"
      >
        {presets
          .filter((preset) => tabFor(preset.id, preset.kind) === tab)
          .map((preset) => (
            <li key={preset.id} className={`${card} flex min-w-0 flex-col gap-3`}>
              <div className="flex items-center gap-3">
                <ConnectionLogo id={preset.id} name={preset.name} />
                <div className="min-w-0">
                  <h2 className="font-medium">{preset.name}</h2>
                  {(preset.reconnect_required || preset.connected) && (
                    <p className="text-aui-xs text-aui-muted">
                      {preset.reconnect_required
                        ? "Reconnect required"
                        : preset.kind === "github_install"
                          ? "Installed"
                          : "Connected"}
                    </p>
                  )}
                </div>
              </div>
              {preset.endpoint && (
                <p className="truncate text-aui-xs text-aui-muted" title={preset.endpoint}>
                  {preset.endpoint}
                </p>
              )}
              {preset.reason && <p className="text-aui-sm text-aui-muted">{preset.reason}</p>}
              {(preset.connected || preset.reconnect_required) && (
                <ConnectionHealth
                  key={`${preset.id}:${revision}`}
                  provider={preset.id}
                  initial={preset.health}
                  canCheck={canConnect && (preset.scope !== "workspace" || canWrite)}
                />
              )}
              {preset.kind === "oauth" ||
              preset.kind === "source_oauth" ||
              preset.kind === "catalog_oauth" ||
              preset.kind === "client_oauth" ||
              preset.kind === "github_install" ? (
                <div className="flex flex-wrap gap-2">
                  {canConnect && (preset.scope !== "workspace" || canWrite) && (
                    <button
                      type="button"
                      className={button}
                      disabled={busy || !preset.available}
                      onClick={() =>
                        connect(
                          preset,
                          preset.kind === "source_oauth" || preset.kind === "catalog_oauth"
                            ? "person"
                            : preset.kind === "client_oauth" || preset.kind === "github_install"
                              ? "workspace"
                              : undefined,
                        )
                      }
                    >
                      {preset.connected || preset.reconnect_required ? "Reconnect" : "Connect"}
                    </button>
                  )}
                  {(preset.connected || preset.reconnect_required) && canConnect && (
                    <>
                      {preset.kind === "oauth" ? (
                        <button
                          type="button"
                          className={button}
                          disabled={busy}
                          onClick={() => reviewTools(preset)}
                        >
                          Review Tools
                        </button>
                      ) : preset.kind === "catalog_oauth" ? (
                        <button
                          type="button"
                          className={button}
                          onClick={() => {
                            setSelected("bigquery");
                            onOpen?.("bigquery");
                          }}
                        >
                          Choose Datasets
                        </button>
                      ) : preset.kind === "source_oauth" ? (
                        <button
                          type="button"
                          className={button}
                          onClick={() => setSelected("drive")}
                        >
                          Choose a source
                        </button>
                      ) : (
                        <a className={button} href="#workspace-connections">
                          Configure installation
                        </a>
                      )}
                      {(preset.scope !== "workspace" || canWrite) && (
                        <button
                          type="button"
                          className={button}
                          disabled={busy}
                          onClick={() =>
                            void act(async () => {
                              await connectionApi.disconnect(preset.id);
                              reload();
                              setRevision((v) => v + 1);
                            })
                          }
                        >
                          {preset.kind === "github_install"
                            ? "Disable installations"
                            : "Disconnect"}
                        </button>
                      )}
                      {canWrite && preset.scope === "person" && preset.kind === "oauth" && (
                        <button
                          type="button"
                          className={button}
                          disabled={busy}
                          onClick={() => connect(preset, "workspace")}
                        >
                          Connect for Workspace
                        </button>
                      )}
                    </>
                  )}
                </div>
              ) : (
                <button
                  type="button"
                  className={`${button} self-start`}
                  onClick={() => {
                    setSelected(preset.id);
                    onOpen?.(preset.id);
                  }}
                >
                  Set Up {preset.name}
                </button>
              )}
              {preset.id !== "other" && (
                <button
                  type="button"
                  className={`${button} self-start`}
                  onClick={() => {
                    setSelected(preset.id);
                    onOpen?.(preset.id);
                  }}
                >
                  View Connection
                </button>
              )}
            </li>
          ))}
      </ul>
      {selected && selected !== "other" && presets.some((p) => p.id === selected) && (
        <ConnectionDetail key={`${selected}:${revision}`} provider={selected} />
      )}
      {review && (
        <section className={card} aria-label="Review connection tools">
          <ToolList server={review} />
          <div className="mt-3 flex gap-2">
            <button className={button} type="button" onClick={() => setReview(null)}>
              Cancel
            </button>
            <button className={primary} type="button" disabled={busy} onClick={approve}>
              Approve Tool List
            </button>
          </div>
        </section>
      )}
      {(selected === "slack" || selected === "github") &&
        (canWrite ? (
          <ClientConnect
            key={selected}
            initialSurface={selected}
            onCancel={() => setSelected(undefined)}
            onCreated={() => {
              setSelected(undefined);
              setRevision((v) => v + 1);
            }}
          />
        ) : (
          <p>A workspace administrator can connect messaging services.</p>
        ))}
      {selected === "drive" &&
        (canKnowledgeWrite ? (
          <ConnectSource
            initialProvider="drive"
            onCancel={() => setSelected(undefined)}
            onCreated={() => {
              setSelected(undefined);
              setRevision((v) => v + 1);
            }}
          />
        ) : (
          <p>A workspace administrator can connect knowledge sources.</p>
        ))}
      {selected === "bigquery" && (
        <BigQueryConnection
          canWrite={canKnowledgeWrite}
          onCancel={() => setSelected(undefined)}
          onVerified={() => setRevision((v) => v + 1)}
          onAuthorize={() => {
            const preset = presets.find((p) => p.id === "bigquery");
            if (preset) connect(preset, "person");
          }}
        />
      )}
      {selected === "other" && (
        <form
          aria-label="Connect tool server with OAuth"
          className={`${card} flex flex-col gap-3`}
          onSubmit={(e) => {
            e.preventDefault();
            void act(async () => {
              const auth = await connectionApi.authorize("other", {
                endpoint: endpoint.trim(),
                scope,
                ...(registered
                  ? {
                      registration: {
                        issuer: issuer.trim(),
                        client_id: clientID.trim(),
                        ...(clientSecret ? { client_secret: clientSecret } : {}),
                      },
                    }
                  : {}),
              });
              sessionStorage.setItem("humagen.connection.provider", auth.provider ?? "other");
              window.location.assign(auth.url);
            });
          }}
        >
          <label className="flex flex-col gap-1 text-aui-sm">
            Tool server URL
            <input
              className={field}
              type="url"
              required
              value={endpoint}
              onChange={(e) => setEndpoint(e.target.value)}
              placeholder="https://tools.example.com/mcp"
            />
          </label>
          <p className="text-aui-xs text-aui-muted">
            {scope === "workspace"
              ? "Your workspace will be able to use this account. Each person still approves the tools they use."
              : "Only agents acting for you can use this account."}{" "}
            The service must be in your allowed tool hosts.
          </p>
          <label className="flex items-center gap-2 text-aui-sm">
            <input
              type="checkbox"
              checked={registered}
              onChange={(e) => setRegistered(e.target.checked)}
            />
            Use a registered application
          </label>
          {registered && (
            <div className="flex flex-col gap-3">
              <p className="text-aui-xs text-aui-muted">
                Use the application registered with this service when it does not register clients
                automatically. The issuer must match the service's authorization server.
              </p>
              <Label text="Authorization issuer">
                <input
                  className={field}
                  type="url"
                  required
                  value={issuer}
                  onChange={(e) => setIssuer(e.target.value)}
                  autoComplete="off"
                />
              </Label>
              <Label text="Client ID">
                <input
                  className={field}
                  required
                  value={clientID}
                  onChange={(e) => setClientID(e.target.value)}
                  autoComplete="off"
                />
              </Label>
              <Label text="Client secret (if required)">
                <input
                  className={field}
                  type="password"
                  value={clientSecret}
                  onChange={(e) => setClientSecret(e.target.value)}
                  autoComplete="off"
                />
              </Label>
            </div>
          )}
          <Problem message={problem} />
          <button
            className={primary}
            type="submit"
            disabled={busy || !canConnect || !presets.find((p) => p.id === "other")?.available}
          >
            {busy ? "Connecting…" : "Connect with OAuth"}
          </button>
        </form>
      )}
      {tab === "tools" && (
        <YourConnections
          key={`personal-${revision}`}
          embedded
          canConnect={canConnect}
          open={selected}
          onOpen={setSelected}
        />
      )}
      {tab !== "tools" && (
        <div id="workspace-connections">
          <Integrations
            key={`${tab}-${revision}`}
            embedded
            only={tab === "workspace" ? "client" : "source"}
            canWrite={canWrite}
            canKnowledgeWrite={canKnowledgeWrite}
          />
        </div>
      )}
    </Page>
  );
}

type ConnectionTab = "tools" | "knowledge" | "workspace";

const tabLede: Record<ConnectionTab, string> = {
  tools:
    "Accounts and MCP tool servers agents act through for you. Each is yours alone; the workspace decides which services are allowed.",
  knowledge:
    "Documents and data indexed for search. Agents read and cite them; they never act through them.",
  workspace:
    "Apps installed once for the whole workspace, so agents can answer in Slack and on GitHub. Only administrators manage these.",
};

/** Which tab a service belongs on: agent tools, knowledge, or workspace apps. */
function tabFor(id?: string, kind?: string): ConnectionTab {
  if (kind === "client_oauth" || kind === "github_install" || id === "slack" || id === "github")
    return "workspace";
  if (kind === "source_oauth" || kind === "catalog_oauth" || id === "drive" || id === "bigquery")
    return "knowledge";
  return "tools";
}
