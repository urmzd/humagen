import type { PromptConfig } from "@vultus/core";
import {
  ArrowUpIcon,
  ChevronDownIcon,
  FileIcon,
  ListChecksIcon,
  PaperclipIcon,
  XIcon,
  ZapIcon,
} from "@vultus/render-web";
import {
  type FormEvent,
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { QueueStrip } from "./QueueStrip";
import type { QueueControls } from "./useQueue";

export interface PromptAttachment {
  id: string;
  name: string;
}

const MAX_HEIGHT = 200;

/**
 * Web prompt bar. Slack has a native composer, so this is web only; its
 * suggestions map to Slack's setSuggestedPrompts (see render-slack).
 *
 * Enter sends, Shift+Enter adds a line. While a turn runs, Send becomes Stop
 * (Escape also stops). While the agent waits on a decision, the input stays
 * usable so people can answer in words instead of clicking.
 *
 * With a `queue` (see useQueue), typing never waits: while a turn runs, Enter
 * queues the message (shown above the input, editable and reorderable) and
 * Cmd/Ctrl+Enter sends it now, interrupting the running turn. The send button
 * splits into Queue / Send now.
 */
export function PromptInput({
  config = {},
  running = false,
  disabled = false,
  showSuggestions = true,
  autoFocus = false,
  attachments,
  onAttach,
  onRemoveAttachment,
  onSubmit,
  onStop,
  queue,
  footer,
}: {
  config?: PromptConfig;
  running?: boolean;
  disabled?: boolean;
  /** Typically only before the first message of a conversation. */
  showSuggestions?: boolean;
  autoFocus?: boolean;
  /** Files staged for the next message. Shown as removable chips. */
  attachments?: PromptAttachment[];
  /** Shows the attach button. The host opens its own picker. */
  onAttach?: () => void;
  onRemoveAttachment?: (id: string) => void;
  onSubmit?: (message: string) => void;
  onStop?: () => void;
  /** Message queue. When set, it owns sending, Send Now, and Stop. */
  queue?: QueueControls;
  /** Rendered under the input inside the composer, e.g. a StatusLine. */
  footer?: ReactNode;
}) {
  const [value, setValue] = useState("");
  const [menu, setMenu] = useState(false);
  const inputId = useId();
  const hintId = useId();
  const ref = useRef<HTMLTextAreaElement>(null);

  // Grow with content up to a cap, then scroll.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-measure on every value change
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`;
    el.style.overflowY = el.scrollHeight > MAX_HEIGHT ? "auto" : "hidden";
  }, [value]);

  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);

  const busy = queue ? queue.running : running;
  const hasText = value.trim().length > 0;
  const canSend = !disabled && hasText && (!busy || Boolean(queue));
  const stop = () => (queue ? queue.stop() : onStop?.());
  const canStop = busy && (queue ? queue.state.status === "running" : Boolean(onStop));

  const send = (message: string, now = false) => {
    const text = message.trim();
    if (!text || disabled) return;
    if (queue) {
      if (now) queue.sendNow(text);
      else queue.send(text);
    } else {
      if (running) return;
      onSubmit?.(text);
    }
    setValue("");
    setMenu(false);
    ref.current?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send(value, Boolean(queue) && (e.metaKey || e.ctrlKey));
    } else if (e.key === "Escape" && canStop) {
      e.preventDefault();
      stop();
    }
  };

  const onFormSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (busy && !(queue && hasText)) stop();
    else send(value);
  };
  const queueing = Boolean(queue) && busy && hasText;

  const suggestions = showSuggestions ? (config.suggestions ?? []) : [];

  return (
    <div className="flex flex-col gap-3">
      {suggestions.length > 0 && (
        <ul className="grid gap-2 sm:grid-cols-2" aria-label="Suggested prompts">
          {suggestions.map((s) => (
            <li key={s.title} className="min-w-0">
              <button
                type="button"
                disabled={busy || disabled}
                onClick={() => send(s.message)}
                className="group flex w-full min-w-0 flex-col items-start gap-0.5 rounded-aui-lg border border-aui-border bg-aui-surface px-3.5 py-2.5 text-left transition-colors hover:border-aui-accent/60 hover:bg-aui-accent-soft focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent disabled:pointer-events-none disabled:opacity-50"
              >
                <span className="w-full truncate font-medium text-aui-fg text-aui-sm">
                  {s.title}
                </span>
                {s.message !== s.title && (
                  <span className="w-full truncate text-aui-muted text-aui-xs">{s.message}</span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        onSubmit={onFormSubmit}
        aria-label="Message the agent"
        data-aui="composer"
        data-running={busy || undefined}
        className={`flex flex-col gap-1 rounded-aui-lg border bg-aui-surface p-1.5 shadow-aui-md transition-[border-color,box-shadow] ${
          disabled
            ? "border-aui-border opacity-60"
            : "border-aui-border focus-within:border-aui-accent/60 focus-within:ring-4 focus-within:ring-aui-accent-soft"
        }`}
      >
        {queue && <QueueStrip queue={queue} />}
        {attachments && attachments.length > 0 && (
          <ul className="flex flex-wrap gap-1.5 px-1 pt-1" aria-label="Attachments">
            {attachments.map((a) => (
              <li
                key={a.id}
                className="inline-flex h-7 max-w-56 items-center gap-1.5 rounded-aui-sm border border-aui-border bg-aui-bg pr-1 pl-2 text-aui-fg text-aui-xs"
              >
                <FileIcon size={12} className="text-aui-muted" />
                <span className="truncate">{a.name}</span>
                {onRemoveAttachment && (
                  <button
                    type="button"
                    aria-label={`Remove ${a.name}`}
                    onClick={() => onRemoveAttachment(a.id)}
                    className="inline-flex size-5 items-center justify-center rounded-[0.25rem] text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                  >
                    <XIcon size={12} />
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        <label className="sr-only" htmlFor={inputId}>
          Message
        </label>
        <textarea
          id={inputId}
          ref={ref}
          rows={1}
          value={value}
          disabled={disabled}
          aria-describedby={hintId}
          placeholder={config.placeholder ?? "Ask anything"}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={onKeyDown}
          className="max-h-[200px] min-h-9 w-full resize-none bg-transparent px-2 py-1.5 text-aui-base text-aui-fg outline-none placeholder:text-aui-muted disabled:cursor-not-allowed"
        />
        <div className="flex items-center gap-2">
          {onAttach && (
            <button
              type="button"
              onClick={onAttach}
              disabled={disabled}
              aria-label="Attach files"
              title="Attach files"
              className="inline-flex size-8 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50"
            >
              <PaperclipIcon size={16} />
            </button>
          )}
          <p id={hintId} className="hidden truncate px-1 text-aui-2xs text-aui-muted sm:block">
            {busy && queue ? (
              <>
                <kbd className="font-aui-sans font-medium">Enter</kbd> to queue,{" "}
                <kbd className="font-aui-sans font-medium">⌘</kbd>+
                <kbd className="font-aui-sans font-medium">Enter</kbd> to send now,{" "}
                <kbd className="font-aui-sans font-medium">Esc</kbd> to stop
              </>
            ) : busy ? (
              <>
                <kbd className="font-aui-sans font-medium">Esc</kbd> to stop
              </>
            ) : (
              <>
                <kbd className="font-aui-sans font-medium">Enter</kbd> to send,{" "}
                <kbd className="font-aui-sans font-medium">Shift</kbd>+
                <kbd className="font-aui-sans font-medium">Enter</kbd> for a new line
              </>
            )}
          </p>
          {queueing ? (
            <div className="relative ml-auto flex shrink-0 items-center">
              <button
                type="submit"
                aria-label="Queue message"
                title="Queue message (Enter)"
                className="inline-flex h-8 items-center gap-1 rounded-l-full bg-aui-primary pr-2 pl-3 font-medium text-aui-primary-fg text-aui-xs shadow-aui-sm transition-opacity hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent"
              >
                <ListChecksIcon size={13} />
                Queue
              </button>
              <button
                type="button"
                aria-label="More send options"
                aria-haspopup="menu"
                aria-expanded={menu}
                onClick={() => setMenu(!menu)}
                className="inline-flex h-8 items-center rounded-r-full border-aui-primary-fg/20 border-l bg-aui-primary pr-2 pl-1.5 text-aui-primary-fg shadow-aui-sm hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent"
              >
                <ChevronDownIcon size={13} />
              </button>
              {menu && (
                <div
                  role="menu"
                  aria-label="Send options"
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      e.stopPropagation();
                      setMenu(false);
                    }
                  }}
                  className="aui-pop-in aui-glass absolute right-0 bottom-full z-30 mb-1.5 flex w-52 flex-col rounded-aui p-1 text-aui-sm"
                >
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => send(value)}
                    className="flex items-center gap-2 rounded-aui-sm px-2 py-1.5 text-left hover:bg-aui-hover focus-visible:bg-aui-hover focus-visible:outline-none"
                  >
                    <ListChecksIcon size={13} className="text-aui-muted" />
                    <span className="flex-1">Queue</span>
                    <kbd className="text-aui-2xs text-aui-muted">Enter</kbd>
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => send(value, true)}
                    className="flex items-center gap-2 rounded-aui-sm px-2 py-1.5 text-left hover:bg-aui-hover focus-visible:bg-aui-hover focus-visible:outline-none"
                  >
                    <ZapIcon size={13} className="text-aui-muted" />
                    <span className="flex-1">Send now</span>
                    <kbd className="text-aui-2xs text-aui-muted">⌘ Enter</kbd>
                  </button>
                </div>
              )}
            </div>
          ) : (
            <button
              type="submit"
              disabled={disabled || (busy ? !canStop : !canSend)}
              aria-label={busy ? "Stop generating" : "Send message"}
              title={busy ? "Stop generating" : "Send message"}
              className="ml-auto inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-aui-primary text-aui-primary-fg shadow-aui-sm transition-[opacity,transform] hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent active:scale-95 disabled:opacity-30 motion-reduce:transition-none"
            >
              {busy ? (
                <span aria-hidden className="size-2.5 rounded-[2px] bg-current" />
              ) : (
                <ArrowUpIcon size={16} strokeWidth={2.5} />
              )}
            </button>
          )}
        </div>
        {footer}
      </form>
    </div>
  );
}
