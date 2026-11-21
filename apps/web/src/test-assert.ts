import assert from "node:assert";
import { syncBuiltinESMExports } from "node:module";

// A failing assert builds its message with util.inspect at depth 1000 and
// with custom inspectors turned off. Given a happy-dom node or window, that
// walks every element, style, timer and global the window can reach: one
// assert.equal(button, null) took a test process past 2 GB in a second and,
// left alone, past 70 GB, which stopped the machine.
//
// This module wraps node:assert's comparisons, for every import of
// node:assert and node:assert/strict in the process. Before node compares,
// each DOM node or window in either value becomes a token,
// { dom: '<button aria-label="Send"> "Send"', id: 1 }, the same token for the
// same node, and each NodeList an array of them. A failure prints the tokens.
// DOM objects therefore compare by identity, which is what a test means.
//
// The test script loads this with --import before any test; testload.ts
// imports it too, for a test file run on its own.

const MAX_TEXT = 60;
const clip = (text: string): string => {
  const t = text.replace(/\s+/g, " ").trim();
  return t.length > MAX_TEXT ? `${t.slice(0, MAX_TEXT)}…` : t;
};

interface DomLike {
  nodeType: number;
  nodeName: string;
  localName?: string;
  textContent?: string | null;
  getAttribute?: (name: string) => string | null;
}

const isNode = (v: unknown): v is DomLike =>
  typeof v === "object" &&
  v !== null &&
  typeof (v as DomLike).nodeType === "number" &&
  typeof (v as DomLike).nodeName === "string" &&
  "ownerDocument" in v;

const isWindow = (v: unknown): boolean =>
  typeof v === "object" && v !== null && "document" in v && "location" in v && "navigator" in v;

/** A DOM object in a few words: <button aria-label="Send"> "Send", #text "Hi", #document, Window. */
export function describeDom(v: unknown): string {
  if (isWindow(v)) return "Window";
  if (!isNode(v)) return Object.prototype.toString.call(v);
  if (v.nodeType === 1) {
    const attrs = ["id", "role", "aria-label", "name", "type", "data-testid"]
      .map((a) => [a, v.getAttribute?.(a)] as const)
      .filter(([, value]) => value != null)
      .map(([a, value]) => ` ${a}="${clip(String(value))}"`)
      .join("");
    const text = clip(v.textContent ?? "");
    return `<${v.localName ?? v.nodeName.toLowerCase()}${attrs}>${text ? ` ${JSON.stringify(text)}` : ""}`;
  }
  if (v.nodeType === 3) return `#text ${JSON.stringify(clip(v.textContent ?? ""))}`;
  return v.nodeName;
}

const MAX_DEPTH = 4;
const MAX_ENTRIES = 200;

interface ListLike {
  length: number;
  item: (i: number) => unknown;
}
const isList = (v: unknown): v is ListLike =>
  typeof v === "object" &&
  v !== null &&
  typeof (v as ListLike).length === "number" &&
  typeof (v as ListLike).item === "function";

const isPlain = (v: object): boolean => {
  const proto = Object.getPrototypeOf(v);
  return Array.isArray(v) || proto === Object.prototype || proto === null;
};

/** Whether v is, or holds in its arrays, plain objects and node lists, a DOM node or window. */
export function holdsDom(v: unknown, depth = 0, seen = new Set<object>()): boolean {
  if (typeof v !== "object" || v === null) return false;
  if (isNode(v) || isWindow(v) || isList(v)) return true;
  if (depth >= MAX_DEPTH || seen.has(v) || !isPlain(v)) return false;
  seen.add(v);
  return Object.values(v)
    .slice(0, MAX_ENTRIES)
    .some((x) => holdsDom(x, depth + 1, seen));
}

/** A stand-in for one DOM object: small to print, the same object for the same node. */
interface Token {
  dom: string;
  id: number;
}

/**
 * v with each DOM node or window swapped for its token and each node list
 * for an array of them. Node's assert never sees the DOM: Node 24's
 * AssertionError walks actual and expected even when given a message.
 */
function swapDom(v: unknown, tokens: Map<object, Token>, depth = 0): unknown {
  if (typeof v !== "object" || v === null) return v;
  if (isNode(v) || isWindow(v)) {
    let t = tokens.get(v);
    if (!t) tokens.set(v, (t = { dom: describeDom(v), id: tokens.size + 1 }));
    return t;
  }
  if (isList(v))
    return Array.from({ length: Math.min(v.length, MAX_ENTRIES) }, (_, i) =>
      swapDom(v.item(i), tokens, depth + 1),
    );
  if (depth >= MAX_DEPTH || !isPlain(v)) return v;
  if (Array.isArray(v)) return v.slice(0, MAX_ENTRIES).map((x) => swapDom(x, tokens, depth + 1));
  const out: Record<string, unknown> = Object.create(Object.getPrototypeOf(v));
  for (const [k, x] of Object.entries(v).slice(0, MAX_ENTRIES))
    out[k] = swapDom(x, tokens, depth + 1);
  return out;
}

const PAIRED = [
  "equal",
  "notEqual",
  "strictEqual",
  "notStrictEqual",
  "deepEqual",
  "notDeepEqual",
  "deepStrictEqual",
  "notDeepStrictEqual",
  "partialDeepStrictEqual",
] as const;

const WRAPPED = Symbol.for("humagen.test-assert");

type Paired = (actual: unknown, expected: unknown, message?: string | Error) => void;

function guard(target: Record<string, unknown>): void {
  for (const name of PAIRED) {
    const original = target[name] as Paired | undefined;
    if (typeof original !== "function" || (original as { [WRAPPED]?: true })[WRAPPED]) continue;
    const wrapped: Paired = function (this: unknown, actual, expected, message) {
      if (!holdsDom(actual) && !holdsDom(expected))
        return original.call(this, actual, expected, message);
      const tokens = new Map<object, Token>();
      return original.call(this, swapDom(actual, tokens), swapDom(expected, tokens), message);
    };
    Object.defineProperty(wrapped, "name", { value: name });
    Object.defineProperty(wrapped, WRAPPED, { value: true });
    target[name] = wrapped;
  }
}

guard(assert as unknown as Record<string, unknown>);
guard(assert.strict as unknown as Record<string, unknown>);
// Named imports of node:assert and node:assert/strict read these too.
syncBuiltinESMExports();
