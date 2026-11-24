import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { type AgentSpec, api } from "./api.ts";
import { field, Label, type Pickable, PickMany, Problem, Section } from "./ui.tsx";

type Review = NonNullable<AgentSpec["review"]>;
export type ReviewApprove = NonNullable<Review["approve"]>;
export type ReviewBlockOn = NonNullable<Review["block_on"]>;

/** What an agent's spec says when it says nothing: never approve, and block on errors. */
export const REVIEW_DEFAULTS = { approve: "never", block_on: "error" } as const satisfies Review;
const APPROVALS: readonly ReviewApprove[] = ["never", "when_clean"];
const BLOCKS: readonly ReviewBlockOn[] = ["error", "warning", "info"];
/** In `skills.loadable`, alone: any published skill of the run's project chain. */
const ANY = "*";

/** The review policy the spec holds, with its defaults filled in. */
export function reviewOf(spec: AgentSpec): { approve: ReviewApprove; block_on: ReviewBlockOn } {
  return {
    approve: spec.review?.approve ?? REVIEW_DEFAULTS.approve,
    block_on: spec.review?.block_on ?? REVIEW_DEFAULTS.block_on,
  };
}

/**
 * `obj` with `key` set to `value`, in the place the key already holds, so a
 * draft compared with what was saved differs only where it changed. Undefined
 * takes the key out.
 */
function put<T extends object, K extends keyof T>(obj: T, key: K, value: T[K] | undefined): T {
  if (value === undefined) {
    const { [key]: _, ...rest } = obj;
    return rest as T;
  }
  if (!(key in obj)) return { ...obj, [key]: value };
  return Object.fromEntries(Object.entries(obj).map(([k, v]) => [k, k === key ? value : v])) as T;
}

/**
 * The spec's review policy with `patch` applied. A field the patch does not
 * name is kept exactly as the spec holds it, written or left out. A patched
 * value that matches what was saved goes back to its saved form, so changing
 * a choice and changing it back leaves nothing unsaved. Every other field of
 * the spec is kept, in its place.
 */
export function withReview(spec: AgentSpec, patch: Partial<Review>, saved?: AgentSpec): AgentSpec {
  let review: Review = spec.review ?? {};
  for (const key of ["approve", "block_on"] as const) {
    if (!(key in patch)) continue;
    const value = patch[key];
    const back = saved && reviewOf(saved)[key] === value;
    review = put(review, key, (back ? saved?.review?.[key] : value) as never);
  }
  const unchanged = saved && JSON.stringify(review) === JSON.stringify(saved.review ?? {});
  return put(spec, "review", unchanged ? saved.review : review);
}

/**
 * The spec with its loadable skills set, in their place. The names that were
 * saved go back to their saved form; none leaves `skills` out. Every other
 * field is kept.
 */
export function withLoadable(spec: AgentSpec, names: string[], saved?: AgentSpec): AgentSpec {
  if (saved && JSON.stringify(names) === JSON.stringify(saved.skills?.loadable ?? []))
    return put(spec, "skills", saved.skills);
  return put(spec, "skills", names.length ? put(spec.skills ?? {}, "loadable", names) : undefined);
}

/**
 * How the agent's pull request reviews decide: whether the host may approve a
 * clean one, and the least severity that requests changes. Saved with the
 * agent's next revision.
 */
export function ReviewSettings({
  spec,
  saved,
  editable,
  onChange,
}: {
  spec: AgentSpec;
  /** The spec as saved, which a choice changed back returns to. */
  saved?: AgentSpec;
  editable: boolean;
  onChange: (next: AgentSpec) => void;
}) {
  const words = defaultCopy.agentReview;
  const review = reviewOf(spec);
  return (
    <Section title={words.title} hint={words.hint}>
      <div className="grid min-w-0 gap-4 sm:grid-cols-2">
        <Label text={words.approve} hint={words.approveHint[review.approve]}>
          <select
            className={field}
            aria-label={words.approve}
            value={review.approve}
            disabled={!editable}
            onChange={(e) =>
              onChange(withReview(spec, { approve: e.target.value as ReviewApprove }, saved))
            }
          >
            {APPROVALS.map((a) => (
              <option key={a} value={a}>
                {words.approves[a]}
              </option>
            ))}
          </select>
        </Label>
        <Label text={words.blockOn} hint={words.blockOnHint}>
          <select
            className={field}
            aria-label={words.blockOn}
            value={review.block_on}
            disabled={!editable}
            onChange={(e) =>
              onChange(withReview(spec, { block_on: e.target.value as ReviewBlockOn }, saved))
            }
          >
            {BLOCKS.map((b) => (
              <option key={b} value={b}>
                {words.severities[b]}
              </option>
            ))}
          </select>
        </Label>
      </div>
    </Section>
  );
}

/** The library scopes an agent homed in `home` loads skills from: the workspace and its project chain. */
async function loadableScopes(home: string) {
  const scopes: { kind: "workspace" | "project"; project?: string; label: string }[] = [
    { kind: "workspace", label: defaultCopy.agentSkillDefaults.workspace },
  ];
  const me = await api.me();
  if (!me.permissions.includes("projects:read")) return null;
  const projects = home.replace(/^~/, "") ? await api.projects() : [];
  let at = home.replace(/^~/, "");
  const seen = new Set<string>();
  for (let depth = 0; at && depth < 8 && !seen.has(at); depth++) {
    seen.add(at);
    const project = projects.find((p) => p.name === at);
    // A personal project's skills never load.
    if (!project || project.personal_owner) break;
    scopes.push({ kind: "project", project: project.name, label: project.title || project.name });
    at = project.parent ?? "";
  }
  return scopes;
}

/**
 * The library skills the agent may load with `use_skill`, each run as a
 * sub-agent of its own. Offered by name from the workspace's library and its
 * project's; saved with the agent's next revision.
 */
export function LoadableSkills({
  spec,
  saved,
  home = "",
  editable,
  onChange,
}: {
  spec: AgentSpec;
  /** The spec as saved, which a list changed back returns to. */
  saved?: AgentSpec;
  /** The project the agent is homed in, whose library it also loads from. */
  home?: string;
  editable: boolean;
  onChange: (next: AgentSpec) => void;
}) {
  const words = defaultCopy.agentLoadableSkills;
  const [options, setOptions] = useState<Pickable[] | null>(null);
  const [unreadable, setUnreadable] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setProblem(null);
    loadableScopes(home)
      .then(async (scopes) => {
        if (!scopes) {
          if (live) {
            setUnreadable(true);
            setOptions([]);
          }
          return;
        }
        const lists = await Promise.all(
          scopes.map((s) =>
            api
              .librarySkills(s.kind === "project" ? { kind: "project", project: s.project } : s)
              .then((rows) => rows.map((row) => ({ row, scope: s.label }))),
          ),
        );
        // A run loads only a published skill. A row is the latest revision, so
        // one that is a draft is offered when an earlier revision was published.
        const drafts = lists
          .flat()
          .filter(({ row }) => !row.published_at)
          .slice(0, 32);
        const published = new Set(
          lists
            .flat()
            .filter(({ row }) => row.published_at)
            .map(({ row }) => row.id),
        );
        await Promise.all(
          drafts.map(({ row }) =>
            api
              .librarySkill(row.id)
              .then((revisions) => {
                if (revisions.some((r) => r.published_at)) published.add(row.id);
              })
              .catch(() => {}),
          ),
        );
        if (!live) return;
        const byName = new Map<string, Pickable>();
        for (const { row, scope } of lists.flat())
          if (published.has(row.id) && !byName.has(row.name))
            byName.set(row.name, {
              value: row.name,
              label: row.name,
              hint: [scope, row.description].filter(Boolean).join(" · "),
            });
        setUnreadable(false);
        setOptions([...byName.values()].sort((a, b) => a.label.localeCompare(b.label)));
      })
      .catch((e) => {
        if (!live) return;
        setProblem(e instanceof Error ? e.message : words.failed);
        setOptions([]);
      });
    return () => {
      live = false;
    };
  }, [home, words.failed]);
  const value = spec.skills?.loadable ?? [];
  const anySkill = value.includes(ANY);
  // A saved name the libraries read here do not hold may be in the library of
  // the project a run is in, so it is kept and said to be elsewhere.
  const known = new Set((options ?? []).map((o) => o.value));
  const kept: Pickable[] = value
    .filter((v) => !known.has(v))
    .filter((v) => v !== ANY)
    .map((v) => ({
      value: v,
      label: v,
      hint: options && !unreadable ? words.elsewhere : undefined,
    }));
  const any: Pickable = { value: ANY, label: words.any, hint: words.anyHint };
  // Any skill stands alone: while it is chosen it can only be removed, and
  // choosing it replaces the names.
  return (
    <Section title={words.title} hint={words.hint}>
      <Problem message={problem} />
      {unreadable && <p className="text-aui-xs text-aui-muted">{words.unreadable}</p>}
      <PickMany
        label={words.title}
        add={words.add}
        disabled={!editable}
        empty={words.empty}
        value={value}
        options={anySkill ? [any] : [any, ...kept, ...(options ?? [])]}
        onChange={(next) =>
          onChange(withLoadable(spec, next.includes(ANY) && !anySkill ? [ANY] : next, saved))
        }
      />
    </Section>
  );
}
