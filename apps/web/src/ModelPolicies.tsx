import { PlusIcon, TrashIcon } from "@humagen/render-web";
import { useState } from "react";
import { api, type ModelPolicy, type Revision, type Settings as Workspace } from "./api.ts";
import { chainProblem, ModelChain, POLICY, starterPolicy } from "./ModelChain.tsx";
import { button, card, field, Label, Page, Problem, primary } from "./ui.tsx";

/**
 * The Agents hub's Models page: the workspace's model policies. Agents name a
 * policy instead of a model, so this is where the models agents run on are
 * chosen. Which providers the server can reach is set up by an admin.
 */
export function Models({
  workspace,
  agents,
  canEdit,
  onWorkspace,
}: {
  workspace: Workspace | null;
  agents: Revision[];
  canEdit: boolean;
  onWorkspace: (next: Workspace) => void;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  return (
    <Page
      title="Models"
      lede={
        <>
          Policies say which models agents run on, in order, and which errors move a call to the
          next. An agent that follows a policy picks up a change at its next call, so one edit moves
          every agent on it off a failing provider.
          {canEdit ? "" : " Only an admin can change these."}
        </>
      }
    >
      <Problem message={problem} />
      {workspace && (
        <ModelPolicies
          key={JSON.stringify(workspace.model_policies ?? [])}
          saved={workspace.model_policies ?? []}
          agents={agents}
          canEdit={canEdit}
          onSave={(model_policies) => {
            setProblem(null);
            api
              .saveSettings({ ...workspace, model_policies })
              .then(onWorkspace)
              .catch(() => setProblem("The policies could not be saved."));
          }}
        />
      )}
    </Page>
  );
}

/** Every model policy in the workspace, edited together and saved at once. */
export function ModelPolicies({
  saved,
  agents,
  canEdit,
  onSave,
}: {
  saved: ModelPolicy[];
  agents: Revision[];
  canEdit: boolean;
  onSave: (next: ModelPolicy[]) => void;
}) {
  const [draft, setDraft] = useState<ModelPolicy[]>(saved);
  const dirty = JSON.stringify(draft) !== JSON.stringify(saved);
  const users = (name: string) =>
    agents
      .filter((a) => a.spec.provider === POLICY && a.spec.model === name)
      .map((a) => a.spec.name);
  // Models named anywhere in the workspace are offered in every chain.
  const used = [
    ...draft.flatMap((p) => p.models),
    ...agents
      .filter((a) => a.spec.provider !== POLICY)
      .map((a) => ({ provider: a.spec.provider, model: a.spec.model })),
  ];
  const edit = (i: number, patch: Partial<ModelPolicy>) =>
    setDraft((d) => d.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  const problem =
    draft
      .map((p) =>
        !/^[a-z0-9][a-z0-9-]{0,62}$/.test(p.name)
          ? `"${p.name}" is not a valid name: lowercase letters, digits and dashes.`
          : chainProblem(p.models, p.fallback_on ?? []),
      )
      .find(Boolean) ??
    (new Set(draft.map((p) => p.name)).size !== draft.length ? "Two policies share a name." : null);

  return (
    <div className="flex flex-col gap-6">
      {draft.length === 0 && (
        <p className={`${card} text-aui-sm text-aui-muted`}>
          No policies yet. Agents each name their own models.
        </p>
      )}
      {draft.map((p, i) => {
        const by = users(saved[i]?.name ?? p.name);
        return (
          <section
            // biome-ignore lint/suspicious/noArrayIndexKey: a policy's name is edited in place, so its place is its identity
            key={i}
            aria-label={`Policy ${p.name}`}
            className="flex flex-col gap-3 border-aui-border border-t pt-5 first:border-t-0 first:pt-0"
          >
            <div className="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-3 sm:grid-cols-[14rem_minmax(0,1fr)_auto]">
              <Label
                text="Name"
                hint={
                  by.length > 0
                    ? "Agents follow it by name, so it cannot change while they do."
                    : undefined
                }
              >
                <input
                  className={field}
                  value={p.name}
                  disabled={!canEdit || by.length > 0}
                  onChange={(e) => edit(i, { name: e.target.value })}
                />
              </Label>
              <div className="col-span-2 row-start-2 sm:col-span-1 sm:row-start-auto">
                <Label text="Description">
                  <input
                    className={field}
                    maxLength={300}
                    value={p.description ?? ""}
                    disabled={!canEdit}
                    placeholder="What this policy is for"
                    onChange={(e) => edit(i, { description: e.target.value })}
                  />
                </Label>
              </div>
              {canEdit && (
                <button
                  type="button"
                  className="col-start-2 row-start-1 inline-flex size-9 pointer-coarse:size-11 items-center justify-center self-end rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40 sm:col-start-3"
                  disabled={by.length > 0}
                  aria-label={`Remove policy ${p.name}`}
                  title={by.length > 0 ? `Used by ${by.join(", ")}` : "Remove policy"}
                  onClick={() => setDraft((d) => d.filter((_, j) => j !== i))}
                >
                  <TrashIcon size={16} />
                </button>
              )}
            </div>
            <p className="text-aui-xs text-aui-muted">
              {by.length === 0 ? "No agent follows it yet." : `Followed by ${by.join(", ")}.`}
            </p>
            <ModelChain
              label={`Models of ${p.name}`}
              models={p.models}
              fallbackOn={p.fallback_on ?? []}
              editable={canEdit}
              used={used}
              onChange={(models, fallback_on) =>
                edit(i, { models, fallback_on: fallback_on.length > 0 ? fallback_on : undefined })
              }
            />
          </section>
        );
      })}
      {canEdit && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-aui-border border-t pt-4">
          <button
            type="button"
            className={button}
            onClick={() => setDraft((d) => [...d, starterPolicy(d.map((p) => p.name))])}
          >
            <PlusIcon size={14} />
            Add Policy
          </button>
          <span className="flex items-center gap-2">
            {problem && <span className="text-aui-xs text-aui-bad">{problem}</span>}
            {dirty && (
              <button type="button" className={button} onClick={() => setDraft(saved)}>
                Discard
              </button>
            )}
            <button
              type="button"
              className={primary}
              disabled={!dirty || problem !== null}
              onClick={() => onSave(draft)}
            >
              Save Policies
            </button>
          </span>
        </div>
      )}
    </div>
  );
}
