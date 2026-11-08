import { defaultCopy } from "@humagen/components";
import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { button, Problem, primary } from "./ui.tsx";
import { browserVoice, VoiceController, type VoiceDeps } from "./voiceController.ts";
import { announced, initialVoice, voicePhase, waitsOnPerson } from "./voiceMode.ts";

const words = defaultCopy.voiceMode;

const reducedMotion = () => !!globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

/**
 * Voice mode: a focused view over the session, full height on a phone. It says what is
 * happening in one large line, follows the sound with one visual, captions both sides,
 * and offers Approve and Deny when the agent asks. Every turn is also written in the
 * session as messages, the person's marked spoken. End returns to the session.
 */
export function VoiceMode({
  session,
  agent,
  onEnd,
  deps = browserVoice,
}: {
  session: string;
  /** The session's agent, named in the captions. */
  agent?: string;
  onEnd: () => void;
  deps?: VoiceDeps;
}) {
  const [state, setState] = useState(initialVoice);
  const ctl = useMemo(() => new VoiceController(session, deps, setState), [session, deps]);
  const dialog = useRef<HTMLDivElement>(null);
  const orb = useRef<HTMLDivElement>(null);
  const phase = voicePhase(state);
  const label =
    phase === "tool" && state.tool ? words.using(state.tool) : (words.phase[phase] ?? "");
  // A screen reader hears settled phases only, each once.
  const [said, setSaid] = useState(label);
  useEffect(() => {
    if (announced(phase)) setSaid(label);
  }, [phase, label]);

  useEffect(() => {
    void ctl.start();
    dialog.current?.focus();
    return () => ctl.end();
  }, [ctl]);

  // The visual follows the sound while someone speaks; it is still while voice mode
  // waits on the person, and always still under reduced motion.
  const still = waitsOnPerson(phase) || reducedMotion();
  useEffect(() => {
    const el = orb.current;
    if (!el) return;
    if (still || (phase !== "hearing" && phase !== "speaking")) {
      el.style.transform = "";
      return;
    }
    let frame = 0;
    const step = () => {
      el.style.transform = `scale(${(1 + ctl.level * 0.4).toFixed(3)})`;
      frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [ctl, phase, still]);

  const end = () => {
    ctl.end();
    onEnd();
  };
  const isSpace = (e: KeyboardEvent) =>
    e.code === "Space" &&
    !(e.target instanceof HTMLButtonElement || e.target instanceof HTMLInputElement);
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      end();
    } else if (isSpace(e)) {
      e.preventDefault();
      if (!e.repeat) ctl.hold(true);
    }
  };
  const onKeyUp = (e: KeyboardEvent<HTMLDivElement>) => {
    if (isSpace(e)) {
      e.preventDefault();
      ctl.hold(false);
    }
  };

  const asks = [...ctl.asks.values()];
  const who = agent ?? "Agent";
  return (
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-label={words.title}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      onKeyUp={onKeyUp}
      className="fixed inset-0 z-50 flex bg-aui-bg outline-none"
    >
      <div className="mx-auto flex h-full w-full max-w-lg flex-col items-center justify-between gap-6 px-4 py-6 sm:py-10">
        <p className="text-aui-sm text-aui-muted">
          {words.title} · {who}
        </p>
        <div className="flex w-full min-w-0 flex-col items-center gap-6">
          <div
            aria-hidden
            className={`flex size-40 items-center justify-center rounded-full border border-aui-border bg-aui-hover ${
              !still && (phase === "thinking" || phase === "tool")
                ? "motion-safe:animate-pulse"
                : ""
            }`}
          >
            <div
              ref={orb}
              data-phase={phase}
              className={`size-24 rounded-full ${phase === "paused" || phase === "ended" || phase === "failed" ? "bg-aui-muted/40" : "bg-aui-accent"}`}
            />
          </div>
          <p aria-hidden className="text-center font-medium text-2xl text-aui-fg">
            {label}
          </p>
          <output aria-live="polite" className="sr-only">
            {said}
          </output>
          <div className="flex w-full min-w-0 flex-col gap-2 text-aui-sm">
            {state.heard && (
              <p className="min-w-0 break-words">
                <span className="font-medium text-aui-fg">{words.you}: </span>
                <span className="text-aui-muted">{state.heard}</span>
              </p>
            )}
            {state.said && (
              <p className="min-w-0 break-words">
                <span className="font-medium text-aui-fg">{who}: </span>
                <span className="text-aui-muted">{state.said}</span>
              </p>
            )}
            {ctl.queued.map((q) => (
              <p key={q} className="text-aui-xs text-aui-muted">
                {words.queued(q)}
              </p>
            ))}
          </div>
          {asks.map((ask) => (
            <section
              key={ask.question}
              aria-label={words.phase.approval}
              className="flex w-full min-w-0 flex-col gap-3 rounded-aui-lg border border-aui-border p-4"
            >
              <p className="min-w-0 break-words text-aui-sm text-aui-fg">
                {words.asks(ask.agent ?? who, ask.tool ?? "a tool")}
              </p>
              {ask.prompt && (
                <p className="min-w-0 break-words text-aui-xs text-aui-muted">{ask.prompt}</p>
              )}
              <div className="flex gap-2">
                <button
                  type="button"
                  className={primary}
                  onClick={() => void ctl.decide(ask.question, "approve")}
                >
                  {words.approve}
                </button>
                <button
                  type="button"
                  className={button}
                  onClick={() => void ctl.decide(ask.question, "deny")}
                >
                  {words.deny}
                </button>
              </div>
            </section>
          ))}
          <Problem message={state.problem ?? null} />
        </div>
        <div className="flex w-full flex-col items-center gap-3">
          <div className="flex gap-3">
            {state.paused ? (
              <button type="button" className={button} onClick={() => void ctl.resume()}>
                {words.resume}
              </button>
            ) : (
              <button
                type="button"
                className={button}
                disabled={state.mic === "refused"}
                onClick={() => ctl.pause()}
              >
                {words.pause}
              </button>
            )}
            <button type="button" className={primary} onClick={end}>
              {words.end}
            </button>
          </div>
          <p className="text-center text-aui-xs text-aui-muted">{words.hint}</p>
        </div>
      </div>
    </div>
  );
}
