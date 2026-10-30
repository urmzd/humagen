/**
 * The layout checks every captured screen must pass. `collect` runs in the
 * page and measures what a person sees; `judge` is a pure function of that
 * measurement, so the checks are the same on every machine and are tested on
 * their own (checks.test.ts).
 */

export interface Box {
  left: number;
  right: number;
  top: number;
  bottom: number;
}

export interface TextRun {
  /** The first words of the text, to say which one broke. */
  text: string;
  /** One box per line it takes, cut to what its scrolling or clipping parents show. */
  boxes: Box[];
  /** Inside the page's main column, where text must never overlap. */
  main: boolean;
  /**
   * The layer it is drawn in: "" for the page, or the name of the floating
   * piece it is in (`data-layer`), such as a menu or a list that opens over
   * the page. Text overlaps only text in its own layer.
   */
  layer?: string;
}

export interface Control {
  /** Its accessible name or text, to say which one broke. */
  label: string;
  /** Where it is drawn, cut to what its scrolling or clipping parents show: what the gutter check reads. */
  box: Box;
  /**
   * Its whole size, even where a strip it scrolls in hides part of it: what
   * the target check reads. A field or a checkbox inside a label is the label.
   */
  size: { width: number; height: number };
  /** A link inside a sentence, which is exempt from the target size, as WCAG 2.5.8 allows. */
  inline: boolean;
  /** A field a person types into: its text size, which below 16px makes Safari zoom on focus. */
  font?: number;
}

export interface Snapshot {
  width: number;
  height: number;
  /** The document's scroll width: equal to `width` when nothing runs off the side. */
  scrollWidth: number;
  /** The text of every rendered h1, including visually hidden ones. */
  titles: string[];
  texts: TextRun[];
  controls: Control[];
}

export type Check = "overflow" | "gutter" | "target" | "title" | "overlap" | "zoom";

export interface Problem {
  check: Check;
  detail: string;
}

export interface Rules {
  /** The side margin nothing may cross, in pixels. Leave out to skip the check (desktop). */
  gutter?: number;
  /** The smallest a control may be, in pixels, on each side. */
  target: number;
  /** The smallest text a field may have, so a phone does not zoom when it takes focus. */
  fieldText?: number;
}

/**
 * WCAG 2.5.8 asks for 24 by 24, and 2.5.5 for 44 by 44, which a touch screen
 * keeps: a control's target there includes the margin styles.css gives it.
 * A phone keeps 16 pixels clear on each side and fields at 16px text.
 */
export const PHONE: Rules = { gutter: 16, target: 44, fieldText: 16 };
export const DESKTOP: Rules = { target: 24 };
/** A phone on its side: as wide as a small desktop, still pressed with a finger. */
export const LANDSCAPE: Rules = { target: 44, fieldText: 16 };

/** Sub-pixel rounding is not a broken layout. */
const SLACK = 0.5;

const px = (n: number) => Math.round(n * 10) / 10;

/** How much two boxes share, as a width and a height; zero or less when they do not meet. */
function overlap(a: Box, b: Box): { w: number; h: number } {
  return {
    w: Math.min(a.right, b.right) - Math.max(a.left, b.left),
    h: Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top),
  };
}

export function judge(s: Snapshot, rules: Rules): Problem[] {
  const problems: Problem[] = [];
  if (s.scrollWidth > s.width + SLACK) {
    problems.push({
      check: "overflow",
      detail: `the page is ${s.scrollWidth}px wide in a ${s.width}px window`,
    });
  }
  if (s.titles.length !== 1) {
    problems.push({
      check: "title",
      detail: `${s.titles.length} h1 elements${s.titles.length ? `: ${s.titles.map((t) => JSON.stringify(t)).join(", ")}` : ""}`,
    });
  }
  if (rules.gutter !== undefined) {
    const lo = rules.gutter - SLACK;
    const hi = s.width - rules.gutter + SLACK;
    const past = (b: Box) => b.left < lo || b.right > hi;
    for (const t of s.texts) {
      const b = t.boxes.find(past);
      if (b)
        problems.push({
          check: "gutter",
          detail: `text "${t.text}" spans ${px(b.left)} to ${px(b.right)}px`,
        });
    }
    for (const c of s.controls) {
      if (past(c.box)) {
        problems.push({
          check: "gutter",
          detail: `control "${c.label}" spans ${px(c.box.left)} to ${px(c.box.right)}px`,
        });
      }
    }
  }
  for (const c of s.controls) {
    if (c.inline) continue;
    const w = c.size.width;
    const h = c.size.height;
    if (w + SLACK < rules.target || h + SLACK < rules.target) {
      problems.push({ check: "target", detail: `control "${c.label}" is ${px(w)} by ${px(h)}px` });
    }
    if (rules.fieldText !== undefined && c.font !== undefined && c.font + SLACK < rules.fieldText) {
      problems.push({ check: "zoom", detail: `field "${c.label}" has ${px(c.font)}px text` });
    }
  }
  // Lines of text in the main column never cover one another. Two lines that
  // only touch, as tight line heights make them, are not covering.
  const lines = s.texts
    .filter((t) => t.main)
    .flatMap((t, i) => t.boxes.map((box) => ({ i, text: t.text, box, layer: t.layer })));
  const seen = new Set<string>();
  for (let a = 0; a < lines.length; a++) {
    for (let b = a + 1; b < lines.length; b++) {
      const x = lines[a];
      const y = lines[b];
      if (!x || !y || x.i === y.i) continue;
      // A menu or a list that opens over the page covers it by design.
      if ((x.layer ?? "") !== (y.layer ?? "")) continue;
      const o = overlap(x.box, y.box);
      const shorter = Math.min(x.box.bottom - x.box.top, y.box.bottom - y.box.top);
      if (o.w > 2 && o.h > Math.max(2, shorter * 0.3)) {
        const key = `${x.i}:${y.i}`;
        if (seen.has(key)) continue;
        seen.add(key);
        problems.push({ check: "overlap", detail: `text "${x.text}" covers "${y.text}"` });
      }
    }
  }
  return problems;
}

/**
 * Measures the page as it is drawn. It runs in the browser, so it uses
 * nothing from this module: Playwright sends only its source.
 */
export function collect(): Snapshot {
  const width = window.innerWidth;
  const height = window.innerHeight;
  const root = document.documentElement;
  const main = document.querySelector("main");
  const FLOATING = "[role=menu], [role=dialog], [role=listbox], [role=tooltip]";
  const shown = (el: Element) =>
    el.checkVisibility({ opacityProperty: true, visibilityProperty: true });

  // Measure only the part of a box that its scrolling or clipping parents show.
  // Scrolled-out messages must not count as covering the conversation header.
  function cut(from: Element, r: DOMRect | Box): Box | null {
    let { left, right, top, bottom } = r;
    for (
      let a: Element | null = from;
      a && a !== root && a !== document.body;
      a = a.parentElement
    ) {
      const st = getComputedStyle(a);
      const b = a.getBoundingClientRect();
      if (st.overflowX !== "visible") {
        left = Math.max(left, b.left);
        right = Math.min(right, b.right);
      }
      if (st.overflowY !== "visible") {
        top = Math.max(top, b.top);
        bottom = Math.min(bottom, b.bottom);
      }
      if (st.position === "fixed") break;
    }
    // Visually hidden text and controls are one pixel or less: nobody sees them.
    return right - left > 1 && bottom - top > 1 ? { left, right, top, bottom } : null;
  }

  const titles = [...document.querySelectorAll("h1")]
    .filter((h) => h.checkVisibility())
    .map((h) => (h.textContent ?? "").trim());

  const texts: TextRun[] = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const value = (n.nodeValue ?? "").trim();
    const parent = n.parentElement;
    if (!value || !parent || !shown(parent)) continue;
    if (parent.closest("script, style, noscript, [aria-hidden='true'] svg")) continue;
    const range = document.createRange();
    range.selectNodeContents(n);
    const boxes = [...range.getClientRects()]
      .map((r) => cut(parent, r))
      .filter((b): b is Box => b !== null);
    if (boxes.length === 0) continue;
    texts.push({
      text: value.slice(0, 48),
      boxes,
      main: Boolean(main?.contains(parent)),
      // A menu, dialog, listbox or tooltip floats over the page on purpose: its
      // text is checked against its own lines, not against the page under it.
      layer:
        parent.closest("[data-layer]")?.getAttribute("data-layer") ??
        parent.closest(FLOATING)?.getAttribute("role") ??
        "",
    });
  }

  const controls: Control[] = [];
  const selector =
    "button, a[href], input:not([type=hidden]), select, textarea, summary, [role=button], [role=tab], [role=link], [role=menuitem], [role=option], [role=switch], [role=checkbox], [role=radio]";
  for (const el of document.querySelectorAll(selector)) {
    if (!shown(el)) continue;
    // A control inside another is measured as the outer one.
    if (el.parentElement?.closest(selector)) continue;
    const box = cut(el, el.getBoundingClientRect());
    if (!box) continue;
    // Pressing a field's or a checkbox's label works it, so the label is its target.
    const target = (el.matches("input, select, textarea") && el.closest("label")) || el;
    const drawn = target.getBoundingClientRect();
    // On a touch screen a small control is given an invisible margin, its
    // ::after (styles.css), which a finger lands on too. What its scrolling
    // or clipping parents hide of the margin is not there to press.
    const after = getComputedStyle(target, "::after");
    let whole: { width: number; height: number } = drawn;
    if (after.content !== "none" && after.position === "absolute") {
      const grow = (v: string) => Math.max(0, -Number.parseFloat(v) || 0);
      const top = grow(after.top);
      const left = grow(after.left);
      const right = grow(after.right);
      const bottom = grow(after.bottom);
      const pad = cut(target, {
        left: drawn.left - left,
        right: drawn.right + right,
        top: drawn.top - top,
        bottom: drawn.bottom + bottom,
      });
      if (pad)
        whole = {
          width: Math.max(drawn.width, pad.right - pad.left),
          height: Math.max(drawn.height, pad.bottom - pad.top),
        };
    }
    const st = getComputedStyle(el);
    // In a sentence (WCAG 2.5.8's inline exception): a control with words of
    // its own, drawn in the flow of a paragraph that says more than it does.
    // An icon beside a heading is not in a sentence, and is measured.
    // A wrapper in the flow (a hover card's anchor) is part of the sentence.
    let block = el.parentElement;
    while (
      block &&
      block.tagName === "SPAN" &&
      getComputedStyle(block).display.startsWith("inline")
    )
      block = block.parentElement;
    const own = (el.textContent ?? "").trim();
    const inline =
      (st.display === "inline" || st.display === "inline-block") &&
      own.length > 0 &&
      Boolean(
        block &&
          block.tagName === "P" &&
          getComputedStyle(block).display === "block" &&
          (block.textContent ?? "").trim().length > own.length,
      );
    const label = (
      el.getAttribute("aria-label") ||
      el.textContent ||
      (target !== el ? target.textContent : "") ||
      el.getAttribute("placeholder") ||
      el.getAttribute("title") ||
      el.getAttribute("name") ||
      el.tagName
    )
      .trim()
      .replace(/\s+/g, " ")
      .slice(0, 48);
    const typed =
      el.matches("textarea, select, [contenteditable=true]") ||
      (el.matches("input") &&
        ![
          "checkbox",
          "radio",
          "range",
          "color",
          "file",
          "button",
          "submit",
          "reset",
          "image",
        ].includes((el as HTMLInputElement).type));
    controls.push({
      label: label || el.tagName.toLowerCase(),
      box,
      size: { width: whole.width, height: whole.height },
      inline,
      ...(typed ? { font: Number.parseFloat(st.fontSize) } : {}),
    });
  }

  return { width, height, scrollWidth: root.scrollWidth, titles, texts, controls };
}
