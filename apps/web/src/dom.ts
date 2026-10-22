/**
 * Helpers for the .dom.test.tsx files (see dom.test.ts for the setup).
 * Nothing here is used by the app.
 */

/** A request a stubbed fetch saw. */
export interface Seen {
  method: string;
  path: string;
  body?: unknown;
}

/**
 * Replaces fetch with routes answered in memory: "GET /api/agents/x/history"
 * maps to the JSON returned for it, or to a promise of it, to answer later.
 * A route is matched by its path alone, and is handed the request's query to
 * read, and may answer with a Response of its own, such as a 403. A route
 * left out answers 404, so a test sees a request it did not expect. Returns
 * what was requested, and a way to put the real fetch back.
 */
export function stubFetch(routes: Record<string, (body: unknown, query: URLSearchParams) => unknown>): { seen: Seen[]; restore: () => void } {
  const real = globalThis.fetch;
  const seen: Seen[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, "http://localhost/");
    const method = (init?.method ?? "GET").toUpperCase();
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    seen.push({ method, path: url.pathname + url.search, body });
    const route = routes[`${method} ${url.pathname}`];
    if (!route) return new Response(JSON.stringify({ error: "not stubbed" }), { status: 404 });
    const out = await route(body, url.searchParams);
    // A route may answer with a whole Response, such as a refusal.
    if (out instanceof Response) return out;
    return out === undefined ? new Response(null, { status: 204 }) : new Response(JSON.stringify(out), { status: 200 });
  }) as typeof fetch;
  return { seen, restore: () => (globalThis.fetch = real) };
}

/**
 * Installs an EventSource that never delivers anything, for a page that
 * follows a run's events while a test looks at it. Returns a way to put back
 * whatever was there.
 */
export function stubEventSource(): () => void {
  const g = globalThis as { EventSource?: unknown };
  const real = g.EventSource;
  g.EventSource = class {
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onmessage: ((m: { data: string }) => void) | null = null;
    addEventListener() {}
    close() {}
  };
  return () => {
    g.EventSource = real;
  };
}

/**
 * Installs an EventSource a test delivers to: send(event) hands a run's
 * event to every source open on that run's topic, as the platform's stream
 * would. Returns send and a way to put back whatever was there.
 */
export function liveEventSource(): { send: (event: { run_id: string; agent: string; kind: string; body?: unknown; at?: string }) => void; restore: () => void } {
  const g = globalThis as { EventSource?: unknown };
  const real = g.EventSource;
  const open = new Set<{ topic: string; onmessage: ((m: { data: string }) => void) | null }>();
  g.EventSource = class {
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
    onmessage: ((m: { data: string }) => void) | null = null;
    topic: string;
    constructor(url: string) {
      this.topic = new URL(url, "http://localhost/").searchParams.get("topic") ?? "";
      open.add(this);
    }
    addEventListener() {}
    close() {
      open.delete(this);
    }
  };
  let seq = 0;
  const send = (e: { run_id: string; agent: string; kind: string; body?: unknown; at?: string }) => {
    const topic = `agent:${e.agent}:${e.run_id}`;
    const data = JSON.stringify({
      seq: ++seq,
      topic,
      run_id: e.run_id,
      tenant: "t",
      agent: e.agent,
      rev: 1,
      origin: "test",
      kind: e.kind,
      at: e.at ?? "2026-10-01T09:00:00Z",
      ...(e.body === undefined ? {} : { payload: btoa(JSON.stringify(e.body)) }),
    });
    for (const s of open) if (s.topic === topic) s.onmessage?.({ data });
  };
  return {
    send,
    restore: () => {
      g.EventSource = real;
    },
  };
}
