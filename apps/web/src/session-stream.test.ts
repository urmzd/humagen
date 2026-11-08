import assert from "node:assert/strict";
import { test } from "node:test";
import { followSession } from "./sessionStream.ts";

test("session messages and details share one resumable source and release it after the last listener", () => {
  const original = globalThis.EventSource;
  const sources: {
    url: string;
    listeners: Map<string, (event: { data: string }) => void>;
    closed: boolean;
  }[] = [];
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
  try {
    const snapshots: string[] = [];
    const runs: string[] = [];
    const resets: string[] = [];
    const closeDetails = followSession("s_stream", {
      session: (session) => snapshots.push(session.id),
    });
    const closeMessage = followSession("s_stream", {
      run: (event) => runs.push(event.run ?? ""),
      reset: (session) => resets.push(session.id),
    });
    assert.equal(sources.length, 1);
    assert.equal(sources[0]?.url, "/api/sessions/s_stream/stream");
    sources[0]?.listeners.get("session")?.({ data: JSON.stringify({ id: "s_stream" }) });
    sources[0]?.listeners.get("run")?.({ data: JSON.stringify({ run: "run-one" }) });
    assert.deepEqual(snapshots, ["s_stream"]);
    assert.deepEqual(runs, ["run-one"]);
    sources[0]?.listeners.get("session-reset")?.({ data: JSON.stringify({ id: "s_stream" }) });
    assert.deepEqual(resets, ["s_stream"]);
    assert.deepEqual(
      snapshots,
      ["s_stream", "s_stream"],
      "reset refreshes ordinary snapshot consumers too",
    );
    closeDetails();
    assert.equal(sources[0]?.closed, false);
    closeMessage();
    assert.equal(sources[0]?.closed, true);
  } finally {
    globalThis.EventSource = original;
  }
});
