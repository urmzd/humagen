/**
 * Projects as the console lays them out. Pure: the pages draw what these
 * return, and the tests check the layout without a browser.
 */
import { defaultCopy } from "@humagen/components";
import type {
  Project,
  ProjectAccount,
  ProjectKind,
  ProjectKindSpec,
  ProjectStep,
  ProjectUpdate,
  ProjectView,
  ProjectVisibility,
} from "./api.ts";

const words = defaultCopy.projects;

/** The kinds humagen defines, in the order the console lists them. */
export const KINDS: ProjectKind[] = ["company", "team", "account"];

/** A built-in kind: what every kind behaves as. */
export type BuiltIn = "company" | "team" | "account";

/** The shape of a kind's name, built in or the workspace's own. */
export const KIND_NAME = /^[a-z][a-z0-9-]{0,31}$/;

/**
 * The built-in kind a kind behaves as, as the platform reads it: itself when
 * humagen defines it, the workspace's `like` for one of its own, and team for a
 * kind the console has not been told of.
 */
export function likeOf(kind: ProjectKind, kinds: ProjectKindSpec[] = []): BuiltIn {
  if ((KINDS as string[]).includes(kind)) return kind as BuiltIn;
  return kinds.find((k) => k.name === kind)?.like === "account" ? "account" : "team";
}

/**
 * Every kind a workspace has, in the order the console lists them:
 * humagen's, then the workspace's own by title, then any a project has that the
 * workspace no longer lists, by name.
 */
export function kindsOf(
  kinds: ProjectKindSpec[] = [],
  projects: Pick<ProjectView, "kind">[] = [],
): ProjectKind[] {
  const own = [...kinds]
    .sort((a, b) => (a.title || a.name).localeCompare(b.title || b.name))
    .map((k) => k.name);
  const left = [...new Set(projects.map((p) => p.kind))]
    .filter((k) => !(KINDS as string[]).includes(k) && !own.includes(k))
    .sort();
  return [...KINDS, ...own, ...left];
}

/** How far one level of a list of projects indents. */
export const INDENT_PX = 16;
/** Projects go at most this many levels below the workspace. */
export const MAX_DEPTH = 4;

/** What people see for a project: its title, else its name. The workspace is called by the workspace's name. */
export function titleOf(
  p: Pick<Project, "name" | "title"> | ProjectStep,
  workspace = words.workspace,
): string {
  return p.name === "" ? workspace : p.title || p.name;
}

/** A project the reader can name, for a picker or a sentence that names one by its title. */
export type ProjectRef = Pick<Project, "name" | "title">;

/** Display context names without exposing generated personal project identifiers. */
export function readableProject(
  name: string,
  projects: readonly ProjectRef[] = [],
  workspace = words.workspace,
  personal?: string,
): string {
  if (!name || name === "~") return workspace;
  if (name === personal) return words.personalTitle;
  const title = projects.find((project) => project.name === name)?.title;
  if (title && title !== name) return title;
  return /^personal-[a-z0-9-]+$/i.test(name) ? words.personalTitle : title || name;
}

/**
 * A project by what people see for it, its title, found by name among the
 * projects the reader has. One they do not have is named as it was given.
 */
export function titleIn(projects: readonly ProjectRef[], name: string): string {
  const p = projects.find((x) => x.name === name);
  return p ? titleOf(p) : name;
}

/** A picker's options for the projects the reader has, each by its title. */
export const projectOptions = (projects: readonly ProjectRef[]) =>
  projects.map((p) => ({ value: p.name, label: titleOf(p) }));

/**
 * A kind in words: "Account", or the title the workspace gives its own. The
 * workspace's kinds are not optional: without them its own kinds show by
 * their lowercase names.
 */
export const kindWord = (kind: ProjectKind, kinds: ProjectKindSpec[]): string =>
  words.kind[kind] ?? (kinds.find((k) => k.name === kind)?.title || kind);

/** Projects of a kind, as the Projects page's kind filter names them: "Accounts", or the title the workspace gives its own. */
export const groupWord = (kind: ProjectKind, kinds: ProjectKindSpec[]): string =>
  words.group[kind] ?? (kinds.find((k) => k.name === kind)?.title || kind);

/** The projects above one, top first, by title. Empty at the top level. */
export function above(p: ProjectView): string[] {
  return p.path.slice(0, -1).map((step) => titleOf(step));
}

/** Whether a search finds a project: by name, title, description or customer. */
export function matches(p: ProjectView, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [p.name, p.title, p.description, p.account?.customer].some((s) =>
    s?.toLowerCase().includes(q),
  );
}

/** How the Projects page orders its cards: by last activity, the default, by title, or newest first. */
export type CardSort = "activity" | "name" | "created";

/** When something last happened in a project: its activity, else when its record changed, else when it was made. */
export const lastActive = (
  p: Pick<ProjectView, "active_at" | "updated_at" | "created_at">,
): string => p.active_at || p.updated_at || p.created_at;

/** Whether only a project's members read it: no entry names everyone. */
export const isPrivate = (p: Pick<ProjectView, "members">): boolean =>
  !p.members.some((m) => m.who === "*");

/**
 * How widely a project is read, as its page says it: the server's
 * `visibility` when it sends one, else read from its entries as the server
 * does. A Personal project, or one whose one entry is a person, is private;
 * an entry for everyone makes it the workspace's; anything else is shared.
 */
export function visibilityOf(
  p: Pick<ProjectView, "members" | "personal_owner"> & { visibility?: ProjectVisibility },
): ProjectVisibility {
  if (p.visibility) return p.visibility;
  if (p.personal_owner) return "private";
  if (!isPrivate(p)) return "workspace";
  const [only, ...rest] = p.members;
  return only && rest.length === 0 && !only.who.startsWith("role:") ? "private" : "shared";
}

const newest = (a: string, b: string) => (Date.parse(b) || 0) - (Date.parse(a) || 0);
const byTitle = (a: ProjectView, b: ProjectView) =>
  titleOf(a).localeCompare(titleOf(b), undefined, { sensitivity: "base" }) ||
  a.name.localeCompare(b.name);

/**
 * The Projects page's cards: every project the reader has but the workspace,
 * sub-projects included and none nested, narrowed to a kind and a search.
 * Pinned ones come first, and each part is in the order asked for: last
 * activity newest first, title A to Z, or creation newest first.
 */
export function cardsOf(
  projects: ProjectView[],
  opts: { q?: string; kind?: ProjectKind; sort?: CardSort; pinned?: readonly string[] } = {},
): ProjectView[] {
  const pins = new Set(opts.pinned ?? []);
  const order = (a: ProjectView, b: ProjectView) =>
    opts.sort === "name"
      ? byTitle(a, b)
      : (opts.sort === "created"
          ? newest(a.created_at, b.created_at)
          : newest(lastActive(a), lastActive(b))) || byTitle(a, b);
  return projects
    .filter(
      (p) => p.name !== "" && (!opts.kind || p.kind === opts.kind) && matches(p, opts.q ?? ""),
    )
    .sort((a, b) => Number(pins.has(b.name)) - Number(pins.has(a.name)) || order(a, b));
}

/** Where a new project would sit: the workspace, or a project. */
export type Place = Pick<ProjectView, "name" | "kind" | "depth">;
/** The workspace as a place: the root, company-wide, at depth 0. */
export const WORKSPACE: Place = { name: "", kind: "company", depth: 0 };

/**
 * Whether a project of `kind` may sit under `parent`, as the platform rules,
 * each kind read as the built-in it behaves as: room below it, company-wide
 * only under the workspace or another company-wide project, and an account
 * never under an account.
 */
export function fitsUnder(
  parent: Place,
  kind: ProjectKind,
  kinds: ProjectKindSpec[] = [],
): boolean {
  if (parent.depth >= MAX_DEPTH) return false;
  const like = likeOf(kind, kinds);
  if (like === "company") return parent.kind === "company";
  if (like === "account") return likeOf(parent.kind, kinds) !== "account";
  return true;
}

/** The kinds a person may make under `parent`, humagen's then the workspace's own. Company-wide needs projects:govern. */
export function kindsUnder(
  parent: Place,
  canGovern: boolean,
  kinds: ProjectKindSpec[] = [],
): ProjectKind[] {
  return kindsOf(kinds).filter(
    (k) => fitsUnder(parent, k, kinds) && (k !== "company" || canGovern),
  );
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
  const kept = Object.fromEntries(
    Object.entries(a)
      .filter(([, v]) => typeof v === "string" && v.trim() !== "")
      .map(([k, v]) => [k, (v as string).trim()]),
  );
  return Object.keys(kept).length > 0 ? (kept as ProjectAccount) : undefined;
}

/**
 * What PUT /api/projects/{name} is sent: every field a person may change, from
 * the version read, with `change` over them. The platform clears a field left
 * out, so each one not changed is carried as it was, and an emptied one is
 * left out to clear it.
 */
export function updateOf(
  p: Project,
  change: Partial<Omit<ProjectUpdate, "version">> = {},
): ProjectUpdate {
  const next = {
    title: p.title,
    description: p.description,
    account: p.account,
    instructions: p.instructions,
    ...change,
  };
  const account = accountOf(next.account);
  return {
    version: p.version,
    ...(next.title?.trim() ? { title: next.title.trim() } : {}),
    ...(next.description?.trim() ? { description: next.description.trim() } : {}),
    ...(account ? { account } : {}),
    ...(next.instructions?.trim() ? { instructions: next.instructions } : {}),
  };
}

/** Who sees an agent: everyone in the workspace, one person, or a project's members. */
export type AgentAccess =
  | { kind: "workspace" }
  | { kind: "private"; project: string }
  | { kind: "members"; project: string; title: string };

/** A home as a project name: "" for the workspace (none or "~"), "personal-x" for "~personal-x". */
export const homeName = (home?: string): string => (home ?? "").replace(/^~/, "");

/**
 * Who sees an agent homed in `home`. A home in a Personal project (the
 * reader's, or one the tree marks as someone's) is private; any other
 * project's members see it; no home is the workspace.
 */
export function accessOf(
  home: string | undefined,
  personal: string | undefined,
  tree: readonly ProjectView[],
): AgentAccess {
  const project = homeName(home);
  if (!project) return { kind: "workspace" };
  const p = tree.find((x) => x.name === project);
  if (
    project === homeName(personal) ||
    p?.personal_owner ||
    (!p && /^personal-[a-z0-9-]+$/i.test(project))
  ) {
    return { kind: "private", project };
  }
  return { kind: "members", project, title: p ? titleOf(p) : project };
}

/** Who sees an agent, in words: "Workspace", "Private", "Members of Field". */
export function accessLabel(access: AgentAccess): string {
  const w = defaultCopy.agentAccess;
  if (access.kind === "private") return w.private;
  if (access.kind === "members") return w.membersOf(access.title);
  return w.workspace;
}
