import assert from "node:assert/strict";
import { test } from "node:test";
import { isReviewText, reviewPins } from "./review-pins.ts";

const review = JSON.stringify({
  format: "humagen.github.review.v1",
  summary: "One problem.",
  comments: [],
  findings: [
    {
      severity: "error",
      title: "Greeting ignores whitespace",
      suggestion: "Trim the name.",
      prompt: "Why?",
      evidence: [
        { observation: "name is used as given", source: "src/greeting.ts:2" },
        {
          observation: "pinned",
          source: "https://github.com/acme/app/blob/abc123/src/greeting.test.ts#L1",
        },
        { observation: "ran it", source: "go test ./..." },
      ],
    },
  ],
});

test("a structured review's cited lines become diff pins", () => {
  assert.equal(isReviewText(review), true);
  assert.equal(isReviewText("# Notes"), false);
  assert.deepEqual(reviewPins(review), [
    {
      path: "src/greeting.ts",
      line: 2,
      severity: "error",
      text: "Greeting ignores whitespace: name is used as given",
    },
    {
      path: "src/greeting.test.ts",
      line: 1,
      severity: "error",
      text: "Greeting ignores whitespace: pinned",
    },
  ]);
  assert.deepEqual(reviewPins('{"format":"other","findings":[]}'), []);
  assert.deepEqual(reviewPins("not json"), []);
});
