import { defaultCopy } from "@humagen/components";
import { BotIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@humagen/render-web/ui/item";
import type { ReactNode } from "react";
import { useState } from "react";
import type { StoreAgent, StorePackage } from "./api.ts";
import { agentShown } from "./branding.ts";
import { follow, Pill, Reference, Tag } from "./cards.tsx";
import { href } from "./route.ts";
import { SplitButton } from "./SplitButton.tsx";
import { AgentMark, button, Confirm, primary } from "./ui.tsx";

/**
 * The store as a catalog and a package's own page, drawn from props alone.
 * The catalog answers "what is this and do I have it": a title, what it does,
 * how many agents, what it needs, and Install. What each agent is granted is
 * on the package's page, said once where the agents share it.
 */
const words = defaultCopy.agentStore;

export interface StoreActions {
  /** agents:write: Install and Remove are offered. */
  canWrite: boolean;
  /** The package being installed or removed, by name. */
  busy?: string;
  onInstall: (pkg: StorePackage) => void;
  /** `force` retires agents changed in the workspace too; the confirm named them. */
  onRemove: (pkg: StorePackage, force: boolean) => void;
}

const customizedIn = (pkg: StorePackage) =>
  pkg.agents.filter((a) => a.installed && a.customized).map((a) => a.name);
const takenIn = (pkg: StorePackage) => pkg.agents.filter((a) => a.taken).map((a) => a.name);
const partial = (pkg: StorePackage) => pkg.installed && pkg.missing.length > 0;
const unique = (all: string[]) => [...new Set(all)].sort();

/** One state for a package, or none: a package not installed has no pill. */
function PackageStatus({ pkg }: { pkg: StorePackage }) {
  if (partial(pkg)) return <Pill tone="warn">{words.incomplete}</Pill>;
  if (pkg.installed) return <Pill tone="ok">{words.installed}</Pill>;
  if (takenIn(pkg).length > 0) return <Pill tone="warn">{words.nameTaken}</Pill>;
  return null;
}

/** What a package's agents need connected before their tools work. */
function Needs({ pkg }: { pkg: StorePackage }) {
  const connectors = unique(pkg.agents.flatMap((a) => a.connectors));
  const servers = unique(pkg.agents.flatMap((a) => a.tool_servers));
  return (
    <>
      {connectors.map((surface) => (
        <Reference
          key={surface}
          kind={surface === "github" || surface === "slack" ? surface : undefined}
          label={words.connector(surface)}
        />
      ))}
      {servers.map((name) => (
        <Tag key={name}>{words.toolServer(name)}</Tag>
      ))}
    </>
  );
}

/** Install, Restore Missing and Remove for one package, with Remove's confirm. */
function PackageActions({
  pkg,
  actions,
  quiet,
}: {
  pkg: StorePackage;
  actions: StoreActions;
  /** On a card, Install is outlined: the page's one filled button is on the package's page. */
  quiet?: boolean;
}) {
  const [removing, setRemoving] = useState(false);
  if (!actions.canWrite) return null;
  const working = actions.busy === pkg.name;
  const customized = customizedIn(pkg);
  if (removing)
    return (
      <Confirm
        question={words.removeQuestion(pkg.title, customized)}
        action={words.remove}
        onConfirm={() => {
          setRemoving(false);
          actions.onRemove(pkg, customized.length > 0);
        }}
        onKeep={() => setRemoving(false)}
      />
    );
  const restore = partial(pkg);
  return (
    <>
      {(!pkg.installed || restore) && (
        <button
          type="button"
          className={quiet || pkg.installed ? button : primary}
          disabled={working || takenIn(pkg).length > 0}
          aria-label={`${restore ? words.restore : words.install} ${pkg.title}`}
          onClick={() => actions.onInstall(pkg)}
        >
          {working ? words.installing : restore ? words.restore : words.install}
        </button>
      )}
      {pkg.installed && !quiet && (
        <button
          type="button"
          className={button}
          disabled={working}
          aria-label={`${words.remove} ${pkg.title}`}
          onClick={() => setRemoving(true)}
        >
          {working ? words.removing : words.remove}
        </button>
      )}
    </>
  );
}

/**
 * The catalog: one row per package, as a list to compare and act on. A row
 * says what the package is, the agents it adds and what they need, and its
 * state; its one control installs it, or, once installed, opens it to
 * configure, with the rest under the control's arrow.
 */
export function StoreCatalog({
  packages,
  actions,
  onOpen,
}: {
  packages: StorePackage[];
  actions: StoreActions;
  onOpen: (pkg: StorePackage) => void;
}) {
  if (packages.length === 0) return <p className="text-aui-sm text-aui-muted">{words.empty}</p>;
  return (
    <ItemGroup aria-label={words.title} className="gap-2">
      {packages.map((pkg) => (
        <PackageRow key={pkg.name} pkg={pkg} actions={actions} onOpen={() => onOpen(pkg)} />
      ))}
    </ItemGroup>
  );
}

function PackageRow({
  pkg,
  actions,
  onOpen,
}: {
  pkg: StorePackage;
  actions: StoreActions;
  onOpen: () => void;
}) {
  const [removing, setRemoving] = useState(false);
  const working = actions.busy === pkg.name;
  const customized = customizedIn(pkg);
  const restore = partial(pkg);
  const blocked = takenIn(pkg).length > 0;
  const needs = unique(pkg.agents.flatMap((a) => a.connectors));
  const control = !actions.canWrite ? (
    <Button size="sm" variant="outline" onClick={onOpen}>
      {words.view}
    </Button>
  ) : removing ? (
    <Confirm
      question={words.removeQuestion(pkg.title, customized)}
      action={words.remove}
      onConfirm={() => {
        setRemoving(false);
        actions.onRemove(pkg, customized.length > 0);
      }}
      onKeep={() => setRemoving(false)}
    />
  ) : restore ? (
    // Some of its agents were retired: putting them back is what the row is for now.
    <SplitButton
      primary
      label={words.restore}
      name={`${words.restore} ${pkg.title}`}
      onClick={() => actions.onInstall(pkg)}
      busy={working ? words.installing : undefined}
      menuLabel={words.moreFor(pkg.title)}
      choices={[
        { label: words.configure, onSelect: onOpen },
        { label: words.remove, destructive: true, onSelect: () => setRemoving(true) },
      ]}
    />
  ) : pkg.installed ? (
    <SplitButton
      label={words.configure}
      name={`${words.configure} ${pkg.title}`}
      onClick={onOpen}
      menuLabel={words.moreFor(pkg.title)}
      busy={working ? words.removing : undefined}
      choices={[
        ...needs.map((surface) => ({
          label: words.connect(words.surface(surface)),
          // In-app, as a link would: no full page load.
          onSelect: () => {
            window.history.pushState(null, "", href({ page: "connections" }));
            window.dispatchEvent(new PopStateEvent("popstate"));
          },
        })),
        { label: words.remove, destructive: true, onSelect: () => setRemoving(true) },
      ]}
    />
  ) : (
    <SplitButton
      primary
      label={words.install}
      name={`${words.install} ${pkg.title}`}
      onClick={() => actions.onInstall(pkg)}
      disabled={blocked}
      busy={working ? words.installing : undefined}
      menuLabel={words.moreFor(pkg.title)}
      choices={[{ label: words.view, onSelect: onOpen }]}
    />
  );
  return (
    <div role="listitem" aria-label={pkg.title}>
      <Item variant="outline" size="sm">
        <ItemMedia variant="icon">
          <BotIcon size={16} aria-hidden />
        </ItemMedia>
        <ItemContent className="min-w-0">
          <ItemTitle>
            <a
              href={href({ page: "agent-store", package: pkg.name })}
              onClick={(event) => follow(event, onOpen)}
              className="rounded-aui hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              {pkg.title}
            </a>
            <PackageStatus pkg={pkg} />
          </ItemTitle>
          <ItemDescription>{pkg.description}</ItemDescription>
          <p className="flex min-w-0 flex-wrap items-center gap-1.5 pt-0.5 text-aui-muted text-aui-xs">
            {/* Which agents it adds, by name: what a person gets is said in the row. */}
            <span className="min-w-0 truncate">
              {pkg.agents.map((a) => agentShown(a.name)).join(", ")}
            </span>
            <Needs pkg={pkg} />
          </p>
        </ItemContent>
        <ItemActions>{control}</ItemActions>
      </Item>
    </div>
  );
}

const chainOf = (a: StoreAgent) =>
  words.modelChain([
    words.modelName(a.provider, a.model),
    ...a.fallbacks.map((f) => words.modelName(f.provider, f.model)),
  ]);

/** The value every agent shares, or undefined when they differ. */
function shared(agents: StoreAgent[], of: (a: StoreAgent) => string): string | undefined {
  const first = agents[0] ? of(agents[0]) : undefined;
  return agents.length > 1 && agents.every((a) => of(a) === first) ? first : undefined;
}

const toolLine = (a: StoreAgent) => a.tools.join(", ");

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-aui-xs text-aui-muted">{label}</dt>
      <dd className="m-0 flex min-w-0 flex-wrap items-center gap-1.5 text-aui-fg text-aui-xs">
        {children}
      </dd>
    </>
  );
}

/**
 * One agent on a package's page: its mark, name and what it does, then only
 * what the package's summary did not already say for every agent. The agents
 * it hands work to are listed under it, indented.
 */
function AgentRow({
  agent,
  pkg,
  sharedChain,
  sharedTools,
  seen,
}: {
  agent: StoreAgent;
  pkg: StorePackage;
  sharedChain?: string;
  sharedTools?: string;
  /** The agents above this one, so a cycle of calls is drawn once. */
  seen: string[];
}) {
  const called = (agent.access.spawn ?? [])
    .filter((name) => !seen.includes(name))
    .map((name) => pkg.agents.find((a) => a.name === name))
    .filter((a): a is StoreAgent => !!a);
  const name = <span className="font-medium text-aui-fg text-aui-sm">@{agent.name}</span>;
  return (
    <li data-agent={agent.name} className="flex min-w-0 flex-col gap-3">
      <div className="flex min-w-0 items-start gap-3">
        <span className="flex h-5 shrink-0 items-center">
          {/* An agent has its color once the workspace has it; until then, a plain bot. */}
          {agent.installed ? (
            <AgentMark name={agent.name} size={18} />
          ) : (
            <BotIcon size={16} aria-hidden className="text-aui-muted" />
          )}
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            {agent.installed ? (
              <a
                href={href({ page: "agent", name: agent.name, tab: "prompt" })}
                className="rounded-aui hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                {name}
              </a>
            ) : (
              name
            )}
            {agent.installed && agent.customized && <Pill>{words.changed}</Pill>}
            {agent.taken && <Pill tone="warn">{words.nameTaken}</Pill>}
          </div>
          <p className="max-w-[70ch] text-aui-muted text-aui-xs">{agent.description}</p>
          {!sharedChain && <p className="text-aui-muted text-aui-xs">{chainOf(agent)}</p>}
          {!sharedTools && agent.tools.length > 0 && (
            <p className="font-mono text-aui-2xs text-aui-muted">{toolLine(agent)}</p>
          )}
        </div>
      </div>
      {called.length > 0 && (
        <ul
          aria-label={words.callsNamed(called.map((a) => a.name))}
          className="ml-[9px] flex min-w-0 flex-col gap-3 border-aui-border border-l pl-5"
        >
          {called.map((a) => (
            <AgentRow
              key={a.name}
              agent={a}
              pkg={pkg}
              sharedChain={sharedChain}
              sharedTools={sharedTools}
              seen={[...seen, agent.name]}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

/**
 * A package's page: what it does, Install, what it needs and runs on (once,
 * where every agent shares it), then its agents in the order work flows.
 */
export function StorePackageDetail({
  pkg,
  actions,
  onBack,
  note,
}: {
  pkg: StorePackage;
  actions: StoreActions;
  onBack: () => void;
  /** What the last install or removal did, such as "Added code-reviewer." */
  note?: string;
}) {
  const sharedChain = shared(pkg.agents, chainOf);
  const sharedTools = shared(pkg.agents, toolLine);
  const calledByAnother = new Set(pkg.agents.flatMap((a) => a.access.spawn ?? []));
  const roots = pkg.agents.filter((a) => !calledByAnother.has(a.name));
  const taken = takenIn(pkg);
  const customized = customizedIn(pkg);
  const needs = pkg.agents.some((a) => a.connectors.length + a.tool_servers.length > 0);
  return (
    <article className="flex min-w-0 max-w-[52rem] flex-col gap-6">
      <nav aria-label={words.title} className="text-aui-muted text-aui-xs">
        <a
          href={href({ page: "agent-store" })}
          onClick={(event) => follow(event, onBack)}
          className="rounded-aui hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {words.title}
        </a>
        <span aria-hidden> / </span>
        <span aria-current="page">{pkg.title}</span>
      </nav>
      <header className="flex min-w-0 flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 max-w-[62ch] flex-1 basis-80 flex-col gap-2">
          <div className="flex items-center gap-2">
            <h2 className="humagen-display font-semibold text-aui-fg text-aui-lg">{pkg.title}</h2>
            <PackageStatus pkg={pkg} />
          </div>
          <p className="text-aui-muted text-aui-sm">{pkg.description}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <PackageActions pkg={pkg} actions={actions} />
        </div>
      </header>
      {(partial(pkg) || taken.length > 0 || customized.length > 0 || note) && (
        <div className="flex flex-col gap-1 text-aui-muted text-aui-xs">
          {partial(pkg) && <p>{words.missing(pkg.missing)}</p>}
          {taken.length > 0 && <p>{words.taken(taken)}</p>}
          {customized.length > 0 && <p>{words.customized(customized)}</p>}
          {note && <output className="block">{note}</output>}
        </div>
      )}
      <dl className="m-0 grid min-w-0 grid-cols-[minmax(5rem,auto)_minmax(0,1fr)] items-baseline gap-x-6 gap-y-2 border-aui-border border-y py-4">
        {needs && (
          <Fact label={words.needs}>
            <Needs pkg={pkg} />
          </Fact>
        )}
        {sharedChain && <Fact label={words.runsOn}>{sharedChain}</Fact>}
        {sharedTools && (
          <Fact label={words.everyAgentTools}>
            <span className="font-mono text-aui-2xs text-aui-muted">{sharedTools}</span>
          </Fact>
        )}
        <Fact label={words.version("").trim()}>
          <span className="font-mono text-aui-2xs text-aui-muted">
            {pkg.version.replace(/^sha256:/, "").slice(0, 7)}
          </span>
        </Fact>
      </dl>
      <section className="flex min-w-0 flex-col gap-4">
        <h3 className="font-medium text-aui-fg text-aui-sm">
          {words.agents}{" "}
          <span className="font-normal text-aui-muted tabular-nums">{pkg.agents.length}</span>
        </h3>
        <ul aria-label={words.uses} className="flex min-w-0 flex-col gap-4">
          {roots.map((a) => (
            <AgentRow
              key={a.name}
              agent={a}
              pkg={pkg}
              sharedChain={sharedChain}
              sharedTools={sharedTools}
              seen={[]}
            />
          ))}
        </ul>
      </section>
    </article>
  );
}
