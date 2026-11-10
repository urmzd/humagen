import { useCallback, useEffect, useRef, useState } from "react";
import { api, call, type ThreadSummary } from "./api.ts";
import { sessionCenter, sessionThread } from "./sessionCenter.ts";
import { within } from "./timeout.ts";

export type Standing = "" | "running" | "failed";

/** How many conversations one page holds. Small, so the first paint is quick. */
const PAGE = 30;

const keep = (t: ThreadSummary, agent: string, standing: Standing) =>
  (!agent || t.agents.includes(agent)) &&
  (standing === "" || (standing === "running" ? t.active > 0 : t.failed > 0));

async function sessionPage(agent: string, standing: Standing, after?: string) {
  const page = await sessionCenter.sessions({
    roots: "true",
    mine: "true",
    limit: String(PAGE),
    ...(agent ? { agent } : {}),
    ...(standing ? { status: standing === "running" ? "working" : "failed" } : {}),
    ...(after ? { after } : {}),
  });
  return { threads: page.sessions.map(sessionThread), next: page.next || null };
}

/**
 * The Recents list, a page at a time.
 *
 * - The first page is fetched again whenever `signal` changes, which is how a
 *   run starting or ending reaches the list. The fresh page replaces the top
 *   of what is loaded, and older pages a person already asked for stay where
 *   they are, so neither the rows nor the scroll position jump.
 * - A conversation that moves to the top between pages is shown once, in its
 *   new place.
 * - Changing a filter starts again from the first page.
 * - If "See more" fails, the rows already loaded stay, and it can be tried again.
 *
 * A server that does not page yet answers with everything and no cursor. The
 * same list is then paged here instead, so the screen behaves the same.
 */
export function useRecents(
  agent: string,
  standing: Standing,
  signal: unknown,
  sessionMode = false,
) {
  const [rows, setRows] = useState<ThreadSummary[]>([]);
  // A cursor for the next page, null when there is none.
  const [next, setNext] = useState<string | null>(null);
  // Set when the server sent everything at once: how many of `rows` are shown.
  const [local, setLocal] = useState<number | null>(null);
  const [loading, setLoading] = useState<"first" | "more" | null>("first");
  const [failed, setFailed] = useState(false);
  // The first page could not be had, or took too long. Nothing is listed, and it can be tried again.
  const [stalled, setStalled] = useState(false);
  const [again, setAgain] = useState(0);

  // Every request carries the filters it was made for. An answer for filters
  // that have since changed is dropped.
  const key = `${agent}|${standing}`;
  const current = useRef(key);
  const loaded = useRef({ key: "", pages: 0 });

  // biome-ignore lint/correctness/useExhaustiveDependencies: signal is the reason to run again, not a value read here
  useEffect(() => {
    current.current = key;
    const fresh = loaded.current.key !== key;
    if (fresh) {
      setRows([]);
      setNext(null);
      setLocal(null);
      setFailed(false);
      setLoading("first");
    }
    within(
      sessionMode
        ? sessionPage(agent, standing)
        : api.threadsPage({ limit: PAGE, origin: "api", agent, standing }),
    )
      .then((page) => {
        if (current.current !== key) return;
        setStalled(false);
        if (page.next === undefined) {
          // Not paged by the server. Filter and page what it sent.
          setRows(page.threads.filter((t) => keep(t, agent, standing)));
          setLocal((n) => (fresh || n === null ? PAGE : n));
          setNext(null);
        } else if (fresh || loaded.current.pages <= 1 || page.next === null) {
          // Also when the fresh page is the whole list: nothing older is left to keep.
          setRows(page.threads);
          setNext(page.next);
          setLocal(null);
          loaded.current = { key, pages: 1 };
        } else {
          // Older pages are loaded. Keep the ones past the end of the fresh page.
          const seen = new Set(page.threads.map((t) => t.id));
          const edge = page.threads[page.threads.length - 1]?.updated_at ?? "";
          setRows((old) => [
            ...page.threads,
            ...old.filter((t) => !seen.has(t.id) && t.updated_at < edge),
          ]);
        }
        loaded.current = { key, pages: Math.max(fresh ? 1 : loaded.current.pages, 1) };
      })
      .catch(() => {
        // A failed refresh leaves what is on screen. With nothing on screen, say so and offer to try again.
        if (current.current === key && fresh) setStalled(true);
      })
      .finally(() => {
        if (current.current === key) setLoading((l) => (l === "first" ? null : l));
      });
  }, [key, signal, again, sessionMode]);

  const more = useCallback(() => {
    if (local !== null) {
      setLocal(local + PAGE);
      return;
    }
    if (!next || loading) return;
    setLoading("more");
    setFailed(false);
    within(
      sessionMode
        ? sessionPage(agent, standing, next)
        : api.threadsPage({ limit: PAGE, origin: "api", agent, standing, before: next }),
    )
      .then((page) => {
        if (current.current !== key) return;
        setRows((old) => {
          const seen = new Set(old.map((t) => t.id));
          return [...old, ...page.threads.filter((t) => !seen.has(t.id))];
        });
        setNext(page.next ?? null);
        loaded.current = { key, pages: loaded.current.pages + 1 };
      })
      .catch(() => {
        if (current.current === key) setFailed(true);
      })
      .finally(() => {
        if (current.current === key) setLoading(null);
      });
  }, [agent, standing, key, next, local, loading, sessionMode]);

  return {
    threads: local === null ? rows : rows.slice(0, local),
    hasMore: local === null ? next !== null : rows.length > local,
    loading,
    failed,
    stalled,
    retry: () => {
      setStalled(false);
      setLoading("first");
      setAgain((n) => n + 1);
    },
    more,
  };
}

const roots = new Map<string, string>();

/** The root of a session: the one a person started, which Recents lists. */
async function rootOf(id: string): Promise<string> {
  let at = id;
  // An agent's session names the session that called it as its parent.
  for (let depth = 0; depth < 8 && at.startsWith("s_"); depth++) {
    const session = await call<{ parent?: string }>(
      "GET",
      `/api/sessions/${encodeURIComponent(at)}`,
    );
    if (!session.parent?.startsWith("s_")) break;
    at = session.parent;
  }
  return at;
}

/**
 * The Recents row the open session belongs to. Recents lists the sessions
 * people started; an agent's own session under one is part of it, so it is
 * shown as its root's row rather than as a second row with the same title.
 * Null while that is being found out, when no row is marked.
 */
export function useRecentsRow(open: string | null, listed: boolean): string | null {
  const direct = !open || listed || !open.startsWith("s_") || roots.has(open);
  const [found, setFound] = useState<{ of: string; root: string } | null>(null);
  useEffect(() => {
    if (direct || !open) return;
    let live = true;
    rootOf(open)
      .catch(() => open)
      .then((root) => {
        roots.set(open, root);
        if (live) setFound({ of: open, root });
      });
    return () => {
      live = false;
    };
  }, [open, direct]);
  if (!open) return null;
  if (listed) return open;
  if (roots.has(open)) return roots.get(open) ?? open;
  if (!open.startsWith("s_")) return open;
  return found?.of === open ? found.root : null;
}
