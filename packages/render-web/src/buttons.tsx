import type { ActionHandler } from "@agent-ui/core";
import type { ButtonProps, ConfirmSpec, PrimitiveProps } from "@agent-ui/primitives";
import { createContext, useContext, useEffect, useId, useRef, useState } from "react";
import { AlertTriangleIcon, ThumbsDownIcon, ThumbsUpIcon } from "./icons";

export const ActionContext = createContext<ActionHandler>(() => {});

const base =
  "inline-flex h-8 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-aui px-3 font-medium text-aui-sm transition-[background-color,border-color,color,opacity,box-shadow] outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-offset-2 focus-visible:ring-offset-aui-bg disabled:pointer-events-none disabled:opacity-50";

export const buttonVariant = {
  primary: "bg-aui-primary text-aui-primary-fg shadow-aui-sm hover:opacity-90",
  danger: "bg-aui-bad text-aui-bad-fg shadow-aui-sm hover:opacity-90",
  default: "border border-aui-border bg-aui-surface text-aui-fg shadow-aui-sm hover:bg-aui-hover",
  ghost: "text-aui-muted hover:bg-aui-hover hover:text-aui-fg",
} as const;

export function buttonClass(variant: keyof typeof buttonVariant = "default") {
  return `${base} ${buttonVariant[variant]}`;
}

/**
 * A confirm step takes over the whole Actions row rather than swapping one
 * button in place, so "Edit" and "Cancel" can't sit next to "Yes, continue".
 */
const ConfirmContext = createContext<((spec: ConfirmSpec, run: () => void) => void) | null>(null);

export function Button({ label, action, url, style, confirm, accessibilityLabel }: ButtonProps) {
  const onAction = useContext(ActionContext);
  const askConfirm = useContext(ConfirmContext);
  const [inlineConfirm, setInlineConfirm] = useState(false);
  const cls = buttonClass(style ?? "default");

  if (url) {
    return (
      <a
        className={cls}
        href={url}
        aria-label={accessibilityLabel}
        target="_blank"
        rel="noreferrer"
      >
        {label}
      </a>
    );
  }
  const fire = () => action && onAction({ id: action.id, payload: action.payload });

  // Outside an Actions row there is no row to take over; confirm in place.
  if (inlineConfirm && confirm) {
    return (
      <ConfirmPanel spec={confirm} onConfirm={fire} onCancel={() => setInlineConfirm(false)} />
    );
  }
  return (
    <button
      type="button"
      className={cls}
      aria-label={accessibilityLabel}
      aria-haspopup={confirm ? "dialog" : undefined}
      onClick={() => {
        if (!confirm) return fire();
        if (askConfirm) askConfirm(confirm, fire);
        else setInlineConfirm(true);
      }}
    >
      {label}
    </button>
  );
}

function ConfirmPanel({
  spec,
  onConfirm,
  onCancel,
}: {
  spec: ConfirmSpec;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const titleId = useId();
  const textId = useId();
  const back = useRef<HTMLButtonElement>(null);
  // Land on the safe choice; Escape backs out.
  useEffect(() => back.current?.focus(), []);
  return (
    <div
      role="alertdialog"
      aria-labelledby={titleId}
      aria-describedby={textId}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onCancel();
        }
      }}
      className="aui-pop-in flex w-full flex-wrap items-center gap-x-3 gap-y-2 rounded-aui border border-aui-bad/30 bg-aui-bad/5 py-2 pr-2 pl-3"
    >
      <AlertTriangleIcon size={16} className="text-aui-bad" />
      <p className="min-w-0 flex-1 text-aui-sm">
        <span id={titleId} className="font-semibold text-aui-fg">
          {spec.title}
        </span>{" "}
        <span id={textId} className="text-aui-muted">
          {spec.text}
        </span>
      </p>
      <span className="flex gap-2">
        <button ref={back} type="button" className={buttonClass("default")} onClick={onCancel}>
          {spec.deny}
        </button>
        <button type="button" className={buttonClass("danger")} onClick={onConfirm}>
          {spec.confirm}
        </button>
      </span>
    </div>
  );
}

export function Actions({ children, blockId }: PrimitiveProps["Actions"]) {
  const [pending, setPending] = useState<{ spec: ConfirmSpec; run: () => void } | null>(null);
  const row = useRef<HTMLFieldSetElement>(null);
  const kind = blockId?.split(":")[0];

  const cancel = () => {
    setPending(null);
    // Give focus back to the row instead of dropping it on <body>.
    requestAnimationFrame(() =>
      row.current?.querySelector<HTMLElement>("button[aria-haspopup]")?.focus(),
    );
  };

  return (
    <fieldset
      ref={row}
      data-aui="actions"
      data-aui-block={kind}
      aria-label={kind ? `${kind[0]?.toUpperCase()}${kind.slice(1)} actions` : "Actions"}
      className="m-0 flex min-w-0 flex-wrap items-center gap-2 border-0 p-0"
    >
      {pending ? (
        <ConfirmPanel
          spec={pending.spec}
          onCancel={cancel}
          onConfirm={() => {
            setPending(null);
            pending.run();
          }}
        />
      ) : (
        <ConfirmContext.Provider value={(spec, run) => setPending({ spec, run })}>
          {children}
        </ConfirmContext.Provider>
      )}
    </fieldset>
  );
}

export function Feedback({ up, down, selected, labels }: PrimitiveProps["Feedback"]) {
  const onAction = useContext(ActionContext);
  const btn = (on: boolean) =>
    `inline-flex size-7 items-center justify-center rounded-aui-sm transition-colors outline-none focus-visible:ring-2 focus-visible:ring-aui-accent ${
      on
        ? "bg-aui-accent-soft text-aui-accent"
        : "text-aui-muted hover:bg-aui-hover hover:text-aui-fg"
    }`;
  const upLabel = labels?.up ?? "Helpful";
  const downLabel = labels?.down ?? "Not helpful";
  return (
    <fieldset
      data-aui="feedback"
      aria-label="Rate this answer"
      className="m-0 -ml-1 flex min-w-0 items-center gap-0.5 border-0 p-0"
    >
      <button
        type="button"
        aria-pressed={selected === "up"}
        aria-label={upLabel}
        title={upLabel}
        className={btn(selected === "up")}
        onClick={() => onAction(up)}
      >
        <ThumbsUpIcon
          size={14}
          fill={selected === "up" ? "currentColor" : "none"}
          fillOpacity={0.2}
        />
      </button>
      <button
        type="button"
        aria-pressed={selected === "down"}
        aria-label={downLabel}
        title={downLabel}
        className={btn(selected === "down")}
        onClick={() => onAction(down)}
      >
        <ThumbsDownIcon
          size={14}
          fill={selected === "down" ? "currentColor" : "none"}
          fillOpacity={0.2}
        />
      </button>
      {selected && (
        <output className="aui-fade-up ml-1.5 text-aui-muted text-aui-xs">
          Thanks for the feedback
        </output>
      )}
    </fieldset>
  );
}
