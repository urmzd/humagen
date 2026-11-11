import assert from "node:assert/strict";
import { test } from "node:test";
import type { Corpus, CorpusMember, Source } from "./api.ts";
import { corpusFreshness, memberFreshness, sourceFreshness, worstFreshness } from "./freshness.ts";

const now = Date.parse("2026-11-10T12:00:00Z");
const HOUR = 3600;
const before = (seconds: number) => new Date(now - seconds * 1000).toISOString();

type Lifecycle = NonNullable<Source["lifecycle"]>;
const lifecycle = (over: Partial<Lifecycle> = {}): Lifecycle => ({
  provider: "drive",
  mode: "indexed",
  status: "in_sync",
  connected_at: before(30 * 24 * HOUR),
  stop_after_seconds: 86400,
  sync_every_seconds: HOUR,
  attempts: [],
  ...over,
});
const attempt = (status: Lifecycle["attempts"][number]["status"], finished = true) => ({
  id: status,
  started_at: before(120),
  status,
  ...(finished ? { finished_at: before(60) } : {}),
});
const source = (name: string, lc?: Lifecycle): Source => ({
  name,
  documents: 1,
  passages: 1,
  excluded: 0,
  variants: [],
  updated_at: before(0),
  ...(lc ? { lifecycle: lc } : {}),
});
const member = (name: string, over: Partial<CorpusMember> = {}): CorpusMember => ({
  kind: "source",
  name,
  items: 1,
  status: "ready",
  ...over,
});

test("a source synced within its interval is fresh, and says when", () => {
  const at = before(HOUR / 2);
  assert.deepEqual(sourceFreshness(lifecycle({ last_good_at: at }), now), { state: "fresh", at });
});

test("a source is stale only past twice its own refresh interval", () => {
  const exactly = before(2 * HOUR);
  assert.equal(sourceFreshness(lifecycle({ last_good_at: exactly }), now)?.state, "fresh");
  const past = before(2 * HOUR + 1);
  assert.deepEqual(sourceFreshness(lifecycle({ last_good_at: past }), now), {
    state: "stale",
    at: past,
  });
  // The interval is the source's own: a daily source is fresh at three hours.
  assert.equal(
    sourceFreshness(
      lifecycle({ last_good_at: before(3 * HOUR), sync_every_seconds: 24 * HOUR }),
      now,
    )?.state,
    "fresh",
  );
});

test("a source with no refresh interval is fresh once synced, however long ago", () => {
  const old = lifecycle({ last_good_at: before(400 * 24 * HOUR), sync_every_seconds: undefined });
  assert.equal(sourceFreshness(old, now)?.state, "fresh");
  assert.equal(sourceFreshness({ ...old, sync_every_seconds: 0 }, now)?.state, "fresh");
});

test("a source with no good sync is never synced", () => {
  assert.deepEqual(sourceFreshness(lifecycle({ status: "connected" }), now), { state: "never" });
});

test("an attempt in flight is syncing, and keeps the last good time", () => {
  const at = before(10 * HOUR);
  assert.deepEqual(
    sourceFreshness(
      lifecycle({ last_good_at: at, attempts: [attempt("failed"), attempt("syncing", false)] }),
      now,
    ),
    { state: "syncing", at },
  );
  assert.equal(sourceFreshness(lifecycle({ status: "indexing" }), now)?.state, "syncing");
});

test("a failed last attempt is failed, even over an old good sync", () => {
  const at = before(10 * HOUR);
  assert.deepEqual(
    sourceFreshness(
      lifecycle({ last_good_at: at, attempts: [attempt("succeeded"), attempt("failed")] }),
      now,
    ),
    { state: "failed", at },
  );
  assert.equal(sourceFreshness(lifecycle({ status: "needs_attention" }), now)?.state, "failed");
  // A failure followed by a good sync is over.
  assert.equal(
    sourceFreshness(
      lifecycle({ last_good_at: before(60), attempts: [attempt("failed"), attempt("succeeded")] }),
      now,
    )?.state,
    "fresh",
  );
});

test("syncing does not apply to a source with no lifecycle or one searched live", () => {
  assert.equal(sourceFreshness(undefined, now), null);
  assert.equal(sourceFreshness(lifecycle({ mode: "live" }), now), null);
});

test("a member is judged by its source, and otherwise only by a failing or syncing status", () => {
  const sources = [source("drive-hr", lifecycle({ last_good_at: before(5 * HOUR) }))];
  assert.equal(memberFreshness(member("drive-hr"), sources, now)?.state, "stale");
  // A warehouse and a source not loaded have no sync time in the console.
  assert.equal(memberFreshness(member("wh", { kind: "catalog" }), sources, now), null);
  assert.deepEqual(
    memberFreshness(member("wh", { kind: "catalog", status: "failed" }), sources, now),
    {
      state: "failed",
    },
  );
  assert.deepEqual(memberFreshness(member("unloaded", { status: "syncing" }), sources, now), {
    state: "syncing",
  });
  // A warehouse named as a source is not that source.
  assert.equal(memberFreshness(member("drive-hr", { kind: "catalog" }), sources, now), null);
  assert.equal(memberFreshness(member("", { status: "hidden" }), sources, now), null);
  assert.equal(memberFreshness(member("drive-hr", { status: "missing" }), sources, now), null);
});

test("a corpus rolls up to the worst state among its members", () => {
  assert.equal(worstFreshness([]), null);
  assert.equal(worstFreshness(["fresh", "syncing"]), "syncing");
  assert.equal(worstFreshness(["syncing", "never", "fresh"]), "never");
  assert.equal(worstFreshness(["never", "stale", "fresh"]), "stale");
  assert.equal(worstFreshness(["stale", "fresh", "failed", "syncing"]), "failed");

  const sources = [
    source("fresh", lifecycle({ last_good_at: before(60) })),
    source("stale", lifecycle({ last_good_at: before(5 * HOUR) })),
    source("upload"),
  ];
  const corpus = (members: CorpusMember[]) => ({ members }) as Corpus;
  assert.equal(corpusFreshness(corpus([member("fresh")]), sources, now), "fresh");
  assert.equal(corpusFreshness(corpus([member("fresh"), member("stale")]), sources, now), "stale");
  assert.equal(
    corpusFreshness(
      corpus([
        member("fresh"),
        member("stale"),
        member("wh", { kind: "catalog", status: "failed" }),
      ]),
      sources,
      now,
    ),
    "failed",
  );
  // Nothing to judge: no members, or only ones syncing does not apply to.
  assert.equal(corpusFreshness(corpus([]), sources, now), null);
  assert.equal(corpusFreshness(corpus([member("upload")]), sources, now), null);
});
