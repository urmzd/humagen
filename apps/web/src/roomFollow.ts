import { runStateTypes, subscribe } from "./api.ts";

/**
 * Follows one conversation as its room (RFC 0004): a message posted in it, a
 * run in it changing state, or, in a large room, a page saying how much
 * happened since the last one. Each tells `changed` to read the
 * conversation's newest turns again, at most once every `wait` milliseconds,
 * since a page says only how much happened and never what. Returns a way to
 * stop. Where no stream can be opened it follows nothing, and the page still
 * refreshes with the workspace's stream.
 */
export function followRoom(thread: string, changed: () => void, wait = 150): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const soon = () => {
    clearTimeout(timer);
    timer = setTimeout(changed, wait);
  };
  let close: () => void;
  try {
    close = subscribe(
      { thread, type: runStateTypes },
      { onThread: soon, onEvent: soon, onPage: soon },
    );
  } catch {
    return () => {};
  }
  return () => {
    clearTimeout(timer);
    close();
  };
}
