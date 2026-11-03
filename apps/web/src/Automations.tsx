import { defaultCopy } from "@humagen/components";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import type { GitHubAutomation } from "../../../packages/client/src/generated/types.ts";
import { api, type Client, type Revision, type ScheduleView } from "./api.ts";
import type { ProjectRef } from "./projects.ts";
import { ScheduleForm } from "./Schedules.tsx";
import { AgentMark, button, card, field, Label, Page, Problem, primary } from "./ui.tsx";

const words = defaultCopy.automations;
const blank: GitHubAutomation = {
  name: "",
  repository: "",
  event: "check_run",
  action: "completed",
  conclusion: "failure",
  agent: "",
  instructions: "",
  paused: false,
};

export function Automations({
  schedules,
  agents,
  projects,
  can,
  onChanged,
  onSchedule,
}: {
  schedules: ScheduleView[];
  agents: Revision[];
  projects: ProjectRef[];
  can: (permission: string) => boolean;
  onChanged: () => void;
  onSchedule: (name: string) => void;
}) {
  const [clients, setClients] = useState<Client[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [kind, setKind] = useState<"schedule" | "github" | null>(null);
  const [editing, setEditing] = useState<{ client: string; name: string } | null>(null);
  const [clientID, setClientID] = useState("");
  const [draft, setDraft] = useState<GitHubAutomation>(blank);
  const [saving, setSaving] = useState(false);
  const fail = useCallback((error: unknown) =>
    setProblem(error instanceof Error ? error.message : "Could not save automation."), []);
  const reload = useCallback(() => {
    if (can("clients:read"))
      api
        .clients()
        .then((r) => setClients(r.clients.filter((c) => c.surface === "github")))
        .catch(fail);
  }, [can, fail]);
  useEffect(() => { reload(); }, [reload]);
  const patch = (next: Partial<GitHubAutomation>) => setDraft((old) => ({ ...old, ...next }));
  const client = clients.find((c) => c.id === clientID);
  const allowedAgents = agents.filter(
    (a) =>
      client?.agents?.includes("*") ||
      client?.default_agent === a.spec.name ||
      client?.agents?.includes(a.spec.name),
  );
  const save = async (e: FormEvent) => {
    e.preventDefault();
    if (!client) return;
    setProblem(null);
    setSaving(true);
    try {
      const rules = [...(client.github?.automations ?? [])].filter((r) => r.name !== editing?.name);
      await api.saveClient(
        { ...client, github: { ...client.github, automations: [...rules, draft] } },
        client.enabled,
      );
      setKind(null);
      setEditing(null);
      reload();
    } catch (error) {
      fail(error);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Page title={words.title} lede={words.lede}>
      {problem && <Problem message={problem} />}
      {!kind && (
        <div className="flex flex-wrap gap-2">
          {can("schedules:write") && (
            <button className={primary} type="button" onClick={() => setKind("schedule")}>
              {words.newSchedule}
            </button>
          )}
          {can("clients:write") && (
            <button
              className={button}
              type="button"
              onClick={() => {
                setKind("github");
                setDraft(blank);
                setEditing(null);
                setClientID(clients.find((c) => !c.managed)?.id ?? "");
              }}
            >
              {words.newGitHub}
            </button>
          )}
        </div>
      )}
      {kind === "schedule" && (
        <ScheduleForm
          agents={agents}
          projects={projects}
          onClose={() => setKind(null)}
          onSaved={() => {
            setKind(null);
            onChanged();
          }}
        />
      )}
      {kind === "github" && (
        <form onSubmit={save} className={`${card} flex flex-col gap-4`}>
          <h2 className="font-medium">{editing ? words.editTitle : words.newTitle}</h2>
          <div className="grid gap-3 sm:grid-cols-2">
            <Label text={words.labels.name}>
              <input
                className={field}
                required
                maxLength={64}
                value={draft.name}
                onChange={(e) => patch({ name: e.target.value })}
              />
            </Label>
            <Label text={words.labels.installation}>
              <select
                className={field}
                required
                value={clientID}
                onChange={(e) => {
                  setClientID(e.target.value);
                  patch({ agent: "" });
                }}
                disabled={!!editing}
              >
                <option value="">Choose installation</option>
                {clients
                  .filter((c) => !c.managed)
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </Label>
            <Label text={words.labels.repository}>
              <input
                className={field}
                required
                placeholder="owner/repo"
                value={draft.repository}
                onChange={(e) => patch({ repository: e.target.value })}
              />
            </Label>
            <Label text={words.labels.event}>
              <select
                className={field}
                value={draft.event}
                onChange={(e) => {
                  const event = e.target.value as GitHubAutomation["event"];
                  patch({
                    event,
                    action: event === "push" ? "" : event === "check_run" ? "completed" : "labeled",
                    conclusion: event === "check_run" ? "failure" : "",
                    label: "",
                  });
                }}
              >
                <option value="check_run">Check run</option>
                <option value="issues">Issue webhook</option>
                <option value="pull_request">Pull request webhook</option>
                <option value="push">Push webhook</option>
              </select>
            </Label>
            {draft.event !== "push" && (
              <Label text={words.labels.action}>
                <input
                  className={field}
                  required
                  maxLength={64}
                  value={draft.action}
                  onChange={(e) => patch({ action: e.target.value })}
                />
              </Label>
            )}
            {draft.event === "check_run" && (
              <Label text={words.labels.conclusion}>
                <select
                  className={field}
                  value={draft.conclusion ?? ""}
                  onChange={(e) => patch({ conclusion: e.target.value })}
                >
                  <option value="">Any conclusion</option>
                  <option value="failure">Failure</option>
                  <option value="success">Success</option>
                  <option value="cancelled">Cancelled</option>
                  <option value="timed_out">Timed out</option>
                </select>
              </Label>
            )}
            {(draft.event === "issues" || draft.event === "pull_request") && (
              <Label text={words.labels.label}>
                <input
                  className={field}
                  maxLength={128}
                  value={draft.label ?? ""}
                  onChange={(e) => patch({ label: e.target.value })}
                />
              </Label>
            )}
            <Label text={words.labels.agent}>
              <select
                className={field}
                required
                value={draft.agent}
                onChange={(e) => patch({ agent: e.target.value })}
              >
                <option value="">Choose agent</option>
                {allowedAgents.map((a) => (
                  <option key={a.spec.name} value={a.spec.name}>
                    {a.spec.name}
                  </option>
                ))}
              </select>
            </Label>
          </div>
          <Label text={words.labels.instructions}>
            <textarea
              className={field}
              required
              maxLength={16000}
              rows={5}
              value={draft.instructions}
              onChange={(e) => patch({ instructions: e.target.value })}
            />
          </Label>
          <p className="text-aui-sm text-aui-muted">{words.guard}</p>
          <Label text={words.labels.paused}>
            <input
              type="checkbox"
              checked={draft.paused}
              onChange={(e) => patch({ paused: e.target.checked })}
            />
          </Label>
          <div className="flex gap-2">
            <button className={primary} disabled={saving || !client} type="submit">
              {saving ? "Saving…" : words.save}
            </button>
            <button className={button} type="button" onClick={() => setKind(null)}>
              Cancel
            </button>
          </div>
        </form>
      )}
      <section className="flex flex-col gap-3">
        <h2 className="font-medium">{words.schedules}</h2>
        {!schedules.length && <p className="text-aui-muted">{words.noSchedules}</p>}
        {schedules.map((s) => (
          <button
            className={`${card} flex items-center gap-3 text-left`}
            key={s.name}
            type="button"
            onClick={() => onSchedule(s.name)}
          >
            <AgentMark name={s.agent} />
            <span className="flex-1">
              {s.name}
              <span className="block text-aui-sm text-aui-muted">
                {s.expression} · {s.agent}
              </span>
            </span>
            <span>{s.paused ? "Paused" : "Active"}</span>
          </button>
        ))}
      </section>
      <section className="flex flex-col gap-3">
        <h2 className="font-medium">{words.triggers}</h2>
        {!clients.some(
          (c) => c.github?.automations?.length || Object.keys(c.github?.auto_labels ?? {}).length,
        ) && <p className="text-aui-muted">{words.noTriggers}</p>}
        {clients.flatMap((c) =>
          (c.github?.automations ?? []).map((rule) => (
            <div
              className={`${card} flex flex-wrap items-center gap-3`}
              key={`${c.id}/${rule.name}`}
            >
              <AgentMark name={rule.agent} />
              <div className="min-w-0 flex-1">
                <strong>{rule.name}</strong>
                <p className="break-words text-aui-sm text-aui-muted">
                  {rule.event} in {rule.repository} · {rule.action}
                  {rule.conclusion ? ` · ${rule.conclusion}` : ""}
                  {rule.label ? ` · ${rule.label}` : ""}
                </p>
                <p>{rule.instructions}</p>
              </div>
              <span>{rule.paused || !c.enabled ? "Paused" : "Active"}</span>
              {can("clients:write") && !c.managed && (
                <button
                  type="button"
                  className={button}
                  onClick={() => {
                    setEditing({ client: c.id, name: rule.name });
                    setClientID(c.id);
                    setDraft(rule);
                    setKind("github");
                  }}
                >
                  Edit
                </button>
              )}
            </div>
          )),
        )}
        {clients.flatMap((c) =>
          Object.entries(c.github?.auto_labels ?? {}).flatMap(([repo, labels]) =>
            Object.entries(labels).map(([label, agent]) => (
              <div className={card} key={`${c.id}/${repo}/${label}`}>
                <AgentMark name={agent} /> {repo} · Label equals {label} · {agent}
              </div>
            )),
          ),
        )}
      </section>
    </Page>
  );
}
