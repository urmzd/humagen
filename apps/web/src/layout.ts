/**
 * Where things sit on a narrow screen: a row that scrolls sideways inside
 * itself, and a panel that opens over the page. Pure: ui.tsx reads the
 * measurements and applies what these return, and the tests check the
 * arithmetic without a browser.
 */

/** The room kept between anything on the page and the screen's edge. */
export const GUTTER_PX = 16;

/** How wide a strip's faded end is. A chosen item is never left under it. */
export const FADE_PX = 32;

/** A span along one axis, in a strip's content: where it starts and ends. */
export interface Span {
  start: number;
  end: number;
}

/**
 * Where a strip scrolls to so an item shows whole, with `pad` to spare past
 * it where the strip goes on, so the next item peeks out from under the fade.
 * Unchanged when the item already shows that way. `view` is the strip: how far
 * it is scrolled, how wide it is, and how wide its content is.
 */
export function revealAt(item: Span, view: { scroll: number; width: number; content: number }, pad = FADE_PX): number {
  const max = Math.max(view.content - view.width, 0);
  // Near either end, the end itself is the place: nothing is faded there.
  const room = Math.min(pad, Math.max((view.width - (item.end - item.start)) / 2, 0));
  let next = view.scroll;
  if (item.start - room < next) next = item.start - room;
  else if (item.end + room > next + view.width) next = item.end + room - view.width;
  return Math.round(Math.min(Math.max(next, 0), max));
}

/** Which ends of a strip have more past them: what the stylesheet fades. */
export type More = "start" | "end" | "both" | "none";

export function moreOf(view: { scroll: number; width: number; content: number }): More {
  // A pixel of slack, for the fractions a zoomed screen scrolls by.
  const before = view.scroll > 1;
  const after = view.scroll + view.width < view.content - 1;
  return before && after ? "both" : before ? "start" : after ? "end" : "none";
}

/**
 * How far to move a panel sideways so it sits inside the screen with `gutter`
 * to spare on each side: right when its left edge is too close, left when its
 * right edge is. A panel wider than the room left starts at the left gutter.
 */
export function nudgeInto(panel: { left: number; width: number }, viewport: number, gutter = GUTTER_PX): number {
  const right = panel.left + panel.width;
  if (panel.left < gutter) return gutter - panel.left;
  if (right > viewport - gutter) return Math.max(viewport - gutter - right, gutter - panel.left);
  return 0;
}
