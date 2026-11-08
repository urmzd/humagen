import { defaultCopy } from "@humagen/components";
import { Alert, AlertDescription, AlertTitle } from "@humagen/render-web/ui/alert";
import { useEffect, useState } from "react";
import type { AgentEvaluationHealth } from "../../../packages/client/src/generated/types.ts";
import { BuilderButton } from "./AgentBuilder.tsx";
import { healthLabel } from "./AgentHealth.tsx";
import type { Run } from "./adapter.ts";
import { type AgentControl, api, call, type Group, type Revision } from "./api.ts";
import { FilterSelect, Pill, Tag } from "./cards.tsx";
import {
  AgentMark,
  Empty,
  longest,
  Page,
  Problem,
  primary,
  SearchField,
  Tape,
  took,
} from "./ui.tsx";

/**
 * Every agent in the tenant, one lane each: who it is, its recent runs on a
 * tape, and how the last day went. The tapes share one scale, so a slow agent
 * reads as slow next to the others. Opening a lane shows the agent's editor.
 */
export function Agents({
  agents,
  stats,
  runs = [],
  builtin = [],
  workspaceDefault = "",
  homes = {},
  canWrite,
  onOpen,
  onNew,
  onBuild,
  onBuilderSession,
  state: selectedState,
  onState,
  operational = false,
}: {
  /** Describe an agent to the built-in builder instead of writing it by hand, when the workspace has it. */
  onBuild?: () => void;
  onBuilderSession?: (id: string) => void;
  state?: string;
  onState?: (state: string) => void;
  operational?: boolean;
  agents: Revision[];
  /** The last 24 hours, by agent name. */
  stats: Map<string, Group>;
  /** Recent runs across the tenant, drawn on each agent's tape. */
  runs?: Run[];
  /** Agents the platform provides. They can be opened and read, not changed. */
  builtin?: string[];
  /** Who answers a message that tags no agent in the workspace itself. */
  workspaceDefault?: string;
  /** Where each agent that lives in a project lives. */
  homes?: Record<string, string>;
  canWrite: boolean;
  onOpen: (name: string) => void;
  onNew: () => void;
}) {
  const words = defaultCopy.agentOperations;
  const [roster, setRoster] = useState<Record<string, AgentControl>>({});
  const [problem, setProblem] = useState("");
  const [localState, setLocalState] = useState("");
  const state = selectedState ?? localState;
  const setState = onState ?? setLocalState;
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
  const scale = longest(runs);
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const matching = q
    ? agents.filter((a) =>
        `${a.spec.name} ${a.spec.description ?? ""} ${a.spec.model}`.toLowerCase().includes(q),
      )
    : agents;
  const shown = matching.filter(
    (agent) =>
      !state ||
      (state === "draft"
        ? roster[agent.spec.name]?.pause_reason === "setup"
        : state === "needs_review"
          ? health[agent.spec.name]?.status === "needs_review" ||
            ["failed", "needs_review"].includes(roster[agent.spec.name]?.state ?? "")
          : roster[agent.spec.name]?.state === state),
  );
  return (
    <Page
      title="Agents"
      lede="See how agents are performing. Create or change one in a builder session, then review its evaluations before approving the change."
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
      {agents.length > 0 && agents.every((a) => a.spec.provider === "offline") && (
        <Alert>
          <AlertTitle>Test model</AlertTitle>
          <AlertDescription>
            Agents here answer with the offline test model, so replies are not real answers.
          </AlertDescription>
        </Alert>
      )}
      {/* Filters share one row; the header keeps only actions. */}
      {(operational || agents.length > 6) && (
        <div className="flex min-w-0 items-center gap-2">
          {agents.length > 6 && (
            <SearchField
              label={words.search}
              placeholder={words.searchAmong(agents.length)}
              className="min-w-0 max-w-sm flex-1"
              value={query}
              onChange={setQuery}
            />
          )}
          {operational && (
            <FilterSelect
              label={words.filter}
              value={state}
              onChange={setState}
              options={[
                { value: "", label: words.all },
                { value: "draft", label: words.reviewDrafts },
                ...Object.entries(words.directoryStates).map(([key, label]) => ({
                  value: key,
                  label,
                })),
              ]}
            />
          )}
        </div>
      )}
      {agents.length === 0 ? (
        <Empty title="No agents yet">
          {canWrite ? "Create one to start running work." : "Ask an admin to define one."}
        </Empty>
      ) : shown.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">
          {query.trim()
            ? `No agent matches “${query.trim()}”.`
            : "No agent is in this state right now."}
        </p>
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
                <th scope="col" className="w-36 px-2 py-2 font-normal">
                  Model
                </th>
                <th scope="col" className="hidden w-28 px-2 py-2 font-normal @4xl:table-cell">
                  Activity
                </th>
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
                const busy = own.some((r) => r.status === "running" || r.status === "accepted");
                const status = operational ? roster[name]?.state : undefined;
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
                        <AgentMark name={name} size={28} state={busy ? "thinking" : "idle"} />
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
                            {(a.package || builtin.includes(name)) && (
                              <span title={a.package ? words.packageHint : undefined}>
                                <Tag>Built in</Tag>
                              </span>
                            )}
                            {a.customized && <Pill>{words.customized}</Pill>}
                            {a.update_available && (
                              <Pill tone="accent">{words.updateAvailable}</Pill>
                            )}
                            {status && status !== "ready" && (
                              <Pill
                                tone={
                                  status === "failed"
                                    ? "bad"
                                    : status === "needs_review" || status === "waiting_on_people"
                                      ? "warn"
                                      : "neutral"
                                }
                              >
                                {roster[name]?.pause_reason === "setup"
                                  ? "Draft"
                                  : words.directoryStates[status]}
                              </Pill>
                            )}
                            {name === workspaceDefault && (
                              <span title={defaultCopy.workspaceDefault}>
                                <Tag>{words.workspaceDefault}</Tag>
                              </span>
                            )}
                            {homes[name] && homes[name] !== "~" && (
                              <Tag>{defaultCopy.agentPlace.livesIn(homes[name])}</Tag>
                            )}
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
                    <td className="whitespace-nowrap px-2 py-3">
                      <div className="truncate text-aui-sm">{a.spec.model}</div>
                    </td>
                    <td className="hidden px-2 py-3 @4xl:table-cell">
                      {own.length > 0 && <Tape runs={own} slots={20} scale={scale} />}
                    </td>
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
    </Page>
  );
}
