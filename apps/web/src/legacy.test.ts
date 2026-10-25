import assert from "node:assert/strict";
import { test } from "node:test";
import { apiTarget, moveStored, OLD_SIDEBAR_KEY } from "./legacy.ts";

/** A Storage in memory that can be told to throw. */
function memory(seed: Record<string, string> = {}, fail = false): Storage {
  const m = new Map(Object.entries(seed));
  const guard = () => {
    if (fail) throw new Error("storage is off");
  };
  return {
    get length() {
      return m.size;
    },
    clear: () => m.clear(),
    key: (i: number) => [...m.keys()][i] ?? null,
    getItem: (k: string) => (guard(), m.get(k) ?? null),
    setItem: (k: string, v: string) => (guard(), void m.set(k, v)),
    removeItem: (k: string) => (guard(), void m.delete(k)),
  };
}

test("a value under the old key moves to the new one, once", () => {
  const s = memory({ [OLD_SIDEBAR_KEY]: "0" });
  moveStored(OLD_SIDEBAR_KEY, "humagen.sidebar", () => s);
  assert.equal(s.getItem("humagen.sidebar"), "0");
  assert.equal(s.getItem(OLD_SIDEBAR_KEY), null);
  s.setItem("humagen.sidebar", "1");
  moveStored(OLD_SIDEBAR_KEY, "humagen.sidebar", () => s);
  assert.equal(s.getItem("humagen.sidebar"), "1", "a second start changes nothing");
});

test("with both keys, the new value stays and the old key goes", () => {
  const s = memory({ [OLD_SIDEBAR_KEY]: "0", "humagen.sidebar": "1" });
  moveStored(OLD_SIDEBAR_KEY, "humagen.sidebar", () => s);
  assert.equal(s.getItem("humagen.sidebar"), "1");
  assert.equal(s.getItem(OLD_SIDEBAR_KEY), null);
});

test("storage that throws, or is missing, throws nothing", () => {
  assert.doesNotThrow(() => moveStored(OLD_SIDEBAR_KEY, "humagen.sidebar", () => memory({}, true)));
  assert.doesNotThrow(() =>
    moveStored(OLD_SIDEBAR_KEY, "humagen.sidebar", () => {
      throw new Error("no window");
    }),
  );
});

test("the dev server proxies to HUMAGEN_API, else the old WARP_API with a warning", () => {
  const warned: string[] = [];
  const warn = (l: string) => warned.push(l);
  assert.equal(apiTarget({ HUMAGEN_API: "http://a" }, warn), "http://a");
  assert.equal(apiTarget({ HUMAGEN_API: "http://a", WARP_API: "http://b" }, warn), "http://a");
  assert.deepEqual(warned, []);
  assert.equal(apiTarget({ WARP_API: "http://b" }, warn), "http://b");
  assert.equal(warned.length, 1);
  assert.match(warned[0] ?? "", /WARP_API is the old name of HUMAGEN_API/);
  assert.equal(apiTarget({}, warn), "http://127.0.0.1:8080");
});
