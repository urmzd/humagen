import type { ItemStop, ItemStopWords } from "./meta";

/**
 * An item's own Stop, at the top level of its row and always visible: a
 * small square and the word, never hidden in a menu or behind hover. While
 * the stop is asked and not yet recorded it says so and cannot be pressed
 * again; once recorded it is a quiet "Stopped", never an error.
 */
export function ItemStopControl({ stop, words }: { stop: ItemStop; words: ItemStopWords }) {
  if (stop.stopped)
    return (
      <output
        data-aui="item-stopped"
        aria-label={words.stoppedLabel(stop.what)}
        className="inline-flex shrink-0 items-center gap-1 px-1 text-aui-2xs text-aui-muted"
      >
        <span aria-hidden className="size-2 rounded-[2px] border border-current" />
        {words.stopped}
      </output>
    );
  if (!stop.onStop) return null;
  const onStop = stop.onStop;
  return (
    <button
      type="button"
      data-aui="item-stop"
      disabled={stop.pending}
      aria-label={stop.pending ? words.stoppingLabel(stop.what) : words.stopLabel(stop.what)}
      title={words.stopLabel(stop.what)}
      onClick={(event) => {
        // The row around it opens on a click; Stop is its own action.
        event.stopPropagation();
        onStop();
      }}
      className="inline-flex h-6 pointer-coarse:min-h-11 shrink-0 items-center gap-1.5 rounded-aui-sm px-1.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-progress disabled:opacity-60 motion-reduce:transition-none"
    >
      <span aria-hidden className="size-2 rounded-[2px] bg-current" />
      {stop.pending ? words.stopping : words.stop}
    </button>
  );
}
