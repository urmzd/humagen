import { type ActionHandler, ActionId, type ActionPayload } from "@vultus/core";
import type { ButtonProps, ConfirmSpec, PrimitiveProps } from "@vultus/primitives";
import {
  Children,
  createContext,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { useGroup } from "./form";
import { AlertTriangleIcon, ChevronDownIcon } from "./icons";
import { SuppressContext } from "./meta";

export const ActionContext = createContext<ActionHandler>(() => {});

const base =
  "inline-flex h-7 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-aui px-2.5 font-medium text-aui-xs transition-[background-color,border-color,color,opacity,box-shadow] outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-offset-2 focus-visible:ring-offset-aui-bg disabled:pointer-events-none disabled:opacity-50";

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

/** Actions that need words, not a click: the web asks for them inline. */
const TEXT_ACTIONS: Partial<Record<string, string>> = {
  [ActionId.ask]: "Ask a question about this action",
  [ActionId.clarifyOther]: "Tell me what you need",
  [ActionId.reply]: "Type your reply",
};

/** One-line free text that sends with the action; Enter sends, Escape backs out. */
function InlineText({
  placeholder,
  label,
  onSend,
  onCancel,
}: {
  placeholder: string;
  label: string;
  onSend: (text: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState("");
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => ref.current?.focus(), []);
  return (
    <span className="aui-pop-in flex basis-full items-center gap-1.5 rounded-aui border border-aui-border bg-aui-bg py-0.5 pr-0.5 pl-2 focus-within:border-aui-accent/60">
      <textarea
        ref={ref}
        rows={1}
        value={value}
        aria-label={label}
        placeholder={placeholder}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            if (value.trim()) onSend(value.trim());
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          }
        }}
        className="h-6 min-w-0 flex-1 resize-none bg-transparent py-0.5 text-aui-sm leading-5 outline-none placeholder:text-aui-muted"
      />
      <button
        type="button"
        disabled={!value.trim()}
        onClick={() => onSend(value.trim())}
        className={`${buttonClass("primary")} h-6 px-2`}
      >
        Send
      </button>
    </span>
  );
}

export function Button({
  label,
  action,
  url,
  style,
  confirm,
  accessibilityLabel,
  collects,
}: ButtonProps) {
  const onAction = useContext(ActionContext);
  const askConfirm = useContext(ConfirmContext);
  const [inlineConfirm, setInlineConfirm] = useState(false);
  const [typing, setTyping] = useState(false);
  const group = useGroup(collects);
  const dense = useContext(DenseContext);
  const cls = `${buttonClass(style ?? "default")} ${dense ? "h-6 px-2" : ""}`;

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
  const fire = (extra?: Partial<ActionPayload>) => {
    if (!action) return;
    const payload: ActionPayload = { ...action.payload, ...extra };
    // Read the group at click time, the way Slack sends state.values with the click.
    if (collects) payload.selection = group?.selected ?? [];
    onAction({ id: action.id, payload });
  };

  // Outside an Actions row there is no row to take over; confirm in place.
  if (inlineConfirm && confirm) {
    return (
      <ConfirmPanel
        spec={confirm}
        onConfirm={() => fire()}
        onCancel={() => setInlineConfirm(false)}
      />
    );
  }

  const prompt = action ? TEXT_ACTIONS[action.id] : undefined;
  // "Approve Selected" becomes "Approve 2 of 3" so the count is in the verb.
  const shown =
    group && / Selected$/.test(label)
      ? `${label.replace(/ Selected$/, "")} ${group.selected.length} of ${group.ids.length}`
      : label;

  return (
    <>
      <button
        type="button"
        data-aui="button"
        data-style={style ?? "default"}
        className={cls}
        aria-label={accessibilityLabel ?? (shown !== label ? shown : undefined)}
        aria-haspopup={confirm ? "dialog" : undefined}
        aria-expanded={prompt ? typing : undefined}
        disabled={group ? group.selected.length === 0 : undefined}
        onClick={() => {
          if (prompt) return setTyping(!typing);
          if (!confirm) return fire();
          if (askConfirm) askConfirm(confirm, () => fire());
          else setInlineConfirm(true);
        }}
      >
        {shown}
      </button>
      {prompt && typing && (
        <InlineText
          placeholder={prompt}
          label={prompt}
          onCancel={() => setTyping(false)}
          onSend={(text) => {
            setTyping(false);
            // `text` is the field; `option` stays for handlers written before it existed.
            fire({ option: text, text });
          }}
        />
      )}
    </>
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
      className="aui-pop-in flex w-full flex-wrap items-center gap-x-3 gap-y-1.5 rounded-aui border border-aui-bad/30 bg-aui-bad/5 py-1.5 pr-1.5 pl-2.5"
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
  const suppressed = useContext(SuppressContext);
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

  if (kind && suppressed.has(`block:${kind}`)) return null;
  // Same as Slack: a row with nothing in it is left out.
  if (Children.toArray(children).length === 0) return null;
  return (
    <fieldset
      ref={row}
      data-aui="actions"
      data-aui-block={kind}
      aria-label={kind ? `${kind[0]?.toUpperCase()}${kind.slice(1)} actions` : "Actions"}
      className="m-0 flex min-w-0 flex-wrap items-center gap-1.5 border-0 p-0"
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

/** Buttons inside a dense row (a task row) shrink to 24px. */
export const DenseContext = createContext(false);

/**
 * Secondary choices. Right after a Button it attaches to it as a split button
 * (the chevron half); on its own it is a "⋯" overflow menu. Enter, Space, or
 * ArrowDown opens; arrows move; Escape closes and returns focus.
 */
export function Menu({ label, items }: PrimitiveProps["Menu"]) {
  const onAction = useContext(ActionContext);
  const dense = useContext(DenseContext);
  const [open, setOpen] = useState(false);
  const [split, setSplit] = useState<keyof typeof buttonVariant | null>(null);
  const [alignEnd, setAlignEnd] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const menuId = useId();

  useLayoutEffect(() => {
    const prev = trigger.current?.parentElement?.previousElementSibling;
    setSplit(
      prev?.getAttribute("data-aui") === "button"
        ? ((prev.getAttribute("data-style") as keyof typeof buttonVariant | null) ?? "default")
        : null,
    );
  }, []);

  const focusItem = (i: number) => {
    const els = list.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]");
    if (!els || els.length === 0) return;
    els[(i + els.length) % els.length]?.focus();
  };
  const show = (at = 0) => {
    const rect = trigger.current?.getBoundingClientRect();
    setAlignEnd(rect ? rect.left + 240 > window.innerWidth - 8 : false);
    setOpen(true);
    requestAnimationFrame(() => focusItem(at));
  };
  const close = (refocus = true) => {
    setOpen(false);
    if (refocus) trigger.current?.focus();
  };

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!trigger.current?.parentElement?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const h = dense ? "h-6" : "h-7";
  const triggerCls = split
    ? `${buttonClass(split)} ${h} w-6 rounded-l-none border-l border-l-[color-mix(in_oklab,currentColor_25%,transparent)] px-0`
    : `${buttonClass("ghost")} ${h} w-7 px-0`;

  return (
    <span
      data-aui="menu"
      data-split={split ? "" : undefined}
      className={`relative inline-flex ${split ? "-ml-1.5" : ""}`}
    >
      <button
        ref={trigger}
        type="button"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        className={triggerCls}
        onClick={() => (open ? close(false) : show())}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" || ((e.key === "Enter" || e.key === " ") && !open)) {
            e.preventDefault();
            show();
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            show(-1);
          }
        }}
      >
        {split ? (
          <ChevronDownIcon size={13} />
        ) : (
          <span aria-hidden className="text-aui-base leading-none">
            ⋯
          </span>
        )}
      </button>
      {open && (
        <div
          ref={list}
          id={menuId}
          role="menu"
          aria-label={label}
          tabIndex={-1}
          onKeyDown={(e) => {
            const els = Array.from(
              list.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]") ?? [],
            );
            const i = els.indexOf(document.activeElement as HTMLButtonElement);
            if (e.key === "ArrowDown") {
              e.preventDefault();
              focusItem(i + 1);
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              focusItem(i - 1);
            } else if (e.key === "Home") {
              e.preventDefault();
              focusItem(0);
            } else if (e.key === "End") {
              e.preventDefault();
              focusItem(-1);
            } else if (e.key === "Escape" || e.key === "Tab") {
              e.preventDefault();
              e.stopPropagation();
              close();
            }
          }}
          className={`aui-pop-in aui-glass absolute top-full z-50 mt-1 flex w-60 flex-col rounded-aui p-1 text-aui-sm ${alignEnd ? "right-0" : "left-0"}`}
        >
          {items.map((item) => (
            <button
              key={item.label}
              type="button"
              role="menuitem"
              tabIndex={-1}
              onClick={() => {
                close();
                onAction({ id: item.action.id, payload: item.action.payload });
              }}
              className={`flex flex-col rounded-aui-sm px-2 py-1.5 text-left outline-none hover:bg-aui-hover focus-visible:bg-aui-hover ${item.danger ? "text-aui-bad" : "text-aui-fg"}`}
            >
              <span className="font-medium">{item.label}</span>
              {item.description && (
                <span className="text-aui-2xs text-aui-muted">{item.description}</span>
              )}
            </button>
          ))}
        </div>
      )}
    </span>
  );
}
