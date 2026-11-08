import { EllipsisIcon } from "@humagen/render-web";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@humagen/render-web/ui/dropdown-menu";
import { type FormEvent, useEffect, useState } from "react";
import type { GitHubAutomation } from "../../../packages/client/src/generated/types.ts";
import { api, type Client } from "./api.ts";
import { Pill } from "./cards.tsx";
import { button, field, Label, Problem, primary, rows } from "./ui.tsx";

type Event = GitHubAutomation["event"];

type EventKind = { id: Event; label: string; actions: string[] };

const pullRequest: EventKind = {
  id: "pull_request",
  label: "Pull request",
  actions: ["opened", "ready_for_review", "synchronize", "labeled", "closed"],
};

const events: EventKind[] = [
  pullRequest,
  { id: "issues", label: "Issue", actions: ["opened", "labeled", "reopened", "closed"] },
  { id: "check_run", label: "Check run", actions: ["completed"] },
  { id: "push", label: "Push", actions: [] },
];

/** A rule as one line: what happens, and where. */
export function ruleWords(r: GitHubAutomation): string {
  const what = events.find((e) => e.id === r.event)?.label ?? r.event;
  return [
    r.event === "push" ? what : `${what} ${r.action.replaceAll("_", " ")}`,
    r.label && `labeled ${r.label}`,
    r.conclusion && `ending ${r.conclusion}`,
  ]
    .filter(Boolean)
    .join(", ");
}

/** The agents a client may send a rule's session to: its default and the ones it routes to. */
function routable(client: Client, all: string[]): string[] {
  if (client.agents?.includes("*")) return all;
  return [...new Set([client.default_agent ?? "", ...(client.agents ?? [])])].filter(Boolean);
}

/**
 * The GitHub events that start a session on this app, and the agent each
 * one goes to. Saved on the app's own record, as the rest of its settings.
 */
export function GitHubRules({
  client,
  canWrite,
  onChanged,
}: {
  client: Client;
  canWrite: boolean;
  onChanged: () => void;
}) {
  const rules = client.github?.automations ?? [];
  const [agents, setAgents] = useState<string[]>([]);
  const [editing, setEditing] = useState<number | "new" | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    if (!canWrite) return;
    let live = true;
    api
      .agents()
      .then((list) => live && setAgents(list.map((r) => r.spec.name)))
      .catch(() => live && setAgents([]));
    return () => {
      live = false;
    };
  }, [canWrite]);
  const save = async (next: GitHubAutomation[]) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      await api.saveClient(
        { ...client, github: { ...client.github, automations: next } },
        client.enabled,
      );
      setEditing(null);
      onChanged();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : "The rules could not be saved.");
    } finally {
      setBusy(false);
    }
  };
  const choices = routable(client, agents);
  return (
    <section aria-label="Start a session when" className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h4 className="text-aui-sm font-medium">Start a session when…</h4>
        {canWrite && editing === null && (
          <button type="button" className={button} onClick={() => setEditing("new")}>
            Add Rule
          </button>
        )}
      </div>
      <Problem message={problem} />
      {rules.length > 0 && (
        <ul className={rows}>
          {rules.map((r, i) => (
            <li key={r.name} aria-label={r.name} className="flex min-w-0 items-center gap-2 py-2">
              <div className="min-w-0 flex-1">
                <p className="truncate text-aui-sm">{ruleWords(r)}</p>
                <p className="truncate text-aui-xs text-aui-muted">
                  {r.repository} → @{r.agent}
                </p>
              </div>
              {r.paused && <Pill>Paused</Pill>}
              {canWrite && (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    aria-label={`More for ${r.name}`}
                    className="inline-flex size-8 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11"
                  >
                    <EllipsisIcon size={16} />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem onSelect={() => setEditing(i)}>Edit</DropdownMenuItem>
                    <DropdownMenuItem
                      disabled={busy}
                      onSelect={() =>
                        void save(rules.map((x, j) => (j === i ? { ...x, paused: !x.paused } : x)))
                      }
                    >
                      {r.paused ? "Resume" : "Pause"}
                    </DropdownMenuItem>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      variant="destructive"
                      disabled={busy}
                      onSelect={() => void save(rules.filter((_, j) => j !== i))}
                    >
                      Remove
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </li>
          ))}
        </ul>
      )}
      {editing !== null && (
        <RuleForm
          key={String(editing)}
          initial={editing === "new" ? undefined : rules[editing]}
          agents={choices}
          taken={rules.filter((_, j) => j !== editing).map((r) => r.name)}
          busy={busy}
          onCancel={() => setEditing(null)}
          onSave={(rule) =>
            void save(
              editing === "new"
                ? [...rules, rule]
                : rules.map((x, j) => (j === editing ? rule : x)),
            )
          }
        />
      )}
    </section>
  );
}

function RuleForm({
  initial,
  agents,
  taken,
  busy,
  onCancel,
  onSave,
}: {
  initial?: GitHubAutomation;
  agents: string[];
  taken: string[];
  busy: boolean;
  onCancel: () => void;
  onSave: (rule: GitHubAutomation) => void;
}) {
  const [rule, setRule] = useState<GitHubAutomation>(
    initial ?? {
      name: "",
      repository: "",
      event: "pull_request",
      action: "opened",
      agent: agents[0] ?? "",
      instructions: "",
      paused: false,
    },
  );
  const [problem, setProblem] = useState<string | null>(null);
  const set = (patch: Partial<GitHubAutomation>) => setRule((r) => ({ ...r, ...patch }));
  const event = events.find((e) => e.id === rule.event) ?? pullRequest;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const next = {
      ...rule,
      name: rule.name.trim(),
      repository: rule.repository.trim(),
      action: rule.event === "push" ? "" : rule.action.trim(),
      label: rule.label?.trim() || undefined,
      conclusion: rule.event === "check_run" ? rule.conclusion?.trim() || undefined : undefined,
      instructions: rule.instructions.trim(),
    };
    const why = !next.name
      ? "Give the rule a name."
      : taken.includes(next.name)
        ? `A rule is already named ${next.name}.`
        : !/^[\w.-]+\/[\w.-]+$/.test(next.repository)
          ? "The repository is owner/name."
          : next.event !== "push" && !next.action
            ? "Say which action starts it."
            : !next.agent
              ? "Choose the agent it goes to."
              : !next.instructions
                ? "Say what the agent should do."
                : null;
    setProblem(why);
    if (!why) onSave(next);
  };
  return (
    <form
      aria-label={initial ? `Edit ${initial.name}` : "New rule"}
      className="flex min-w-0 flex-col gap-3"
      onSubmit={submit}
      noValidate
    >
      <Problem message={problem} />
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
        <Label text="Name">
          <input
            className={field}
            value={rule.name}
            maxLength={64}
            onChange={(e) => set({ name: e.target.value })}
            autoComplete="off"
          />
        </Label>
        <Label text="Repository">
          <input
            className={field}
            value={rule.repository}
            placeholder="owner/name"
            onChange={(e) => set({ repository: e.target.value })}
            autoComplete="off"
            spellCheck={false}
          />
        </Label>
        <Label text="Event">
          <select
            className={field}
            value={rule.event}
            onChange={(e) => {
              const next = events.find((x) => x.id === e.target.value) ?? pullRequest;
              set({ event: next.id, action: next.actions[0] ?? "" });
            }}
          >
            {events.map((x) => (
              <option key={x.id} value={x.id}>
                {x.label}
              </option>
            ))}
          </select>
        </Label>
        {rule.event !== "push" && (
          <Label text="Action">
            <select
              className={field}
              value={event.actions.includes(rule.action) ? rule.action : ""}
              onChange={(e) => set({ action: e.target.value })}
            >
              {!event.actions.includes(rule.action) && <option value="">{rule.action}</option>}
              {event.actions.map((a) => (
                <option key={a} value={a}>
                  {a.replaceAll("_", " ")}
                </option>
              ))}
            </select>
          </Label>
        )}
        {(rule.event === "pull_request" || rule.event === "issues") && (
          <Label text="Label" hint="Optional.">
            <input
              className={field}
              value={rule.label ?? ""}
              maxLength={128}
              onChange={(e) => set({ label: e.target.value })}
              autoComplete="off"
            />
          </Label>
        )}
        {rule.event === "check_run" && (
          <Label text="Conclusion" hint="Optional, such as failure.">
            <input
              className={field}
              value={rule.conclusion ?? ""}
              maxLength={64}
              onChange={(e) => set({ conclusion: e.target.value })}
              autoComplete="off"
            />
          </Label>
        )}
        <Label text="Agent">
          <select
            className={field}
            value={rule.agent}
            onChange={(e) => set({ agent: e.target.value })}
          >
            {!agents.includes(rule.agent) && (
              <option value={rule.agent}>{rule.agent || "Choose"}</option>
            )}
            {agents.map((a) => (
              <option key={a} value={a}>
                @{a}
              </option>
            ))}
          </select>
        </Label>
      </div>
      <Label text="Instructions">
        <textarea
          className={`${field} min-h-24`}
          value={rule.instructions}
          maxLength={16000}
          onChange={(e) => set({ instructions: e.target.value })}
        />
      </Label>
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className={primary} disabled={busy}>
          Save Rule
        </button>
      </div>
    </form>
  );
}
