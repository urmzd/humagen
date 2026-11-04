import assert from "node:assert/strict";
import { test } from "node:test";
import { replayText } from "./replay.ts";

test("a replay sends the same words to the chosen agent alone", () => {
  assert.equal(
    replayText("writer", "@lead draft the launch note"),
    "@writer draft the launch note",
  );
  assert.equal(replayText("writer", " @lead @helper  check it"), "@writer check it");
  assert.equal(replayText("writer", "plain words"), "@writer plain words");
});
