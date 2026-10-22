/**
 * The project tree as the console lays it out. Pure: the pages draw what these
 * return, and the tests check the layout without a browser.
 */
import { defaultCopy } from "@warp/components";
import type { Project, ProjectAccount, ProjectKind, ProjectStep, ProjectUpdate, ProjectView } from "./api.ts";

const words = defaultCopy.projects;

/** The kinds, in the order the Projects page groups them. */
export const KINDS: ProjectKind[] = ["company", "team", "account"];

/** How far one level of the tree indents. */
export const INDENT_PX = 16;
/** On a narrow screen the tree indents at most this many levels, and deeper rows say where they sit. */
export const NARROW_INDENT = 1;
/** A viewport narrower than this is a phone, where the tree stops nesting. */
export const NARROW_BELOW = 640;
/** Projects go at most this many levels below the workspace. */
export const MAX_DEPTH = 4;

export const narrowAt = (width: number): boolean => width < NARROW_BELOW;

/** What people see for a project: its title, else its name. The workspace is called by the workspace's name. */
export function titleOf(p: Pick<Project, "name" | "title"> | ProjectStep, workspace = words.workspace): string {
  return p.name === "" ? workspace : p.title || p.name;
}

/** A kind in words: "Account". */
export const kindWord = (kind: ProjectKind): string => words.kind[kind] ?? kind;

/** The projects above one, top first, by title. Empty at the top level. */
export function above(p: ProjectView): string[] {
  return p.path.slice(0, -1).map((step) => titleOf(step));
}

/** Whether a search finds a project: by name, title, description or customer. */
export function matches(p: ProjectView, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [p.name, p.title, p.description, p.account?.customer].some((s) => s?.toLowerCase().includes(q));
}

/** One row of the tree. */
export interface TreeRow {
  project: ProjectView;
  /** Levels to indent, each INDENT_PX wide. */
  indent: number;
  /**
   * The projects above it, top first, when the indent does not show where it
   * sits: its parent is in another group or left out by a search, or it is
   * deeper than a narrow screen nests. Empty when the row it sits under is
   * its parent.
   */
  path: string[];
}

export interface TreeGroup {
  kind: ProjectKind;
  title: string;
  rows: TreeRow[];
}

/**
 * The Projects page's groups: Company-wide, Teams, then Accounts, each in tree
 * order. A row nests under its parent when the parent is in the same group
 * and shown; otherwise it starts the group's left edge and prints its path.
 * On a narrow screen nothing nests deeper than one level.
 */
export function treeOf(projects: ProjectView[], opts: { narrow: boolean; q?: string; kind?: ProjectKind }): TreeGroup[] {
  const shown = projects.filter((p) => (!opts.kind || p.kind === opts.kind) && matches(p, opts.q ?? ""));
  return KINDS.map((kind) => {
    const level = new Map<string, number>();
    const rows = shown
      .filter((p) => p.kind === kind)
      .map((p): TreeRow => {
        const parent = level.get(p.parent ?? "");
        const depth = parent === undefined ? 0 : parent + 1;
        level.set(p.name, depth);
        const indent = opts.narrow ? Math.min(depth, NARROW_INDENT) : depth;
        // The indent says where a row sits only when it nests at its full depth.
        const placed = depth > 0 && indent === depth;
        return { project: p, indent, path: placed ? [] : above(p) };
      });
    return { kind, title: words.group[kind] ?? kind, rows };
  }).filter((g) => g.rows.length > 0);
}

/** Where a new project would sit: the workspace, or a project. */
export type Place = Pick<ProjectView, "name" | "kind" | "depth">;
/** The workspace as a place: the root, company-wide, at depth 0. */
export const WORKSPACE: Place = { name: "", kind: "company", depth: 0 };

/**
 * Whether a project of `kind` may sit under `parent`, as the platform rules:
 * room below it, company-wide only under the workspace or another company-wide
 * project, and an account never under an account.
 */
export function fitsUnder(parent: Place, kind: ProjectKind): boolean {
  if (parent.depth >= MAX_DEPTH) return false;
  if (kind === "company") return parent.kind === "company";
  if (kind === "account") return parent.kind !== "account";
  return true;
}

/** The kinds a person may make under `parent`. Company-wide needs projects:govern. */
export function kindsUnder(parent: Place, canGovern: boolean): ProjectKind[] {
  return KINDS.filter((k) => fitsUnder(parent, k) && (k !== "company" || canGovern));
}

const NAME = /^[A-Za-z0-9][A-Za-z0-9 _-]{0,62}$/;

/** Whether a name can be a project's: what the platform accepts. */
export const validName = (name: string): boolean => NAME.test(name);

/** A name made from a title, for the form to offer until the person types their own. */
export function nameFrom(title: string): string {
  return title
    .replace(/[^A-Za-z0-9 _-]/g, "")
    .replace(/^[^A-Za-z0-9]+/, "")
    .replace(/\s+/g, " ")
    .slice(0, 63)
    .trimEnd();
}

/** An account with its empty fields left out, or nothing when every field is empty. */
export function accountOf(a: ProjectAccount | undefined): ProjectAccount | undefined {
  if (!a) return undefined;
  const kept = Object.fromEntries(Object.entries(a).filter(([, v]) => typeof v === "string" && v.trim() !== "").map(([k, v]) => [k, (v as string).trim()]));
  return Object.keys(kept).length > 0 ? (kept as ProjectAccount) : undefined;
}

/**
 * What PUT /api/projects/{name} is sent: every field a person may change, from
 * the version read, with `change` over them. The platform clears a field left
 * out, so each one not changed is carried as it was, and an emptied one is
 * left out to clear it.
 */
export function updateOf(p: Project, change: Partial<Omit<ProjectUpdate, "version">> = {}): ProjectUpdate {
  const next = { title: p.title, description: p.description, account: p.account, instructions: p.instructions, ...change };
  const account = accountOf(next.account);
  return {
    version: p.version,
    ...(next.title?.trim() ? { title: next.title.trim() } : {}),
    ...(next.description?.trim() ? { description: next.description.trim() } : {}),
    ...(account ? { account } : {}),
    ...(next.instructions?.trim() ? { instructions: next.instructions } : {}),
  };
}
