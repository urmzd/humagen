import type { Task } from "./types";

/** A capability grant is named by the access it gives, not its spec key. */
export const changeToolLabel = (tool: string): string =>
  tool === "person_tools" || tool === "mcp:person_tools:write" ? "Personal connection tools" : tool;

export interface ChangePreview {
  change: {
    id: string;
    agent: string;
    kind?: string;
    evaluated: boolean;
    passed: boolean;
    error?: string;
    override_reason?: string;
    scores: { suite: string; before: number; after: number; passed: boolean }[];
  };
  before_prompt: string;
  after_prompt: string;
  tools: string[];
  connections?: {
    name: string;
    service: string;
    scope: "workspace" | "personal";
    status: string;
  }[];
  listens?: { connection: string; place: string; project?: string; enabled?: boolean }[];
}

/** A recorded preview tool result, never a card inferred from model prose. */
export function readAgentChange(task: Task): ChangePreview | undefined {
  if (
    !["preview_agent_change", "preview_workspace_prompt"].includes(task.tool ?? "") ||
    task.status !== "complete" ||
    !task.output
  )
    return;
  try {
    const value = JSON.parse(task.output);
    const change = value?.change;
    if (
      !change ||
      typeof change.id !== "string" ||
      typeof change.agent !== "string" ||
      typeof change.evaluated !== "boolean" ||
      typeof change.passed !== "boolean" ||
      typeof value.before_prompt !== "string" ||
      typeof value.after_prompt !== "string" ||
      (value.tools !== null &&
        (!Array.isArray(value.tools) ||
          !value.tools.every((tool: unknown) => typeof tool === "string")))
    )
      return;
    if (
      change.scores !== null &&
      (!Array.isArray(change.scores) ||
        !change.scores.every((score: unknown) => {
          if (!score || typeof score !== "object") return false;
          const row = score as Record<string, unknown>;
          return (
            typeof row.suite === "string" &&
            typeof row.before === "number" &&
            Number.isFinite(row.before) &&
            typeof row.after === "number" &&
            Number.isFinite(row.after) &&
            typeof row.passed === "boolean"
          );
        }))
    )
      return;
    return {
      change: {
        id: change.id,
        agent: change.agent,
        ...(typeof change.kind === "string" ? { kind: change.kind } : {}),
        evaluated: change.evaluated,
        passed: change.passed,
        scores: change.scores ?? [],
        ...(typeof change.error === "string" ? { error: change.error } : {}),
        ...(typeof change.override_reason === "string"
          ? { override_reason: change.override_reason }
          : {}),
      },
      before_prompt: value.before_prompt,
      after_prompt: value.after_prompt,
      tools: value.tools ?? [],
      ...(Array.isArray(value.connections) &&
      value.connections.every((item: unknown) => {
        if (!item || typeof item !== "object") return false;
        const row = item as Record<string, unknown>;
        return (
          typeof row.name === "string" &&
          typeof row.service === "string" &&
          (row.scope === "workspace" || row.scope === "personal") &&
          typeof row.status === "string"
        );
      })
        ? {
            connections: value.connections.map(
              ({
                name,
                service,
                scope,
                status,
              }: NonNullable<ChangePreview["connections"]>[number]) => ({
                name,
                service,
                scope,
                status,
              }),
            ),
          }
        : {}),
      ...(Array.isArray(value.listens) &&
      value.listens.every((item: unknown) => {
        if (!item || typeof item !== "object") return false;
        const row = item as Record<string, unknown>;
        return (
          typeof row.connection === "string" &&
          typeof row.place === "string" &&
          (row.project === undefined || typeof row.project === "string") &&
          (row.enabled === undefined || typeof row.enabled === "boolean")
        );
      })
        ? {
            listens: value.listens.map(
              ({
                connection,
                place,
                project,
                enabled,
              }: NonNullable<ChangePreview["listens"]>[number]) => ({
                connection,
                place,
                ...(project === undefined ? {} : { project }),
                ...(enabled === undefined ? {} : { enabled }),
              }),
            ),
          }
        : {}),
    };
  } catch {
    return;
  }
}
