import {
  AnthropicIcon,
  CircleIcon,
  GeminiIcon,
  GripIcon,
  OllamaIcon,
  PlusIcon,
  SparkleIcon,
  XIcon,
} from "@humagen/render-web";
import { useRef, useState } from "react";
import type { ModelRef } from "./api.ts";
import {
  chainProblem,
  conditions,
  MAX_FALLBACKS,
  modelName,
  providerCatalog,
  providerName,
} from "./models.ts";
import { button, field, Menu, type Option } from "./ui.tsx";

export {
  chainProblem,
  chainWords,
  defaultModel,
  MAX_FALLBACKS,
  POLICY,
  starterPolicy,
} from "./models.ts";

const OTHER = "__other__";
const key = (provider: string, model: string) => `${provider}::${model}`;

/** A provider's mark: its logo where one is known, a neutral glyph otherwise. */
export function ProviderMark({ provider, size = 16 }: { provider: string; size?: number }) {
  const cls = "shrink-0 text-aui-fg";
  if (provider === "anthropic") return <AnthropicIcon size={size} className={cls} />;
  if (provider === "google") return <GeminiIcon size={size} className={cls} />;
  if (provider === "ollama") return <OllamaIcon size={size} className={cls} />;
  if (provider === "offline") return <CircleIcon size={size} className="shrink-0 text-aui-muted" />;
  return <SparkleIcon size={size} className={cls} />;
}

/** Every model a row may pick, grouped by provider, plus "Another model" per provider. */
function modelOptions(models: ModelRef[]): Option[] {
  const out: Option[] = [];
  const known = new Set(providerCatalog.map((p) => p.id));
  // A provider no longer offered, such as the offline test model, still names itself for an
  // agent already on it, but is never offered to a new row.
  const retired: Record<string, { name: string; models: Record<string, string> }> = {
    offline: { name: "Offline test model", models: { offline: "Offline" } },
  };
  for (const p of providerCatalog) {
    const mark = <ProviderMark provider={p.id} />;
    for (const m of p.models)
      out.push({ value: key(p.id, m.id), label: m.name, group: p.name, mark });
    for (const m of models) {
      if (m.provider === p.id && m.model.trim() && !p.models.some((x) => x.id === m.model))
        out.push({ value: key(p.id, m.model), label: m.model, group: p.name, mark });
    }
    out.push({
      value: key(p.id, OTHER),
      label: `Another ${p.name} model`,
      group: p.name,
      mark,
      hint: "Type its id",
    });
  }
  for (const m of models) {
    if (known.has(m.provider)) continue;
    const old = retired[m.provider];
    out.push({
      value: key(m.provider, m.model.trim() ? m.model : OTHER),
      label: old?.models[m.model] ?? (m.model || m.provider),
      group: old?.name ?? m.provider,
      mark: <ProviderMark provider={m.provider} />,
      hint: old ? "No longer offered; pick another model" : undefined,
    });
  }
  return out.filter((o, i) => out.findIndex((x) => x.value === o.value) === i);
}

/**
 * An ordered list of models and when a call moves down it. The first answers;
 * when it fails with one of the chosen errors, the next is tried. A bad request
 * or a cancelled run never moves on: no other model could fix it. Rows reorder
 * by dragging their handle, or with the arrow keys while the handle has focus.
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
  const edit = (i: number, patch: Partial<ModelRef>) =>
    write(models.map((m, j) => (j === i ? { ...m, ...patch } : m)));
  const moveTo = (from: number, to: number) => {
    if (from === to || to < 0 || to >= models.length) return;
    const next = models.slice();
    const [m] = next.splice(from, 1);
    if (m) next.splice(to, 0, m);
    write(next);
  };
  const problem = chainProblem(models, fallbackOn);
  const anyError = fallbackOn.length === 0;
  const toggle = (c: string, on: boolean) => {
    const next = on ? [...fallbackOn, c] : fallbackOn.filter((x) => x !== c);
    onChange(models, next);
  };
  const options = modelOptions(models);
  const rows = useRef<(HTMLLIElement | null)[]>([]);
  const handles = useRef<(HTMLButtonElement | null)[]>([]);
  const dragging = useRef<number | null>(null);
  const [lifted, setLifted] = useState<number | null>(null);
  const [said, setSaid] = useState("");
  const nameOf = (m: ModelRef) =>
    m.model.trim() ? modelName(m.provider, m.model) : providerName(m.provider);
  const place = (from: number, to: number) => {
    const m = models[from];
    moveTo(from, to);
    if (m) setSaid(`${nameOf(m)} moved to position ${to + 1} of ${models.length}.`);
    requestAnimationFrame(() => handles.current[to]?.focus());
  };
  const onDrag = (y: number) => {
    const from = dragging.current;
    if (from === null) return;
    let to = 0;
    rows.current.forEach((el, j) => {
      if (!el || j === from) return;
      const r = el.getBoundingClientRect();
      if (y > r.top + r.height / 2) to++;
    });
    if (to !== from) {
      moveTo(from, to);
      dragging.current = to;
      setLifted(to);
    }
  };
  const next = () => {
    const used = new Set(models.map((m) => key(m.provider, m.model)));
    const first = models[0]?.provider ?? "anthropic";
    const pool = [
      ...(providerCatalog.find((p) => p.id === first)?.models ?? []).map((m) => ({
        provider: first,
        model: m.id,
      })),
      ...providerCatalog.flatMap((p) => p.models.map((m) => ({ provider: p.id, model: m.id }))),
    ];
    return (
      pool.find((m) => m.provider !== "offline" && !used.has(key(m.provider, m.model))) ?? {
        provider: first,
        model: "",
      }
    );
  };

  return (
    <div className="@container flex min-w-0 flex-col gap-4">
      <p aria-live="polite" className="sr-only">
        {said}
      </p>
      <ol
        className="flex flex-col divide-y divide-aui-border rounded-aui-lg border border-aui-border"
        aria-label={`${label}, in the order they are tried`}
      >
        {models.map((m, i) => {
          const role = i === 0 ? "Primary" : `Fallback ${i}`;
          const listed = providerCatalog
            .find((p) => p.id === m.provider)
            ?.models.some((x) => x.id === m.model);
          // An id field only for a model the catalog does not list under an offered provider.
          const offered = providerCatalog.some((p) => p.id === m.provider);
          const custom = offered && !listed;
          const value = m.model.trim() ? key(m.provider, m.model) : key(m.provider, OTHER);
          return (
            <li
              // biome-ignore lint/suspicious/noArrayIndexKey: a row is its position in the chain
              key={i}
              ref={(el) => {
                rows.current[i] = el;
              }}
              data-lifted={lifted === i}
              className="flex min-w-0 flex-wrap items-center gap-2 bg-aui-bg px-2 py-2 first:rounded-t-aui-lg last:rounded-b-aui-lg data-[lifted=true]:bg-aui-hover data-[lifted=true]:shadow-sm"
            >
              {editable && models.length > 1 && (
                <button
                  type="button"
                  ref={(el) => {
                    handles.current[i] = el;
                  }}
                  aria-label={`Reorder ${nameOf(m)}, ${role.toLowerCase()}. Drag, or use the up and down arrow keys`}
                  title="Drag to reorder"
                  className="flex size-7 shrink-0 cursor-grab touch-none items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent active:cursor-grabbing"
                  onPointerDown={(e) => {
                    e.preventDefault();
                    e.currentTarget.setPointerCapture(e.pointerId);
                    dragging.current = i;
                    setLifted(i);
                  }}
                  onPointerMove={(e) => onDrag(e.clientY)}
                  onPointerUp={() => {
                    const at = dragging.current;
                    dragging.current = null;
                    setLifted(null);
                    if (at !== null && models[at])
                      setSaid(
                        `${nameOf(models[at])} is now ${at === 0 ? "primary" : `fallback ${at}`}.`,
                      );
                  }}
                  onPointerCancel={() => {
                    dragging.current = null;
                    setLifted(null);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "ArrowUp" && i > 0) {
                      e.preventDefault();
                      place(i, i - 1);
                    } else if (e.key === "ArrowDown" && i < models.length - 1) {
                      e.preventDefault();
                      place(i, i + 1);
                    }
                  }}
                >
                  <GripIcon size={14} />
                </button>
              )}
              <span className="w-20 shrink-0 text-aui-xs font-medium text-aui-muted">{role}</span>
              <Menu
                label={`Model, ${role.toLowerCase()}`}
                value={value}
                disabled={!editable}
                options={options}
                className="min-w-48 flex-1"
                onChange={(v) => {
                  const [provider = m.provider, model = ""] = v.split("::");
                  edit(i, { provider, model: model === OTHER ? "" : model });
                }}
              />
              {custom && (
                <input
                  aria-label={`Model id, ${role.toLowerCase()}`}
                  placeholder={`${providerName(m.provider)} model id`}
                  className={`${field} min-w-40 flex-1`}
                  value={m.model}
                  disabled={!editable}
                  onChange={(e) => edit(i, { model: e.target.value })}
                />
              )}
              {editable && models.length > 1 && (
                <button
                  type="button"
                  aria-label={`Remove ${nameOf(m)}`}
                  title="Remove"
                  className="flex size-7 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                  onClick={() => write(models.filter((_, j) => j !== i))}
                >
                  <XIcon size={14} />
                </button>
              )}
            </li>
          );
        })}
      </ol>
      {editable && models.length - 1 < MAX_FALLBACKS && (
        <button
          type="button"
          className={`${button} self-start`}
          onClick={() => write([...models, next()])}
        >
          <PlusIcon size={14} />
          Add Fallback
        </button>
      )}

      {models.length > 1 && (
        <fieldset className="flex flex-col gap-2">
          <legend className="pb-1 text-aui-sm font-medium text-aui-fg">
            Move to the next model on
          </legend>
          <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
            <input
              type="radio"
              className="mt-1"
              disabled={!editable}
              checked={anyError}
              onChange={() => onChange(models, [])}
            />
            <span>
              Any recoverable error
              <span className="block text-aui-xs text-aui-muted">
                Everything but a bad request, a cancelled run or a spent budget.
              </span>
            </span>
          </label>
          <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
            <input
              type="radio"
              className="mt-1"
              disabled={!editable}
              checked={!anyError}
              onChange={() => onChange(models, ["unavailable", "rate_limit"])}
            />
            <span>
              Only these errors
              <span className="block text-aui-xs text-aui-muted">
                Anything else fails the call on the model it reached.
              </span>
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
