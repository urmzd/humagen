import type { PromptConfig } from "@agent-ui/core";
import { type FormEvent, type KeyboardEvent, useEffect, useId, useRef, useState } from "react";

/**
 * Web prompt bar. Slack has a native composer, so this is web only; its
 * suggestions map to Slack's setSuggestedPrompts (see render-slack).
 *
 * Enter sends, Shift+Enter adds a line. While a turn runs, the send button
 * becomes Stop. While the agent waits on a decision, the input stays usable
 * so people can answer in words instead of clicking.
 */
export function PromptInput({
  config = {},
  running = false,
  disabled = false,
  showSuggestions = true,
  onSubmit,
  onStop,
}: {
  config?: PromptConfig;
  running?: boolean;
  disabled?: boolean;
  /** Typically only before the first message of a conversation. */
  showSuggestions?: boolean;
  onSubmit: (message: string) => void;
  onStop?: () => void;
}) {
  const [value, setValue] = useState("");
  const inputId = useId();
  const ref = useRef<HTMLTextAreaElement>(null);

  // Grow with content up to a cap, then scroll.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-measure on every value change
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [value]);

  const send = (message: string) => {
    const text = message.trim();
    if (!text || running || disabled) return;
    onSubmit(text);
    setValue("");
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send(value);
    }
  };

  const onFormSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (running) onStop?.();
    else send(value);
  };

  const suggestions = showSuggestions ? (config.suggestions ?? []) : [];

  return (
    <div className="flex flex-col gap-2">
      {suggestions.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Suggested prompts">
          {suggestions.map((s) => (
            <li key={s.title}>
              <button
                type="button"
                disabled={running || disabled}
                onClick={() => send(s.message)}
                className="rounded-full border border-aui-border bg-aui-surface px-3 py-1 text-sm text-aui-fg hover:border-aui-accent hover:bg-aui-accent-soft disabled:opacity-50"
              >
                {s.title}
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        onSubmit={onFormSubmit}
        className="flex items-end gap-2 rounded-aui border border-aui-border bg-aui-surface p-2 focus-within:border-aui-accent focus-within:ring-2 focus-within:ring-aui-accent-soft"
      >
        <label className="sr-only" htmlFor={inputId}>
          Message
        </label>
        <textarea
          id={inputId}
          ref={ref}
          rows={1}
          value={value}
          disabled={disabled}
          placeholder={config.placeholder ?? "Ask anything"}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={onKeyDown}
          className="max-h-[200px] min-h-9 flex-1 resize-none bg-transparent px-2 py-1.5 text-sm text-aui-fg outline-none placeholder:text-aui-muted"
        />
        <button
          type="submit"
          disabled={disabled || (!running && !value.trim())}
          aria-label={running ? "Stop" : "Send"}
          className="inline-flex size-9 shrink-0 items-center justify-center rounded-md bg-aui-primary text-aui-primary-fg transition-opacity hover:opacity-90 disabled:opacity-40"
        >
          {running ? (
            <span aria-hidden className="size-3 rounded-[2px] bg-current" />
          ) : (
            <svg aria-hidden="true" viewBox="0 0 16 16" className="size-4 fill-current">
              <path d="M8 2.5 13 7.5l-1.06 1.06L8.75 5.37V13.5h-1.5V5.37L4.06 8.56 3 7.5z" />
            </svg>
          )}
        </button>
      </form>
    </div>
  );
}
