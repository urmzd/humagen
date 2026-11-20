/** How wide a row's preview is beside the sidebar. */
export const PEEK_WIDTH = 288;

/**
 * Where a preview opens: beside the row, centred on it, when there is room on
 * its right; else under it across the screen, as on a phone. Kept inside the
 * window. `card.height` is the card's measured height once it has drawn.
 */
export function placePeek(
  row: { top: number; bottom: number; right: number; left: number },
  view: { width: number; height: number },
  card = { width: PEEK_WIDTH, height: 64 },
): { top: number; left: number; width: number } {
  const gap = 8;
  const margin = 16;
  if (row.right + gap + card.width + margin <= view.width) {
    const centred = (row.top + row.bottom) / 2 - card.height / 2;
    const top = Math.max(margin, Math.min(centred, view.height - card.height - margin));
    return { top, left: row.right + gap, width: card.width };
  }
  const width = Math.min(card.width + 32, view.width - 32);
  const below = row.bottom + 4;
  const top =
    below + card.height + margin <= view.height
      ? below
      : Math.max(margin, row.top - card.height - 4);
  return { top, left: Math.max(margin, (view.width - width) / 2), width };
}
