import type { PromptConfig } from "@agent-ui/core";
import { ArrowUpIcon, FileIcon, PaperclipIcon, XIcon } from "@agent-ui/render-web";
import {
  type FormEvent,
  type KeyboardEvent,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

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
  onSubmit: (message: string) => void;
  onStop?: () => void;
}) {
  const [value, setValue] = useState("");
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

  const canSend = !running && !disabled && value.trim().length > 0;

  const send = (message: string) => {
    const text = message.trim();
    if (!text || running || disabled) return;
    onSubmit(text);
    setValue("");
    ref.current?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send(value);
    } else if (e.key === "Escape" && running && onStop) {
      e.preventDefault();
      onStop();
    }
  };

  const onFormSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (running) onStop?.();
    else send(value);
  };

  const suggestions = showSuggestions ? (config.suggestions ?? []) : [];

  return (
    <div className="flex flex-col gap-3">
      {suggestions.length > 0 && (
        <ul className="grid gap-2 sm:grid-cols-2" aria-label="Suggested prompts">
          {suggestions.map((s) => (
            <li key={s.title} className="min-w-0">
              <button
                type="button"
                disabled={running || disabled}
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
        className={`flex flex-col gap-1 rounded-aui-lg border bg-aui-surface p-2 shadow-aui-md transition-[border-color,box-shadow] ${
          disabled
            ? "border-aui-border opacity-60"
            : "border-aui-border focus-within:border-aui-accent/60 focus-within:ring-4 focus-within:ring-aui-accent-soft"
        }`}
      >
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
          className="max-h-[200px] min-h-10 w-full resize-none bg-transparent px-2 py-2 text-aui-base text-aui-fg outline-none placeholder:text-aui-muted disabled:cursor-not-allowed"
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
            {running ? (
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
          <button
            type="submit"
            disabled={disabled || (!running && !canSend)}
            aria-label={running ? "Stop generating" : "Send message"}
            title={running ? "Stop generating" : "Send message"}
            className="ml-auto inline-flex size-8 shrink-0 items-center justify-center rounded-full bg-aui-primary text-aui-primary-fg shadow-aui-sm transition-[opacity,transform] hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent active:scale-95 disabled:opacity-30 motion-reduce:transition-none"
          >
            {running ? (
              <span aria-hidden className="size-2.5 rounded-[2px] bg-current" />
            ) : (
              <ArrowUpIcon size={16} strokeWidth={2.5} />
            )}
          </button>
        </div>
      </form>
    </div>
  );
}
