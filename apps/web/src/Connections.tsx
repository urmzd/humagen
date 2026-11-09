import { defaultCopy } from "@humagen/components";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@humagen/render-web/ui/sheet";
import { toast } from "@humagen/render-web/ui/sonner";
import { type FormEvent, type MouseEvent, useCallback, useEffect, useRef, useState } from "react";
import {
  ApiError,
  api,
  type Client,
  type PersonContext,
  type PersonToolServer,
  type ToolCredential,
  type ToolCredentials,
} from "./api.ts";
import { ClientConnect } from "./ClientConnect.tsx";
import { ConnectionDetail } from "./ConnectionDetail.tsx";
import { ConnectionHealth } from "./ConnectionHealth.tsx";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { ConnectionSection } from "./ConnectionSection.tsx";
import { ItemCard, Pill, type Tone } from "./cards.tsx";
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
import { GitHubRules } from "./GitHubRules.tsx";
import { InstalledClientSetup } from "./InstalledClientSetup.tsx";
import { href, type Route } from "./route.ts";
import { AddToolServer, ToolList, YourToolServers } from "./ToolServers.tsx";
import { credentialWords, hostOf, withoutToolServer } from "./toolServers.ts";
import { button, card, field, Label, Page, Problem, primary, Segmented } from "./ui.tsx";

/** A plain click goes there in place; a modified one is the browser's. */
const follow = (go: () => void) => (e: MouseEvent<HTMLAnchorElement>) => {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go();
};

/**
 * MCPs that take your own key: those the workspace offers, reached with
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
    api.toolCredentials().then(setState).catch(fail("MCPs could not be loaded."));
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
    <ConnectionSection embedded={embedded} title="MCPs" narrow>
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
    <section className={`${card} flex flex-col gap-2`} aria-label={c.provider}>
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

/** The groups a connection belongs to, in the order the page shows them. */
type Group = "apps" | "mcps";

const groups: { id: Group; title: string; lede?: string }[] = [
  { id: "apps", title: "Apps", lede: "Installed once for the workspace." },
  { id: "mcps", title: "MCPs" },
];

/** One item on the page: a logo, one status, one action, and the rest in a menu and the sheet. */
type Item = {
  id: string;
  group: Group;
  name: string;
  logo: string;
  /** One short fact under the name, such as the host a tool reaches. */
  note?: string;
  pill?: { text: string; tone: Tone };
  action?: { label: string; run: () => void; disabled?: boolean };
  menu: { label: string; run: () => void; destructive?: boolean }[];
};

/** Why an item is not available, as one pill. */
function unavailable(reason?: string): { text: string; tone: Tone } {
  return reason?.includes("MCP hosts")
    ? { text: "Host not allowed", tone: "neutral" }
    : { text: "Not enabled", tone: "neutral" };
}

/**
 * Connections: apps installed for the workspace and MCPs, each item once. A
 * card shows where it stands and the one thing to do next; its menu and side
 * sheet hold the rest. Data sources are added inside a corpus, not here.
 */
export function Connections({
  provider,
  canConnect = true,
  canWrite = false,
  onOpen,
  onNavigate,
}: {
  provider?: string;
  canConnect?: boolean;
  canWrite?: boolean;
  onOpen?: (id?: string) => void;
  /** Goes to another page of the console, for links in the sheet. */
  onNavigate?: (route: Route) => void;
}) {
  const [presets, setPresets] = useState<ConnectionPreset[]>([]);
  const [creds, setCreds] = useState<ToolCredentials | null>(null);
  const [context, setContext] = useState<PersonContext | null>(null);
  const [clients, setClients] = useState<Client[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | undefined>(
    provider === "callback" ? undefined : provider,
  );
  const [addWay, setAddWay] = useState<"address" | "oauth">("address");
  const [endpoint, setEndpoint] = useState("");
  const [registered, setRegistered] = useState(false);
  const [issuer, setIssuer] = useState("");
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const [review, setReview] = useState<{ id: string; server: PersonToolServer } | null>(null);
  const selectedRef = useRef(selected);
  const reviewRequest = useRef(0);
  const sheetOpener = useRef<HTMLElement | null>(null);
  const selectConnection = useCallback((id?: string) => {
    if (selectedRef.current === id) return;
    if (id) {
      // Focus goes back to what opened the sheet: the card's button, or its ⋯
      // button when a menu item did (the menu hands focus back to it first).
      const active = document.activeElement;
      sheetOpener.current =
        active instanceof HTMLElement &&
        active !== document.body &&
        !active.closest('[role="menu"]')
          ? active
          : null;
    }
    selectedRef.current = id;
    reviewRequest.current++;
    setReview(null);
    setProblem(null);
    setSelected(id);
  }, []);
  const reload = useCallback(() => {
    connectionApi
      .presets()
      .then(setPresets)
      .catch((e: unknown) =>
        setProblem(e instanceof Error ? e.message : "Connections could not be loaded."),
      );
    api
      .toolCredentials()
      .then(setCreds)
      .catch(() => setCreds(null));
    api
      .personContext()
      .then(setContext)
      .catch(() => setContext(null));
    // What the workspace installed; a person who may not read it sees the cards alone.
    api
      .clients()
      .then((r) => setClients(r.clients))
      .catch(() => setClients([]));
  }, []);
  useEffect(reload, [reload]);
  // An old address named a credential tool by its provider alone.
  const resolve = useCallback(
    (id?: string) =>
      id && !presets.some((p) => p.id === id) && creds?.providers.some((c) => c.provider === id)
        ? `cred:${id}`
        : id,
    [presets, creds],
  );
  // The address decides the sheet when it changes, not every time the data reloads.
  const routed = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (provider === "callback") return;
    const target = resolve(provider);
    if (routed.current === target) return;
    routed.current = target;
    selectConnection(target);
  }, [provider, resolve, selectConnection]);
  const callbackStarted = useRef(false);
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
  const close = () => {
    selectConnection(undefined);
    onOpen?.(undefined);
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
      // In an open sheet the error belongs beside its form; elsewhere it is a toast.
      if (target && selectedRef.current === target) setProblem(message);
      else toast.error(message);
    } finally {
      setBusy(false);
    }
  };
  const changed = () => {
    reload();
    setRevision((v) => v + 1);
  };
  const connect = (preset: ConnectionPreset, scope: "person" | "workspace") =>
    void act(async () => {
      const auth = await connectionApi.authorize(preset.id, {
        scope,
        ...(preset.endpoint ? { endpoint: preset.endpoint } : {}),
      });
      sessionStorage.setItem("humagen.connection.provider", auth.provider ?? preset.id);
      window.location.assign(auth.url);
    });
  const disconnect = (preset: ConnectionPreset) =>
    void act(async () => {
      await connectionApi.disconnect(preset.id);
      changed();
      toast.success(
        preset.kind === "github_install"
          ? `${preset.name} installations disabled`
          : `${preset.name} disconnected`,
      );
    });
  /** Asks a server for its tools, to approve before an agent may use them. */
  const startReview = (id: string, server: PersonToolServer) => {
    if (busy) return;
    open(id);
    setReview(null);
    const request = ++reviewRequest.current;
    void act(async () => {
      const listed = await api.reviewPersonTools({ ...server, tools: undefined });
      if (request === reviewRequest.current && selectedRef.current === id) {
        setReview({ id, server: listed });
      }
    });
  };
  const approve = () =>
    void act(async () => {
      if (!review || review.id !== selectedRef.current) return;
      const current = await api.personContext();
      const next = await api.setPersonContext({
        run: current.run,
        version: current.version,
        tool_servers: [
          ...current.tool_servers.filter((s) => s.name !== review.server.name),
          review.server,
        ],
      });
      setContext(next);
      setReview(null);
      setRevision((v) => v + 1);
      toast.success(`${review.server.name} tools approved`);
    });
  const removeServer = (name: string) =>
    void act(async () => {
      if (!context) return;
      setContext(await api.setPersonContext(withoutToolServer(context, name)));
      if (selectedRef.current === `server:${name}`) close();
    });

  const presetIds = new Set(presets.map((p) => p.id));
  const credentialWays = creds ? ways(creds) : [];
  const items: Item[] = [];
  for (const preset of presets) {
    const off = !preset.available;
    const live = preset.connected && !preset.reconnect_required;
    const enable = canWrite && off ? { label: "Enable", run: () => open(preset.id) } : undefined;
    if (preset.kind === "client_oauth" || preset.kind === "github_install") {
      const installed =
        preset.connected || clients.some((c) => c.surface === preset.id && c.enabled);
      items.push({
        id: preset.id,
        group: "apps",
        name: preset.name,
        logo: preset.id,
        pill: installed ? { text: "Installed", tone: "ok" } : off ? unavailable() : undefined,
        action: !canWrite
          ? undefined
          : installed
            ? { label: "Manage", run: () => open(preset.id) }
            : (enable ?? {
                label: "Install",
                disabled: busy,
                run: () => connect(preset, "workspace"),
              }),
        menu: [
          ...(installed && canWrite && preset.connected
            ? [
                {
                  label: preset.kind === "github_install" ? "Disable Installations" : "Disconnect",
                  run: () => disconnect(preset),
                  destructive: true,
                },
              ]
            : []),
        ],
      });
    } else if (preset.kind === "oauth") {
      items.push({
        id: preset.id,
        group: "mcps",
        name: preset.name,
        logo: preset.id,
        note: preset.endpoint ? hostOf(preset.endpoint) : undefined,
        pill: preset.reconnect_required
          ? { text: "Reconnect", tone: "warn" }
          : live
            ? { text: "Connected", tone: "ok" }
            : off
              ? unavailable(preset.reason)
              : undefined,
        action: !canConnect
          ? undefined
          : preset.reconnect_required
            ? { label: "Reconnect", disabled: busy, run: () => connect(preset, "person") }
            : live
              ? {
                  label: "Review Tools",
                  disabled: busy || !preset.endpoint,
                  run: () =>
                    startReview(preset.id, {
                      name: preset.id,
                      url: preset.endpoint ?? "",
                      oauth: preset.id,
                    } as PersonToolServer),
                }
              : (enable ??
                (off
                  ? undefined
                  : {
                      label: "Connect",
                      disabled: busy,
                      run: () => connect(preset, "person"),
                    })),
        menu: [
          ...(preset.connected && canConnect
            ? [{ label: "Disconnect", run: () => disconnect(preset), destructive: true }]
            : []),
        ],
      });
    }
    // Data sources (Drive, BigQuery and the rest) are added inside a corpus, not here.
  }
  // MCPs the workspace offers that take a key you give, rather than a sign-in.
  for (const c of creds?.providers ?? []) {
    if (presetIds.has(c.provider)) continue;
    const id = `cred:${c.provider}`;
    const none = credentialWays.length === 0;
    items.push({
      id,
      group: "mcps",
      name: c.provider,
      logo: c.provider,
      note: c.endpoint ? hostOf(c.endpoint) : undefined,
      pill: c.connected
        ? { text: "Connected", tone: "ok" }
        : none
          ? { text: "Not enabled", tone: "neutral" }
          : undefined,
      action:
        canConnect && !none
          ? { label: c.connected ? "Replace" : "Connect", run: () => open(id) }
          : canWrite && none
            ? { label: "Enable", run: () => open(id) }
            : undefined,
      menu: [
        ...(c.connected && canConnect
          ? [
              {
                label: "Remove",
                destructive: true,
                run: () =>
                  void act(async () => {
                    await api.disconnectTool(c.provider);
                    changed();
                  }),
              },
            ]
          : []),
      ],
    });
  }
  // MCPs you added by URL. One you approved through a sign-in card is that card.
  for (const server of context?.tool_servers ?? []) {
    if (presetIds.has(server.name)) continue;
    const id = `server:${server.name}`;
    items.push({
      id,
      group: "mcps",
      name: server.name,
      logo: server.name,
      note: hostOf(server.url),
      pill: { text: "Added", tone: "ok" },
      action: canConnect
        ? { label: "Review Tools", disabled: busy, run: () => startReview(id, server) }
        : undefined,
      menu: [
        ...(canConnect
          ? [{ label: "Remove", destructive: true, run: () => removeServer(server.name) }]
          : []),
      ],
    });
  }
  const other = presets.find((p) => p.id === "other");
  const mayAdd = canConnect && context !== null;

  const current = items.find((it) => it.id === selected);
  const currentPreset = presets.find((p) => p.id === selected);
  const adding = selected === "other";
  const sheetOpen = !!current || adding;
  const link = (route: Route, label: string) => (
    <a
      href={href(route)}
      onClick={onNavigate ? follow(() => onNavigate(route)) : undefined}
      className="rounded-aui text-aui-sm text-aui-fg underline focus-visible:outline-2 focus-visible:outline-aui-accent"
    >
      {label}
    </a>
  );
  /** What an administrator does to enable an item that is off. */
  const enableNote = (reason?: string) => {
    if (!reason) return null;
    // An administrator reads how to turn it on; anyone else, why it is off.
    if (canWrite && reason.includes("MCP hosts"))
      return (
        <p className="text-aui-sm text-aui-muted">
          Its host is not allowed yet.{" "}
          {link({ page: "settings", section: "limits" }, "Allow it in Limits")}
        </p>
      );
    if (canWrite && reason.includes("secure account connections"))
      return (
        <p className="text-aui-sm text-aui-muted">
          Sign-in is off. Turn it on in the server's spec, under service connection authorization.
        </p>
      );
    return <p className="text-aui-sm text-aui-muted">{reason}</p>;
  };

  return (
    <Page title="Connections">
      <Problem message={sheetOpen ? null : problem} />
      {busy && <output>Connecting…</output>}
      {groups.map((g) => {
        const shown = items.filter((it) => it.group === g.id);
        const add = g.id === "mcps" && mayAdd;
        // A group with nothing in it and nothing to do is left out.
        if (shown.length === 0 && !add) return null;
        return (
          <section
            key={g.id}
            aria-labelledby={`connections-${g.id}`}
            className="flex min-w-0 flex-col gap-3"
          >
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1">
              <div className="min-w-0">
                <h2 id={`connections-${g.id}`} className="text-aui-base font-medium text-aui-fg">
                  {g.title}
                </h2>
                {g.lede && <p className="text-aui-sm text-aui-muted">{g.lede}</p>}
              </div>
              {add && (
                <button type="button" className={button} onClick={() => open("other")}>
                  Add MCP
                </button>
              )}
            </div>
            {shown.length > 0 && (
              <ul
                aria-label={g.title}
                className="grid min-w-0 grid-cols-[repeat(auto-fill,minmax(min(100%,16rem),1fr))] gap-3"
              >
                {shown.map((it) => (
                  <li key={it.id} aria-label={it.name} className="min-w-0">
                    <ItemCard
                      mark={<ConnectionLogo id={it.logo} name={it.name} />}
                      title={it.name}
                      status={it.pill && <Pill tone={it.pill.tone}>{it.pill.text}</Pill>}
                      description={it.note}
                      menu={it.menu.map((m) => ({
                        label: m.label,
                        onSelect: m.run,
                        destructive: m.destructive,
                        disabled: busy && m.destructive,
                      }))}
                      menuLabel={`More for ${it.name}`}
                      // The whole card opens its details, so the menu holds only what else it can do.
                      onOpen={() => open(it.id)}
                      actions={
                        it.action && (
                          <button
                            type="button"
                            className={button}
                            disabled={it.action.disabled}
                            onClick={it.action.run}
                          >
                            {it.action.label}
                          </button>
                        )
                      }
                    />
                  </li>
                ))}
              </ul>
            )}
          </section>
        );
      })}
      <Sheet
        open={sheetOpen}
        onOpenChange={(next) => {
          if (!next) close();
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
            <SheetTitle>{adding ? "Add MCP" : (current?.name ?? "Connection")}</SheetTitle>
            <SheetDescription>
              {adding
                ? other?.available
                  ? "By URL, or by signing in to it."
                  : "By URL."
                : current?.group === "apps"
                  ? "Installed once for the workspace."
                  : (current?.note ?? "MCP")}
            </SheetDescription>
          </SheetHeader>
          <div className="flex min-w-0 flex-col gap-4">
            <Problem message={problem} />
            {/* Add MCP offers sign-in only when it is on, and says nothing about it otherwise. */}
            {currentPreset &&
              !adding &&
              !currentPreset.available &&
              enableNote(currentPreset.reason)}
            {currentPreset && currentPreset.id !== "other" && currentPreset.connected && (
              <ConnectionDetail
                key={`${currentPreset.id}:${revision}`}
                provider={currentPreset.id}
              />
            )}
            {currentPreset && (currentPreset.connected || currentPreset.reconnect_required) && (
              <ConnectionHealth
                key={`${currentPreset.id}:health:${revision}`}
                provider={currentPreset.id}
                initial={currentPreset.health}
                canCheck={canConnect && (currentPreset.scope !== "workspace" || canWrite)}
              />
            )}
            {review?.id === selected && review && (
              <section className={card} aria-label="Review connection tools">
                <ToolList server={review.server} />
                <div className="mt-3 flex flex-wrap gap-2">
                  <button className={button} type="button" onClick={() => setReview(null)}>
                    Cancel
                  </button>
                  <button className={primary} type="button" disabled={busy} onClick={approve}>
                    Approve Tool List
                  </button>
                </div>
              </section>
            )}
            {current?.group === "apps" &&
              (canWrite ? (
                <>
                  {clients
                    .filter((c) => c.surface === selected)
                    .map((c) => (
                      <section
                        key={c.id}
                        className={`${card} flex flex-col gap-2`}
                        aria-label={c.name}
                      >
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <h3 className="font-medium">{c.name}</h3>
                          <Pill tone={c.enabled ? "ok" : "neutral"}>
                            {c.enabled ? "Enabled" : "Disabled"}
                          </Pill>
                        </div>
                        {c.managed === "installation" && (
                          <InstalledClientSetup
                            key={`${c.id}:${c.version}`}
                            client={c}
                            onChanged={changed}
                          />
                        )}
                        {c.surface === "github" && (
                          <GitHubRules
                            key={`${c.id}:rules:${c.version}`}
                            client={c}
                            canWrite={c.managed !== "file"}
                            onChanged={changed}
                          />
                        )}
                        {link(
                          {
                            page: "operations",
                            section: "integrations",
                            selected: `client:${c.id}`,
                          },
                          "Events, links and actions",
                        )}
                      </section>
                    ))}
                  {(selected === "slack" || selected === "github") && (
                    <details className="text-aui-sm">
                      <summary className="cursor-pointer text-aui-muted">Set up by hand</summary>
                      <div className="pt-3">
                        <ClientConnect
                          key={selected}
                          initialSurface={selected}
                          onCancel={close}
                          onCreated={() => {
                            close();
                            changed();
                          }}
                        />
                      </div>
                    </details>
                  )}
                </>
              ) : (
                <p className="text-aui-sm">A workspace administrator installs workspace apps.</p>
              ))}
            {selected?.startsWith("cred:") && !canConnect && (
              <p className="text-aui-sm text-aui-muted">{cannotConnect}</p>
            )}
            {selected?.startsWith("cred:") &&
              creds &&
              (() => {
                const c = creds.providers.find((p) => `cred:${p.provider}` === selected);
                return c ? (
                  <ConnectionCard
                    connection={c}
                    ways={credentialWays}
                    references={creds.references}
                    canConnect={canConnect}
                    open={canConnect && credentialWays.length > 0}
                    busy={busy}
                    onOpen={() => {}}
                    onCancel={close}
                    onSave={(way, text) =>
                      void act(async () => {
                        await api.connectTool(
                          c.provider,
                          way === "value" ? { value: text } : { ref: text.trim() },
                        );
                        changed();
                        close();
                      })
                    }
                    onRemove={() =>
                      void act(async () => {
                        await api.disconnectTool(c.provider);
                        changed();
                      })
                    }
                  />
                ) : null;
              })()}
            {selected?.startsWith("server:") &&
              (() => {
                const s = context?.tool_servers.find((t) => `server:${t.name}` === selected);
                return s ? (
                  <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-aui-sm">
                    <dt className="text-aui-muted">Address</dt>
                    <dd className="break-all">{s.url}</dd>
                    <dt className="text-aui-muted">Credential</dt>
                    <dd className="break-words">{credentialWords(s)}</dd>
                    <dt className="text-aui-muted">Approved tools</dt>
                    <dd>{s.tools?.length ?? 0}</dd>
                  </dl>
                ) : null;
              })()}
            {selected === "other" && (
              <>
                {other?.available && (
                  <Segmented<"address" | "oauth">
                    label="How to add it"
                    value={addWay}
                    options={[
                      { id: "address", label: "URL" },
                      { id: "oauth", label: "Sign In" },
                    ]}
                    onChange={setAddWay}
                  />
                )}
                {(addWay === "address" || !other?.available) && context && (
                  <AddToolServer
                    context={context}
                    references={creds?.references ?? []}
                    allowedHosts={creds?.allowed_hosts}
                    onCancel={close}
                    onAdded={(next) => {
                      setContext(next);
                      close();
                      toast.success("MCP added");
                    }}
                  />
                )}
                {addWay === "oauth" && other?.available && (
                  <form
                    aria-label="Connect MCP with OAuth"
                    className="flex flex-col gap-3"
                    onSubmit={(e) => {
                      e.preventDefault();
                      void act(async () => {
                        const auth = await connectionApi.authorize("other", {
                          endpoint: endpoint.trim(),
                          scope: "person",
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
                        sessionStorage.setItem(
                          "humagen.connection.provider",
                          auth.provider ?? "other",
                        );
                        window.location.assign(auth.url);
                      });
                    }}
                  >
                    <Label text="URL">
                      <input
                        className={field}
                        type="url"
                        required
                        value={endpoint}
                        onChange={(e) => setEndpoint(e.target.value)}
                        placeholder="https://tools.example.com/mcp"
                      />
                    </Label>
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
                          For a service that does not register clients itself. The issuer must match
                          its authorization server.
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
                    <button
                      className={`${primary} self-start`}
                      type="submit"
                      disabled={busy || !canConnect}
                    >
                      {busy ? "Connecting…" : "Connect with OAuth"}
                    </button>
                  </form>
                )}
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </Page>
  );
}
