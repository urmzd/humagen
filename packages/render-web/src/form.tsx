import type { PrimitiveProps } from "@vultus/primitives";
import { createContext, useContext, useId, useState, useSyncExternalStore } from "react";

interface Group {
  ids: string[];
  selected: string[];
}

/**
 * Per-surface form state. A Checkboxes group registers its options here and a
 * Button that names the group in `collects` reads the live selection at click
 * time, the way Slack fills `state.values` into an interaction payload.
 */
export class FormStore {
  private groups = new Map<string, Group>();
  private listeners = new Set<() => void>();
  private version = 0;

  /** First registration wins, so a re-render never resets what the viewer changed. */
  init(name: string, ids: string[], selected: string[]) {
    if (!this.groups.has(name)) this.groups.set(name, { ids, selected });
  }

  set(name: string, selected: string[]) {
    const group = this.groups.get(name);
    if (!group) return;
    this.groups.set(name, { ...group, selected: group.ids.filter((id) => selected.includes(id)) });
    this.version += 1;
    for (const l of this.listeners) l();
  }

  get(name: string): Group | undefined {
    return this.groups.get(name);
  }

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  snapshot = () => this.version;
}

export const FormContext = createContext<FormStore | null>(null);

const noop = () => () => {};
const zero = () => 0;

/** The live selection of a named Checkboxes group, or undefined outside a surface. */
export function useGroup(name: string | undefined): Group | undefined {
  const store = useContext(FormContext);
  useSyncExternalStore(store?.subscribe ?? noop, store?.snapshot ?? zero, zero);
  return name ? store?.get(name) : undefined;
}

/** Accessible checkbox list with select all/none. Its selection rides along with a collecting Button. */
export function Checkboxes({ name, options, selected }: PrimitiveProps["Checkboxes"]) {
  const store = useContext(FormContext);
  const enabledIds = options.filter((o) => !o.disabled).map((o) => o.id);
  // Register synchronously so a Button later in the same render already sees the group.
  useState(() => store?.init(name, enabledIds, selected));
  const [local, setLocal] = useState(selected);
  const group = useGroup(name);
  const current = group?.selected ?? local;
  const set = (next: string[]) => {
    if (store) store.set(name, next);
    else setLocal(next);
  };
  const labelId = useId();
  const all = enabledIds.length > 0 && enabledIds.every((id) => current.includes(id));
  // Same as Slack: with nothing that can be ticked, there is no group to show.
  if (enabledIds.length === 0) return null;

  return (
    <fieldset
      data-aui="checkboxes"
      aria-labelledby={labelId}
      className="m-0 flex min-w-0 flex-col border-0 p-0"
    >
      <div className="flex items-center gap-2 pb-1 text-aui-2xs text-aui-muted">
        <span id={labelId} className="tabular-nums">
          {current.length} of {enabledIds.length} selected
        </span>
        <button
          type="button"
          onClick={() => set(all ? [] : enabledIds)}
          className="ml-auto rounded-aui-sm px-1 font-medium underline-offset-2 hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {all ? "Select none" : "Select all"}
        </button>
      </div>
      <ul className="flex flex-col">
        {options.map((o) => {
          const checked = current.includes(o.id);
          return (
            <li key={o.id}>
              <label
                className={`-mx-1.5 flex min-w-0 cursor-pointer items-start gap-2.5 rounded-aui-sm px-1.5 py-1 transition-colors hover:bg-aui-hover ${o.disabled ? "cursor-not-allowed opacity-50" : ""}`}
              >
                <input
                  type="checkbox"
                  name={name}
                  value={o.id}
                  checked={checked}
                  disabled={o.disabled}
                  onChange={(e) =>
                    set(e.target.checked ? [...current, o.id] : current.filter((id) => id !== o.id))
                  }
                  className="mt-0.5 size-3.5 shrink-0 accent-[var(--aui-accent)]"
                />
                <span className="flex min-w-0 flex-col">
                  <span
                    className={`text-aui-sm ${checked ? "text-aui-fg" : "text-aui-muted line-through decoration-aui-muted/50"}`}
                  >
                    {o.label}
                  </span>
                  {o.description && (
                    <span className="truncate text-aui-2xs text-aui-muted" title={o.description}>
                      {o.description}
                    </span>
                  )}
                </span>
              </label>
            </li>
          );
        })}
      </ul>
    </fieldset>
  );
}
