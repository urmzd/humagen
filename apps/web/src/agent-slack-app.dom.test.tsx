import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { AgentSlackApp } from "./AgentSlackApp.tsx";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const unlinked = {
  manifest: { display_information: { name: "Barbara" } },
  create_url: "https://api.slack.com/apps?new_app=1&manifest_json=%7B%7D",
};

test("an agent's own Slack app is created from its manifest and connected from its token", async () => {
  let sent: unknown;
  net = stubFetch({
    "GET /api/agents/barbara/slack-app": () => unlinked,
    "POST /api/agents/barbara/slack-app": (body) => {
      sent = body;
      return { ...unlinked, client: "slack-barbara", bot_user: "U0BARB" };
    },
  });
  render(<AgentSlackApp name="barbara" canLink />);
  const create = (await screen.findByText("Create in Slack")) as HTMLAnchorElement;
  assert.equal(create.getAttribute("href"), unlinked.create_url);
  const connect = screen.getByText("Connect") as HTMLButtonElement;
  assert.ok(connect.disabled, "nothing to connect without a bot token");
  fireEvent.change(screen.getByLabelText("Bot token"), { target: { value: "xoxb-1" } });
  fireEvent.change(screen.getByLabelText("Signing secret"), { target: { value: "sig" } });
  fireEvent.click(connect);
  await screen.findByText(/Connected as slack-barbara/);
  assert.deepEqual(sent, { token: { value: "xoxb-1" }, signing_secret: { value: "sig" } });
});

test("someone who may not connect clients sees whether the agent has its own app, and whom to ask", async () => {
  net = stubFetch({ "GET /api/agents/barbara/slack-app": () => unlinked });
  render(<AgentSlackApp name="barbara" canLink={false} />);
  await screen.findByText("Ask a workspace admin to connect one.");
  assert.equal(screen.queryByText("Create in Slack"), null);
});
