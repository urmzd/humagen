import { defaultCopy } from "@humagen/components";
import { CalendarIcon, GitHubIcon, SlackIcon } from "@humagen/render-web";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import type { GitHubAutomation } from "../../../packages/client/src/generated/types.ts";
import { api, type Client, type Revision, type ScheduleView } from "./api.ts";
import { AgentTag, ItemCard, Pill, Tag } from "./cards.tsx";
import { inWords } from "./cron.ts";
import type { ProjectRef } from "./projects.ts";
import { href } from "./route.ts";
import { ScheduleForm } from "./Schedules.tsx";
import {
  agentOptions,
  ago,
  button,
  card,
  Empty,
  field,
  Label,
  Menu,
  Page,
  Problem,
  primary,
  Segmented,
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

/** Where a rule's answers go, in a few words, for its card. */
function sentTo(rule: GitHubAutomation, slack: Client[]): string {
  if (rule.deliver === "github") return words.sentToGitHub;
  if (rule.deliver === "slack") {
    const via = slack.find((c) => c.id === rule.slack_client)?.name ?? rule.slack_client;
    return words.sentToSlack(rule.slack_channel ?? "", via ?? "");
  }
  return words.sentToSession;
}

/** What fires a GitHub rule, as a short phrase: "Check run failure in owner/repo". */
function firedBy(rule: GitHubAutomation): string {
  const event = words.events[rule.event as keyof typeof words.events] ?? rule.event;
  const detail = rule.conclusion || rule.label || (rule.event === "push" ? "" : rule.action);
  return `${event}${detail ? ` ${detail}` : ""} in ${rule.repository}`;
}

type Kind = "schedule" | "github";

/**
 * Scheduled work: one list of automations, each fired at set times or by a
 * GitHub event, each run a session. One button makes either kind.
 */
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
  const [kind, setKind] = useState<Kind | null>(null);
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
  const installations = clients.filter((c) => !c.managed);
  // With nothing picked yet (or one installation, so no picker), the first installation is it.
  const chosenID = clientID || installations[0]?.id || "";
  const client = clients.find((c) => c.id === chosenID);
  const allowedAgents = agents.filter(
    (a) =>
      client?.agents?.includes("*") ||
      client?.default_agent === a.spec.name ||
      client?.agents?.includes(a.spec.name),
  );
  const canSchedule = can("schedules:write");
  const canGitHub = can("clients:write");
  const start = () => {
    setProblem(null);
    setEditing(null);
    setDraft(blank);
    setClientID(installations[0]?.id ?? "");
    setKind(canSchedule ? "schedule" : "github");
  };
  const close = () => {
    setKind(null);
    setEditing(null);
  };
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
      close();
      reload();
    } catch (error) {
      fail(error);
    } finally {
      setSaving(false);
    }
  };
  const rules = clients.flatMap((c) =>
    (c.github?.automations ?? []).map((rule) => ({ client: c, rule })),
  );
  const empty = schedules.length === 0 && rules.length === 0;
  return (
    <Page
      title={words.title}
      lede={words.lede}
      actions={
        !kind &&
        (canSchedule || canGitHub) && (
          <button className={primary} type="button" onClick={start}>
            {words.newSchedule}
          </button>
        )
      }
    >
      {problem && <Problem message={problem} />}
      {kind && (
        <div className="flex min-w-0 flex-col gap-3">
          {!editing && canSchedule && canGitHub && (
            <Segmented<Kind>
              label={words.runs}
              value={kind}
              options={[
                { id: "schedule", label: words.atTimes, icon: <CalendarIcon size={14} /> },
                { id: "github", label: words.onGitHub, icon: <GitHubIcon size={14} /> },
              ]}
              onChange={setKind}
            />
          )}
          {kind === "schedule" ? (
            <ScheduleForm
              agents={agents}
              projects={projects}
              onClose={close}
              onSaved={() => {
                close();
                onChanged();
              }}
            />
          ) : installations.length === 0 ? (
            <div className={`${card} flex flex-wrap items-center justify-between gap-3`}>
              <p className="text-aui-sm">{words.noGitHub}</p>
              <div className="flex gap-2">
                <button className={button} type="button" onClick={close}>
                  Cancel
                </button>
                <a className={primary} href={href({ page: "connections" })}>
                  {words.openConnections}
                </a>
              </div>
            </div>
          ) : (
            <form onSubmit={save} className={`${card} flex min-w-0 flex-col gap-4`}>
              <h2 className="font-medium">
                {editing ? words.editTitle(editing.name) : words.newTitle}
              </h2>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <Label text={words.labels.name}>
                  <input
                    className={field}
                    required
                    maxLength={64}
                    disabled={!!editing}
                    value={draft.name}
                    onChange={(e) => patch({ name: e.target.value })}
                  />
                </Label>
                {installations.length > 1 && (
                  <Label text={words.labels.installation}>
                    <Menu
                      label={words.labels.installation}
                      value={chosenID}
                      disabled={!!editing}
                      options={installations.map((c) => ({
                        value: c.id,
                        label: c.name,
                        mark: <GitHubIcon size={14} />,
                      }))}
                      onChange={(id) => {
                        setClientID(id);
                        patch({ agent: "" });
                      }}
                    />
                  </Label>
                )}
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
                    options={Object.entries(words.events).map(([value, label]) => ({
                      value,
                      label,
                    }))}
                    onChange={(value) => {
                      const event = value as GitHubAutomation["event"];
                      patch({
                        event,
                        action:
                          event === "push" ? "" : event === "check_run" ? "completed" : "opened",
                        conclusion: event === "check_run" ? "failure" : "",
                        label: "",
                      });
                    }}
                  />
                </Label>
                {draft.event === "check_run" ? (
                  <Label text={words.labels.conclusion}>
                    <Menu
                      label={words.labels.conclusion}
                      value={draft.conclusion ?? ""}
                      options={[
                        { value: "", label: "Any" },
                        { value: "failure", label: "Failure" },
                        { value: "success", label: "Success" },
                        { value: "cancelled", label: "Cancelled" },
                        { value: "timed_out", label: "Timed out" },
                      ]}
                      onChange={(conclusion) => patch({ conclusion })}
                    />
                  </Label>
                ) : (
                  draft.event !== "push" && (
                    <Label text={words.labels.action}>
                      <input
                        className={field}
                        required
                        maxLength={64}
                        value={draft.action}
                        onChange={(e) => patch({ action: e.target.value })}
                      />
                    </Label>
                  )
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
                        mark: <GitHubIcon size={14} />,
                      },
                      { value: "slack", label: words.deliverSlack, mark: <SlackIcon size={14} /> },
                      { value: "none", label: words.deliverNone },
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
                      {slack.length > 1 && (
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
                      )}
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
                  rows={4}
                  value={draft.instructions}
                  onChange={(e) => patch({ instructions: e.target.value })}
                />
              </Label>
              <div className="flex flex-wrap items-center gap-2">
                <label className="mr-auto inline-flex items-center gap-2 text-aui-sm">
                  <input
                    type="checkbox"
                    checked={draft.paused}
                    onChange={(e) => patch({ paused: e.target.checked })}
                  />
                  {words.labels.paused}
                </label>
                <button className={button} type="button" onClick={close}>
                  Cancel
                </button>
                <button className={primary} disabled={saving || !client} type="submit">
                  {saving ? "Saving…" : words.save}
                </button>
              </div>
            </form>
          )}
        </div>
      )}
      {empty && !kind ? (
        <Empty title={words.empty} />
      ) : (
        <ul
          className="grid min-w-0 grid-cols-[repeat(auto-fill,minmax(min(100%,20rem),1fr))] gap-3"
          aria-label={words.title}
        >
          {schedules.map((s) => (
            <li key={`schedule/${s.name}`} className="min-w-0">
              <ItemCard
                title={s.name}
                onOpen={() => onSchedule(s.name)}
                time={s.recent[0] ? ago(s.recent[0].created_at) : undefined}
                facts={
                  <>
                    <Pill tone={s.paused ? "neutral" : "ok"}>
                      {s.paused ? words.paused : words.active}
                    </Pill>
                    <Tag icon={<CalendarIcon size={12} />}>{inWords(s.expression)}</Tag>
                    <AgentTag name={s.agent} />
                  </>
                }
              />
            </li>
          ))}
          {rules.map(({ client: c, rule }) => (
            <li key={`github/${c.id}/${rule.name}`} className="min-w-0">
              <ItemCard
                title={rule.name}
                onOpen={
                  canGitHub && !c.managed
                    ? () => {
                        setProblem(null);
                        setEditing({ client: c.id, name: rule.name });
                        setClientID(c.id);
                        setDraft(rule);
                        setKind("github");
                      }
                    : undefined
                }
                facts={
                  <>
                    <Pill tone={rule.paused || !c.enabled ? "neutral" : "ok"}>
                      {rule.paused || !c.enabled ? words.paused : words.active}
                    </Pill>
                    <Tag icon={<GitHubIcon size={12} />}>{firedBy(rule)}</Tag>
                    <AgentTag name={rule.agent} />
                    <Tag icon={rule.deliver === "slack" ? <SlackIcon size={12} /> : undefined}>
                      {sentTo(rule, slack)}
                    </Tag>
                  </>
                }
              />
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}
