import assert from "node:assert/strict";
import { mock, test } from "node:test";
import { followSession } from "./sessionStream.ts";
import { type Locks, openStream, STREAM_SLOTS, type Visibility } from "./stream.ts";

type Fake = {
  url: string;
  closed: boolean;
  listeners: Map<string, (event: { data: string }) => void>;
};

/** An EventSource that records what was opened and closed. */
function fakeSources(): { sources: Fake[]; restore: () => void } {
  const original = globalThis.EventSource;
  const sources: Fake[] = [];
  class Source {
    listeners = new Map<string, (event: { data: string }) => void>();
    closed = false;
    url: string;
    constructor(url: string) {
      this.url = url;
      sources.push(this);
    }
    addEventListener(type: string, callback: (event: { data: string }) => void) {
      this.listeners.set(type, callback);
    }
    close() {
      this.closed = true;
    }
  }
  globalThis.EventSource = Source as unknown as typeof EventSource;
  return {
    sources,
    restore: () => {
      globalThis.EventSource = original;
    },
  };
}

/** A page whose visibility a test sets. */
function fakePage(): Visibility & { show: () => void; hide: () => void; listening: () => number } {
  const listeners = new Set<() => void>();
  const page = {
    visibilityState: "visible",
    addEventListener: (_: "visibilitychange", listener: () => void) => void listeners.add(listener),
    removeEventListener: (_: "visibilitychange", listener: () => void) =>
      void listeners.delete(listener),
    show: () => {
      page.visibilityState = "visible";
      for (const listener of listeners) listener();
    },
    hide: () => {
      page.visibilityState = "hidden";
      for (const listener of listeners) listener();
    },
    listening: () => listeners.size,
  };
  return page;
}

test("a stream gives its connection back once its tab has been hidden, and takes one when shown", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  const { sources, restore } = fakeSources();
  try {
    const page = fakePage();
    const wired: boolean[] = [];
    const stop = openStream("/api/events", (_, resumed) => wired.push(resumed), {
      page,
      graceMs: 1000,
    });
    assert.equal(sources.length, 1);
    assert.deepEqual(wired, [false]);

    page.hide();
    mock.timers.tick(999);
    assert.equal(sources[0]?.closed, false, "a tab hidden for a moment keeps its stream");
    mock.timers.tick(1);
    assert.equal(sources[0]?.closed, true);

    page.show();
    assert.equal(sources.length, 2, "a shown tab opens its stream again");
    assert.deepEqual(wired, [false, true], "and is told what it missed must be read again");

    stop();
    assert.equal(sources[1]?.closed, true);
    assert.equal(page.listening(), 0);
  } finally {
    restore();
    mock.timers.reset();
  }
});

test("a tab shown again within the grace keeps the stream it had", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  const { sources, restore } = fakeSources();
  try {
    const page = fakePage();
    const stop = openStream("/api/events", () => {}, { page, graceMs: 1000 });
    page.hide();
    mock.timers.tick(500);
    page.show();
    mock.timers.tick(5000);
    assert.equal(sources.length, 1);
    assert.equal(sources[0]?.closed, false);
    stop();
  } finally {
    restore();
    mock.timers.reset();
  }
});

test("a tab opened in the background holds its stream only for the grace", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  const { sources, restore } = fakeSources();
  try {
    const page = fakePage();
    page.visibilityState = "hidden";
    const stop = openStream("/api/events", () => {}, { page, graceMs: 1000 });
    assert.equal(sources.length, 1);
    mock.timers.tick(1000);
    assert.equal(sources[0]?.closed, true);
    stop();
    page.show();
    assert.equal(sources.length, 1, "a stopped stream never opens again");
  } finally {
    restore();
    mock.timers.reset();
  }
});

test("a session stream opened again starts its listeners from the record", () => {
  const { sources, restore } = fakeSources();
  const g = globalThis as { document?: unknown };
  const had = "document" in g;
  const before = g.document;
  const page = fakePage();
  g.document = page;
  // No Web Locks here, so the stream opens at once: the slots have a test of their own.
  const navigatorWas = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  Object.defineProperty(globalThis, "navigator", { value: {}, configurable: true });
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const resets: string[] = [];
    const snapshots: string[] = [];
    const stop = followSession("s_hidden", {
      reset: (session) => resets.push(session.id),
      session: (session) => snapshots.push(session.id),
    });
    const frame = { data: JSON.stringify({ id: "s_hidden" }) };
    sources[0]?.listeners.get("session")?.(frame);
    assert.deepEqual(resets, [], "the first connection's snapshot is no reset");

    page.hide();
    mock.timers.tick(60_000);
    assert.equal(sources[0]?.closed, true);
    page.show();
    sources[1]?.listeners.get("session")?.(frame);
    sources[1]?.listeners.get("session")?.(frame);
    assert.deepEqual(resets, ["s_hidden"], "only the first snapshot after resuming resets");
    assert.deepEqual(snapshots, ["s_hidden", "s_hidden", "s_hidden"]);
    stop();
  } finally {
    mock.timers.reset();
    if (navigatorWas) Object.defineProperty(globalThis, "navigator", navigatorWas);
    if (had) g.document = before;
    else delete g.document;
    restore();
  }
});

/** Web Locks for one origin: a name is held until its holder's promise resolves. */
function fakeLocks(): Locks & { held: Set<string> } {
  const held = new Set<string>();
  return {
    held,
    request: async (name, _options, take) => {
      if (held.has(name)) {
        await take(null);
        return;
      }
      held.add(name);
      try {
        await take({ name });
      } finally {
        held.delete(name);
      }
    },
  };
}

const turn = () => new Promise<void>((resolve) => setImmediate(resolve));

test("streams across tabs never hold more than the shared slots, and the rest poll until one is free", async () => {
  const { sources, restore } = fakeSources();
  try {
    const locks = fakeLocks();
    const page = fakePage();
    const stops: (() => void)[] = [];
    for (let i = 0; i < STREAM_SLOTS; i++)
      stops.push(openStream(`/api/events?tab=${i}`, () => {}, { page, locks }));
    await turn();
    assert.equal(sources.length, STREAM_SLOTS);

    let polls = 0;
    const wired: boolean[] = [];
    const stop = openStream("/api/events?tab=late", (_, resumed) => wired.push(resumed), {
      page,
      locks,
      pollMs: 5,
      poll: () => polls++,
    });
    await new Promise((resolve) => setTimeout(resolve, 40));
    assert.equal(sources.length, STREAM_SLOTS, "a stream with no slot opens no connection");
    assert.ok(polls >= 2, "and reads again while it waits");

    stops[0]?.();
    await new Promise((resolve) => setTimeout(resolve, 40));
    assert.equal(sources.length, STREAM_SLOTS + 1, "a freed slot is taken");
    assert.deepEqual(wired, [true], "by a stream that knows it missed what came before");
    const settledPolls = polls;
    await new Promise((resolve) => setTimeout(resolve, 30));
    assert.equal(polls, settledPolls, "a connected stream stops polling");

    stop();
    for (const s of stops) s();
    await turn();
    assert.equal(locks.held.size, 0, "every slot is given back");
    assert.ok(sources.every((s) => s.closed));
  } finally {
    restore();
  }
});

test("a hidden tab gives its slot back with its connection", async () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  const { sources, restore } = fakeSources();
  try {
    const locks = fakeLocks();
    const page = fakePage();
    const stop = openStream("/api/events", () => {}, { page, locks, graceMs: 1000 });
    await turn();
    assert.equal(locks.held.size, 1);
    page.hide();
    mock.timers.tick(1000);
    await turn();
    assert.equal(sources[0]?.closed, true);
    assert.equal(locks.held.size, 0);
    page.show();
    await turn();
    assert.equal(sources.length, 2);
    assert.equal(locks.held.size, 1);
    stop();
  } finally {
    restore();
    mock.timers.reset();
  }
});
