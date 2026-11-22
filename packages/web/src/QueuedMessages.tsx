import type { QueuedMessage } from "@humagen/core";
import { PencilIcon, XIcon } from "@humagen/render-web";
import { useEffect, useRef, useState } from "react";
import type { QueueControls } from "./useQueue";

const iconBtn =
  "inline-flex size-7 shrink-0 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11";
const textBtn =
  "inline-flex min-h-7 items-center rounded-aui px-1.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11";

function Queued({
  item,
  next,
  queue,
}: {
  item: QueuedMessage;
  /** Send Now chose it: it goes as soon as the turn it interrupts ends. */
  next: boolean;
  queue: QueueControls;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(item.text);
  const box = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (editing) box.current?.select();
  }, [editing]);
  const save = () => {
    if (draft.trim()) queue.edit(item.id, draft.trim());
    setEditing(false);
  };
  const bubble =
    "max-w-[80%] rounded-aui-lg border border-aui-border border-dashed px-3.5 py-2 text-aui-sm";
  return (
    <li data-aui="queued-message" className="flex w-full flex-col items-end gap-1">
      {editing ? (
        <textarea
          ref={box}
          value={draft}
          rows={Math.min(8, draft.split("\n").length)}
          aria-label="Edit queued message"
          onChange={(e) => setDraft(e.target.value)}
          onBlur={save}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              save();
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setDraft(item.text);
              setEditing(false);
            }
          }}
          className={`${bubble} w-[80%] resize-none border-solid bg-transparent text-aui-fg outline-none focus-visible:border-aui-fg`}
        />
      ) : (
        <p className={`${bubble} whitespace-pre-wrap break-words text-aui-muted`}>{item.text}</p>
      )}
      <span className="flex items-center gap-1 text-aui-xs text-aui-muted">
        <span className="px-1.5">{next ? "Sending next" : "Queued"}</span>
        {!next && (
          <button
            type="button"
            className={textBtn}
            aria-label={`Send now: ${item.text}`}
            onClick={() => queue.promote(item.id)}
          >
            Send now
          </button>
        )}
        <button
          type="button"
          className={iconBtn}
          aria-label={`Edit "${item.text}"`}
          title="Edit"
          onClick={() => {
            setDraft(item.text);
            setEditing(true);
          }}
        >
          <PencilIcon size={14} />
        </button>
        <button
          type="button"
          className={iconBtn}
          aria-label={`Remove "${item.text}"`}
          title="Remove"
          onClick={() => queue.remove(item.id)}
        >
          <XIcon size={14} />
        </button>
      </span>
    </li>
  );
}

/**
 * Messages waiting behind the running turn, in the conversation: each where
 * it will be sent, as the person's own message, greyed until its turn. Each
 * can be sent now, edited or removed. When the queue is held (after a
 * failure or a Stop) a line says why and offers Resume.
 */
export function QueuedMessages({ queue }: { queue: QueueControls }) {
  const { state } = queue;
  if (state.pending.length === 0) return null;
  const held = state.status === "held";
  const reason =
    state.heldReason === "failed"
      ? "Paused after a failure"
      : state.heldReason === "stopped"
        ? "Paused after you stopped"
        : "Waiting for you";
  return (
    <section
      aria-label="Queued messages"
      data-aui="queued-messages"
      className="flex flex-col items-end gap-3"
    >
      <ol className="flex w-full flex-col items-end gap-3">
        {state.pending.map((item) => (
          <Queued key={item.id} item={item} next={state.interruptFor === item.id} queue={queue} />
        ))}
      </ol>
      {(held || state.pending.length > 1) && (
        <span className="flex items-center gap-1 text-aui-xs text-aui-muted">
          {held && <output className="px-1.5">{reason}</output>}
          {held && (
            <button type="button" className={`${textBtn} text-aui-fg`} onClick={queue.resume}>
              Resume
            </button>
          )}
          {state.pending.length > 1 && (
            <button type="button" className={textBtn} onClick={queue.clear}>
              Clear queue
            </button>
          )}
        </span>
      )}
    </section>
  );
}
