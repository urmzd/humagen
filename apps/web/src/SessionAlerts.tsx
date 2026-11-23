import { defaultCopy } from "@humagen/components";
import { BellIcon, buttonClass, CheckIcon } from "@humagen/render-web";
import { cn } from "@humagen/render-web/lib/utils";
import { Modal } from "@humagen/web";
import { type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";
import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import { turnOf } from "./adapter.ts";
import { alertAnchor, alertReason, byTime, groupAlerts } from "./alertPlacement.ts";
import { api, call, type ThreadTurn } from "./api.ts";
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
import { useWide } from "./wide.ts";

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
  // Undo is offered for a while, then the change stands.
  useEffect(() => {
    if (!undo) return;
    const timer = setTimeout(() => setUndo(undefined), UNDO_MS);
    return () => clearTimeout(timer);
  }, [undo]);
  /** Change one alert, or every copy of a repeated one, together. */
  const change = async (
    alerts: Alert | Alert[],
    state: "open" | "resolved" | "snoozed",
  ): Promise<boolean> => {
    const all = Array.isArray(alerts) ? alerts : [alerts];
    if (busy || all.length === 0) return false;
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
      return true;
    } catch (error) {
      setProblem(error instanceof Error ? error.message : "The alert was not updated.");
      return false;
    } finally {
      setBusy(false);
    }
  };
  return { rows, problem, busy, undo, refresh, change };
}
export type SessionAttention = ReturnType<typeof useSessionAlerts>;
/** How long Undo is offered after an alert changes. */
export const UNDO_MS = 8000;

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

/**
 * An alert's one text action: the same small quiet button as a failed
 * answer's Retry, so the transcript has one kind of action control.
 */
const textAction = cn(buttonClass("default"), "h-6 shrink-0 px-2 py-0 text-aui-xs");
const iconAction =
  "inline-flex size-9 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50";

/** Red for a failure or urgency, amber for the rest, muted once settled. */
const tone = (alert: Alert, open: boolean) =>
  !open
    ? "bg-aui-muted/50"
    : alert.priority === "urgent" || alert.severity === "critical" || alert.action === "retry"
      ? "bg-aui-bad"
      : "bg-aui-warn";

/** The agent an alert is about, if any: an eval regression is Humagen's own. */
const agentOf = (alert: Alert) => (alert.kind === "eval_below" ? "humagen" : alert.agent);

/** What a line says, the same for the eye and for a screen reader. */
export function alertSaid(alert: Alert): string {
  const agent = agentOf(alert);
  return agent
    ? `${agentShown(agent)} ${alertReason({ title: alert.title, agent: alert.agent })}`
    : alert.title;
}

/**
 * A failed run's own alert: its failure card already says the run failed
 * and offers Retry, so the alert adds Mark Done there instead of a line.
 */
export function foldsIntoFailure(alert: Alert, turns: ThreadTurn[]): string | undefined {
  if (!alert.run_id || !(alert.kind === "run_failed" || alert.action === "retry")) return;
  const run = turns.find((t) => t.run?.id === alert.run_id)?.run;
  return run?.status === "failed" ? run.id : undefined;
}

/** How many runs not on view a session reads to place their alerts. */
const ROOT_READS = 20;
/**
 * How long an alert waits for its run's root. A read that hangs past it
 * counts as failed, so the alert is placed by its time rather than hidden.
 */
export const ROOT_WAIT_MS = 4000;

/** `read`, or a rejection once `ms` pass first. A later answer is ignored. */
export function withinTime<T>(read: Promise<T>, ms: number): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const late = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`no answer in ${ms}ms`)), ms);
  });
  return Promise.race([read, late]).finally(() => clearTimeout(timer));
}

/**
 * The run at the top of each call tree an alert is about, when that run is
 * not on view itself, such as a specialist another agent called: read once
 * each, so its alert sits after the answer that called it. `waiting` names
 * the runs still being read.
 */
export function useAlertRoots(
  alerts: Alert[],
  turns: ThreadTurn[],
  wait = ROOT_WAIT_MS,
): { roots: Map<string, string>; waiting: Set<string> } {
  // A run's root, or "" once a read of it failed.
  const [roots, setRoots] = useState<Map<string, string>>(() => new Map());
  const asked = useRef(new Set<string>());
  const shown = new Set(turns.map((t) => t.run?.id).filter(Boolean));
  const off = [
    ...new Set(alerts.map((a) => a.run_id).filter((id): id is string => !!id && !shown.has(id))),
  ];
  const wanted = off
    .filter((id) => !asked.current.has(id))
    .slice(0, Math.max(0, ROOT_READS - asked.current.size));
  const key = wanted.join(",");
  useEffect(() => {
    if (!key) return;
    for (const id of key.split(",")) {
      asked.current.add(id);
      withinTime(api.run(id), wait).then(
        // A run nobody called is its own root: a turn, on a page not read yet.
        (run) => setRoots((m) => new Map(m).set(id, run.root ?? "")),
        // A failed or hanging read: the alert is placed at its own time.
        () => setRoots((m) => new Map(m).set(id, "")),
      );
    }
  }, [key, wait]);
  // Read, or about to be: its alert waits for the answer rather than moving once it comes.
  const reading = new Set([...asked.current, ...wanted]);
  return { roots, waiting: new Set(off.filter((id) => reading.has(id) && !roots.has(id))) };
}

type Fix = { alert: Alert; agent: string };

/**
 * A session's alerts, one quiet line each, where they happened in the
 * transcript. The same alert raised again on one answer is one line with a
 * count. Each line has one action; the rest wait in its ⋯ menu. As
 * `controls`, the alerts of a failed answer are only Mark Done and the menu,
 * drawn in that answer's failure card.
 */
export function SessionAlerts({
  alerts,
  attention,
  canWrite,
  agents,
  onOpen,
  beside = false,
  controls = false,
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
  controls?: boolean;
}) {
  const [fix, setFix] = useState<Fix>();
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [reviewing, setReviewing] = useState<string>();
  const [reviewProblem, setReviewProblem] = useState<{ id: string; message: string }>();
  const wide = useWide();
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
  const groups = groupAlerts(alerts);
  const line = (group: (typeof groups)[number]) => (
    <AlertLine
      // The oldest copy names the line, so settling it keeps the same line (and focus).
      key={group.alerts[group.alerts.length - 1]!.id}
      alerts={group.alerts}
      open={group.open}
      canWrite={canWrite}
      agents={agents}
      beside={typeof beside === "function" ? beside(group.lead) : beside}
      wide={wide}
      controls={controls}
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
  );
  let drawn: ReactNode = null;
  if (controls) {
    // A failed answer's alerts act together: one Mark Done, one menu. Once
    // settled, the card keeps a muted Done with Reopen in its menu.
    const open = byTime(alerts.filter(openAlert));
    const all = byTime(alerts);
    drawn =
      open.length > 0
        ? line({ key: "failure", alerts: [...open].reverse(), lead: open.at(-1)!, open: true })
        : all.length > 0
          ? line({ key: "failure", alerts: [...all].reverse(), lead: all.at(-1)!, open: false })
          : null;
  } else if (groups.length > 0)
    // One slot's lines sit together, as one entry of the transcript.
    drawn = <div className="flex min-w-0 flex-col">{groups.map(line)}</div>;
  return (
    <>
      {drawn}
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
  wide,
  controls,
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
  wide: boolean;
  controls: boolean;
  attention: SessionAttention;
  reviewing?: string;
  reviewProblem?: { id: string; message: string };
  onReview?: (alert: Alert) => void;
  onOpen?: (id: string) => void;
  onFix: (alert: Alert, agent: string) => void;
}) {
  const detail = useId();
  // A line, or the controls in a failure card: either way the element focus returns to.
  const self = useRef<HTMLFieldSetElement>(null);
  const words = defaultCopy.attention;
  const alert = alerts[0]!;
  const anchor = alertAnchor(alert);
  const agent = agentOf(alert);
  const page = alertPage(alert);
  // Where View goes: the page the alert names, Connections, or the answer it is about.
  const view: Route | string | undefined = page
    ? page
    : alert.action === "reconnect"
      ? "/connections"
      : (beside || controls) && anchor
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
  // A settled line asks nothing, and a phone has no room: the action waits in the menu.
  // In a failure card, the card's own Retry is the action.
  const main = open && wide && !controls ? actions[0] : undefined;
  const rest = main ? actions.slice(1) : actions;
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
  // Undo is brief: a settled alert can always be opened again from its menu.
  if (!open && canWrite)
    menu.push({
      label: words.reopen,
      disabled: attention.busy,
      onSelect: () => void attention.change(alerts, "open"),
    });
  const state = (open ? "open" : (alert.state ?? "resolved")) as "open" | "snoozed" | "resolved";
  const said = [alert.body, alert.resolve_when && words.doneWhen(alert.resolve_when)]
    .filter(Boolean)
    .join("\n\n");
  const markDone = open && canWrite && (
    <button
      type="button"
      className={iconAction}
      aria-label={words.markDone}
      title={words.markDone}
      disabled={attention.busy}
      onClick={async () => {
        // The line stays, settled: focus stays on it rather than falling to the page.
        if (await attention.change(alerts, "resolved")) self.current?.focus();
      }}
    >
      <CheckIcon size={16} aria-hidden />
    </button>
  );
  const more = menu.length > 0 && <MoreMenu label={words.more} items={menu} />;
  const name = `Alert: ${alertSaid(alert)}`;
  if (controls)
    return (
      <fieldset
        ref={self}
        tabIndex={-1}
        aria-label={name}
        data-alert-line=""
        data-alert-state={state}
        className="inline-flex min-w-0 items-center gap-1 rounded-aui focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        {!open && <Pill size="compact">{words.states[state]}</Pill>}
        {markDone}
        {more}
      </fieldset>
    );
  return (
    <section
      ref={self}
      tabIndex={-1}
      aria-label={name}
      aria-describedby={said ? detail : undefined}
      data-alert-line=""
      data-alert-message={anchor}
      data-alert-state={state}
      data-reviewed={alert.review ? "" : undefined}
      className={`flex min-w-0 flex-col rounded-aui px-1 text-aui-xs focus-visible:outline-2 focus-visible:outline-aui-accent ${open ? "" : "text-aui-muted"}`}
    >
      <div className="flex min-h-9 min-w-0 items-center gap-2">
        {/* The status keeps one slot, so every line's mark and name line up. */}
        <span
          role="img"
          aria-label={words.states[state]}
          className={`size-2 shrink-0 rounded-full ${tone(alert, open)}`}
        />
        {agent ? (
          <span className={`flex shrink-0 items-center gap-1.5 ${open ? "" : "opacity-60"}`}>
            <AgentMark name={agent} size={16} />
            <span className={`font-medium ${open ? "text-aui-fg" : ""}`}>{agentShown(agent)}</span>
          </span>
        ) : (
          <BellIcon size={14} aria-hidden />
        )}
        <span className="min-w-0 truncate" title={said || undefined}>
          {agent ? alertReason({ title: alert.title, agent: alert.agent }) : alert.title}
        </span>
        {/* Outside the reason, so a narrow line cuts the words and keeps the count. */}
        {alerts.length > 1 && (
          <span className="shrink-0 tabular-nums text-aui-muted">
            <span aria-hidden>{words.count(alerts.length)}</span>
            <span className="sr-only">{words.countLabel(alerts.length)}</span>
          </span>
        )}
        <span aria-hidden className="flex-1" />
        {said && (
          <span id={detail} className="sr-only">
            {said}
          </span>
        )}
        {/* A settled state reads at the right, as a card's status does. */}
        {!open && <Pill size="compact">{words.states[state]}</Pill>}
        {main &&
          (main.kind === "link" ? (
            <a
              className={textAction}
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
              className={textAction}
              disabled={main.disabled}
              onClick={main.run}
            >
              {main.label}
            </button>
          ))}
        {markDone}
        {/* With nothing in its menu the line keeps the menu's place, so states line up. */}
        {more || <span aria-hidden className="size-9 shrink-0 pointer-coarse:size-11" />}
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
  // Shown as focused even after a click, so the eye lands on the line.
  line.focus({ focusVisible: true } as FocusOptions);
  return true;
}

/** How many open lines a slot draws: a repeated alert is one line. */
export const openLines = (alerts: Alert[]) => groupAlerts(alerts).filter((g) => g.open).length;
