/** When a conversation last moved, as a person reads it: today's time, else a date. */
export function updatedWhen(iso: string, now = new Date()): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "";
  const sameDay = at.toDateString() === now.toDateString();
  return sameDay
    ? at.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })
    : at.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        ...(at.getFullYear() === now.getFullYear() ? {} : { year: "numeric" }),
      });
}

/**
 * Where a preview opens: beside the row when there is room on its right,
 * else under it across the screen, as on a phone. Kept inside the window.
 */
export function placePeek(
  row: { top: number; bottom: number; right: number; left: number },
  view: { width: number; height: number },
  card = { width: 288, height: 132 },
): { top: number; left: number; width: number } {
  const gap = 8;
  if (row.right + gap + card.width + 16 <= view.width) {
    const top = Math.min(Math.max(16, row.top), view.height - card.height - 16);
    return { top, left: row.right + gap, width: card.width };
  }
  const width = Math.min(card.width + 32, view.width - 32);
  const below = row.bottom + 4;
  const top =
    below + card.height + 16 <= view.height ? below : Math.max(16, row.top - card.height - 4);
  return { top, left: Math.max(16, (view.width - width) / 2), width };
}
