import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { KindChips } from "./InboxPage.tsx";
import { FADE_PX, GUTTER_PX } from "./layout.ts";
import { kinds as mailKinds } from "./mail.ts";
import { SIDEBAR_KEY, sidebarStartsOpen } from "./sidebarState.ts";
import { Menu, MoreMenu, Popover, Segmented, Tabs } from "./ui.tsx";

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

/** A phone: every humagen-strip is 350px wide at x=20, and what is in it sits end to end, each item 80px wide. */
function phoneStrips() {
  const isStrip = (el: HTMLElement) => el.classList.contains("humagen-strip");
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

/** An agent's tabs, as AgentDetail lists them: before it loads, only the four that need nothing loaded. */
const agentTabs = (loaded: boolean) =>
  [
    "prompt",
    "model",
    "access",
    ...(loaded ? ["queue"] : []),
    "context",
    ...(loaded ? ["memory", "evals", "alerts", "revisions"] : []),
  ].map((id) => ({ id, label: id }));

test("a tab strip that grows after it shows, as an agent's does once it loads, fades where there is more", async () => {
  phoneStrips();
  const { rerender } = render(<Tabs label="lead" value="prompt" tabs={agentTabs(false)} onChange={() => {}} />);
  const strip = screen.getByRole("tablist");
  assert.equal(strip.dataset.more, undefined, "four tabs fit");
  rerender(<Tabs label="lead" value="prompt" tabs={agentTabs(true)} onChange={() => {}} />);
  await waitFor(() => assert.equal(strip.dataset.more, "end", "the tabs that came in past the edge read as more to scroll to"));
  assert.equal(strip.scrollLeft, 0);
});

test("a tab pushed out of view by tabs that come in before it is brought back", async () => {
  phoneStrips();
  const { rerender } = render(<Tabs label="lead" value="context" tabs={agentTabs(false)} onChange={() => {}} />);
  const strip = screen.getByRole("tablist");
  assert.equal(strip.scrollLeft, 0, "Context, at 240-320, shows whole");
  rerender(<Tabs label="lead" value="context" tabs={agentTabs(true)} onChange={() => {}} />);
  // Queue comes in before it, which moves Context to 320-400, past the strip's 350.
  await waitFor(() => assert.equal(strip.scrollLeft, 400 + FADE_PX - STRIP.width));
  assert.equal(strip.dataset.more, "both");
});

test("a segmented choice that does not fit scrolls inside its box, each choice on one line", () => {
  phoneStrips();
  const kinds = ["all", "company", "team", "account", "initiative", "partner"].map((id) => ({ id, label: id === "company" ? "Company-wide" : id }));
  render(<Segmented label="Kind" value="partner" options={kinds} onChange={() => {}} />);
  const box = screen.getByRole("group", { name: "Kind" });
  // A fieldset is as wide as its content unless it may shrink, which is what ran past the gutter.
  assert.match(box.className, /\bmin-w-0\b/);
  assert.match(box.className, /\bmax-w-full\b/);
  const strip = box.querySelector<HTMLElement>(".humagen-strip");
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
const menuPanel = () => screen.getByRole("listbox").closest<HTMLElement>(".humagen-glass-pop");

test("a menu near the left edge of a phone moves inside the gutter too", () => {
  const left = phonePanel(".humagen-glass-pop");
  render(<Menu label="Agent" value="lead" options={options} align="right" onChange={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Agent: lead" }));
  assert.equal(menuPanel()?.style.translate, `${GUTTER_PX - left}px 0`);
});

test("a menu near the right edge of a phone moves left into the gutter", () => {
  // Hanging from the left of a control at 300px, it would end at 620.
  phonePanel(".humagen-glass-pop", { left: 300, width: 60 }, "left");
  render(<Menu label="Agent" value="lead" options={options} onChange={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Agent: lead" }));
  assert.equal(menuPanel()?.style.translate, `${SCREEN - GUTTER_PX - (300 + 320)}px 0`);
});

test("a menu with room stays where it hangs", () => {
  phonePanel(".humagen-glass-pop", { left: 40, width: 60 }, "left");
  render(<Menu label="Agent" value="lead" options={options} onChange={() => {}} />);
  fireEvent.click(screen.getByRole("button", { name: "Agent: lead" }));
  assert.equal(menuPanel()?.style.translate, "");
});

test("a browser that closed the sidebar before the rename starts with it closed", () => {
  window.localStorage.clear();
  window.localStorage.setItem("warp.sidebar", "0");
  assert.equal(sidebarStartsOpen(1440), false);
  assert.equal(window.localStorage.getItem(SIDEBAR_KEY), "0");
  assert.equal(window.localStorage.getItem("warp.sidebar"), null);
  // Nothing kept: open on a wide window, closed on a phone.
  window.localStorage.clear();
  assert.equal(sidebarStartsOpen(1440), true);
  assert.equal(sidebarStartsOpen(390), false);
});


function tabletActions(main: {left:number;width:number}|null, controlLeft=320) {
  const menuWidth=224;
  patch(HTMLElement.prototype,"clientWidth",el=>el===document.documentElement?768:undefined);
  patch(HTMLElement.prototype,"offsetWidth",el=>el.getAttribute("role")==="menu"?menuWidth:undefined);
  patch(HTMLElement.prototype,"offsetLeft",el=>el.getAttribute("role")==="menu"?24-menuWidth:undefined);
  patchRect(el=>el.tagName==="MAIN"?main??undefined:el.querySelector(':scope > [role="menu"]')?{left:controlLeft,width:24}:undefined);
  const actions=<MoreMenu label="Project Actions" items={[{label:"Delete Project",onSelect:()=>{}}]}/>;
  render(main?<><aside aria-label="Navigation"/><main>{actions}</main></>:actions);
  fireEvent.click(screen.getByRole("button",{name:"Project Actions"}));
  const panel=screen.getByRole("menu",{name:"Project Actions"});
  const left=()=>controlLeft+24-menuWidth+(Number.parseFloat(panel.style.translate)||0);
  return {panel,left,width:menuWidth};
}

test("tablet page actions stay in the main column instead of opening behind the sidebar",()=>{
  const main={left:256,width:512};
  const menu=tabletActions(main);
  assert.ok(menu.left()>=main.left+GUTTER_PX,`Menu starts at ${menu.left()}, behind the content column`);
  assert.ok(menu.left()+menu.width<=main.left+main.width-GUTTER_PX);
});

test("a narrower main column also bounds the right edge of its page actions",()=>{
  const main={left:256,width:344};
  const menu=tabletActions(main,760);
  assert.ok(menu.left()>=main.left+GUTTER_PX);
  assert.ok(menu.left()+menu.width<=main.left+main.width-GUTTER_PX,`Menu ends at ${menu.left()+menu.width}, outside the content column`);
});

test("page actions outside a main column retain their viewport placement",()=>{
  const menu=tabletActions(null);
  assert.equal(menu.left(),120);
});

test("an open page action menu remeasures its main column after viewport resize",()=>{
  const main={left:256,width:512};
  const menu=tabletActions(main);
  main.left=320;main.width=448;
  fireEvent(window,new Event("resize"));
  assert.ok(menu.left()>=main.left+GUTTER_PX);
  assert.ok(menu.left()+menu.width<=768-GUTTER_PX);
});


test("sidebar column resize reclamps an open menu and releases the observer on close",()=>{
  const original=globalThis.ResizeObserver;
  let resize:()=>void=()=>assert.fail("No main-column observer installed");
  let observed:Element|undefined;
  let released=false;
  class ColumnObserver {
    constructor(callback:ResizeObserverCallback){resize=()=>callback([],this as unknown as ResizeObserver)}
    observe(target:Element){observed=target}
    unobserve(){}
    disconnect(){released=true}
  }
  Object.assign(globalThis,{ResizeObserver:ColumnObserver});
  patched.push(()=>Object.assign(globalThis,{ResizeObserver:original}));
  const main={left:256,width:512};
  const menu=tabletActions(main);
  assert.equal(observed,screen.getByRole("main"));
  main.left=320;main.width=448;
  act(()=>resize());
  assert.ok(menu.left()>=main.left+GUTTER_PX);
  fireEvent.click(screen.getByRole("button",{name:"Project Actions"}));
  assert.equal(released,true);
});

test("main-column menu bounds still work without ResizeObserver",()=>{
  const original=globalThis.ResizeObserver;
  Object.assign(globalThis,{ResizeObserver:undefined});
  patched.push(()=>Object.assign(globalThis,{ResizeObserver:original}));
  const main={left:256,width:512};
  const menu=tabletActions(main);
  assert.ok(menu.left()>=main.left+GUTTER_PX);
  main.left=320;main.width=448;
  fireEvent(window,new Event("resize"));
  assert.ok(menu.left()>=main.left+GUTTER_PX);
});
