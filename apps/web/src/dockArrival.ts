import { type RefObject, useLayoutEffect, useState } from "react";

/*
 * The first message sent from Home opens its conversation, and the chat bar
 * with its footer moves from the middle of Home to the bottom of the
 * conversation as one piece. Home remembers where the bar and its footer
 * were, and what they looked like, the moment it sends; the conversation's
 * loading screen draws that same picture, inert, where Home left it, and
 * slides it to its own place with the page's motion. The conversation's own
 * composer then takes over in the same place, so nothing disappears, jumps or
 * reflows. A reader who asked for less motion sees it arrive in place.
 */

export interface Departure {
  /** Where the bar and its footer were, in the viewport. */
  top: number;
  left: number;
  width: number;
  /** What they looked like: their markup, drawn inert while the conversation loads. */
  html: string;
}

let departure: Departure | null = null;

/** Home's bar is leaving for a conversation: remember where it was and how it looked. */
export function rememberDock(el: HTMLElement | null): void {
  if (!el) return;
  const r = el.getBoundingClientRect();
  departure = { top: r.top, left: r.left, width: r.width, html: el.outerHTML };
}

/** What Home left behind, if anything: read once, by whoever draws the bar next. */
export function takeDeparture(): Departure | null {
  const d = departure;
  departure = null;
  return d;
}

/** Forget a departure nobody took, such as when the send failed. */
export function forgetDeparture(): void {
  departure = null;
}

/**
 * Slides `el` from where Home's bar was to where it is laid out. Returns the
 * picture Home left, to draw in its place while the conversation loads.
 */
export function useDockArrival(el: RefObject<HTMLElement | null>): string | null {
  const [from] = useState(takeDeparture);
  useLayoutEffect(() => {
    const node = el.current;
    if (!from || !node) return;
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    if (reduce) return;
    const to = node.getBoundingClientRect();
    const dy = from.top - to.top;
    if (Math.abs(dy) < 1) return;
    node.style.transition = "none";
    node.style.transform = `translateY(${dy}px)`;
    // Read once so the start is laid out before the move begins.
    void node.offsetHeight;
    node.style.transition = "transform var(--aui-dur-page) var(--aui-ease-arrive)";
    node.style.transform = "";
    const done = () => {
      node.style.transition = "";
    };
    node.addEventListener("transitionend", done, { once: true });
    return () => node.removeEventListener("transitionend", done);
  }, [from, el]);
  return from?.html ?? null;
}
