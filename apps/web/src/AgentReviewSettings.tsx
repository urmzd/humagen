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

/** The review policy the spec holds, with its defaults filled in. */
export function reviewOf(spec: AgentSpec): { approve: ReviewApprove; block_on: ReviewBlockOn } {
  return {
    approve: spec.review?.approve ?? REVIEW_DEFAULTS.approve,
    block_on: spec.review?.block_on ?? REVIEW_DEFAULTS.block_on,
  };
}

/**
 * The spec's review policy with `patch` applied. A value equal to its default
 * is left out, as the server leaves it out, so choosing the default again
 * leaves nothing unsaved. Every other field of the spec is kept.
 */
export function withReview(spec: AgentSpec, patch: Partial<Review>): AgentSpec {
  const next = { ...reviewOf(spec), ...patch };
  const review: Review = {
    ...(next.approve !== REVIEW_DEFAULTS.approve ? { approve: next.approve } : {}),
    ...(next.block_on !== REVIEW_DEFAULTS.block_on ? { block_on: next.block_on } : {}),
  };
  const { review: _, ...rest } = spec;
  return Object.keys(review).length ? { ...rest, review } : rest;
}

/** The spec with its loadable skills set. None leaves `skills` out. Every other field is kept. */
export function withLoadable(spec: AgentSpec, names: string[]): AgentSpec {
  const { skills: _, ...rest } = spec;
  return names.length ? { ...rest, skills: { ...spec.skills, loadable: names } } : rest;
}

/**
 * How the agent's pull request reviews decide: whether the host may approve a
 * clean one, and the least severity that requests changes. Saved with the
 * agent's next revision.
 */
export function ReviewSettings({
  spec,
  editable,
  onChange,
}: {
  spec: AgentSpec;
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
              onChange(withReview(spec, { approve: e.target.value as ReviewApprove }))
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
              onChange(withReview(spec, { block_on: e.target.value as ReviewBlockOn }))
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
  home = "",
  editable,
  onChange,
}: {
  spec: AgentSpec;
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
        if (!live) return;
        const byName = new Map<string, Pickable>();
        for (const { row, scope } of lists.flat())
          if (!byName.has(row.name))
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
  // A saved name the libraries read here do not hold may be in the library of
  // the project a run is in, so it is kept and said to be elsewhere.
  const known = new Set((options ?? []).map((o) => o.value));
  const kept: Pickable[] = value
    .filter((v) => !known.has(v))
    .map((v) =>
      v === "*"
        ? { value: v, label: words.any }
        : { value: v, label: v, hint: options && !unreadable ? words.elsewhere : undefined },
    );
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
        options={[...kept, ...(options ?? [])]}
        onChange={(next) => onChange(withLoadable(spec, next))}
      />
    </Section>
  );
}
