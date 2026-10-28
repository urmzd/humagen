/** Each lookup route checks exactly one permission without enumerating names. */
export function agentHuePath(
  permissions: readonly string[],
): "/api/agent-hues" | "/api/project-agent-hues" | undefined {
  if (permissions.includes("runs:read")) return "/api/agent-hues";
  if (permissions.includes("projects:read")) return "/api/project-agent-hues";
  return undefined;
}

/** Server reservations are shared by every agent mark in the current workspace. */
let hues: Readonly<Record<string, number>> = {};
const listeners = new Set<() => void>();
const pending = new Set<string>();
const requested = new Set<string>();
const attempts = new Map<string, number>();
let scheduled = false;
let generation = 0;
let lookup: ((names: string[]) => Promise<Readonly<Record<string, number>>>) | undefined;

/** The surface host supplies API access and resets it when its identity changes. */
export function configureAgentHues(load: typeof lookup): void {
  generation++;
  lookup = load;
  hues = {};
  pending.clear();
  requested.clear();
  attempts.clear();
  scheduled = false;
  for (const listener of listeners) listener();
}

export function setAgentHues(next: Readonly<Record<string, number>>): void {
  hues = { ...hues, ...next };
  for (const listener of listeners) listener();
}
export const agentHue = (name: string): number | undefined =>
  Object.hasOwn(hues, name) ? hues[name] : undefined;
export const subscribeHues = (listener: () => void): (() => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

/** At most 100 explicit names per call, with at most two retries per identity. */
export function requestAgentHue(name: string): void {
  if (!lookup || !name || agentHue(name) !== undefined || requested.has(name)) return;
  pending.add(name);
  if (scheduled) return;
  scheduled = true;
  const scope = generation;
  queueMicrotask(() => {
    if (scope !== generation) return;
    scheduled = false;
    const names = [...pending];
    pending.clear();
    const load = lookup;
    if (!load) return;
    for (let i = 0; i < names.length; i += 100) {
      const batch = names.slice(i, i + 100);
      for (const name of batch) {
        requested.add(name);
        attempts.set(name, (attempts.get(name) ?? 0) + 1);
      }
      load(batch)
        .then((result) => {
          if (scope === generation) setAgentHues(result);
        })
        .catch(() => {
          if (scope !== generation) return;
          const retry = batch.filter((name) => (attempts.get(name) ?? 0) < 3);
          setTimeout(() => {
            if (scope !== generation) return;
            for (const name of retry) {
              requested.delete(name);
              requestAgentHue(name);
            }
          }, 500);
        });
    }
  });
}
