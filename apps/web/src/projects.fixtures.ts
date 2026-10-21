import type { ProjectView } from "./api.ts";

/** A project as the server sends it, with whatever a test needs set. */
export function projectView(p: Partial<ProjectView> & { name: string }): ProjectView {
  return {
    kind: "team",
    members: [{ who: "*", role: "member" }],
    version: 1,
    created_at: "2026-10-21T09:00:00Z",
    updated_at: "2026-10-21T09:00:00Z",
    depth: p.name === "" ? 0 : 1,
    path: p.name === "" ? [] : [{ name: p.name, kind: p.kind ?? "team" }],
    children: 0,
    conversations: 0,
    active: 0,
    schedules: 0,
    notes: 0,
    proposed: 0,
    access: { role: "member", from: "", via: "*" },
    actions: [],
    ...p,
  };
}
