import { defaultCopy } from "@humagen/components";
import { BellIcon, CheckIcon } from "@humagen/render-web";
import { Modal } from "@humagen/web";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import { turnOf } from "./adapter.ts";
import { alertAnchor, alertReason, byTime, groupAlerts } from "./alertPlacement.ts";
import { call, type ThreadTurn } from "./api.ts";
import { agentShown } from "./branding.ts";
import { Pill } from "./cards.tsx";
import { href, type Route } from "./route.ts";
import {
  type Alert,
  alertActionLabel,
  alertOrigin,
  alertPage,
  openAlert,
  reviewable,
  sessionCenter,
} from "./sessionCenter.ts";
import { AgentMark, button, field, MoreMenu, type PageAction, Problem, primary } from "./ui.tsx";

export { alertAnchor };

/**
 * Every alert of a session, in every state: an open one waits where it
 * happened, and a settled one stays in the history, muted.
 */
export function useSessionAlerts(
  id: string,
  enabled: boolean,
  tick: unknown,
  onChanged: () => void,
) {
  const [rows, setRows] = useState<Alert[]>([]);
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [undo, setUndo] = useState<Alert[]>();
  const request = useRef(0);
  const refresh = useCallback(() => {
    const generation = ++request.current;
    if (!enabled || !id.startsWith("s_")) {
      setRows([]);
      return;
    }
    sessionCenter.alerts({ session: id }).then(
      (next) => {
        if (generation === request.current) {
          setRows(byTime(next));
          setProblem(undefined);
        }
      },
      (error) => {
        if (generation === request.current)
          setProblem(
            error instanceof Error ? error.message : "Session alerts could not be loaded.",
          );
      },
    );
  }, [id, enabled]);
  useEffect(() => {
    refresh();
    return () => {
      request.current++;
    };
  }, [refresh, tick]);
  useEffect(() => {
    setRows([]);
    setUndo(undefined);
  }, [id]);
  /** Change one alert, or every copy of a repeated one, together. */
  const change = async (alerts: Alert | Alert[], state: "open" | "resolved" | "snoozed") => {
    const all = Array.isArray(alerts) ? alerts : [alerts];
    if (busy || all.length === 0) return;
    setBusy(true);
    setProblem(undefined);
    try {
      for (const alert of all)
        await sessionCenter.change(alert.id, {
          state,
          ...(state === "snoozed"
            ? { snoozed_until: new Date(Date.now() + 3_600_000).toISOString() }
            : {}),
        });
      setUndo(state === "open" ? undefined : all);
      refresh();
      onChanged();
    } catch (error) {
      setProblem(error instanceof Error ? error.message : "The alert was not updated.");
    } finally {
      setBusy(false);
    }
  };
  return { rows, problem, busy, undo, refresh, change };
}
export type SessionAttention = ReturnType<typeof useSessionAlerts>;

/** Suppress only projected approvals whose run actually renders a pending approval. */
export function duplicatesInlineApproval(alert: Alert, turns: ThreadTurn[]): boolean {
  if (alert.kind !== "approval" || alert.action !== "approve" || !alert.run_id) return false;
  const run = turns.find((turn) => turn.run?.id === alert.run_id)?.run;
  return (
    !!run &&
    turnOf(run).interrupts.some(
      (item) => item.type === "approval" && item.decision.kind === "pending",
    )
  );
}

/** A plain click on an in-app address moves there, as a link would. */
function visit(to: string) {
  if (!to.startsWith("/")) return;
  window.history.pushState(null, "", to);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

/** An alert's one action, as quiet as the line it sits on. */
const textAction =
  "inline-flex min-h-7 pointer-coarse:min-h-11 shrink-0 items-center rounded-aui px-1.5 font-medium underline underline-offset-4 hover:no-underline focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50";
const iconAction =
  "inline-flex size-9 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50";

/** Red for a failure or urgency, amber for the rest. */
const tone = (alert: Alert) =>
  alert.priority === "urgent" || alert.severity === "critical" || alert.action === "retry"
    ? "bg-aui-bad"
    : "bg-aui-warn";

/**
 * A session's alerts, one quiet line each, where they happened in the
 * transcript. The same alert raised again on one answer is one line with a
 * count. Each line has one action; the rest wait in its ⋯ menu.
 */
export function SessionAlerts({
  alerts,
  attention,
  canWrite,
  agents,
  onOpen,
  beside = false,
}: {
  alerts: Alert[];
  attention: SessionAttention;
  canWrite: boolean;
  agents: string[];
  onOpen?: (id: string) => void;
  /**
   * The lines sit right after the answer they are about, so a link back to
   * that answer goes nowhere new. A function decides it alert by alert.
   */
  beside?: boolean | ((alert: Alert) => boolean);
}) {
  const [fix, setFix] = useState<{ alert: Alert; agent: string }>();
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [reviewing, setReviewing] = useState<string>();
  const [reviewProblem, setReviewProblem] = useState<{ id: string; message: string }>();
  const words = defaultCopy.attention;
  const review = async (alert: Alert) => {
    if (reviewing) return;
    setReviewing(alert.id);
    setReviewProblem(undefined);
    try {
      const session = await sessionCenter.review(alert.id);
      onOpen?.(session.id);
    } catch (error) {
      setReviewProblem({
        id: alert.id,
        message: error instanceof Error ? error.message : words.reviewFailed,
      });
    } finally {
      setReviewing(undefined);
    }
  };
  const startFix = async () => {
    if (!fix || busy) return;
    setBusy(true);
    setProblem(undefined);
    try {
      const session = await call<AgentSession>("POST", "/api/sessions", {
        source_alert: fix.alert.id,
        agent: fix.agent,
        text,
      });
      setFix(undefined);
      onOpen?.(session.id);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : "The fix session could not be opened.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      {/* One slot's lines sit together, as one entry of the transcript. */}
      {alerts.length > 0 && (
        <div className="flex min-w-0 flex-col">
          {groupAlerts(alerts).map((group) => (
            <AlertLine
              key={group.key}
              alerts={group.alerts}
              open={group.open}
              canWrite={canWrite}
              agents={agents}
              beside={typeof beside === "function" ? beside(group.lead) : beside}
              attention={attention}
              reviewing={reviewing}
              reviewProblem={reviewProblem}
              onReview={onOpen ? (alert) => void review(alert) : undefined}
              onOpen={onOpen}
              onFix={(alert, agent) => {
                setFix({ alert, agent });
                setText(
                  `Help resolve this alert: ${alert.title}${alert.body ? `\n\n${alert.body}` : ""}${alert.resolve_when ? `\n\nResolution needed: ${alert.resolve_when}` : ""}\n\nExplain the proposed fix before changing anything.`,
                );
              }}
            />
          ))}
        </div>
      )}
      {fix && (
        <Modal
          title="Start a fix session"
          onClose={() => {
            if (!busy) setFix(undefined);
          }}
        >
          <form
            className="flex flex-col gap-4 p-5"
            onSubmit={(event) => {
              event.preventDefault();
              void startFix();
            }}
          >
            <h3 className="text-aui-lg font-medium">{words.fixWith(agentShown(fix.agent))}</h3>
            <p className="text-aui-sm text-aui-muted">
              This child session keeps the alert’s access restrictions. Resolving the alert remains
              a separate action.
            </p>
            <label className="text-aui-sm">
              Request
              <textarea
                className={`${field} mt-2 min-h-36`}
                value={text}
                onChange={(event) => setText(event.target.value)}
              />
            </label>
            <Problem message={problem ?? null} />
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className={button}
                disabled={busy}
                onClick={() => setFix(undefined)}
              >
                Cancel
              </button>
              <button type="submit" className={primary} disabled={busy || !text.trim()}>
                {busy ? "Opening…" : "Start fix"}
              </button>
            </div>
          </form>
        </Modal>
      )}
    </>
  );
}

type Action = { label: string; run: () => void; disabled?: boolean } & (
  | { kind: "link"; to: string }
  | { kind: "button" }
);

/** One alert, or one alert raised several times, as one line. */
function AlertLine({
  alerts,
  open,
  canWrite,
  agents,
  beside,
  attention,
  reviewing,
  reviewProblem,
  onReview,
  onOpen,
  onFix,
}: {
  alerts: Alert[];
  open: boolean;
  canWrite: boolean;
  agents: string[];
  beside: boolean;
  attention: SessionAttention;
  reviewing?: string;
  reviewProblem?: { id: string; message: string };
  onReview?: (alert: Alert) => void;
  onOpen?: (id: string) => void;
  onFix: (alert: Alert, agent: string) => void;
}) {
  const detail = useId();
  const words = defaultCopy.attention;
  const alert = alerts[0]!;
  const anchor = alertAnchor(alert);
  const agent = alert.kind === "eval_below" ? "humagen" : alert.agent;
  const page = alertPage(alert);
  // Where View goes: the page the alert names, Connections, or the answer it is about.
  const view: Route | string | undefined = page
    ? page
    : alert.action === "reconnect"
      ? "/connections"
      : beside && anchor
        ? undefined
        : // An answer that is not on view, such as a specialist's, opens at its own address.
          alert.run_id && alert.run_id === anchor && !alert.message
          ? { page: "message", id: alert.run_id }
          : alertOrigin(alert);
  const viewLabel = page
    ? alertActionLabel(alert)
    : ((words.open as Record<string, string>)[alert.action ?? ""] ??
      (anchor ? words.view : words.open.session));
  const actions: Action[] = [];
  if (open && alert.review && onOpen)
    actions.push({ kind: "button", label: words.openReview, run: () => onOpen(alert.review!) });
  if (open && !alert.review && onReview && agents.length > 0 && reviewable(alert))
    actions.push({
      kind: "button",
      label: reviewing === alert.id ? words.reviewing : words.review,
      disabled: !!reviewing,
      run: () => onReview(alert),
    });
  if (view) {
    const to = typeof view === "string" ? view : href(view);
    actions.push({ kind: "link", label: viewLabel, to, run: () => visit(to) });
  }
  if (
    open &&
    onOpen &&
    !reviewable(alert) &&
    agent &&
    agents.includes(agent) &&
    (["retry", "answer"].includes(alert.action ?? "") || alert.kind === "eval_below")
  )
    actions.push({
      kind: "button",
      label: words.fixWith(agentShown(agent)),
      run: () => onFix(alert, agent),
    });
  if (!open && alert.review && onOpen)
    actions.push({ kind: "button", label: words.openReview, run: () => onOpen(alert.review!) });
  const [main, ...rest] = actions;
  const menu: PageAction[] = rest.map((a) => ({
    label: a.label,
    disabled: a.disabled,
    onSelect: a.run,
  }));
  if (open && canWrite)
    menu.push({
      label: words.snooze,
      disabled: attention.busy,
      onSelect: () => void attention.change(alerts, "snoozed"),
    });
  const state = (open ? "open" : (alert.state ?? "resolved")) as "open" | "snoozed" | "resolved";
  const said = [alert.body, alert.resolve_when && words.doneWhen(alert.resolve_when)]
    .filter(Boolean)
    .join("\n\n");
  return (
    <section
      tabIndex={-1}
      aria-label={`Alert: ${alert.title}`}
      aria-describedby={said ? detail : undefined}
      data-alert-line=""
      data-alert-message={anchor}
      data-alert-state={state}
      data-reviewed={alert.review ? "" : undefined}
      className={`flex min-w-0 flex-col rounded-aui px-1 text-aui-xs outline-none focus-visible:outline-2 focus-visible:outline-aui-accent ${open ? "" : "text-aui-muted"}`}
    >
      <div className="flex min-h-9 min-w-0 items-center gap-2">
        {open ? (
          <span
            role="img"
            aria-label={words.states.open}
            className={`size-2 shrink-0 rounded-full ${tone(alert)}`}
          />
        ) : (
          <Pill size="compact">{words.states[state]}</Pill>
        )}
        {agent ? (
          <span className={`flex shrink-0 items-center gap-1.5 ${open ? "" : "opacity-60"}`}>
            <AgentMark name={agent} size={16} />
            <span className={`font-medium ${open ? "text-aui-fg" : ""}`}>{agentShown(agent)}</span>
          </span>
        ) : (
          <BellIcon size={14} aria-hidden />
        )}
        <span className="min-w-0 flex-1 truncate" title={said || undefined}>
          {agent ? alertReason({ title: alert.title, agent: alert.agent }) : alert.title}
          {alerts.length > 1 && (
            <span className="ml-1.5 tabular-nums text-aui-muted">
              <span aria-hidden>{words.count(alerts.length)}</span>
              <span className="sr-only">{words.countLabel(alerts.length)}</span>
            </span>
          )}
        </span>
        {said && (
          <span id={detail} className="sr-only">
            {said}
          </span>
        )}
        {main &&
          (main.kind === "link" ? (
            <a
              className={`${textAction} ${open ? "text-aui-fg" : "text-aui-muted"}`}
              href={main.to}
              onClick={(event) => {
                if (event.button || event.metaKey || event.ctrlKey || event.shiftKey) return;
                event.preventDefault();
                main.run();
              }}
            >
              {main.label}
            </a>
          ) : (
            <button
              type="button"
              className={`${textAction} ${open ? "text-aui-fg" : "text-aui-muted"}`}
              disabled={main.disabled}
              onClick={main.run}
            >
              {main.label}
            </button>
          ))}
        {open && canWrite && (
          <button
            type="button"
            className={iconAction}
            aria-label={words.markDone}
            title={words.markDone}
            disabled={attention.busy}
            onClick={() => void attention.change(alerts, "resolved")}
          >
            <CheckIcon size={16} aria-hidden />
          </button>
        )}
        {menu.length > 0 && <MoreMenu label={words.more} items={menu} />}
      </div>
      {reviewProblem && alerts.some((a) => a.id === reviewProblem.id) && (
        <Problem message={reviewProblem.message} />
      )}
    </section>
  );
}

/** The first alert still waiting in the transcript, brought into view and focused. */
export function jumpToOpenAlert(root: ParentNode = document) {
  const line = root.querySelector<HTMLElement>('[data-alert-line][data-alert-state="open"]');
  if (!line) return false;
  line.scrollIntoView?.({ block: "center" });
  line.focus();
  return true;
}

/** How many alerts wait in a session, counting a repeated one once. */
export const openAlertCount = (alerts: Alert[]) => groupAlerts(alerts.filter(openAlert)).length;
