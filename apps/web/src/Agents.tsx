import { defaultCopy } from "@humagen/components";
import { useEffect,useState } from "react";
import type { Run } from "./adapter.ts";
import {api,type AgentControl,type Group,type Revision } from "./api.ts";
import { AgentMark, button, Chip, Empty, field, longest, Page, primary, rows, Stroke, Tape, took,Problem,Label } from "./ui.tsx";

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
  operational=false,
}: {
  /** Describe an agent to the built-in builder instead of writing it by hand, when the workspace has it. */
  onBuild?: () => void;
  operational?:boolean;
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
  const words=defaultCopy.agentOperations;
  const [roster,setRoster]=useState<Record<string,AgentControl>>({});
  const [workspace,setWorkspace]=useState<AgentControl>();
  const [problem,setProblem]=useState("");
  const [busyPause,setBusyPause]=useState(false);
  const [state,setState]=useState("");
  const names=agents.map(agent=>agent.spec.name).join(",");
  useEffect(()=>{if(!operational)return;let active=true;const read=async()=>{try{const wanted=names?names.split(","):[];const pages=[];for(let i=0;i<wanted.length;i+=100)pages.push(await api.agentRoster(wanted.slice(i,i+100)));const current=await api.agentControl();if(active){setRoster(Object.assign({},...pages));setWorkspace(current);setProblem("")}}catch{if(active)setProblem(words.failed)}};void read();const timer=setInterval(()=>void read(),5000);return()=>{active=false;clearInterval(timer)}},[names,operational]);
  const pauseAll=async()=>{if(!workspace||busyPause)return;setBusyPause(true);try{setWorkspace(await api.saveAgentControl("",{version:workspace.version,paused:!workspace.paused,manager:workspace.manager,stop_rules:workspace.stop_rules}));setProblem("")}catch(error){setProblem(error instanceof Error?error.message:words.failed)}finally{setBusyPause(false)}};
  const scale = longest(runs);
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const matching = q
    ? agents.filter((a) => `${a.spec.name} ${a.spec.description ?? ""} ${a.spec.model}`.toLowerCase().includes(q))
    : agents;
  const shown=matching.filter(agent=>!state||(state==="draft"?roster[agent.spec.name]?.effective_paused&&!roster[agent.spec.name]?.paused:roster[agent.spec.name]?.state===state));
  return (
    <Page
      title="Agents"
      lede="An agent is a prompt with a model, the agents it may call, and the context it reads. Open one to change it, test it, and watch it."
      actions={
        canWrite || onBuild ? (
          <div className="flex flex-wrap gap-2">
            {operational&&canWrite&&<button type="button" className={button} disabled={!workspace||busyPause} onClick={()=>void pauseAll()}>{workspace?.paused?words.resumeAll:words.pauseAll}</button>}
            {onBuild && (
              <button type="button" className={canWrite ? button : primary} onClick={onBuild}>
                Build with the Builder
              </button>
            )}
            {canWrite && (
              <button type="button" className={primary} onClick={onNew}>
                New Agent
              </button>
            )}
          </div>
        ) : undefined
      }
    >
      <Problem message={problem}/>
      {operational&&<Label text={words.filter}><select className={`${field} max-w-sm`} value={state} onChange={event=>setState(event.target.value)}><option value="">{words.all}</option>{Object.entries(words.states).map(([key,label])=><option key={key} value={key}>{label}</option>)}</select></Label>}
      {/* A long list is found by name, not by scrolling. */}
      {agents.length > 6 && (
        <input
          type="search"
          aria-label="Find an agent"
          placeholder={`Find among ${agents.length} agents`}
          className={`${field} h-9 max-w-sm`}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      )}
      {agents.length === 0 ? (
        <Empty title="No agents yet">{canWrite ? "Create one to start running work." : "Ask an admin to define one."}</Empty>
      ) : shown.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">No agent matches “{query}”.</p>
      ) : (
        <ul className={rows}>
          {shown.map((a) => {
            const name = a.spec.name;
            const g = stats.get(name);
            const ended = g ? g.completed + g.failed : 0;
            const own = runs.filter((r) => r.agent === name);
            const busy = own.some((r) => r.status === "running" || r.status === "accepted");
            const waiting = own.some((r) => r.status === "parked");
            const figures: [string, string][] =
              g && g.runs > 0
                ? [
                    ["Runs, 24h", String(g.runs)],
                    ["Success", ended ? `${Math.round((100 * g.completed) / ended)}%` : "No data"],
                    ["Median", g.p50_ms ? took(g.p50_ms) : "–"],
                  ]
                : [];
            return (
              <li key={name} className="humagen-enter">
                <Stroke agent={name} state={waiting ? "waiting" : busy ? "working" : "idle"}>
                <button
                  type="button"
                  onClick={() => onOpen(name)}
                  className="grid w-full grid-cols-1 items-center gap-x-8 gap-y-3 py-4 pr-1 pl-4 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent focus-visible:-outline-offset-2 @4xl:grid-cols-[minmax(0,5fr)_minmax(0,4fr)_15rem]"
                >
                  <span className="flex min-w-0 items-start gap-3">
                    <span className="pt-0.5">
                      <AgentMark name={name} size={24} state={busy ? "thinking" : "idle"} />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                        <span className="humagen-display truncate text-aui-lg font-semibold text-aui-fg">{name}</span>
                        {operational&&roster[name]?.state&&<Chip>{words.states[roster[name].state!]}</Chip>}
                        {name === workspaceDefault && <Chip>{defaultCopy.workspaceDefault}</Chip>}
                        {homes[name] && <Chip>{defaultCopy.agentPlace.livesIn(homes[name]==="~"?defaultCopy.projects.companyName:homes[name])}</Chip>}
                        {builtin.includes(name) && <Chip>Built in, read only</Chip>}
                      </span>
                      <span className="line-clamp-1 text-aui-sm text-aui-muted">
                        {a.spec.description || a.spec.system_prompt || "No description or prompt yet."}
                      </span>
                      <span className="block pt-0.5 text-aui-xs text-aui-muted">
                        Revision {a.rev}, {a.spec.provider} / {a.spec.model}
                      </span>
                    </span>
                  </span>

                  <Tape runs={own} slots={36} scale={scale} />

                  {figures.length > 0 ? (
                    <span className="grid grid-cols-3 gap-4">
                      {figures.map(([label, value]) => (
                        <span key={label} className="flex min-w-0 flex-col">
                          <span className="humagen-display text-aui-lg font-semibold text-aui-fg">{value}</span>
                          <span className="truncate text-aui-2xs text-aui-muted">{label}</span>
                        </span>
                      ))}
                    </span>
                  ) : (
                    <span className="text-aui-xs text-aui-muted">No runs in the last 24 hours</span>
                  )}
                </button>
                </Stroke>
              </li>
            );
          })}
        </ul>
      )}
    </Page>
  );
}
