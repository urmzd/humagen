import { defaultCopy } from "@humagen/components";
import type { AgentEvaluationHealth } from "../../../packages/client/src/generated/types.ts";
import { BuilderButton } from "./AgentBuilder.tsx";
import { healthLabel } from "./AgentHealth.tsx";
import { useEffect, useState } from "react";
import type { Run } from "./adapter.ts";
import { type AgentControl, api, call, type Group, type Revision } from "./api.ts";
import {
  AgentMark,
  Empty,
  field,
  longest,
  Page,
  Problem,
  primary,
  rows,
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
      {/* Filters share one row; the header keeps only actions. */}
      {(operational || agents.length > 6) && (
        <div className="flex min-w-0 items-center gap-2">
          {agents.length > 6 && (
            <SearchField
              label={words.search}
              placeholder={words.searchAmong(agents.length)}
              className="min-w-0 max-w-sm flex-1 pointer-coarse:min-h-11"
              value={query}
              onChange={setQuery}
            />
          )}
          {operational && (
            <select
              aria-label={words.filter}
              className={`${field} min-h-11 w-auto! min-w-44 max-w-full py-0! text-aui-sm`}
              value={state}
              onChange={(event) => setState(event.target.value)}
            >
              <option value="">{words.all}</option>
              <option value="draft">{words.reviewDrafts}</option>
              {Object.entries(words.directoryStates).map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </select>
          )}
        </div>
      )}
      {agents.length === 0 ? (
        <Empty title="No agents yet">
          {canWrite ? "Create one to start running work." : "Ask an admin to define one."}
        </Empty>
      ) : shown.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">No agent matches “{query}”.</p>
      ) : (
        <div>
          <div
            aria-hidden="true"
            className="ml-auto grid w-48 grid-cols-3 gap-3 pb-2 text-right text-aui-2xs text-aui-muted"
          >
            {[words.runs, words.success, words.median].map((label) => (
              <span key={label}>{label}</span>
            ))}
          </div>
          <ul className={rows}>
            {shown.map((a) => {
              const name = a.spec.name;
              const g = stats.get(name);
              const ended = g ? g.completed + g.failed : 0;
              const own = runs.filter((r) => r.agent === name);
              const busy = own.some((r) => r.status === "running" || r.status === "accepted");
              const status = operational ? roster[name]?.state : undefined;
              const description = a.spec.description || a.spec.system_prompt || words.noDescription;
              const figures: [string, string][] =
                g && g.runs > 0
                  ? [
                      [words.runs, String(g.runs)],
                      [words.success, ended ? `${Math.round((100 * g.completed) / ended)}%` : "–"],
                      [words.median, g.p50_ms ? took(g.p50_ms) : "–"],
                    ]
                  : [
                      [words.runs, "–"],
                      [words.success, "–"],
                      [words.median, "–"],
                    ];
              return (
                <li key={name} className="humagen-enter">
                  <button
                    type="button"
                    onClick={() => onOpen(name)}
                    className="grid w-full min-w-0 grid-cols-[28px_minmax(0,1fr)] items-start gap-x-3 gap-y-3 py-4 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent focus-visible:-outline-offset-2 @xl:grid-cols-[28px_minmax(0,1fr)_12rem] @4xl:grid-cols-[28px_minmax(0,1fr)_8rem_12rem] @xl:items-center"
                  >
                    <span className="self-start">
                      <AgentMark name={name} size={28} state={busy ? "thinking" : "idle"} />
                    </span>
                    <span className="min-w-0">
                      <span className="flex h-7 min-w-0 items-center gap-2 whitespace-nowrap">
                        <span className="humagen-display min-w-0 truncate text-aui-lg font-semibold text-aui-fg">
                          {name}
                        </span>
                        {a.package ? (
                          // A package agent is maintained by the platform and may be customized here.
                          <span className="flex shrink-0 items-center gap-1.5 text-aui-xs text-aui-muted">
                            <span title={words.packageHint}>{a.package.name}</span>
                            {a.customized && <span>· {words.customized}</span>}
                            {a.update_available && (
                              <span className="text-aui-accent">· {words.updateAvailable}</span>
                            )}
                          </span>
                        ) : (
                          builtin.includes(name) && (
                            <span className="shrink-0 text-aui-xs text-aui-muted">
                              {words.builtin}
                            </span>
                          )
                        )}
                        {status && status !== "ready" && (
                          <span
                            className={`flex min-w-0 items-center gap-1 text-aui-xs ${status === "failed" ? "text-aui-bad" : status === "needs_review" || status === "waiting_on_people" ? "text-aui-warn" : "text-aui-muted"}`}
                          >
                            <span
                              aria-hidden
                              className="size-1.5 shrink-0 rounded-full bg-current"
                            />
                            <span className="truncate">
                              {roster[name]?.pause_reason === "setup"
                                ? "Draft"
                                : words.directoryStates[status]}
                            </span>
                          </span>
                        )}
                        {name === workspaceDefault && (
                          <span
                            className="min-w-0 truncate text-aui-xs text-aui-muted"
                            title={defaultCopy.workspaceDefault}
                          >
                            {words.workspaceDefault}
                          </span>
                        )}
                        {homes[name] && homes[name] !== "~" && (
                          <span
                            className="min-w-0 truncate text-aui-xs text-aui-muted"
                            title={defaultCopy.agentPlace.livesIn(homes[name])}
                          >
                            {defaultCopy.agentPlace.livesIn(homes[name])}
                          </span>
                        )}
                      </span>
                      {operational && (
                        <span
                          className={`block text-aui-xs ${health[name]?.status === "needs_review" ? "text-aui-warn" : "text-aui-muted"}`}
                        >
                          {health[name]
                            ? `${healthLabel(health[name].status)}${health[name].samples ? ` · ${Math.round(health[name].pass_rate)}% passed` : ""}`
                            : "Evaluation health unavailable"}
                        </span>
                      )}
                      <span className="block truncate text-aui-sm text-aui-muted">
                        {description}
                      </span>
                      <span className="block truncate pt-0.5 text-aui-xs text-aui-muted">
                        Revision {a.rev} · {a.spec.provider} / {a.spec.model}
                      </span>
                    </span>
                    <span className="hidden w-32 self-center @4xl:block">
                      {own.length > 0 && <Tape runs={own} slots={20} scale={scale} />}
                    </span>
                    <span className="col-start-2 grid w-48 grid-cols-3 gap-3 self-center justify-self-end text-right @xl:col-start-auto">
                      {figures.map(([label, value]) => (
                        <span key={label} className="flex min-w-0 flex-col">
                          <span
                            className={`humagen-display text-aui-lg font-semibold ${value === "–" ? "text-aui-muted" : "text-aui-fg"}`}
                          >
                            {value}
                          </span>
                          <span className="sr-only">{label}</span>
                        </span>
                      ))}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </Page>
  );
}
