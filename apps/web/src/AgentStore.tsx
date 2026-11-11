import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon } from "@humagen/render-web";
import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import { api, type StoreAgent, type StorePackage } from "./api.ts";
import { ItemCard, itemGrid, Pill, Reference, Tag } from "./cards.tsx";
import { href } from "./route.ts";
import { AgentMark, button, Confirm, Problem, primary, Section } from "./ui.tsx";

/**
 * The agent store, on the Agents page: the platform's optional agents as
 * packages, each a versioned base definition. Install adds a package's
 * agents to the workspace, and again restores any that were retired; Remove
 * retires them after a confirm that says their history stays. Whether a
 * package is installed is what the server reads from the workspace's agents.
 */
export function AgentStore({
  canWrite,
  onChanged,
}: {
  /** agents:write: Install and Remove are offered. */
  canWrite: boolean;
  /** Called after an install or removal, so the agent list is read again. */
  onChanged?: () => void;
}) {
  const words = defaultCopy.agentStore;
  const [packages, setPackages] = useState<StorePackage[] | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState("");
  const [removing, setRemoving] = useState("");
  const [note, setNote] = useState<Record<string, string>>({});
  useEffect(() => {
    let live = true;
    api
      .storePackages()
      .then((all) => live && setPackages(all))
      .catch(() => live && setProblem(words.failed));
    return () => {
      live = false;
    };
  }, [words.failed]);
  const put = (next: StorePackage) =>
    setPackages((all) => (all ?? []).map((p) => (p.name === next.name ? next : p)));
  const install = async (pkg: StorePackage) => {
    setBusy(pkg.name);
    setProblem("");
    try {
      const out = await api.installStorePackage(pkg.name);
      put(out.package);
      setNote((n) => ({
        ...n,
        [pkg.name]: out.added.length ? words.installedNote(out.added) : "",
      }));
      onChanged?.();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  };
  const remove = async (pkg: StorePackage, force: boolean) => {
    setBusy(pkg.name);
    setProblem("");
    try {
      const out = await api.removeStorePackage(pkg.name, force);
      put(out.package);
      setNote((n) => ({
        ...n,
        [pkg.name]: out.retired.length ? words.removedNote(out.retired) : "",
      }));
      onChanged?.();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e));
      // Someone may have changed an agent meanwhile: read the store again.
      api
        .storePackages()
        .then(setPackages)
        .catch(() => {});
    } finally {
      setBusy("");
    }
  };
  return (
    <Section title={words.title}>
      <Problem message={problem} />
      {packages === null ? (
        !problem && <p className="text-aui-sm text-aui-muted">{words.loading}</p>
      ) : packages.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.empty}</p>
      ) : (
        <div className={itemGrid}>
          {packages.map((pkg) => {
            const customized = pkg.agents
              .filter((a) => a.installed && a.customized)
              .map((a) => a.name);
            const taken = pkg.agents.filter((a) => a.taken).map((a) => a.name);
            const partial = pkg.installed && pkg.missing.length > 0;
            const working = busy === pkg.name;
            return (
              <ItemCard
                key={pkg.name}
                title={pkg.title}
                status={pkg.installed ? <Pill tone="ok">{words.installed}</Pill> : undefined}
                description={pkg.description}
                facts={<Tag>{words.version(pkg.version)}</Tag>}
                actions={
                  canWrite &&
                  (removing === pkg.name ? (
                    <Confirm
                      question={words.removeQuestion(pkg.title, customized)}
                      action={words.remove}
                      onConfirm={() => {
                        setRemoving("");
                        // The confirm named the changed agents, so they are retired too.
                        void remove(pkg, customized.length > 0);
                      }}
                      onKeep={() => setRemoving("")}
                    />
                  ) : (
                    <>
                      {(!pkg.installed || partial) && (
                        <button
                          type="button"
                          className={pkg.installed ? button : primary}
                          disabled={working || taken.length > 0}
                          aria-label={`${partial ? words.restore : words.install} ${pkg.title}`}
                          onClick={() => void install(pkg)}
                        >
                          {working ? words.installing : partial ? words.restore : words.install}
                        </button>
                      )}
                      {pkg.installed && (
                        <button
                          type="button"
                          className={button}
                          disabled={working}
                          aria-label={`${words.remove} ${pkg.title}`}
                          onClick={() => setRemoving(pkg.name)}
                        >
                          {working ? words.removing : words.remove}
                        </button>
                      )}
                    </>
                  ))
                }
              >
                <ul aria-label={words.uses} className="mb-2 flex min-w-0 flex-col">
                  {pkg.agents.map((a) => (
                    <AgentUses key={a.name} agent={a} open={!pkg.installed} />
                  ))}
                </ul>
                {(partial || taken.length > 0 || customized.length > 0 || note[pkg.name]) && (
                  <div className="flex flex-col gap-1 text-aui-xs text-aui-muted">
                    {partial && <p>{words.missing(pkg.missing)}</p>}
                    {taken.length > 0 && <p>{words.taken(taken)}</p>}
                    {customized.length > 0 && <p>{words.customized(customized)}</p>}
                    {note[pkg.name] && <output className="block">{note[pkg.name]}</output>}
                  </div>
                )}
              </ItemCard>
            );
          })}
        </div>
      )}
    </Section>
  );
}

/**
 * One agent of a package and what installing grants it, above the card's
 * Install button. Closed, it is one line: the agent and a summary of its
 * model, tools, connections and calls. Open, it is a definition list of
 * Model, Needs, May call and Tools, each label in a fixed column with its
 * chips wrapping beside it, never under it. A package not installed yet
 * opens its agents, since that is when a person reads what they grant.
 */
function AgentUses({ agent, open }: { agent: StoreAgent; open: boolean }) {
  const words = defaultCopy.agentStore;
  const calls = agent.access.spawn ?? [];
  const row = (label: string, chips: ReactNode[]) =>
    chips.length > 0 && (
      <>
        <dt className="flex min-h-5 items-start pt-0.5 text-aui-2xs text-aui-muted">{label}</dt>
        <dd className="m-0 flex min-w-0 flex-wrap items-center gap-1.5">{chips}</dd>
      </>
    );
  return (
    <li data-agent={agent.name} className="min-w-0">
      <details open={open} className="group/uses min-w-0">
        <summary className="flex min-h-7 cursor-pointer list-none items-center gap-1.5 rounded-aui text-aui-xs hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11 [&::-webkit-details-marker]:hidden">
          <ChevronRightIcon
            size={12}
            aria-hidden
            className="shrink-0 text-aui-muted transition-transform group-open/uses:rotate-90"
          />
          <AgentMark name={agent.name} size={14} />
          <span className="min-w-0 text-aui-muted">
            <span className="text-aui-fg">@{agent.name}</span>{" "}
            {words.summary(
              words.modelName(agent.provider, agent.model),
              agent.tools.length,
              [
                ...agent.connectors.map(words.connector),
                ...agent.tool_servers.map(words.toolServer),
              ],
              calls.length,
            )}
          </span>
        </summary>
        <dl className="m-0 grid min-w-0 grid-cols-[4rem_minmax(0,1fr)] gap-x-2 gap-y-1.5 pt-1 pb-2 pl-[18px]">
          {row(words.model, [
            <Tag key="model">{words.modelName(agent.provider, agent.model)}</Tag>,
            ...agent.fallbacks.map((f) => (
              <Tag key={`${f.provider}/${f.model}`}>{words.fallback(f.provider, f.model)}</Tag>
            )),
          ])}
          {row(words.needs, [
            ...agent.connectors.map((surface) => (
              <Reference
                key={surface}
                kind={surface === "github" || surface === "slack" ? surface : undefined}
                label={words.connector(surface)}
              />
            )),
            ...agent.tool_servers.map((name) => (
              <Tag key={`server:${name}`}>{words.toolServer(name)}</Tag>
            )),
          ])}
          {row(
            words.calls,
            calls.map((name) => <Reference key={name} kind="agent" label={`@${name}`} />),
          )}
          {row(
            words.tools,
            agent.tools.map((tool) => <Tag key={tool}>{tool}</Tag>),
          )}
          {agent.installed &&
            row(words.page, [
              // An agent the workspace has opens its page; one it lacks is only named.
              <Reference
                key="page"
                kind="agent"
                label={words.openAgent}
                title={agent.description}
                href={href({ page: "agent", name: agent.name, tab: "prompt" })}
              />,
            ])}
        </dl>
      </details>
    </li>
  );
}
