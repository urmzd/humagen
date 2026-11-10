import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import type { AgentEvaluationHealth } from "../../../packages/client/src/generated/types.ts";
import { BuilderButton } from "./AgentBuilder.tsx";
import { healthLabel } from "./AgentHealth.tsx";
import { AgentStore } from "./AgentStore.tsx";
import type { Run } from "./adapter.ts";
import {
  type AgentControl,
  api,
  call,
  type Group,
  type ProjectView,
  type Revision,
} from "./api.ts";
import { Pill, Tag } from "./cards.tsx";
import { accessLabel, accessOf } from "./projects.ts";
import { href } from "./route.ts";
import { AgentMark, Empty, Page, Problem, primary, SearchField, took } from "./ui.tsx";

/**
 * Every agent in the tenant, one row each: who it is, its model, and its
 * sessions added up (working and waiting now, the last day's runs, success and
 * median time). An agent has no state of its own; each session does, and a
 * count opens those sessions. Opening a row shows the agent.
 */
export function Agents({
  agents,
  stats,
  runs = [],
  workspaceDefault = "",
  homes = {},
  tree = [],
  personalProject,
  canWrite,
  onOpen,
  onNew,
  onBuild,
  onBuilderSession,
  operational = false,
  onStoreChanged,
}: {
  /** Shows the agent store under the list; called after an install or removal, to read the agents again. */
  onStoreChanged?: () => void;
  /** Describe an agent to the built-in builder instead of writing it by hand, when the workspace has it. */
  onBuild?: () => void;
  onBuilderSession?: (id: string) => void;
  operational?: boolean;
  agents: Revision[];
  /** The last 24 hours, by agent name. */
  stats: Map<string, Group>;
  /** Recent runs across the tenant, drawn on each agent's tape. */
  runs?: Run[];
  /** Who answers a message that tags no agent in the workspace itself. */
  workspaceDefault?: string;
  /** The project each agent is homed in, which says who sees it. */
  homes?: Record<string, string>;
  /** The project tree, for the title of the project whose members see an agent. */
  tree?: ProjectView[];
  /** The signed-in person's Personal project: an agent homed there is private to them. */
  personalProject?: string;
  canWrite: boolean;
  onOpen: (name: string) => void;
  onNew: () => void;
}) {
  const words = defaultCopy.agentOperations;
  const [roster, setRoster] = useState<Record<string, AgentControl>>({});
  const [problem, setProblem] = useState("");
  const [health, setHealth] = useState<Record<string, AgentEvaluationHealth>>({});
  const names = agents.map((agent) => agent.spec.name).join(",");
  useEffect(() => {
    if (!operational) return;
    let active = true;
    const read = async () => {
      try {
        const wanted = names ? names.split(",") : [];
        const pages = [];
        for (let i = 0; i < wanted.length; i += 100)
          pages.push(await api.agentRoster(wanted.slice(i, i + 100)));
        if (active) {
          setRoster(Object.assign({}, ...pages));
          setProblem("");
        }
      } catch {
        if (active) setProblem(words.failed);
      }
    };
    void read();
    const timer = setInterval(() => void read(), 5000);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [names, operational, words.failed]);
  useEffect(() => {
    if (!operational) return;
    let live = true;
    const wanted = names ? names.split(",") : [];
    setHealth({});
    const read = async () => {
      for (let start = 0; live && start < wanted.length; start += 4) {
        await Promise.all(
          wanted.slice(start, start + 4).map(async (name) => {
            try {
              const next = await call<AgentEvaluationHealth>(
                "GET",
                `/api/agents/${encodeURIComponent(name)}/health`,
              );
              if (live) setHealth((current) => ({ ...current, [name]: next }));
            } catch {
              /* A missing evaluation stays unknown, never healthy. */
            }
          }),
        );
      }
    };
    void read();
    return () => {
      live = false;
    };
  }, [names, operational]);
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const matching = q
    ? agents.filter((a) =>
        `${a.spec.name} ${a.spec.description ?? ""} ${a.spec.model}`.toLowerCase().includes(q),
      )
    : agents;
  const shown = matching;
  return (
    <Page
      title="Agents"
      lede="How each agent's sessions are going. An agent has no state of its own; its sessions do."
      actions={
        canWrite || onBuild || onBuilderSession ? (
          onBuilderSession ? (
            <BuilderButton onOpen={onBuilderSession} />
          ) : (
            <button type="button" className={primary} onClick={onBuild ?? onNew}>
              {words.newAgent}
            </button>
          )
        ) : undefined
      }
    >
      <Problem message={problem} />
      {agents.length > 6 && (
        <SearchField
          label={words.search}
          placeholder={words.searchAmong(agents.length)}
          className="min-w-0 max-w-sm"
          value={query}
          onChange={setQuery}
        />
      )}
      {agents.length === 0 ? (
        <Empty title="No agents yet">
          {canWrite ? "Create one to start running work." : "Ask an admin to define one."}
        </Empty>
      ) : shown.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{`No agent matches “${query.trim()}”.`}</p>
      ) : (
        // One table, so agents compare at a glance: health, model and the day's numbers line up.
        <div className="-mx-1 overflow-x-auto">
          <table className="w-full min-w-[48rem] table-fixed border-collapse text-left text-aui-sm">
            <thead>
              <tr className="border-aui-border border-b text-aui-2xs text-aui-muted">
                <th scope="col" className="px-1 py-2 font-normal">
                  Agent
                </th>
                {operational && (
                  <th scope="col" className="w-32 px-2 py-2 font-normal">
                    Health
                  </th>
                )}
                {[words.working, words.waiting].map((label) => (
                  <th key={label} scope="col" className="w-20 px-2 py-2 text-right font-normal">
                    {label}
                  </th>
                ))}
                {[words.runs, words.success, words.median].map((label) => (
                  <th key={label} scope="col" className="w-20 px-2 py-2 text-right font-normal">
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {shown.map((a) => {
                const name = a.spec.name;
                const g = stats.get(name);
                const ended = g ? g.completed + g.failed : 0;
                const own = runs.filter((r) => r.agent === name);
                // An agent has no state; these count its sessions that are working or waiting now.
                const working = own.filter(
                  (r) => r.status === "running" || r.status === "accepted",
                ).length;
                const waiting = own.filter((r) => r.status === "parked").length;
                const control = operational ? roster[name] : undefined;
                const access = accessOf(homes[name], personalProject, tree);
                const description =
                  a.spec.description || a.spec.system_prompt || words.noDescription;
                const figures =
                  g && g.runs > 0
                    ? [
                        String(g.runs),
                        ended ? `${Math.round((100 * g.completed) / ended)}%` : "–",
                        g.p50_ms ? took(g.p50_ms) : "–",
                      ]
                    : ["–", "–", "–"];
                const h = health[name];
                return (
                  <tr
                    key={name}
                    onClick={() => onOpen(name)}
                    className="humagen-enter cursor-pointer border-aui-border border-b align-middle transition-colors hover:bg-aui-hover"
                  >
                    <td className="px-1 py-3">
                      <div className="flex min-w-0 items-center gap-3">
                        <AgentMark name={name} size={28} />
                        <div className="min-w-0 flex-1">
                          <div className="flex min-w-0 flex-wrap items-center gap-1.5 overflow-hidden">
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation();
                                onOpen(name);
                              }}
                              className="min-h-6 min-w-6 max-w-full truncate font-medium text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11 pointer-coarse:min-w-11"
                            >
                              {name}
                            </button>
                            {control?.pause_reason === "setup" && <Tag>{words.draft}</Tag>}
                            {control?.effective_paused && control.pause_reason !== "setup" && (
                              <Tag>{words.pausedByAdmin}</Tag>
                            )}
                            {name === workspaceDefault && (
                              <span title={defaultCopy.workspaceDefault}>
                                <Tag>{words.workspaceDefault}</Tag>
                              </span>
                            )}
                            {access.kind !== "workspace" && <Tag>{accessLabel(access)}</Tag>}
                          </div>
                          <p className="truncate text-aui-xs text-aui-muted">{description}</p>
                        </div>
                      </div>
                    </td>
                    {operational && (
                      <td className="whitespace-nowrap px-2 py-3">
                        {h && (h.samples > 0 || h.status === "needs_review") && (
                          <Pill tone={h.status === "needs_review" ? "warn" : "ok"}>
                            {healthLabel(h.status)}
                            {h.samples ? ` ${Math.round(h.pass_rate)}%` : ""}
                          </Pill>
                        )}
                      </td>
                    )}
                    {(
                      [
                        ["working", working],
                        ["waiting", waiting],
                      ] as const
                    ).map(([status, count]) => (
                      <td
                        key={status}
                        className="whitespace-nowrap px-2 py-3 text-right tabular-nums"
                      >
                        {count > 0 ? (
                          <a
                            href={href({ page: "sessions", agent: name, status })}
                            onClick={(e) => e.stopPropagation()}
                            className="text-aui-fg underline-offset-2 hover:underline"
                          >
                            {count}
                          </a>
                        ) : (
                          <span className="text-aui-muted">–</span>
                        )}
                      </td>
                    ))}
                    {figures.map((value, i) => (
                      <td
                        key={[words.runs, words.success, words.median][i]}
                        className={`whitespace-nowrap px-2 py-3 text-right tabular-nums ${value === "–" ? "text-aui-muted" : "text-aui-fg"}`}
                      >
                        {value}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {onStoreChanged && <AgentStore canWrite={canWrite} onChanged={onStoreChanged} />}
    </Page>
  );
}
