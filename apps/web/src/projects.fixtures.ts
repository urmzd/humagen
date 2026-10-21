/** A workspace's project tree, for tests and the gallery. Nothing here is used by the app. */
import type { ProjectDetail, ProjectKind, ProjectView } from "./api.ts";

const at = "2026-10-01T09:00:00Z";

/** A project as GET /api/projects lists it, under `parent` (absent for the top level). */
export function view(
  name: string,
  opts: {
    kind?: ProjectKind;
    parent?: ProjectView;
    title?: string;
    description?: string;
    customer?: string;
    instructions?: string;
    conversations?: number;
    active?: number;
    children?: number;
    version?: number;
    managed?: string;
  } = {},
): ProjectView {
  const kind = opts.kind ?? "team";
  const path = [...(opts.parent?.path ?? []), { name, kind, ...(opts.title ? { title: opts.title } : {}) }];
  return {
    name,
    kind,
    ...(opts.title ? { title: opts.title } : {}),
    ...(opts.description ? { description: opts.description } : {}),
    ...(opts.parent ? { parent: opts.parent.name } : {}),
    ...(opts.customer ? { account: { customer: opts.customer } } : {}),
    ...(opts.instructions ? { instructions: opts.instructions } : {}),
    ...(opts.managed ? { managed: opts.managed } : {}),
    experts: [],
    agents: { enable: [] },
    version: opts.version ?? 1,
    created_by: "ada",
    created_at: at,
    updated_at: at,
    depth: path.length,
    path,
    children: opts.children ?? 0,
    conversations: opts.conversations ?? 0,
    active: opts.active ?? 0,
    schedules: 0,
    notes: 0,
    proposed: 0,
  };
}

/** The workspace itself, as GET /api/projects/~ reads it. */
export const workspace: ProjectView = {
  ...view("", { kind: "company", instructions: "Acme sells warehouse robots. Answer plainly." }),
  depth: 0,
  path: [],
  version: 3,
  children: 3,
  conversations: 12,
};

export const news = view("Company news", { kind: "company", conversations: 2 });
export const field = view("Field", { title: "Field engineering", children: 3, conversations: 4, active: 1, instructions: "Customer accounts. Never promise dates." });
export const east = view("East", { parent: field, children: 1, conversations: 1 });
export const retail = view("Retail", { parent: east, children: 1 });
/** Four levels below the workspace: as deep as the tree goes. */
export const stores = view("Stores", { parent: retail, conversations: 3 });
export const globex = view("Globex", {
  kind: "account",
  parent: field,
  customer: "Globex Inc",
  description: "Renewals and support for Globex.",
  children: 1,
  conversations: 6,
  version: 4,
});
export const renewals = view("Globex renewals", { parent: globex });
export const initech = view("Initech", { kind: "account", parent: field, customer: "Initech LLC" });
export const platform = view("Platform", { conversations: 9 });

/** The tree in the order the platform lists it: each project before its sub-projects, siblings by name. */
export const tree: ProjectView[] = [news, field, east, retail, stores, globex, renewals, initech, platform];

/** What GET /api/projects/Globex answers: the workspace first in its ancestors, and its own limit, then Field's, then the workspace's. */
export const globexDetail: ProjectDetail = {
  project: globex,
  ancestors: [workspace, field],
  children: [renewals],
  limits: [
    { scope: { kind: "project", name: "Globex" }, from: "Globex" },
    { scope: { kind: "project", name: "Field" }, from: "Field" },
    { scope: { kind: "tenant" }, from: "" },
  ],
};
