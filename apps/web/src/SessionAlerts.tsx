import { Modal } from "@humagen/web";
import { useCallback, useEffect, useRef, useState } from "react";
import type { AgentSession } from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
import { href } from "./route.ts";
import { AlertMark } from "./SessionCenter.tsx";
import {
  type Alert,
  alertActionLabel,
  alertPage,
  openAlert,
  orderAlerts,
  sessionCenter,
} from "./sessionCenter.ts";
import { button, field, Problem, primary } from "./ui.tsx";

export function useSessionAlerts(
  id: string,
  enabled: boolean,
  tick: unknown,
  onChanged: () => void,
) {
  const [rows, setRows] = useState<Alert[]>([]);
  const [problem, setProblem] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [undo, setUndo] = useState<Alert>();
  const request = useRef(0);
  const refresh = useCallback(() => {
    const generation = ++request.current;
    if (!enabled || !id.startsWith("s_")) {
      setRows([]);
      return;
    }
    sessionCenter.alerts({ session: id, state: "open" }).then(
      (next) => {
        if (generation === request.current) {
          setRows(orderAlerts(next.filter(openAlert)));
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
  const change = async (alert: Alert, state: "open" | "resolved" | "snoozed") => {
    if (busy) return;
    setBusy(true);
    setProblem(undefined);
    try {
      await sessionCenter.change(alert.id, {
        state,
        ...(state === "snoozed"
          ? { snoozed_until: new Date(Date.now() + 3_600_000).toISOString() }
          : {}),
      });
      setUndo(state === "open" ? undefined : alert);
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
export const alertAnchor = (alert: Alert) => alert.message || alert.run_id;

/** Recorded alerts sit beside their message; old or session-wide anchors stay above history. */
export function SessionAlerts({
  alerts,
  attention,
  canWrite,
  agents,
  onOpen,
}: {
  alerts: Alert[];
  attention: SessionAttention;
  canWrite: boolean;
  agents: string[];
  onOpen?: (id: string) => void;
}) {
  const [fix, setFix] = useState<{ alert: Alert; agent: string }>();
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
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
      {alerts.map((alert) => {
        const anchor = alertAnchor(alert);
        const agent = alert.kind === "eval_below" ? "humagen" : alert.agent;
        // A knowledge alert's action is the page it is about, such as its corpus.
        const page = alertPage(alert);
        const action = page
          ? alertActionLabel(alert)
          : ((
              {
                answer: "Open question",
                approve: "Review approval",
                retry: "Review failed answer",
                reconnect: "Open connections",
              } as Record<string, string>
            )[alert.action ?? ""] ?? "Open session");
        return (
          <section
            key={alert.id}
            aria-label={`Alert: ${alert.title}`}
            data-alert-message={anchor}
            className="flex flex-col gap-2 rounded-aui border border-aui-border bg-aui-surface p-3 text-aui-sm"
          >
            <div className="flex items-start gap-3">
              <AlertMark alert={alert} />
              <strong className="min-w-0 flex-1 font-medium">{alert.title}</strong>
            </div>
            {alert.body && <p className="whitespace-pre-wrap text-aui-muted">{alert.body}</p>}
            {alert.resolve_when && (
              <p className="text-aui-xs text-aui-muted">Resolved when: {alert.resolve_when}</p>
            )}
            <div className="flex flex-wrap gap-2">
              <a
                className={button}
                href={
                  page
                    ? href(page)
                    : alert.action === "reconnect"
                      ? "/connections"
                      : href({
                          page: "conversation",
                          id: alert.session!,
                          ...(anchor ? { message: anchor } : {}),
                        })
                }
              >
                {action}
              </a>
              {canWrite && (
                <>
                  <button
                    type="button"
                    className={button}
                    disabled={attention.busy}
                    onClick={() => void attention.change(alert, "resolved")}
                  >
                    Resolve
                  </button>
                  <button
                    type="button"
                    className={button}
                    disabled={attention.busy}
                    onClick={() => void attention.change(alert, "snoozed")}
                  >
                    Snooze 1h
                  </button>
                </>
              )}
              {onOpen &&
                agent &&
                agents.includes(agent) &&
                (["retry", "answer"].includes(alert.action ?? "") ||
                  alert.kind === "eval_below") && (
                  <button
                    type="button"
                    className={button}
                    onClick={() => {
                      setFix({ alert, agent });
                      setText(
                        `Help resolve this alert: ${alert.title}${alert.body ? `\n\n${alert.body}` : ""}${alert.resolve_when ? `\n\nResolution needed: ${alert.resolve_when}` : ""}\n\nExplain the proposed fix before changing anything.`,
                      );
                    }}
                  >
                    Fix with @{agent}
                  </button>
                )}
            </div>
          </section>
        );
      })}
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
            <h3 className="text-aui-lg font-medium">Fix with @{fix.agent}</h3>
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
