import { defaultCopy } from "@humagen/components";
import { EllipsisIcon } from "@humagen/render-web";
import { Alert, AlertDescription, AlertTitle } from "@humagen/render-web/ui/alert";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@humagen/render-web/ui/dropdown-menu";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@humagen/render-web/ui/sheet";
import { toast } from "@humagen/render-web/ui/sonner";
import { type FormEvent, type MouseEvent, useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api, type ToolCredential, type ToolCredentials } from "./api.ts";
import { BigQueryConnection } from "./BigQueryConnection.tsx";
import { ClientConnect } from "./ClientConnect.tsx";
import { ConnectionDetail } from "./ConnectionDetail.tsx";
import { ConnectionHealth } from "./ConnectionHealth.tsx";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { ConnectionSection } from "./ConnectionSection.tsx";
import { Pill } from "./cards.tsx";
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
  // Configured credentials have their own inline selection, separate from OAuth setup.
  const [credentialProvider, setCredentialProvider] = useState<string | undefined>(
    provider === "callback" ? undefined : provider,
  );
  const [selected, setSelected] = useState<string | undefined>(
    provider === "callback" ? undefined : provider,
  );
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
  const [review, setReview] = useState<{
    provider: string;
    server: Awaited<ReturnType<typeof api.reviewPersonTools>>;
  } | null>(null);
  const selectedRef = useRef(selected);
  const reviewRequest = useRef(0);
  const openers = useRef(new Map<string, HTMLButtonElement>());
  const sheetOpener = useRef<HTMLElement | null>(null);
  const selectConnection = useCallback((id?: string) => {
    if (selectedRef.current === id) return;
    if (id) {
      sheetOpener.current =
        openers.current.get(id) ??
        (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    }
    selectedRef.current = id;
    reviewRequest.current++;
    setReview(null);
    setProblem(null);
    setSelected(id);
  }, []);
  useEffect(() => {
    setCredentialProvider(provider === "callback" ? undefined : provider);
    if (provider !== "callback") selectConnection(provider);
  }, [provider, selectConnection]);
  const sheetOpen = !!selected && presets.some((p) => p.id === selected);
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
        selectConnection(provider);
        toast.success("Connected");
      })
      .catch((e: unknown) =>
        setProblem(e instanceof Error ? e.message : "Connection could not be completed."),
      )
      .finally(() => setBusy(false));
  }, [reload, selectConnection]);
  const open = (id: string) => {
    selectConnection(id);
    onOpen?.(id);
  };
  const act = async (fn: () => Promise<unknown>) => {
    if (busy) return;
    const target = selectedRef.current;
    setBusy(true);
    setProblem(null);
    try {
      await fn();
    } catch (e) {
      const message = e instanceof Error ? e.message : "The connection could not be changed.";
      // In an open setup form the error belongs beside the form; elsewhere it is a toast.
      if (target && selectedRef.current === target) setProblem(message);
      else toast.error(message);
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
  const reviewTools = (preset: ConnectionPreset) => {
    const endpoint = preset.endpoint;
    if (busy || !endpoint) return;
    open(preset.id);
    setReview(null);
    const request = ++reviewRequest.current;
    void act(async () => {
      const server = await api.reviewPersonTools({
        name: preset.id,
        url: endpoint,
        oauth: preset.id,
      });
      if (request === reviewRequest.current && selectedRef.current === preset.id) {
        setReview({ provider: preset.id, server });
      }
    });
  };
  const approve = () =>
    void act(async () => {
      if (!review || review.provider !== selectedRef.current) return;
      const current = await api.personContext();
      await api.setPersonContext({
        run: current.run,
        version: current.version,
        tool_servers: [
          ...current.tool_servers.filter((s) => s.name !== review.server.name),
          review.server,
        ],
      });
      setReview(null);
      setRevision((v) => v + 1);
    });
  return (
    <Page
      title="Connections"
      lede="Connect your own accounts. Agents act as you with them, nobody else uses your credentials, and you can revoke one at any time."
    >
      <Problem message={sheetOpen ? null : problem} />
      <Tabs
        label="Connection kinds"
        value={tab}
        onChange={(next) => {
          setTab(next);
          selectConnection(undefined);
        }}
        tabs={[
          { id: "tools", label: "Agent tools" },
          { id: "knowledge", label: "Data sources" },
          ...(canWrite ? [{ id: "workspace" as const, label: "Workspace apps" }] : []),
        ]}
      />
      <p className="text-aui-sm text-aui-muted">{tabLede[tab]}</p>
      {busy && <output>Connecting…</output>}
      {presets.some((p) => tabFor(p.id, p.kind) === tab && adminOff(p)) && (
        <Alert>
          <AlertTitle>Account connections are not enabled yet</AlertTitle>
          <AlertDescription>
            A workspace administrator turns on secure account connections, then each person connects
            their own account here.
          </AlertDescription>
        </Alert>
      )}
      <ul
        className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
        aria-label="Connect a service"
      >
        {presets
          .filter((preset) => tabFor(preset.id, preset.kind) === tab)
          .map((preset) => (
            <li key={preset.id} className={`${card} flex h-full min-w-0 flex-col gap-3`}>
              <div className="flex min-w-0 items-center gap-3">
                <ConnectionLogo id={preset.id} name={preset.name} />
                <h2 className="min-w-0 flex-1 truncate font-medium">{preset.name}</h2>
                {preset.reconnect_required ? (
                  <Pill tone="warn">Reconnect</Pill>
                ) : preset.connected ? (
                  <Pill tone="ok">
                    {preset.kind === "github_install" ? "Installed" : "Connected"}
                  </Pill>
                ) : adminOff(preset) ? (
                  <Pill>Not enabled</Pill>
                ) : null}
                {preset.id !== "other" && (
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      ref={(node) => {
                        if (node) openers.current.set(preset.id, node);
                        else openers.current.delete(preset.id);
                      }}
                      aria-label={`More for ${preset.name}`}
                      className="-mr-1 inline-flex size-8 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11"
                    >
                      <EllipsisIcon size={16} />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent
                      align="end"
                      onCloseAutoFocus={(event) => {
                        if (selectedRef.current === preset.id) event.preventDefault();
                      }}
                    >
                      <DropdownMenuItem onSelect={() => open(preset.id)}>Details</DropdownMenuItem>
                      {(preset.connected || preset.reconnect_required) && canConnect && (
                        <>
                          {preset.kind === "oauth" && (
                            <DropdownMenuItem disabled={busy} onSelect={() => reviewTools(preset)}>
                              Review tools
                            </DropdownMenuItem>
                          )}
                          {preset.kind === "catalog_oauth" && (
                            <DropdownMenuItem onSelect={() => open(preset.id)}>
                              Choose datasets
                            </DropdownMenuItem>
                          )}
                          {preset.kind === "source_oauth" && (
                            <DropdownMenuItem onSelect={() => open(preset.id)}>
                              Choose a source
                            </DropdownMenuItem>
                          )}
                          {(preset.scope !== "workspace" || canWrite) && (
                            <>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem
                                variant="destructive"
                                disabled={busy}
                                onSelect={() =>
                                  void act(async () => {
                                    await connectionApi.disconnect(preset.id);
                                    reload();
                                    setRevision((v) => v + 1);
                                    toast.success(
                                      preset.kind === "github_install"
                                        ? `${preset.name} installations disabled`
                                        : `${preset.name} disconnected`,
                                    );
                                  })
                                }
                              >
                                {preset.kind === "github_install"
                                  ? "Disable installations"
                                  : "Disconnect"}
                              </DropdownMenuItem>
                            </>
                          )}
                        </>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                )}
              </div>
              {preset.endpoint && (
                <p className="truncate text-aui-xs text-aui-muted" title={preset.endpoint}>
                  {preset.endpoint}
                </p>
              )}
              {preset.reason && !adminOff(preset) && (
                <p className="text-aui-sm text-aui-muted">{preset.reason}</p>
              )}
              {(preset.connected || preset.reconnect_required) && (
                <ConnectionHealth
                  key={`${preset.id}:${revision}`}
                  provider={preset.id}
                  initial={preset.health}
                  canCheck={canConnect && (preset.scope !== "workspace" || canWrite)}
                />
              )}
              <div className="mt-auto pt-1">
                {preset.kind === "oauth" ||
                preset.kind === "source_oauth" ||
                preset.kind === "catalog_oauth" ||
                preset.kind === "client_oauth" ||
                preset.kind === "github_install"
                  ? canConnect &&
                    (preset.scope !== "workspace" || canWrite) && (
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
                    )
                  : canConnect && (
                      <button type="button" className={button} onClick={() => open(preset.id)}>
                        Connect with OAuth
                      </button>
                    )}
              </div>
            </li>
          ))}
      </ul>
      <Sheet
        open={sheetOpen}
        onOpenChange={(next) => {
          if (!next) selectConnection(undefined);
        }}
      >
        <SheetContent
          onCloseAutoFocus={(event) => {
            if (sheetOpener.current?.isConnected) {
              event.preventDefault();
              sheetOpener.current.focus();
            }
          }}
        >
          <SheetHeader>
            <SheetTitle>{presets.find((p) => p.id === selected)?.name ?? "Connection"}</SheetTitle>
            <SheetDescription>Status, access and setup for this connection.</SheetDescription>
          </SheetHeader>
          {selected !== "other" && <Problem message={problem} />}
          {selected && selected !== "other" && presets.some((p) => p.id === selected) && (
            <ConnectionDetail key={`${selected}:${revision}`} provider={selected} />
          )}
          {review?.provider === selected && review && (
            <section className={card} aria-label="Review connection tools">
              <ToolList server={review.server} />
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
                onCancel={() => selectConnection(undefined)}
                onCreated={() => {
                  selectConnection(undefined);
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
                onCancel={() => selectConnection(undefined)}
                onCreated={() => {
                  selectConnection(undefined);
                  setRevision((v) => v + 1);
                }}
              />
            ) : (
              <p>A workspace administrator can connect knowledge sources.</p>
            ))}
          {selected === "bigquery" && (
            <BigQueryConnection
              canWrite={canKnowledgeWrite}
              onCancel={() => selectConnection(undefined)}
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
                    Use the application registered with this service when it does not register
                    clients automatically. The issuer must match the service's authorization server.
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
        </SheetContent>
      </Sheet>
      {tab === "tools" && (
        <YourConnections
          key={`personal-${revision}`}
          embedded
          canConnect={canConnect}
          open={credentialProvider}
          onOpen={setCredentialProvider}
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

/** The workspace has not turned on account connections: said once above the cards, not on each. */
function adminOff(preset: ConnectionPreset): boolean {
  return !!preset.reason?.startsWith("An administrator must enable secure account connections");
}
