import {
  AnthropicIcon,
  CheckIcon,
  ChevronDownIcon,
  CircleIcon,
  GeminiIcon,
  GripIcon,
  OpenAIIcon,
  PlusIcon,
  SearchIcon,
  ServerIcon,
  TrashIcon,
} from "@humagen/render-web";
import {
  type KeyboardEvent,
  type PointerEvent,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { api, type ModelRef } from "./api.ts";
import {
  chainProblem,
  conditions,
  MAX_FALLBACKS,
  modelGroups,
  providerLabel,
  providers,
} from "./models.ts";
import { button, field, Popover } from "./ui.tsx";

export {
  chainProblem,
  chainWords,
  defaultModel,
  MAX_FALLBACKS,
  POLICY,
  starterPolicy,
} from "./models.ts";

const iconButton =
  "inline-flex size-8 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40";

/** A provider's logo, or a plain mark for one the console has no logo for. */
export function ProviderMark({ provider, size = 16 }: { provider: string; size?: number }) {
  if (provider === "anthropic") return <AnthropicIcon size={size} />;
  if (provider === "openai") return <OpenAIIcon size={size} />;
  if (["gemini", "google", "vertex"].includes(provider)) return <GeminiIcon size={size} />;
  if (provider === "offline") return <CircleIcon size={size} />;
  return <ServerIcon size={size} />;
}

type Choice = { name: string; declared: boolean };
let choicesRead: Promise<Choice[]> | null = null;

/** The providers this server runs agents on, read once per page load. */
function useProviderChoices(): Choice[] | null {
  const [choices, setChoices] = useState<Choice[] | null>(null);
  useEffect(() => {
    let live = true;
    choicesRead ??= api.modelProviders().catch(() => {
      choicesRead = null;
      return [];
    });
    choicesRead.then((c) => live && setChoices(c));
    return () => {
      live = false;
    };
  }, []);
  return choices;
}

/**
 * One model, picked from the models each provider offers. Providers are
 * groups and their models sit under them; typing a name that is not listed
 * offers it under each provider, so any model can be named.
 */
function ModelSelect({
  value,
  role,
  groups,
  declared,
  editable,
  onChange,
}: {
  value: ModelRef;
  role: string;
  groups: { provider: string; models: string[] }[];
  declared: (provider: string) => boolean | undefined;
  editable: boolean;
  onChange: (next: ModelRef) => void;
}) {
  const [query, setQuery] = useState("");
  const typed = query.trim();
  const named = value.model.trim();
  const face = (
    <>
      <span className="shrink-0 text-aui-fg">
        <ProviderMark provider={value.provider} />
      </span>
      <span className="min-w-0 flex-1 truncate text-left">
        {named || <span className="text-aui-muted">Choose a model</span>}
      </span>
      <span className="shrink-0 text-aui-xs text-aui-muted">{providerLabel(value.provider)}</span>
    </>
  );
  if (!editable)
    return <span className="flex min-h-9 items-center gap-2 px-1 text-aui-sm">{face}</span>;

  const shown = groups
    .map((g) => ({
      ...g,
      models: g.models.filter((m) => m.toLowerCase().includes(typed.toLowerCase())),
      custom: typed !== "" && g.provider !== "offline" && !g.models.includes(typed),
    }))
    .filter((g) => g.models.length > 0 || g.custom);
  const only = shown.length === 1 && shown[0]?.models.length === 1 ? shown[0] : undefined;

  return (
    <Popover
      label={`Model, ${role.toLowerCase()}: ${named ? `${named} on ${providerLabel(value.provider)}` : "none chosen"}`}
      className={`${field} flex w-full items-center gap-2 text-aui-sm`}
      onOpen={() => setQuery("")}
      button={
        <>
          {face}
          <ChevronDownIcon size={14} />
        </>
      }
    >
      {(close) => {
        const choose = (provider: string, model: string) => {
          close();
          onChange({ provider, model });
        };
        return (
          <div className="flex flex-col gap-1">
            <label className="relative block">
              <span className="sr-only">Search models, or type a model's name</span>
              <span className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-aui-muted">
                <SearchIcon size={14} />
              </span>
              <input
                // biome-ignore lint/a11y/noAutofocus: the search box is the reason the panel opened
                autoFocus
                className={`${field} pl-8`}
                placeholder="Search or type a model"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key !== "Enter" || !only) return;
                  e.preventDefault();
                  choose(only.provider, only.models[0] ?? "");
                }}
              />
            </label>
            <div className="flex max-h-72 flex-col overflow-y-auto">
              {shown.length === 0 && (
                <p className="px-2.5 py-2 text-aui-xs text-aui-muted">No model matches.</p>
              )}
              {shown.map((g) => (
                <div
                  key={g.provider}
                  role="group"
                  aria-label={providerLabel(g.provider)}
                  className="flex flex-col py-1"
                >
                  <span className="flex items-center gap-2 px-2.5 py-1 text-aui-xs font-medium text-aui-fg">
                    <ProviderMark provider={g.provider} size={14} />
                    {providerLabel(g.provider)}
                    {/* The offline provider calls no model, so it needs no setting up. */}
                    {declared(g.provider) === false && g.provider !== "offline" && (
                      <span className="ml-auto font-normal text-aui-muted">Not set up</span>
                    )}
                  </span>
                  {[...g.models, ...(g.custom ? [typed] : [])].map((m) => {
                    const checked = value.provider === g.provider && named === m;
                    return (
                      <button
                        key={m}
                        type="button"
                        aria-pressed={checked}
                        onClick={() => choose(g.provider, m)}
                        className="flex min-h-8 pointer-coarse:min-h-11 items-center gap-2 rounded-aui py-1 pr-2.5 pl-8 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                      >
                        <span className="min-w-0 flex-1 truncate">
                          {g.custom && m === typed && !g.models.includes(m) ? `Use “${m}”` : m}
                        </span>
                        {checked && <CheckIcon size={14} />}
                      </button>
                    );
                  })}
                </div>
              ))}
            </div>
          </div>
        );
      }}
    </Popover>
  );
}

/**
 * An ordered list of models and when a call moves down it. The first answers;
 * when it fails with one of the chosen errors, the next is tried. A bad request
 * or a cancelled run never moves on: no other model could fix it.
 * Rows reorder by dragging their handle, or with the arrow keys on it.
 */
export function ModelChain({
  models,
  fallbackOn,
  editable,
  label = "Models",
  used = [],
  onChange,
}: {
  models: ModelRef[];
  fallbackOn: string[];
  editable: boolean;
  label?: string;
  /** Models named elsewhere in the workspace, offered beside the known ones. */
  used?: ModelRef[];
  onChange: (models: ModelRef[], fallbackOn: string[]) => void;
}) {
  const choices = useProviderChoices();
  const names =
    choices && choices.length > 0 ? choices.map((c) => c.name) : providers.map((p) => p.value);
  const groups = modelGroups(names, [...used, ...models]);
  const declared = (provider: string) => choices?.find((c) => c.name === provider)?.declared;

  const write = (next: ModelRef[]) => onChange(next, next.length < 2 ? [] : fallbackOn);
  const edit = (i: number, next: ModelRef) => write(models.map((m, j) => (j === i ? next : m)));
  const roleOf = (i: number) => (i === 0 ? "Primary" : `Fallback ${i}`);
  const nameOf = (m: ModelRef, i: number) => m.model.trim() || roleOf(i).toLowerCase();

  const list = useRef<HTMLOListElement>(null);
  const [drag, setDrag] = useState<{ from: number; to: number } | null>(null);
  const [refocus, setRefocus] = useState<number | null>(null);
  const [said, setSaid] = useState("");
  const moveTo = (from: number, to: number) => {
    const next = models.slice();
    const [m] = next.splice(from, 1);
    if (!m) return;
    next.splice(to, 0, m);
    write(next);
    setSaid(`${nameOf(m, from)} is now ${roleOf(to).toLowerCase()}.`);
  };
  // A moved row keeps the keyboard on its handle.
  useLayoutEffect(() => {
    if (refocus === null) return;
    list.current?.querySelector<HTMLButtonElement>(`[data-chain-grip="${refocus}"]`)?.focus();
    setRefocus(null);
  }, [refocus]);

  const keyMove = (e: KeyboardEvent, i: number) => {
    const to =
      e.key === "ArrowUp"
        ? i - 1
        : e.key === "ArrowDown"
          ? i + 1
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? models.length - 1
              : null;
    if (to === null) return;
    e.preventDefault();
    if (to < 0 || to >= models.length || to === i) return;
    moveTo(i, to);
    setRefocus(to);
  };
  // The pointer works for mouse, pen and touch alike; the handle holds it until release.
  const startDrag = (e: PointerEvent<HTMLButtonElement>, from: number) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    setDrag({ from, to: from });
  };
  const dragOver = (e: PointerEvent) => {
    if (!drag || !list.current) return;
    let slot = 0;
    for (const row of list.current.querySelectorAll<HTMLElement>(":scope > li")) {
      const r = row.getBoundingClientRect();
      if (e.clientY > r.top + r.height / 2) slot++;
    }
    const to = slot > drag.from ? slot - 1 : slot;
    if (to !== drag.to) setDrag({ ...drag, to });
  };
  const endDrag = (commit: boolean) => {
    if (commit && drag && drag.to !== drag.from) moveTo(drag.from, drag.to);
    setDrag(null);
  };

  const problem = chainProblem(models, fallbackOn);
  const anyError = fallbackOn.length === 0;
  const toggle = (c: string, on: boolean) => {
    const next = on ? [...fallbackOn, c] : fallbackOn.filter((x) => x !== c);
    onChange(models, next);
  };
  const sortable = editable && models.length > 1;

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <ol
        ref={list}
        className="flex flex-col gap-1.5"
        aria-label={`${label}, in the order they are tried`}
      >
        {models.map((m, i) => {
          const role = roleOf(i);
          const moving = drag !== null && drag.from !== drag.to;
          const mark =
            moving && drag.to === i
              ? drag.to < drag.from
                ? "shadow-[inset_0_2px_0_var(--aui-accent)]"
                : "shadow-[inset_0_-2px_0_var(--aui-accent)]"
              : "";
          return (
            <li
              // biome-ignore lint/suspicious/noArrayIndexKey: a chain's place is its identity; two rows may name one model while it is being edited
              key={i}
              className={`flex items-center gap-1.5 rounded-aui-lg border border-aui-border bg-aui-surface py-1.5 pr-1.5 pl-1.5 ${mark} ${drag?.from === i ? "opacity-60" : ""}`}
            >
              {sortable ? (
                <button
                  type="button"
                  data-chain-grip={i}
                  title="Drag to reorder"
                  aria-label={`Reorder ${nameOf(m, i)}, ${role.toLowerCase()} of ${models.length}. Use the arrow keys to move it.`}
                  onKeyDown={(e) => keyMove(e, i)}
                  onPointerDown={(e) => startDrag(e, i)}
                  onPointerMove={dragOver}
                  onPointerUp={() => endDrag(true)}
                  onPointerCancel={() => endDrag(false)}
                  className={`${iconButton} cursor-grab touch-none active:cursor-grabbing`}
                >
                  <GripIcon size={14} />
                </button>
              ) : (
                editable && <span aria-hidden className="w-8 shrink-0" />
              )}
              <span className="w-[4.75rem] shrink-0 text-aui-xs text-aui-muted">{role}</span>
              <div className="min-w-0 flex-1">
                <ModelSelect
                  value={m}
                  role={role}
                  groups={groups}
                  declared={declared}
                  editable={editable}
                  onChange={(next) => edit(i, next)}
                />
              </div>
              {sortable && (
                <button
                  type="button"
                  title="Remove"
                  aria-label={`Remove ${nameOf(m, i)}`}
                  onClick={() => {
                    write(models.filter((_, j) => j !== i));
                    setSaid(`Removed ${nameOf(m, i)}.`);
                  }}
                  className={iconButton}
                >
                  <TrashIcon size={15} />
                </button>
              )}
            </li>
          );
        })}
      </ol>
      <p aria-live="polite" className="sr-only">
        {said}
      </p>
      {editable && models.length - 1 < MAX_FALLBACKS && (
        <button
          type="button"
          className={`${button} self-start`}
          onClick={() =>
            write([...models, { provider: models.at(-1)?.provider ?? "anthropic", model: "" }])
          }
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
