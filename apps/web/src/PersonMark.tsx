// A person's mark, shared by a room's messages and the conversation header.
/** A person's initial, in a circle: who posted, without a picture to fetch. */
export function PersonMark({ name, size = 28 }: { name: string; size?: number }) {
  return (
    <span
      aria-hidden
      className="inline-flex shrink-0 items-center justify-center rounded-full bg-aui-hover font-medium text-aui-fg"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.42) }}
    >
      {(name.trim()[0] ?? "?").toUpperCase()}
    </span>
  );
}
