import { SystemAlertView } from "@humagen/components";
import {
  type ActionHandler,
  ActionId,
  type Budget,
  budgetPressure,
  formatUsd,
  type StopState,
  type SystemAlert,
} from "@humagen/core";
import {
  ActionContext,
  AlertOctagonIcon,
  AlertTriangleIcon,
  CheckCircleIcon,
  InfoIcon,
  PauseIcon,
  PlayIcon,
  Spinner,
  WebSurface,
  XIcon,
} from "@humagen/render-web";
import {
  type ReactNode,
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

/* ---------------- Stop phases ---------------- */

const stopCopy: Record<StopState, string> = {
  requested: "Stopping. Other viewers may still see it finish.",
  acknowledged: "Stop requested. Waiting for the agent to confirm.",
  failed: "Stopped here, but the agent didn't confirm. It may still finish elsewhere.",
  confirmed: "Stopped. You can pick up where it left off.",
};

/** Two-phase stop as one inline status row; Resume once the runtime confirms. */
export function StopBanner({
  turnId,
  stop,
  label = stopCopy[stop],
}: {
  turnId: string;
  stop: StopState;
  label?: string;
}) {
  const onAction = useContext(ActionContext);
  const icon =
    stop === "requested" || stop === "acknowledged" ? (
      <Spinner size={12} className="text-aui-muted" />
    ) : stop === "failed" ? (
      <AlertTriangleIcon size={13} className="text-aui-warn" />
    ) : (
      <PauseIcon size={13} className="text-aui-muted" />
    );
  return (
    <output
      data-stop={stop}
      className="flex min-h-7 items-center gap-2 rounded-aui border border-aui-border border-dashed px-2.5 py-1 text-aui-xs"
    >
      {icon}
      <span className="min-w-0 flex-1 text-aui-muted">
        <span className="font-medium text-aui-fg">Stopped</span> · {label}
      </span>
      {stop === "confirmed" && (
        <button
          type="button"
          onClick={() => onAction({ id: ActionId.resume, payload: { turnId, target: turnId } })}
          className="inline-flex h-6 items-center gap-1 rounded-aui-sm border border-aui-border px-2 font-medium text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <PlayIcon size={10} fill="currentColor" />
          Resume
        </button>
      )}
    </output>
  );
}

/* ---------------- System status ---------------- */

function dismissKey(alert: SystemAlert) {
  // Keyed per incident and status: an escalation (degraded → down) shows again.
  return `aui:dismissed:${alert.id}:${alert.status}`;
}

function readDismissed(alert: SystemAlert) {
  try {
    return window.localStorage.getItem(dismissKey(alert)) === "1";
  } catch {
    return false;
  }
}

/**
 * Dependency incident across the top of the app, using the shared
 * SystemAlertView. Updates in place; announced politely; dismissing hides
 * this incident at this status only.
 */
export function SystemStatusBanner({
  alert,
  onAction,
}: {
  alert: SystemAlert | null | undefined;
  onAction?: ActionHandler;
}) {
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    setDismissed(alert ? readDismissed(alert) : false);
  }, [alert]);
  if (!alert || alert.status === "operational" || dismissed) {
    return <div aria-live="polite" className="sr-only" />;
  }
  return (
    <div aria-live="polite" data-aui="system-status" className="relative">
      <WebSurface onAction={onAction} className="pr-8 [&_[data-aui=context]]:px-1">
        <SystemAlertView alert={alert} />
      </WebSurface>
      <button
        type="button"
        aria-label={`Dismiss ${alert.service} status`}
        onClick={() => {
          try {
            window.localStorage.setItem(dismissKey(alert), "1");
          } catch {}
          setDismissed(true);
        }}
        className="absolute top-1 right-1 inline-flex size-6 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <XIcon size={13} />
      </button>
    </div>
  );
}

/* ---------------- Spend cap ---------------- */

/** Above the composer when the next turn can't run: spend is at or would pass the cap. */
export function SpendBlockedBanner({
  budget,
  onAddBudget,
}: {
  budget: Budget | undefined;
  onAddBudget?: () => void;
}) {
  if (!budget || !budgetPressure(budget).blocked) return null;
  const capped = budget.spentUsd >= budget.limitUsd;
  return (
    <output className="flex items-center gap-2 rounded-aui border border-aui-warn/40 bg-aui-warn/10 px-2.5 py-1.5 text-aui-xs">
      <AlertTriangleIcon size={13} className="text-aui-warn" />
      <span className="min-w-0 flex-1 text-aui-muted">
        <span className="font-medium text-aui-fg">
          {capped ? "Spend cap reached" : "Next turn would pass the spend cap"}
        </span>{" "}
        · {formatUsd(budget.spentUsd)} of {formatUsd(budget.limitUsd)} this {budget.period}
      </span>
      {onAddBudget && (
        <button
          type="button"
          onClick={onAddBudget}
          className="inline-flex h-6 items-center rounded-aui-sm bg-aui-primary px-2 font-medium text-aui-primary-fg hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent"
        >
          Add Budget
        </button>
      )}
    </output>
  );
}

/* ---------------- Toasts ---------------- */

export type ToastKind = "success" | "warn" | "error" | "info";

export interface Toast {
  id: number;
  kind: ToastKind;
  message: ReactNode;
  /** Auto-hide after this many ms; 0 keeps it until dismissed. */
  durationMs: number;
}

let toasts: Toast[] = [];
let nextId = 1;
const listeners = new Set<() => void>();
const emit = () => {
  for (const l of listeners) l();
};

function dismissToast(id: number) {
  toasts = toasts.filter((t) => t.id !== id);
  emit();
}

/** Show short confirmations. Errors stay until dismissed unless a duration is given. */
export function useToasts() {
  const list = useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => toasts,
    () => toasts,
  );
  return {
    toasts: list,
    toast: (kind: ToastKind, message: ReactNode, durationMs = kind === "error" ? 0 : 4000) => {
      const id = nextId++;
      toasts = [...toasts.slice(-3), { id, kind, message, durationMs }];
      emit();
      return id;
    },
    dismiss: dismissToast,
  };
}

const toastIcon: Record<ToastKind, ReactNode> = {
  success: <CheckCircleIcon size={14} className="text-aui-ok" />,
  warn: <AlertTriangleIcon size={14} className="text-aui-warn" />,
  error: <AlertOctagonIcon size={14} className="text-aui-bad" />,
  info: <InfoIcon size={14} className="text-aui-accent" />,
};

function ToastItem({ toast }: { toast: Toast }) {
  const [paused, setPaused] = useState(false);
  const left = useRef(toast.durationMs);
  const started = useRef(0);
  useEffect(() => {
    if (!toast.durationMs || paused) return;
    started.current = Date.now();
    const t = setTimeout(() => dismissToast(toast.id), left.current);
    return () => {
      clearTimeout(t);
      left.current -= Date.now() - started.current;
    };
  }, [paused, toast.id, toast.durationMs]);
  return (
    <li
      role={toast.kind === "error" ? "alert" : "status"}
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
      className="aui-toast-in aui-glass pointer-events-auto flex items-center gap-2 rounded-aui py-1.5 pr-1 pl-2.5 text-aui-sm"
    >
      {toastIcon[toast.kind]}
      <span className="min-w-0 flex-1 text-aui-fg">{toast.message}</span>
      <button
        type="button"
        aria-label="Dismiss"
        onClick={() => dismissToast(toast.id)}
        className="inline-flex size-6 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <XIcon size={12} />
      </button>
    </li>
  );
}

/** Where toasts appear: bottom right, above everything. Render once. */
export function Toaster() {
  const { toasts: list } = useToasts();
  return (
    <ol
      aria-label="Notifications"
      className="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-[min(22rem,calc(100vw-2rem))] flex-col gap-1.5"
    >
      {list.map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </ol>
  );
}
