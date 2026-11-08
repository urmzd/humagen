import assert from "node:assert/strict";
import { test } from "node:test";
import { firstLines, placeOf, referenceOf, slackUrl } from "./trace.ts";

test("references read as places with links back", () => {
  assert.deepEqual(referenceOf("slack://T1/C1/1699.1"), {
    kind: "slack",
    label: "Slack thread",
    url: "https://app.slack.com/client/T1/C1/thread/C1-1699.1",
  });
  assert.equal(referenceOf("slack://T1/D9/1699.1").label, "Slack DM");
  assert.deepEqual(referenceOf("github://urmzd/humagen/pull/243"), {
    kind: "github",
    label: "urmzd/humagen PR #243",
    url: "https://github.com/urmzd/humagen/pull/243",
  });
  assert.equal(
    referenceOf("github://urmzd/humagen/issue/9").url,
    "https://github.com/urmzd/humagen/issues/9",
  );
  assert.equal(referenceOf("schedule://nightly").label, "Schedule nightly");
  assert.equal(referenceOf("schedule://nightly/2026-11-07T02:00").label, "Schedule nightly");
  assert.equal(referenceOf("https://example.com/a/b").label, "example.com/a/b");
  assert.equal(referenceOf("corpus:hr/doc-1").kind, "other");
  assert.equal(slackUrl("", "C1", "1"), "https://slack.com/app_redirect?channel=C1");
});

test("first lines keep two lines and cap their length", () => {
  assert.equal(firstLines("a\n\n b \nc"), "a b");
  assert.equal(firstLines("x".repeat(300)).length, 241);
});

test("a session's place: Slack by its surface, PRs by the root's parent, else the console", () => {
  assert.deepEqual(
    placeOf({ surface: { client: "acme", channel: "C1", ts: "1.2" }, visibility: "shared" }, "T1"),
    {
      kind: "slack",
      label: "Slack thread",
      url: "https://app.slack.com/client/T1/C1/thread/C1-1.2",
      origin: "slack://T1/C1/1.2",
      audience: "channel",
    },
  );
  assert.equal(
    placeOf({ surface: { client: "acme", channel: "D1", ts: "1" }, visibility: "private" })
      .audience,
    "dm",
  );
  const pr = placeOf({
    session: { parent: "github://urmzd/humagen/pull/5" },
    visibility: "shared",
  });
  assert.deepEqual(
    [pr.kind, pr.label, pr.url, pr.audience],
    ["github", "urmzd/humagen PR #5", "https://github.com/urmzd/humagen/pull/5", "shared"],
  );
  assert.deepEqual(placeOf({ session: { parent: "s_abc" }, visibility: "private" }), {
    kind: "console",
    label: "Console",
    audience: "private",
  });
});
