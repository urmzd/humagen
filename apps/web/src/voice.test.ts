import assert from "node:assert/strict";
import { test } from "node:test";
import { recordingType, withVoiceNote } from "./voice.ts";

test("a transcript joins the draft marked as a voice note, as the server marks one from Slack", () => {
  assert.equal(
    withVoiceNote("", { text: " Move standup to ten. ", language: "english" }),
    "[Voice note, english] Move standup to ten.",
  );
  assert.equal(
    withVoiceNote("@helper ", { text: "Draft the recap." }),
    "@helper\n\n[Voice note] Draft the recap.",
  );
});

test("the recorder makes Opus where it can, and what Safari makes where it cannot", () => {
  assert.equal(
    recordingType(() => true),
    "audio/webm;codecs=opus",
  );
  assert.equal(
    recordingType((t) => t === "audio/mp4"),
    "audio/mp4",
  );
  assert.equal(
    recordingType(() => {
      throw new Error("no");
    }),
    undefined,
  );
});
