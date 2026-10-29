import { ActionId, FEEDBACK_CLEAR } from "@humagen/core";
import type { FeedbackPromptLabels, PrimitiveProps } from "@humagen/primitives";
import { createContext, useContext, useEffect, useId, useRef, useState } from "react";
import { ActionContext, buttonClass } from "./buttons";
import { ThumbsDownIcon, ThumbsUpIcon } from "./icons";
import { SuppressContext } from "./meta";

/** Set to false to rate without being asked for a comment. On by default. */
export const FeedbackPromptContext = createContext(true);

const DEFAULT_PROMPT: FeedbackPromptLabels = {
  askUp: "What was helpful?",
  askDown: "What went wrong?",
  placeholder: "Add a comment (optional)",
  send: "Send",
  skip: "Skip",
  thanks: "Thanks for the feedback",
  reasons: [
    { id: "wrong", label: "Wrong" },
    { id: "incomplete", label: "Incomplete" },
    { id: "slow", label: "Too slow" },
    { id: "other", label: "Other" },
  ],
};

type Rated = "up" | "down";

/**
 * Thumbs up and down, then an optional comment.
 *
 * The rating is sent on the click, before anything is asked, so closing the
 * prompt can never lose it. The prompt then offers a comment (and, for a
 * thumbs-down, a quick reason); Send adds it as `ActionId.feedbackComment`,
 * while Skip, Escape and clicking away add nothing. Clicking the selected
 * rating again takes it back and asks nothing.
 */
export function Rating({ up, down, selected, labels, prompt }: PrimitiveProps["Feedback"]) {
  const onAction = useContext(ActionContext);
  const asks = useContext(FeedbackPromptContext) && prompt !== false;
  const words = { ...DEFAULT_PROMPT, ...(prompt || {}) };
  // Shows the click at once; the host's own `selected` takes over when it arrives.
  const [local, setLocal] = useState<Rated | null | undefined>(undefined);
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset when the host's value changes
  useEffect(() => setLocal(undefined), [selected]);
  const current = local === undefined ? selected : (local ?? undefined);
  const [asking, setAsking] = useState<Rated | null>(null);
  const [thanked, setThanked] = useState(false);
  const [comment, setComment] = useState("");
  const [reason, setReason] = useState<string | null>(null);
  const root = useRef<HTMLFieldSetElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const buttons = useRef<Partial<Record<Rated, HTMLButtonElement | null>>>({});
  const title = useId();

  const close = (rating: Rated) => {
    setAsking(null);
    setComment("");
    setReason(null);
    setThanked(true);
    // Back to where the person was, so the keyboard doesn't lose its place.
    buttons.current[rating]?.focus();
  };
  const rate = (rating: Rated) => {
    const ref = rating === "up" ? up : down;
    if (current === rating) {
      onAction({ id: ActionId.feedback, payload: { ...ref.payload, target: FEEDBACK_CLEAR } });
      setLocal(null);
      setAsking(null);
      setThanked(false);
      return;
    }
    onAction(ref);
    setLocal(rating);
    setComment("");
    setReason(null);
    if (asks) setAsking(rating);
    else setThanked(true);
  };
  const send = (rating: Rated) => {
    const text = comment.trim();
    if (text || reason) {
      const ref = rating === "up" ? up : down;
      onAction({
        id: ActionId.feedbackComment,
        payload: {
          ...ref.payload,
          ...(text ? { text } : {}),
          ...(reason ? { option: reason } : {}),
        },
      });
    }
    close(rating);
  };

  useEffect(() => {
    if (!asking) return;
    field.current?.focus();
    // Clicking away is the same as Skip.
    const away = (e: MouseEvent) => {
      if (!root.current?.contains(e.target as Node)) {
        setAsking(null);
        setThanked(true);
      }
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [asking]);

  const btn = (on: boolean) =>
    `inline-flex size-8 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-aui-accent ${
      on
        ? "bg-aui-accent-soft text-aui-accent"
        : "text-aui-muted hover:bg-aui-hover hover:text-aui-fg"
    }`;
  const thumb = (rating: Rated) => {
    const label = rating === "up" ? (labels?.up ?? "Helpful") : (labels?.down ?? "Not helpful");
    const Icon = rating === "up" ? ThumbsUpIcon : ThumbsDownIcon;
    return (
      <button
        ref={(el) => {
          buttons.current[rating] = el;
        }}
        type="button"
        aria-pressed={current === rating}
        aria-label={label}
        title={label}
        className={btn(current === rating)}
        onClick={() => rate(rating)}
      >
        <Icon size={16} fill={current === rating ? "currentColor" : "none"} fillOpacity={0.2} />
      </button>
    );
  };

  return (
    <fieldset
      ref={root}
      data-aui="feedback"
      aria-label="Rate this answer"
      className="relative m-0 flex min-w-0 items-center gap-1 border-0 p-0"
    >
      {thumb("up")}
      {thumb("down")}
      {thanked && !asking && current && (
        <output className="ml-1.5 text-aui-muted text-aui-xs">{words.thanks}</output>
      )}
      {asking && (
        <form
          role="dialog"
          aria-labelledby={title}
          data-aui="feedback-prompt"
          className="absolute top-full left-0 z-20 mt-1 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2 rounded-aui border border-aui-border bg-aui-bg p-2.5 shadow-lg"
          onSubmit={(e) => {
            e.preventDefault();
            send(asking);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              close(asking);
            }
          }}
        >
          <p id={title} className="font-medium text-aui-sm">
            {asking === "up" ? words.askUp : words.askDown}
          </p>
          {asking === "down" && words.reasons.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {words.reasons.map((r) => (
                <button
                  key={r.id}
                  type="button"
                  aria-pressed={reason === r.id}
                  onClick={() => setReason(reason === r.id ? null : r.id)}
                  className="inline-flex h-6 items-center rounded-full border border-aui-border px-2 text-aui-muted text-aui-xs hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:border-aui-accent aria-pressed:bg-aui-accent-soft aria-pressed:text-aui-fg"
                >
                  {r.label}
                </button>
              ))}
            </div>
          )}
          <textarea
            ref={field}
            rows={3}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault();
                send(asking);
              }
            }}
            aria-label={asking === "up" ? words.askUp : words.askDown}
            placeholder={words.placeholder}
            className="w-full resize-none rounded-aui-sm border border-aui-border bg-transparent px-2 py-1 text-aui-sm outline-none placeholder:text-aui-muted focus:border-aui-accent/60"
          />
          <div className="flex justify-end gap-1.5">
            <button
              type="button"
              className={`${buttonClass("default")} h-6 px-2`}
              onClick={() => close(asking)}
            >
              {words.skip}
            </button>
            <button type="submit" className={`${buttonClass("primary")} h-6 px-2`}>
              {words.send}
            </button>
          </div>
        </form>
      )}
    </fieldset>
  );
}

/** The Feedback primitive on the web. A frame that draws its own rating hides this one. */
export function Feedback(props: PrimitiveProps["Feedback"]) {
  if (useContext(SuppressContext).has("block:feedback")) return null;
  return (
    <div className="-ml-1">
      <Rating {...props} />
    </div>
  );
}
