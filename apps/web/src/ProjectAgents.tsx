import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { ApiError, api, type ProjectAgentsView, type ProjectView, type Revision } from "./api.ts";
import { AUTO, availableOf, EVERYONE, defaultFrom, losing, offerWords } from "./availability.ts";
import { titleOf } from "./projects.ts";
import { AgentMark, Chip, FlashNote, Label, Menu, PickMany, Problem, primary, rows, Section, SkeletonRows, Switch, useFlash } from "./ui.tsx";

const words = defaultCopy.projects.agents;
const place = defaultCopy.agentPlace;

/**
 * A project's Agents tab: which agents may answer here and where each comes
 * from, who answers a message that tags none, what the project enables and
 * narrows to, and the agents that live here. On the workspace it holds the
 * Workspace Default. The platform decides availability; this shows its
 * answer and writes the lists as a whole.
 */
export function ProjectAgentsTab({
  name,
  workspaceName,
  agents,
  homes,
  enabledIn,
  projects,
  canWrite,
  canHome,
  managed = false,
  onChanged,
}: {
  /** "" is the workspace. */
  name: string;
  workspaceName: string;
  agents: Revision[];
  /** Where each agent that lives in a project lives. */
  homes: Record<string, string>;
  /** Which projects enable each agent, "~" for the workspace. */
  enabledIn: Record<string, string[]>;
  /** The tree, for titles and for counting what a home would take away. */
  projects: ProjectView[];
  /** May change this project's lists. */
  canWrite: boolean;
  /** May make an agent live here (agents:write). */
  canHome: boolean;
  /**
   * The platform file sets this project's agents. The record does not say
   * which fields the file owns, so callers know it only from a refusal.
   */
  managed?: boolean;
  onChanged: () => void;
}) {
  const [view, setView] = useState<ProjectAgentsView | null>(null);
  const [enable, setEnable] = useState<string[]>([]);
  const [allow, setAllow] = useState<string[] | null>(null);
  const [fallback, setFallback] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [reads, setReads] = useState(0);
  const { flash, say } = useFlash();

  // biome-ignore lint/correctness/useExhaustiveDependencies: reads is the signal to read again
  useEffect(() => {
    let live = true;
    api
      .projectAgents(name)
      .then((v) => {
        if (!live) return;
        setView(v);
        setEnable(v.enable);
        setAllow(v.allow ?? null);
        setFallback(v.default ?? "");
      })
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : words.failed));
    return () => {
      live = false;
    };
  }, [name, reads]);

  if (!view) return problem ? <Problem message={problem} /> : <SkeletonRows count={4} />;

  const root = name === "";
  const title = (n: string) => {
    if (n === "") return workspaceName;
    const p = projects.find((x) => x.name === n);
    return p ? titleOf(p) : n;
  };
  const editable = canWrite && !managed;
  const every = enable.includes(EVERYONE);
  const platform = agents.map((a) => a.spec.name).filter((a) => !homes[a]);
  const options = (names: string[]) => names.map((n) => ({ value: n, label: n, mark: <AgentMark name={n} size={14} /> }));
  const dirty =
    JSON.stringify([enable, allow, fallback]) !== JSON.stringify([view.enable, view.allow ?? null, view.default ?? ""]);
  // Who could be the default: whatever may answer here.
  const available = availableOf(
    agents.map((a) => a.spec.name),
    view,
    homes,
  );
  const inherited = view.default_resolved && !view.default ? view.default_resolved : null;

  const save = () => {
    setSaving(true);
    setProblem(null);
    api
      .setProjectAgents(name, {
        version: view.version,
        enable,
        ...(allow ? { allow } : {}),
        ...(fallback ? { default: fallback } : {}),
      })
      .then((v) => {
        setView(v);
        setEnable(v.enable);
        setAllow(v.allow ?? null);
        setFallback(v.default ?? "");
        say(words.saved);
        onChanged();
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.failed))
      .finally(() => setSaving(false));
  };

  const home = (agent: string, to: string) => {
    setProblem(null);
    api
      .setAgentHome(agent, to)
      .then(() => {
        setReads((n) => n + 1);
        onChanged();
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : place.failed));
  };

  return (
    <div className="flex flex-col gap-8">
      <Problem message={problem} />
      {managed && <p className="text-aui-xs text-aui-muted">{words.managed}</p>}
      {!canWrite && root && <p className="text-aui-xs text-aui-muted">{words.governed}</p>}

      <Section title={words.available} hint={words.availableHint}>
        {view.offers.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{words.noneAvailable}</p>
        ) : (
          <ul className={rows} aria-label={words.available}>
            {view.offers.map((o) => {
              const whence = offerWords(o, name, title);
              return (
                <li key={o.agent} className="flex items-center gap-3 py-2">
                  {o.agent === EVERYONE ? <span className="w-[18px]" aria-hidden /> : <AgentMark name={o.agent} size={18} />}
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-aui-sm text-aui-fg">{o.agent === EVERYONE ? words.everyPlatform : o.agent}</span>
                    {whence && <span className="block truncate text-aui-xs text-aui-muted">{whence}</span>}
                  </span>
                  {view.default_resolved?.agent === o.agent && <Chip>{defaultCopy.auto.defaultChip}</Chip>}
                </li>
              );
            })}
          </ul>
        )}
      </Section>

      <Section title={root ? defaultCopy.workspaceDefault : words.default} hint={words.defaultHint}>
        <Label text={root ? defaultCopy.workspaceDefault : words.default}>
          <Menu
            label={root ? defaultCopy.workspaceDefault : words.default}
            value={fallback}
            disabled={!editable}
            options={[
              { value: "", label: root ? "None" : words.inherit },
              { value: AUTO, label: defaultCopy.auto.handle, hint: defaultCopy.auto.description },
              ...options(available),
            ]}
            onChange={setFallback}
          />
        </Label>
        <p className="text-aui-xs text-aui-muted">
          {inherited
            ? words.inherits(inherited.agent, title(defaultFrom(inherited)))
            : view.default_resolved
              ? words.answers(view.default_resolved.agent)
              : words.nobody}
        </p>
      </Section>

      <Section title={words.enable} hint={words.enableHint}>
        <Switch
          label={words.enableEvery}
          hint={words.enableEveryHint}
          checked={every}
          disabled={!editable}
          onChange={(on) => setEnable(on ? [EVERYONE] : [])}
        />
        {!every && (
          <PickMany
            label={words.enable}
            add={words.addAgent}
            disabled={!editable}
            empty={words.enableNone}
            value={enable}
            options={options(platform)}
            onChange={setEnable}
          />
        )}
      </Section>

      <Section title={words.allow} hint={words.allowHint}>
        <Switch label={words.allow} checked={allow !== null} disabled={!editable} onChange={(on) => setAllow(on ? [] : null)} />
        {allow !== null && (
          <PickMany
            label={words.allow}
            add={words.addAgent}
            disabled={!editable}
            empty={words.allowEmpty}
            value={allow}
            options={options(agents.map((a) => a.spec.name))}
            onChange={setAllow}
          />
        )}
      </Section>

      {editable && (
        <div className="flex items-center gap-3">
          <button type="button" className={primary} disabled={!dirty || saving || (allow !== null && allow.length === 0)} onClick={save}>
            {words.save}
          </button>
          <FlashNote flash={flash} align="left" />
        </div>
      )}

      {!root && (
        <Section title={words.lives} hint={words.livesHint}>
          <PickMany
            label={words.lives}
            add={defaultCopy.livesHere}
            disabled={!canHome}
            empty={words.noneLive}
            value={view.homes}
            options={agents
              .map((a) => a.spec.name)
              .filter((a) => !homes[a] || homes[a] === name)
              .map((a) => {
                const n = losing(name, enabledIn[a] ?? [], projects);
                return { value: a, label: a, hint: n > 0 ? place.loses(n) : undefined, mark: <AgentMark name={a} size={14} /> };
              })}
            onChange={(next) => {
              const added = next.find((a) => !view.homes.includes(a));
              const removed = view.homes.find((a) => !next.includes(a));
              if (added) home(added, name);
              else if (removed) home(removed, "");
            }}
          />
        </Section>
      )}
    </div>
  );
}
