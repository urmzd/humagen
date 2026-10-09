import type { Turn, TurnStatus } from "@agent-ui/core";
import { type KeyboardEvent, type RefObject, useEffect, useRef, useState } from "react";

export interface TurnRailEntry {
  /** Matches `data-turn-id` on the turn's element (AgentMessage stamps it). */
  id: string;
  agent: string;
  /** First line of the answer, for the preview card. */
  preview: string;
  status: TurnStatus;
  /** How much happened in the turn; sets the tick height. Any positive scale. */
  weight: number;
  /** When the turn started, ISO. */
  at?: string;
}

/** A rail entry from a turn: weight grows with answer length and work done. */
export function railEntry(turn: Turn, at?: string): TurnRailEntry {
  const text = turn.text.replace(/\[\[[^\]]+\]\]/g, "").trim();
  return {
    id: turn.id,
    agent: turn.agent.name,
    preview: text.split("\n").find((l) => l.trim()) ?? "",
    status: turn.status,
    weight: text.length + turn.tasks.length * 120 + (turn.attachments?.length ?? 0) * 200 + 40,
    at,
  };
}

const REVEAL_MS = 1400;
const statusWord: Record<TurnStatus, string> = {
  queued: "Queued",
  working: "Working",
  waiting: "Needs input",
  done: "Done",
  failed: "Failed",
  stopped: "Stopped",
};

/**
 * Left-edge turn scrubber. One tick per turn, tall like an equalizer bar when
 * the turn did a lot. Ticks near the turn in view grow (springy height), the
 * rail brightens while the reader scrolls, and it rests faint. Hover shows a
 * preview; click or Enter jumps there; arrows, Home, End move between turns.
 */
export function TurnRail({
  turns,
  scroller,
}: {
  turns: TurnRailEntry[];
  scroller: RefObject<HTMLElement | null>;
}) {
  const [active, setActive] = useState<string | null>(turns.at(-1)?.id ?? null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [scrolling, setScrolling] = useState(false);
  const ids = useRef<string[]>([]);
  ids.current = turns.map((t) => t.id);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  // After a jump, the target stays current while the scroll settles.
  const lockUntil = useRef(0);

  useEffect(() => {
    const root = scroller.current;
    if (!root) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onScroll = () => {
      setScrolling(true);
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => setScrolling(false), REVEAL_MS);
    };
    root.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      root.removeEventListener("scroll", onScroll);
      if (timer) clearTimeout(timer);
    };
  }, [scroller]);

  useEffect(() => {
    const root = scroller.current;
    if (!root || typeof IntersectionObserver === "undefined") return;
    const visible = new Set<string>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const id = e.target.getAttribute("data-turn-id");
          if (!id) continue;
          if (e.isIntersecting) visible.add(id);
          else visible.delete(id);
        }
        if (Date.now() < lockUntil.current) return;
        // Scrolled to the end: the last turn is current even if it starts lower down.
        const atEnd = root.scrollHeight - root.scrollTop - root.clientHeight < 8;
        const top = atEnd ? ids.current.at(-1) : ids.current.find((id) => visible.has(id));
        if (top) setActive(top);
      },
      { root, rootMargin: "0px 0px -55% 0px", threshold: 0 },
    );
    for (const t of turns) {
      const el = root.querySelector(`[data-turn-id="${CSS.escape(t.id)}"]`);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [turns, scroller]);

  if (turns.length === 0) return null;

  const jump = (id: string) => {
    const el = scroller.current?.querySelector(`[data-turn-id="${CSS.escape(id)}"]`);
    const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    lockUntil.current = Date.now() + 900;
    el?.scrollIntoView({ block: "start", behavior: reduce ? "auto" : "smooth" });
    setActive(id);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>, i: number) => {
    const to =
      e.key === "ArrowDown" || e.key === "ArrowRight"
        ? Math.min(turns.length - 1, i + 1)
        : e.key === "ArrowUp" || e.key === "ArrowLeft"
          ? Math.max(0, i - 1)
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? turns.length - 1
              : null;
    if (to === null) return;
    e.preventDefault();
    const t = turns[to];
    if (!t) return;
    jump(t.id);
    buttons.current[to]?.focus();
  };

  const max = Math.max(...turns.map((t) => Math.log(t.weight + 1)));
  const min = Math.min(...turns.map((t) => Math.log(t.weight + 1)));
  const activeIndex = turns.findIndex((t) => t.id === active);
  const focus = hovered ? turns.findIndex((t) => t.id === hovered) : activeIndex;

  return (
    <nav
      aria-label="Jump to a turn"
      className={`group/rail absolute top-1/2 left-0 z-20 hidden -translate-y-1/2 transition-opacity duration-300 focus-within:opacity-100 hover:opacity-100 md:block ${scrolling ? "opacity-100" : "opacity-40"}`}
    >
      <ol className="flex max-h-[70vh] flex-col items-start gap-[3px] overflow-visible py-1 pr-2 pl-1">
        {turns.map((t, i) => {
          const norm = max > min ? (Math.log(t.weight + 1) - min) / (max - min) : 0.5;
          // Ticks near the turn in view grow, like a waveform under the playhead.
          const near = focus >= 0 ? Math.max(0, 1 - Math.abs(i - focus) / 3) : 0;
          const width = 6 + norm * 10 + near * 6;
          const isActive = t.id === active;
          const tone =
            t.status === "failed"
              ? "bg-aui-bad"
              : isActive
                ? "bg-aui-accent"
                : t.status === "waiting"
                  ? "bg-aui-accent/60"
                  : "bg-aui-fg/25 group-hover/rail:bg-aui-fg/40";
          return (
            <li key={t.id} className="relative flex items-center">
              <button
                ref={(el) => {
                  buttons.current[i] = el;
                }}
                type="button"
                aria-current={isActive ? "true" : undefined}
                aria-label={`${t.agent}, ${statusWord[t.status]}: ${t.preview || "no answer yet"}`}
                onClick={() => jump(t.id)}
                onKeyDown={(e) => onKeyDown(e, i)}
                onMouseEnter={() => setHovered(t.id)}
                onMouseLeave={() => setHovered((h) => (h === t.id ? null : h))}
                onFocus={() => setHovered(t.id)}
                onBlur={() => setHovered((h) => (h === t.id ? null : h))}
                className="flex h-2 items-center rounded-full py-px outline-none focus-visible:ring-2 focus-visible:ring-aui-accent"
                style={{ width: 26 }}
              >
                <span
                  aria-hidden
                  className={`aui-rail-tick block rounded-full ${tone}`}
                  style={{ width, height: isActive ? 3 : 2 }}
                />
              </button>
              {hovered === t.id && (
                <div
                  role="tooltip"
                  className="aui-pop-in aui-glass pointer-events-none absolute top-1/2 left-full z-30 ml-1 flex w-60 -translate-y-1/2 flex-col gap-0.5 rounded-aui px-2.5 py-2 text-aui-xs"
                >
                  <span className="flex items-center gap-1.5 text-aui-2xs text-aui-muted">
                    <span className="font-medium text-aui-fg">{t.agent}</span>
                    <span>· {statusWord[t.status]}</span>
                    {t.at && (
                      <span className="ml-auto tabular-nums">
                        {new Date(t.at).toLocaleTimeString(undefined, {
                          hour: "numeric",
                          minute: "2-digit",
                        })}
                      </span>
                    )}
                  </span>
                  <span className="line-clamp-2 text-aui-fg">{t.preview || "No answer yet"}</span>
                </div>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
