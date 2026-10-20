import type { ModelRef } from "./api.ts";
import { chainProblem, conditions, defaultModel, MAX_FALLBACKS, providers } from "./models.ts";
import { button, card, field, Label, Menu } from "./ui.tsx";

export { chainProblem, chainWords, defaultModel, MAX_FALLBACKS, POLICY, starterPolicy } from "./models.ts";

/**
 * An ordered list of models and when a call moves down it. The first answers;
 * when it fails with one of the chosen errors, the next is tried. A bad request
 * or a cancelled run never moves on: no other model could fix it.
 */
export function ModelChain({
  models,
  fallbackOn,
  editable,
  label = "Models",
  onChange,
}: {
  models: ModelRef[];
  fallbackOn: string[];
  editable: boolean;
  label?: string;
  onChange: (models: ModelRef[], fallbackOn: string[]) => void;
}) {
  const write = (next: ModelRef[]) => onChange(next, next.length < 2 ? [] : fallbackOn);
  const edit = (i: number, patch: Partial<ModelRef>) => write(models.map((m, j) => (j === i ? { ...m, ...patch } : m)));
  const move = (i: number, by: -1 | 1) => {
    const next = models.slice();
    const [m] = next.splice(i, 1);
    if (m) next.splice(i + by, 0, m);
    write(next);
  };
  const problem = chainProblem(models, fallbackOn);
  const anyError = fallbackOn.length === 0;
  const toggle = (c: string, on: boolean) => {
    const next = on ? [...fallbackOn, c] : fallbackOn.filter((x) => x !== c);
    onChange(models, next);
  };

  return (
    <div className="flex flex-col gap-4">
      <ol className="flex flex-col gap-2" aria-label={`${label}, in the order they are tried`}>
        {models.map((m, i) => {
          const role = i === 0 ? "Primary" : `Fallback ${i}`;
          return (
            <li key={`${i}:${m.provider}`} className={`${card} grid grid-cols-1 items-end gap-3 sm:grid-cols-[6rem_11rem_minmax(0,1fr)_auto]`}>
              <span className="text-aui-sm font-medium text-aui-fg sm:pb-2">{role}</span>
              <Label text="Provider">
                <Menu
                  label={`Provider, ${role.toLowerCase()}`}
                  value={m.provider}
                  disabled={!editable}
                  options={providers}
                  onChange={(provider) => edit(i, { provider, model: defaultModel[provider] ?? m.model })}
                />
              </Label>
              <Label text="Model">
                <input aria-label={`Model, ${role.toLowerCase()}`} className={field} value={m.model} disabled={!editable} onChange={(e) => edit(i, { model: e.target.value })} />
              </Label>
              {editable && (
                <span className="flex gap-1">
                  <button type="button" className={button} disabled={i === 0} aria-label={`Move ${m.model || role} up`} onClick={() => move(i, -1)}>
                    ↑
                  </button>
                  <button type="button" className={button} disabled={i === models.length - 1} aria-label={`Move ${m.model || role} down`} onClick={() => move(i, 1)}>
                    ↓
                  </button>
                  {models.length > 1 && (
                    <button type="button" className={button} onClick={() => write(models.filter((_, j) => j !== i))}>
                      Remove
                    </button>
                  )}
                </span>
              )}
            </li>
          );
        })}
      </ol>
      {editable && models.length - 1 < MAX_FALLBACKS && (
        <button type="button" className={`${button} self-start`} onClick={() => write([...models, { provider: "anthropic", model: "" }])}>
          Add Fallback
        </button>
      )}

      {models.length > 1 && (
        <fieldset className="flex flex-col gap-2">
          <legend className="pb-1 text-aui-sm font-medium text-aui-fg">Move to the next model on</legend>
          <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
            <input type="radio" className="mt-1" disabled={!editable} checked={anyError} onChange={() => onChange(models, [])} />
            <span>
              Any recoverable error
              <span className="block text-aui-xs text-aui-muted">Everything but a bad request, a cancelled run or a spent budget.</span>
            </span>
          </label>
          <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
            <input type="radio" className="mt-1" disabled={!editable} checked={!anyError} onChange={() => onChange(models, ["unavailable", "rate_limit"])} />
            <span>
              Only these errors
              <span className="block text-aui-xs text-aui-muted">Anything else fails the call on the model it reached.</span>
            </span>
          </label>
          {!anyError && (
            <ul className="grid grid-cols-1 gap-x-6 gap-y-1.5 pl-6 sm:grid-cols-2">
              {conditions.map((c) => (
                <li key={c.value}>
                  <label className="flex items-start gap-2 text-aui-sm text-aui-fg">
                    <input
                      type="checkbox"
                      className="mt-1"
                      disabled={!editable}
                      checked={fallbackOn.includes(c.value)}
                      onChange={(e) => toggle(c.value, e.target.checked)}
                    />
                    <span>
                      {c.label}
                      <span className="block text-aui-xs text-aui-muted">{c.hint}</span>
                    </span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </fieldset>
      )}
      {problem && (
        <p role="alert" className="text-aui-xs text-aui-bad">
          {problem}
        </p>
      )}
    </div>
  );
}

