import { defaultCopy } from "@humagen/components";
import { GitHubIcon, SlackIcon } from "@humagen/render-web";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import type { GitHubAutomation } from "../../../packages/client/src/generated/types.ts";
import { api, type Client, type Revision, type ScheduleView } from "./api.ts";
import type { ProjectRef } from "./projects.ts";
import { href } from "./route.ts";
import { ScheduleForm } from "./Schedules.tsx";
import {
  AgentMark,
  agentOptions,
  button,
  card,
  field,
  Label,
  Menu,
  Page,
  Problem,
  primary,
} from "./ui.tsx";

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
  deliver: "github",
};

/** Where a rule's answers go, in a few words, for its row in the list. */
function sentTo(rule: GitHubAutomation, slack: Client[]): string {
  if (rule.deliver === "github") return words.sentToGitHub;
  if (rule.deliver === "slack") {
    const via = slack.find((c) => c.id === rule.slack_client)?.name ?? rule.slack_client;
    return words.sentToSlack(rule.slack_channel ?? "", via ?? "");
  }
  return words.sentToSession;
}

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
  const [slack, setSlack] = useState<Client[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [kind, setKind] = useState<"schedule" | "github" | null>(null);
  const [editing, setEditing] = useState<{ client: string; name: string } | null>(null);
  const [clientID, setClientID] = useState("");
  const [draft, setDraft] = useState<GitHubAutomation>(blank);
  const [saving, setSaving] = useState(false);
  const fail = useCallback(
    (error: unknown) =>
      setProblem(error instanceof Error ? error.message : "Could not save automation."),
    [],
  );
  const reload = useCallback(() => {
    if (can("clients:read"))
      api
        .clients()
        .then((r) => {
          setClients(r.clients.filter((c) => c.surface === "github"));
          setSlack(r.clients.filter((c) => c.surface === "slack" && c.enabled));
        })
        .catch(fail);
  }, [can, fail]);
  useEffect(() => {
    reload();
  }, [reload]);
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
    <Page
      title={words.title}
      lede={
        <>
          {words.lede}{" "}
          <a className="underline underline-offset-2" href={href({ page: "sessions" })}>
            Open Sessions
          </a>
        </>
      }
    >
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
              <Menu
                label={words.labels.installation}
                value={clientID}
                disabled={!!editing}
                options={[
                  { value: "", label: "Choose installation" },
                  ...clients
                    .filter((c) => !c.managed)
                    .map((c) => ({ value: c.id, label: c.name, mark: <GitHubIcon size={14} /> })),
                ]}
                onChange={(id) => {
                  setClientID(id);
                  patch({ agent: "" });
                }}
              />
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
              <Menu
                label={words.labels.event}
                value={draft.event}
                options={[
                  { value: "check_run", label: "Check run" },
                  { value: "issues", label: "Issue" },
                  { value: "pull_request", label: "Pull request" },
                  { value: "push", label: "Push" },
                ]}
                onChange={(value) => {
                  const event = value as GitHubAutomation["event"];
                  patch({
                    event,
                    action: event === "push" ? "" : event === "check_run" ? "completed" : "labeled",
                    conclusion: event === "check_run" ? "failure" : "",
                    label: "",
                  });
                }}
              />
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
                <Menu
                  label={words.labels.conclusion}
                  value={draft.conclusion ?? ""}
                  options={[
                    { value: "", label: "Any conclusion" },
                    { value: "failure", label: "Failure" },
                    { value: "success", label: "Success" },
                    { value: "cancelled", label: "Cancelled" },
                    { value: "timed_out", label: "Timed out" },
                  ]}
                  onChange={(conclusion) => patch({ conclusion })}
                />
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
              <Menu
                label={words.labels.agent}
                value={draft.agent}
                options={[
                  { value: "", label: "Choose agent" },
                  ...agentOptions(allowedAgents.map((a) => a.spec.name)),
                ]}
                onChange={(agent) => patch({ agent })}
              />
            </Label>
            <Label text={words.labels.deliver}>
              <Menu
                label={words.labels.deliver}
                value={draft.deliver ?? "none"}
                options={[
                  {
                    value: "github",
                    label: words.deliverGitHub,
                    hint: words.deliverGitHubHint,
                    mark: <GitHubIcon size={14} />,
                  },
                  {
                    value: "slack",
                    label: words.deliverSlack,
                    hint: words.deliverSlackHint,
                    mark: <SlackIcon size={14} />,
                  },
                  { value: "none", label: words.deliverNone, hint: words.deliverNoneHint },
                ]}
                onChange={(deliver) =>
                  patch(
                    deliver === "slack"
                      ? { deliver: "slack", slack_client: slack[0]?.id ?? "" }
                      : {
                          deliver: deliver as GitHubAutomation["deliver"],
                          slack_client: undefined,
                          slack_channel: undefined,
                        },
                  )
                }
              />
            </Label>
            {draft.deliver === "slack" &&
              (slack.length === 0 ? (
                <p className="text-aui-sm text-aui-muted sm:col-span-2">{words.noSlack}</p>
              ) : (
                <>
                  <Label text={words.labels.slackClient}>
                    <Menu
                      label={words.labels.slackClient}
                      value={draft.slack_client ?? ""}
                      options={slack.map((c) => ({
                        value: c.id,
                        label: c.name,
                        mark: <SlackIcon size={14} />,
                      }))}
                      onChange={(slack_client) => patch({ slack_client })}
                    />
                  </Label>
                  <Label text={words.labels.slackChannel}>
                    <input
                      className={field}
                      required
                      pattern="[CGD][A-Z0-9]{2,31}"
                      placeholder="C0123ABC"
                      value={draft.slack_channel ?? ""}
                      onChange={(e) =>
                        patch({ slack_channel: e.target.value.trim().toUpperCase() })
                      }
                    />
                  </Label>
                </>
              ))}
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
                {s.expression} | {s.agent}
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
                  {rule.event} in {rule.repository} | {rule.action}
                  {rule.conclusion ? ` | ${rule.conclusion}` : ""}
                  {rule.label ? ` | ${rule.label}` : ""}
                </p>
                <p className="text-aui-sm text-aui-muted">{sentTo(rule, slack)}</p>
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
                <AgentMark name={agent} /> {repo} | Label equals {label} | {agent}
              </div>
            )),
          ),
        )}
      </section>
    </Page>
  );
}
