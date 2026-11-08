import assert from "node:assert/strict";
import { test } from "node:test";
import { runInNewContext } from "node:vm";
import { collect, judge, PHONE, type Snapshot } from "./checks.ts";

// Only the DOM calls consumed by collect are supplied. Geometry is explicit;
// these tests exercise collector semantics without launching a browser.
class ElementFixture {
  parentElement: ElementFixture | null = null;
  nextElementSibling: ElementFixture | null = null;
  childElementCount = 0;
  visible = true;
  position = "static";
  overflowX = "visible";
  overflowY = "visible";
  after: Record<string, string> = {};
  textContent = "";
  attributes: Record<string, string> = {};
  rect = { left: 40, right: 84, top: 40, bottom: 84, width: 44, height: 44 };

  readonly tagName: string;
  constructor(tagName: string) {
    this.tagName = tagName;
  }
  checkVisibility() {
    return this.visible;
  }
  getAttribute(name: string) {
    return this.attributes[name] ?? null;
  }
  getBoundingClientRect() {
    return this.rect;
  }
  matches(selector: string): boolean {
    return selector.split(",").some((part) => {
      const tag = part.trim().match(/^([a-z]+)/)?.[1];
      if (tag && tag.toUpperCase() !== this.tagName) return false;
      const attributes = [...part.matchAll(/\[([^=\]]+)(?:=(?:"([^"]*)"|([^\]]+)))?\]/g)];
      return attributes.every(([, name, quoted, plain]) => {
        const value = quoted ?? plain;
        return (
          name !== undefined &&
          (value === undefined ? name in this.attributes : this.attributes[name] === value)
        );
      });
    });
  }
  closest(selector: string): ElementFixture | null {
    for (let at: ElementFixture | null = this; at; at = at.parentElement)
      if (at.matches(selector)) return at;
    return null;
  }
}

function environment() {
  const root = new ElementFixture("HTML");
  const body = new ElementFixture("BODY");
  body.parentElement = root;
  const title = new ElementFixture("H1");
  title.textContent = "New";
  const controls: ElementFixture[] = [];
  const button = (label: string, width = 44, height = 44) => {
    const el = new ElementFixture("BUTTON");
    el.parentElement = body;
    el.attributes["aria-label"] = label;
    el.rect = { left: 40, top: 40, right: 40 + width, bottom: 40 + height, width, height };
    controls.push(el);
    return el;
  };
  const snapshot = (): Snapshot =>
    JSON.parse(
      JSON.stringify(
        runInNewContext(`(${collect.toString()})()`, {
          window: { innerWidth: 390, innerHeight: 844 },
          NodeFilter: { SHOW_TEXT: 4 },
          document: {
            documentElement: Object.assign(root, { scrollWidth: 390 }),
            body,
            querySelector: () => null,
            querySelectorAll: (selector: string) => (selector === "h1" ? [title] : controls),
            createTreeWalker: () => ({ nextNode: () => null }),
          },
          getComputedStyle: (el: ElementFixture, pseudo?: string) => ({
            position: pseudo ? "static" : el.position,
            content: "none",
            overflowX: el.overflowX,
            overflowY: el.overflowY,
            display: "inline-flex",
            fontSize: "16px",
            ...(pseudo ? el.after : {}),
          }),
        }),
      ),
    );
  return { body, controls, button, snapshot };
}

test("inert ancestor controls are omitted while active small controls still fail target checks", () => {
  const env = environment();
  const inert = new ElementFixture("MAIN");
  inert.attributes.inert = "";
  inert.parentElement = env.body;
  env.button("Underlying small control", 13, 13).parentElement = inert;
  env.button("Active small control", 13, 13);
  const snapshot = env.snapshot();
  assert.deepEqual(
    snapshot.controls.map((control) => control.label),
    ["Active small control"],
  );
  assert.deepEqual(
    judge(snapshot, PHONE).map((problem) => problem.check),
    ["target"],
  );
  delete inert.attributes.inert;
  assert.equal(
    judge(env.snapshot(), PHONE).filter((problem) => problem.check === "target").length,
    2,
  );
});

function backdropEnvironment() {
  const env = environment();
  const backdrop = env.button("Dismiss navigation", 390, 844);
  backdrop.attributes.tabindex = "-1";
  backdrop.position = "fixed";
  backdrop.rect = { left: 0, top: 0, right: 390, bottom: 844, width: 390, height: 844 };
  const navigation = new ElementFixture("ASIDE");
  navigation.attributes["aria-label"] = "Navigation";
  navigation.position = "fixed";
  navigation.rect = { left: 0, right: 320, top: 0, bottom: 844, width: 320, height: 844 };
  navigation.parentElement = env.body;
  backdrop.nextElementSibling = navigation;
  return { ...env, backdrop, navigation };
}

test("only the semantic full-viewport navigation backdrop bypasses the gutter check", () => {
  const env = backdropEnvironment();
  const nav = env.button("Sessions");
  nav.rect = { ...nav.rect, left: 8, right: 52 };
  const snapshot = env.snapshot();
  assert.equal(snapshot.controls[0]?.navigationBackdrop, true);
  assert.deepEqual(
    judge(snapshot, PHONE).map((problem) => problem.detail),
    ['control "Sessions" spans 8 to 52px'],
  );
  const cases: [string, (value: ReturnType<typeof backdropEnvironment>) => void][] = [
    [
      "ordinary edge-to-edge action",
      ({ backdrop }) => {
        backdrop.attributes["aria-label"] = "Publish";
      },
    ],
    [
      "text button",
      ({ backdrop }) => {
        backdrop.textContent = "Close";
      },
    ],
    [
      "icon button",
      ({ backdrop }) => {
        backdrop.childElementCount = 1;
      },
    ],
    [
      "no adjacent navigation",
      ({ backdrop }) => {
        backdrop.nextElementSibling = null;
      },
    ],
    [
      "hidden navigation",
      ({ navigation }) => {
        navigation.visible = false;
      },
    ],
    [
      "ordinary in-flow navigation",
      ({ navigation }) => {
        navigation.position = "static";
      },
    ],
    [
      "oversized backdrop",
      ({ backdrop }) => {
        backdrop.rect = { ...backdrop.rect, left: -10, width: 400 };
      },
    ],
  ];
  for (const [name, change] of cases) {
    const variant = backdropEnvironment();
    change(variant);
    assert.equal(variant.snapshot().controls[0]?.navigationBackdrop, undefined, name);
    assert.equal(
      judge(variant.snapshot(), PHONE).filter((problem) => problem.check === "gutter").length,
      1,
      name,
    );
  }
});

test("fixed navigation controls use drawer bounds while ordinary controls keep page gutters", () => {
  const env = backdropEnvironment();
  const item = env.button("Sessions");
  item.parentElement = env.navigation;
  item.rect = { ...item.rect, left: 8, right: 52 };
  const ordinary = env.button("Ordinary action");
  ordinary.rect = { ...ordinary.rect, left: 8, right: 52 };
  const small = env.button("Small drawer action", 13, 13);
  small.parentElement = env.navigation;
  const snapshot = env.snapshot();
  assert.deepEqual(snapshot.controls[1]?.navigationDrawer, {
    bounds: { left: 0, right: 320, top: 0, bottom: 844 },
    hitbox: { left: 8, right: 52, top: 40, bottom: 84 },
  });
  assert.deepEqual(
    judge(snapshot, PHONE).map((problem) => problem.detail),
    ['control "Ordinary action" spans 8 to 52px', 'control "Small drawer action" is 13 by 13px'],
  );
  snapshot.titles = [];
  snapshot.scrollWidth = 400;
  snapshot.texts = [
    { text: "Edge text", boxes: [{ left: 8, right: 52, top: 100, bottom: 116 }], main: false },
  ];
  const problems = judge(snapshot, PHONE);
  assert.ok(problems.some((problem) => problem.check === "title"));
  assert.ok(problems.some((problem) => problem.check === "overflow"));
  assert.ok(problems.some((problem) => problem.detail === 'text "Edge text" spans 8 to 52px'));
});

test("drawer containment checks the raw control on every edge even when its paint is clipped", () => {
  for (const rect of [
    { left: -4, right: 40, top: 40, bottom: 84 },
    { left: 280, right: 324, top: 40, bottom: 84 },
    { left: 40, right: 84, top: -4, bottom: 40 },
    { left: 40, right: 84, top: 804, bottom: 848 },
  ]) {
    const env = backdropEnvironment();
    env.navigation.overflowX = "hidden";
    env.navigation.overflowY = "hidden";
    const item = env.button("Escaping action");
    item.parentElement = env.navigation;
    item.rect = { ...rect, width: 44, height: 44 };
    const snapshot = env.snapshot();
    const measured = snapshot.controls[1];
    assert.ok(measured);
    assert.ok(measured.box.left >= 0 && measured.box.right <= 320);
    assert.ok(measured.box.top >= 0 && measured.box.bottom <= 844);
    assert.deepEqual(
      judge(snapshot, PHONE).map((problem) => problem.detail),
      ['control "Escaping action" has a hitbox outside the navigation drawer'],
    );
  }
});

test("drawer containment includes an expanded touch target before ancestor clipping", () => {
  const env = backdropEnvironment();
  env.navigation.overflowX = "hidden";
  const item = env.button("Expanded action");
  item.parentElement = env.navigation;
  item.rect = { ...item.rect, left: 8, right: 52 };
  item.after = { content: '""', position: "absolute", left: "-12px" };
  const snapshot = env.snapshot();
  assert.equal(snapshot.controls[1]?.navigationDrawer?.hitbox.left, -4);
  assert.deepEqual(
    judge(snapshot, PHONE).map((problem) => problem.detail),
    ['control "Expanded action" has a hitbox outside the navigation drawer'],
  );
});

test("other fixed asides and in-flow, hidden or out-of-window navigation keep page gutters", () => {
  const variants: [string, (navigation: ElementFixture) => void][] = [
    [
      "other fixed aside",
      (navigation) => {
        navigation.attributes["aria-label"] = "Context";
      },
    ],
    [
      "in-flow navigation",
      (navigation) => {
        navigation.position = "static";
      },
    ],
    [
      "hidden navigation",
      (navigation) => {
        navigation.visible = false;
      },
    ],
    [
      "out-of-window navigation",
      (navigation) => {
        navigation.rect = { ...navigation.rect, left: -4, width: 324 };
      },
    ],
  ];
  for (const [name, change] of variants) {
    const env = environment();
    const navigation = new ElementFixture("ASIDE");
    navigation.attributes["aria-label"] = "Navigation";
    navigation.position = "fixed";
    navigation.rect = { left: 0, right: 320, top: 0, bottom: 844, width: 320, height: 844 };
    navigation.parentElement = env.body;
    change(navigation);
    const item = env.button("Ordinary action");
    item.parentElement = navigation;
    item.rect = { ...item.rect, left: 8, right: 52 };
    const snapshot = env.snapshot();
    assert.equal(snapshot.controls[0]?.navigationDrawer, undefined, name);
    assert.deepEqual(
      judge(snapshot, PHONE).map((problem) => problem.detail),
      ['control "Ordinary action" spans 8 to 52px'],
      name,
    );
  }
});
