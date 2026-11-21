import assert from "node:assert/strict";
import { test } from "node:test";
import { Window } from "happy-dom";

// test-assert.ts swaps DOM nodes for tokens; these check it keeps what each assert means.
const w = new Window();
const button = w.document.createElement("button");
button.textContent = "Send";
test("an object holding a node is strictEqual to itself", () => {
  const same = { button };
  assert.strictEqual(same, same);
});
test("a difference past the 200th entry still fails deepStrictEqual", () => {
  const a = Array.from({ length: 201 }, () => button);
  const b: unknown[] = [...a];
  b[200] = 1;
  assert.throws(() => assert.deepStrictEqual(a, b));
});
test("a difference six levels down still fails", () => {
  assert.throws(() =>
    assert.deepStrictEqual(
      { a: { b: { c: { d: { e: { button, x: 1 } } } } } },
      { a: { b: { c: { d: { e: { button, x: 2 } } } } } },
    ),
  );
});
test("symbol keys are still compared", () => {
  const s = Symbol("k");
  assert.throws(() => assert.deepStrictEqual({ button, [s]: 1 }, { button, [s]: 2 }));
});
test("cyclic values holding nodes compare", () => {
  const a: Record<string, unknown> = { button };
  a.self = a;
  const b: Record<string, unknown> = { button };
  b.self = b;
  assert.deepStrictEqual(a, b);
});
test("a failing assert on a node prints a short message", () => {
  try {
    assert.equal(button, null);
  } catch (e) {
    const { length } = (e as Error).message;
    assert.ok(length < 500, `message was ${length} characters`);
    return;
  }
  assert.fail("did not throw");
});
