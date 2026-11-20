import "./react.ts";
import assert from "node:assert/strict";
import { test } from "node:test";
import { Home } from "@humagen/components";
import { homeView } from "@humagen/render-slack";
import { homeModel } from "./home.ts";

const reads = {
  person: { id: "U1", name: "Ana" },
  assistant: { name: "Humagen", handle: "Humagen" },
  intro: "Ask anything.",
  inbox: { messages: [], open: 0 },
  announcements: [],
  projects: null,
  workspace: "acme",
  build: false,
  href: () => undefined,
};

test("Home introduces the agents someone gave a greeting or starters, with how to ask each", () => {
  const model = homeModel({
    ...reads,
    meet: [
      {
        handle: "barbara",
        name: "Barbara",
        greeting: "I keep customer performance reports current.",
        starters: [
          { title: "Summarize the latest reports", prompt: "Summarize the latest reports." },
        ],
      },
    ],
  });
  const text = JSON.stringify(homeView(<Home home={model} />).view.blocks);
  assert.match(text, /Meet your agents/);
  assert.match(text, /Barbara/);
  assert.match(text, /I keep customer performance reports current\./);
  assert.match(text, /@Humagen @barbara/);
  assert.match(text, /Summarize the latest reports/);
});

test("Home names no agents when none introduced themselves", () => {
  const text = JSON.stringify(homeView(<Home home={homeModel(reads)} />).view.blocks);
  assert.doesNotMatch(text, /Meet your agents/);
});
