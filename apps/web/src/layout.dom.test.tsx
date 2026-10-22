import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { KindChips } from "./InboxPage.tsx";
import { FADE_PX, GUTTER_PX } from "./layout.ts";
import { kinds as mailKinds } from "./mail.ts";
import { Menu, Popover, Segmented, Tabs } from "./ui.tsx";

// happy-dom lays nothing out: every box is 0 wide. These tests give the boxes
// that matter the sizes a 390px phone gives them, through the same properties
// the console reads, and put the real ones back after each test.

const SCREEN = 390;
const STRIP = { left: 20, width: 350 };
const ITEM = 80;

type Getter = (el: HTMLElement) => number | undefined;
const patched: (() => void)[] = [];
function patch(proto: object, name: string, get: Getter) {
  const real = Object.getOwnPropertyDescriptor(proto, name);
  Object.defineProperty(proto, name, {
    configurable: true,
    get(this: HTMLElement) {
      return get(this) ?? real?.get?.call(this) ?? 0;
    },
  });
  patched.push(() => (real ? Object.defineProperty(proto, name, real) : delete (proto as Record<string, unknown>)[name]));
}
function patchRect(get: (el: HTMLElement) => { left: number; width: number } | undefined) {
  const real = HTMLElement.prototype.getBoundingClientRect;
  HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
    const box = get(this);
    if (!box) return real.call(this);
    return { left: box.left, width: box.width, right: box.left + box.width, x: box.left, top: 0, y: 0, height: 36, bottom: 36, toJSON() {} } as DOMRect;
  };
  patched.push(() => (HTMLElement.prototype.getBoundingClientRect = real));
}

/** A phone: every warp-strip is 350px wide at x=20, and what is in it sits end to end, each item 80px wide. */
function phoneStrips() {
  const isStrip = (el: HTMLElement) => el.classList.contains("warp-strip");
  patch(HTMLElement.prototype, "clientWidth", (el) => (isStrip(el) ? STRIP.width : el === document.documentElement ? SCREEN : undefined));
  patch(HTMLElement.prototype, "scrollWidth", (el) => (isStrip(el) ? el.children.length * ITEM : undefined));
  patchRect((el) => {
    if (isStrip(el)) return STRIP;
    const strip = el.parentElement;
    if (strip && isStrip(strip)) return { left: STRIP.left + [...strip.children].indexOf(el) * ITEM - strip.scrollLeft, width: ITEM };
    return undefined;
  });
}

afterEach(() => {
  if (patched.length === 0) return;
  cleanup();
  while (patched.length) patched.pop()?.();
});

const projectTabs = ["inbox", "overview", "context", "agents", "channels", "schedules", "people", "limits", "history"].map((id) => ({ id, label: id }));

test("on a phone the project tab strip scrolls to the chosen tab, so the tab that is open is the one underlined in view", () => {
  phoneStrips();
  const { rerender } = render(<Tabs label="Globex" value="people" tabs={projectTabs} onChange={() => {}} />);
  const strip = screen.getByRole("tablist");
  // People is the seventh tab, at 480-560 of 720: it shows whole, with the fade's width to spare past it.
  assert.equal(strip.scrollLeft, 560 + FADE_PX - STRIP.width);
  assert.equal(strip.dataset.more, "both", "both ends say there is more");

  rerender(<Tabs label="Globex" value="agents" tabs={projectTabs} onChange={() => {}} />);
  // Agents, at 240-320, was cut off at the left: it is brought back.
  assert.equal(strip.scrollLeft, 240 - FADE_PX);

  rerender(<Tabs label="Globex" value="inbox" tabs={projectTabs} onChange={() => {}} />);
  assert.equal(strip.scrollLeft, 0);
  assert.equal(strip.dataset.more, "end", "only the end has more");
});

test("the strip's faded ends follow a person's own scrolling", () => {
  phoneStrips();
  render(<Tabs label="Globex" value="inbox" tabs={projectTabs} onChange={() => {}} />);
  const strip = screen.getByRole("tablist");
  strip.scrollLeft = 720 - STRIP.width;
  fireEvent.scroll(strip);
  assert.equal(strip.dataset.more, "start");
});

test("a strip that fits says nothing more", () => {
  phoneStrips();
  render(<Tabs label="Directory" value="people" tabs={projectTabs.slice(0, 3)} onChange={() => {}} />);
  assert.equal(screen.getByRole("tablist").dataset.more, undefined);
});

test("a segmented choice that does not fit scrolls inside its box, each choice on one line", () => {
  phoneStrips();
  const kinds = ["all", "company", "team", "account", "initiative", "partner"].map((id) => ({ id, label: id === "company" ? "Company-wide" : id }));
  render(<Segmented label="Kind" value="partner" options={kinds} onChange={() => {}} />);
  const box = screen.getByRole("group", { name: "Kind" });
  // A fieldset is as wide as its content unless it may shrink, which is what ran past the gutter.
  assert.match(box.className, /\bmin-w-0\b/);
  assert.match(box.className, /\bmax-w-full\b/);
  const strip = box.querySelector<HTMLElement>(".warp-strip");
  assert.ok(strip);
  assert.equal(strip.scrollLeft, 6 * ITEM - STRIP.width, "the last choice is chosen, so the strip ends at it");
  for (const b of screen.getAllByRole("button")) assert.match(b.className, /\bwhitespace-nowrap\b/, `${b.textContent} stays on one line`);
});

test("the inbox's kind chips fade where there are more, and keep the chosen one in view", () => {
  phoneStrips();
  const { rerender } = render(<KindChips value="" onChange={() => {}} />);
  const strip = screen.getByRole("group", { name: "Kind" });
  assert.equal(strip.dataset.more, "end", "the cut-off chip reads as more to scroll to");
  const last = mailKinds.at(-1)?.id ?? "";
  assert.ok(last);
  rerender(<KindChips value={last} onChange={() => {}} />);
  assert.equal(strip.scrollLeft, strip.children.length * ITEM - STRIP.width, "the last chip is chosen, so the row ends at it");
  assert.equal(strip.dataset.more, "start");
});

/**
 * A 320px panel hanging from the right of a 40px control that ends 64px from
 * the right of a phone, as the composer's agent menu does: it would start 6px
 * from the left edge. Returns where it would start.
 */
function phonePanel(selector: string, control = { left: SCREEN - 64 - 40, width: 40 }, align: "left" | "right" = "right") {
  patch(HTMLElement.prototype, "clientWidth", (el) => (el === document.documentElement ? SCREEN : undefined));
  patch(HTMLElement.prototype, "offsetWidth", (el) => (el.matches(selector) ? 320 : undefined));
  patch(HTMLElement.prototype, "offsetLeft", (el) => (el.matches(selector) ? (align === "right" ? control.width - 320 : 0) : undefined));
  patchRect((el) => (el.querySelector(`:scope > ${selector}`) ? control : undefined));
  return align === "right" ? control.left + control.width - 320 : control.left;
}

test("a popover near the left edge of a phone moves inside the gutter", () => {
  const left = phonePanel('[role="dialog"]');
  assert.equal(left, 6);
  render(
    <Popover label="Choose who answers" align="right" up button="@auto">
      {() => <p>helper</p>}
    </Popover>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Choose who answers" }));
  assert.equal(screen.getByRole("dialog").style.translate, `${GUTTER_PX - left}px 0`);
});

const options = ["lead", "helper"].map((value) => ({ value, label: value }));
const menuPanel = () => screen.getByRole("listbox").closest<HTMLElement>(".warp-glass-pop");

test("a menu near the left edge of a phone moves inside the gutter too", () => {
  const left = phonePanel(".warp-glass-pop");
  render(<Menu label="Agent" value="lead" options={options} align="right" onChange={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Agent: lead" }));
  assert.equal(menuPanel()?.style.translate, `${GUTTER_PX - left}px 0`);
});

test("a menu near the right edge of a phone moves left into the gutter", () => {
  // Hanging from the left of a control at 300px, it would end at 620.
  phonePanel(".warp-glass-pop", { left: 300, width: 60 }, "left");
  render(<Menu label="Agent" value="lead" options={options} onChange={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Agent: lead" }));
  assert.equal(menuPanel()?.style.translate, `${SCREEN - GUTTER_PX - (300 + 320)}px 0`);
});

test("a menu with room stays where it hangs", () => {
  phonePanel(".warp-glass-pop", { left: 40, width: 60 }, "left");
  render(<Menu label="Agent" value="lead" options={options} onChange={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Agent: lead" }));
  assert.equal(menuPanel()?.style.translate, "");
});
