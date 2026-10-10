import type { QueuedMessage } from "@vultus/core";
import { GripIcon, PencilIcon, PlayIcon, XIcon, ZapIcon } from "@vultus/render-web";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import type { QueueControls } from "./useQueue";

const iconBtn =
  "inline-flex size-6 shrink-0 items-center justify-center rounded-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

function QueueItem({
  item,
  index,
  count,
  queue,
  onDragStart,
  onDropAt,
}: {
  item: QueuedMessage;
  index: number;
  count: number;
  queue: QueueControls;
  onDragStart: (id: string) => void;
  onDropAt: (index: number) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(item.text);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const handle = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (editing) input.current?.select();
  }, [editing]);

  const move = (e: KeyboardEvent, plainArrows: boolean) => {
    if (!(e.altKey || plainArrows)) return;
    if (e.key === "ArrowUp" && index > 0) {
      e.preventDefault();
      queue.move(item.id, index - 1);
    } else if (e.key === "ArrowDown" && index < count - 1) {
      e.preventDefault();
      queue.move(item.id, index + 1);
    }
  };
  // Keep focus on the handle after a keyboard move re-orders the list.
  // biome-ignore lint/correctness/useExhaustiveDependencies: refocus when position changes
  useEffect(() => {
    if (document.activeElement?.getAttribute("data-queue-handle") === item.id)
      handle.current?.focus();
  }, [index]);

  return (
    <li
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        onDropAt(index);
      }}
      className={`group flex min-w-0 items-center gap-1 rounded-aui-sm py-0.5 pr-0.5 pl-0.5 transition-colors hover:bg-aui-hover ${over ? "shadow-[inset_0_2px_0_var(--aui-accent)]" : ""}`}
    >
      <button
        ref={handle}
        type="button"
        draggable
        data-queue-handle={item.id}
        onDragStart={(e) => {
          e.dataTransfer.effectAllowed = "move";
          onDragStart(item.id);
        }}
        onKeyDown={(e) => move(e, true)}
        aria-label={`Reorder message ${index + 1} of ${count}. Use arrow keys to move.`}
        className={`${iconBtn} cursor-grab active:cursor-grabbing`}
      >
        <GripIcon size={12} />
      </button>
      <span className="w-4 shrink-0 text-center text-aui-2xs text-aui-muted tabular-nums">
        {index + 1}
      </span>
      {editing ? (
        <input
          ref={input}
          value={draft}
          aria-label={`Edit queued message ${index + 1}`}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => {
            if (draft.trim()) queue.edit(item.id, draft.trim());
            setEditing(false);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              if (draft.trim()) queue.edit(item.id, draft.trim());
              setEditing(false);
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setDraft(item.text);
              setEditing(false);
            }
          }}
          className="h-6 min-w-0 flex-1 rounded-aui-sm border border-aui-accent/50 bg-aui-bg px-1.5 text-aui-sm outline-none"
        />
      ) : (
        <button
          type="button"
          onClick={() => {
            setDraft(item.text);
            setEditing(true);
          }}
          onKeyDown={(e) => move(e, false)}
          title="Edit"
          className="min-w-0 flex-1 truncate rounded-aui-sm px-1 text-left text-aui-sm text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {item.text}
        </button>
      )}
      <span className="flex shrink-0 items-center opacity-70 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100">
        <button
          type="button"
          className={iconBtn}
          aria-label={`Edit "${item.text}"`}
          onClick={() => {
            setDraft(item.text);
            setEditing(true);
          }}
        >
          <PencilIcon size={11} />
        </button>
        <button
          type="button"
          className={`${iconBtn} w-auto gap-1 px-1.5 text-aui-2xs`}
          aria-label={`Send now: ${item.text}`}
          onClick={() => queue.promote(item.id)}
        >
          <ZapIcon size={11} />
          <span className="hidden sm:inline">Send now</span>
        </button>
        <button
          type="button"
          className={iconBtn}
          aria-label={`Remove "${item.text}"`}
          onClick={() => queue.remove(item.id)}
        >
          <XIcon size={12} />
        </button>
      </span>
    </li>
  );
}

/**
 * Messages waiting behind the running turn, inside the composer. Each one can
 * be edited in place, reordered (drag, or Alt+Up/Down), removed, or sent now.
 * When the queue is held (after a failure, a Stop, or in manual mode) a banner
 * says why and offers Resume or Clear.
 */
export function QueueStrip({ queue }: { queue: QueueControls }) {
  const { state } = queue;
  const dragging = useRef<string | null>(null);
  if (state.pending.length === 0) return null;

  const held = state.status === "held";
  const reason =
    state.heldReason === "failed"
      ? "Paused after a failure"
      : state.heldReason === "stopped"
        ? "Paused after you stopped"
        : "Waiting for you";

  return (
    <section aria-label="Queued messages" className="flex flex-col gap-0.5 px-1 pt-0.5">
      <header className="flex items-center gap-2 px-1 text-aui-2xs text-aui-muted">
        {held ? (
          <output className="flex min-w-0 items-center gap-1.5 text-aui-fg">
            <span
              aria-hidden
              className={`size-1.5 shrink-0 rounded-full ${state.heldReason === "failed" ? "bg-aui-warn" : "bg-aui-muted"}`}
            />
            <span className="truncate font-medium">{reason}</span>
            <span className="text-aui-muted">· {state.pending.length} queued</span>
          </output>
        ) : (
          <span className="tabular-nums">
            {state.pending.length} queued
            {state.status === "stopping" ? " · stopping current turn" : ""}
          </span>
        )}
        <span className="ml-auto flex items-center gap-1">
          {held && (
            <button
              type="button"
              onClick={queue.resume}
              className="inline-flex h-5 items-center gap-1 rounded-aui-sm bg-aui-primary px-1.5 font-medium text-aui-primary-fg hover:opacity-90 focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              <PlayIcon size={9} fill="currentColor" />
              Resume
            </button>
          )}
          <button
            type="button"
            onClick={queue.clear}
            className="inline-flex h-5 items-center rounded-aui-sm px-1.5 hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            Clear
          </button>
          <label className="inline-flex h-5 cursor-pointer items-center gap-1 rounded-aui-sm px-1.5 hover:bg-aui-hover hover:text-aui-fg">
            <input
              type="checkbox"
              checked={state.mode === "auto"}
              onChange={(e) => queue.setMode(e.target.checked ? "auto" : "manual")}
              className="size-3 accent-[var(--aui-accent)]"
            />
            Auto-send
          </label>
        </span>
      </header>
      <ol className="flex max-h-36 flex-col overflow-y-auto aui-scroll">
        {state.pending.map((item, i) => (
          <QueueItem
            key={item.id}
            item={item}
            index={i}
            count={state.pending.length}
            queue={queue}
            onDragStart={(id) => {
              dragging.current = id;
            }}
            onDropAt={(to) => {
              if (dragging.current) queue.move(dragging.current, to);
              dragging.current = null;
            }}
          />
        ))}
      </ol>
      <div aria-hidden className="mx-1 mt-0.5 border-aui-border border-t" />
    </section>
  );
}
