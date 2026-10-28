import { messageState, resolveMessage, type UiAction } from "@humagen/core";
import { useCallback, useEffect, useRef, useState } from "react";
import { type PlatformMessage, transport as platform, type Transport, toRequest } from "./mail.ts";
import { within } from "./timeout.ts";

const PAGE = 30;

/** The first page laid over what is loaded: fresh rows first, then older pages already asked for. */
export function merge(
  loaded: PlatformMessage[],
  fresh: PlatformMessage[],
  end: boolean,
): PlatformMessage[] {
  if (end || fresh.length === 0) return fresh;
  const seen = new Set(fresh.map((m) => m.id));
  const edge = fresh[fresh.length - 1]?.at ?? "";
  return [...fresh, ...loaded.filter((m) => !seen.has(m.id) && m.at < edge)];
}

/**
 * The message being read, held apart from the list. A linked message may not
 * be on a loaded page, or not in the state shown (one the reader hid, while
 * Open is shown), and one just resolved leaves the list while its record is
 * still being read. Either way it stays on screen until another is chosen.
 * `messages` is the list with it, `missing` says a linked one could not be
 * read, and `keep` takes the platform's answer to an action on it.
 */
export function useHeld(
  list: PlatformMessage[],
  selected: string | null | undefined,
  loading: boolean,
  via: Transport,
) {
  const [held, setHeld] = useState<PlatformMessage | null>(null);
  const [missing, setMissing] = useState(false);
  const found = list.find((m) => m.id === selected);
  const listed = found !== undefined;

  useEffect(() => {
    if (found) setHeld(found);
  }, [found]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: fetch once per selection that the list does not hold
  useEffect(() => {
    setMissing(false);
    if (!selected || loading || listed || held?.id === selected) return;
    let live = true;
    via
      .get(selected)
      .then((m) => live && setHeld(m))
      .catch(() => live && setMissing(true));
    return () => {
      live = false;
    };
  }, [selected, loading]);

  const keep = useCallback((server: PlatformMessage | null) => {
    if (server) setHeld((was) => (was?.id === server.id ? server : was));
  }, []);
  const messages = held && held.id === selected && !listed ? [held, ...list] : list;
  return { messages, missing, keep };
}

/** A message as it will look once an action lands, for showing a click before the platform confirms it. */
export function after(message: PlatformMessage, action: UiAction, by: string): PlatformMessage {
  return resolveMessage(message, action, { by, at: new Date().toISOString() }) as PlatformMessage;
}

/**
 * The inbox, a page at a time, kept current and answerable.
 *
 * - `signal` changing fetches the first page again and lays it over what is
 *   loaded, so older pages and the scroll position stay.
 * - Changing the kind, or what is shown, starts from the first page.
 * - A click shows at once. If the platform refuses, the message goes back to
 *   how it was and `problem` says why.
 */
export function useInbox(
  kind: string,
  show: "open" | "resolved" | "all",
  signal: unknown,
  via: Transport = platform,
  viewer = "You",
) {
  const [messages, setMessages] = useState<PlatformMessage[]>([]);
  const [next, setNext] = useState<string | null>(null);
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(0);
  const [loading, setLoading] = useState<"first" | "more" | null>("first");
  const [failed, setFailed] = useState(false);
  const [moreFailed, setMoreFailed] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  // Both filters are the server's, so together they name what is loaded.
  const key = `${kind}|${show}`;
  const current = useRef(key);
  const pages = useRef({ key: "", count: 0 });

  // biome-ignore lint/correctness/useExhaustiveDependencies: signal is the reason to run again
  useEffect(() => {
    current.current = key;
    const fresh = pages.current.key !== key;
    if (fresh) {
      setMessages([]);
      setNext(null);
      setFailed(false);
      setMoreFailed(false);
      setLoading("first");
    }
    within(via.list({ limit: PAGE, state: show, kind: kind || undefined }))
      .then((page) => {
        if (current.current !== key) return;
        setUnread(page.unread);
        setOpen(page.open);
        setFailed(false);
        if (fresh || pages.current.count <= 1 || page.next === null) {
          setMessages(page.messages);
          setNext(page.next);
          pages.current = { key, count: 1 };
        } else {
          setMessages((old) => merge(old, page.messages, false));
        }
      })
      .catch(() => {
        // A refresh that fails leaves what is on screen. Only a first load with nothing to show is an error.
        if (current.current === key && fresh) setFailed(true);
      })
      .finally(() => {
        if (current.current === key) setLoading((l) => (l === "first" ? null : l));
      });
  }, [key, signal, via]);

  const more = useCallback(() => {
    if (!next || loading) return;
    setLoading("more");
    setMoreFailed(false);
    within(via.list({ limit: PAGE, state: show, kind: kind || undefined, before: next }))
      .then((page) => {
        if (current.current !== key) return;
        setMessages((old) => {
          const seen = new Set(old.map((m) => m.id));
          return [...old, ...page.messages.filter((m) => !seen.has(m.id))];
        });
        setNext(page.next);
        pages.current = { key, count: pages.current.count + 1 };
      })
      .catch(() => {
        if (current.current === key) setMoreFailed(true);
      })
      .finally(() => {
        if (current.current === key) setLoading(null);
      });
  }, [key, next, loading, via]);

  /**
   * Apply a click: show it, ask the platform, and undo it if the platform says
   * no. Resolves to the message as the platform has it, or null when the
   * action was not for the platform or was refused.
   */
  const act = useCallback(
    (message: PlatformMessage, action: UiAction): Promise<PlatformMessage | null> => {
      const request = toRequest(message, action);
      if (!request) return Promise.resolve(null);
      const shown = after(message, action, viewer);
      const put = (m: PlatformMessage) =>
        setMessages((all) => all.map((x) => (x.id === m.id ? m : x)));
      const wasUnread = Boolean(message.unread) && !shown.unread;
      const closed = messageState(message) === "open" && messageState(shown) !== "open";
      put(shown);
      if (wasUnread) setUnread((n) => Math.max(n - 1, 0));
      if (closed) setOpen((n) => Math.max(n - 1, 0));
      setProblem(null);
      return via
        .send(message, request)
        .then((server) => {
          put(server);
          return server;
        })
        .catch((e: unknown) => {
          put(message);
          if (wasUnread) setUnread((n) => n + 1);
          if (closed) setOpen((n) => n + 1);
          const sorry =
            request.call === "read"
              ? "Could not mark the message as read."
              : "That did not go through.";
          setProblem(e instanceof Error && e.message ? `${sorry} ${e.message}` : sorry);
          return null;
        });
    },
    [via, viewer],
  );

  return {
    messages,
    hasMore: next !== null,
    unread,
    open,
    loading,
    failed,
    moreFailed,
    problem,
    more,
    act,
  };
}
