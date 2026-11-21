import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { ActionId, type UiAction } from "@humagen/core";
import { NowContext, WebSurface } from "@humagen/render-web";
import { AgentMessage, ConnectLinkContext } from "@humagen/web";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import {
  connectTurn,
  failedTurn,
  referencesTurn,
  runningTurn,
  toolsTurn,
} from "./transcript.fixtures.ts";

afterEach(() => cleanup());

const line = defaultCopy.conversation.toolLine;
const call = defaultCopy.conversation.callFailure;

function show(node: ReactNode, onAction: (a: UiAction) => void = () => {}) {
  return render(<WebSurface onAction={onAction}>{node}</WebSurface>);
}

test("the failed count opens the steps at the failures, each saying why", () => {
  show(<AgentMessage turn={failedTurn()} disclaimer={false} />);
  assert.equal(screen.queryAllByTestId("failure-reason").length, 0);
  fireEvent.click(screen.getByRole("button", { name: line.showFailed(2) }));
  const reasons = screen.getAllByTestId("failure-reason").map((r) => r.textContent ?? "");
  assert.equal(reasons.length, 2);
  assert.ok(reasons[0]?.includes("no GitHub connection can read urmzd/humagen"));
  // Only the first line is said; the rest waits behind Show Details.
  assert.ok(!reasons[0]?.includes("refused before it was sent"));
  const failed = screen
    .getAllByTestId("tool-row")
    .find((r) => r.getAttribute("data-failed") === "true") as HTMLElement;
  fireEvent.click(within(failed).getByRole("button", { name: line.showDetails }));
  assert.ok(within(failed).getByText(/refused before it was sent/));
  assert.ok(within(failed).getByRole("button", { name: line.hideDetails }));
});

test("a failed delegated run says why, opens its run, and Retry asks the agent again", () => {
  const seen: UiAction[] = [];
  show(
    <AgentMessage turn={failedTurn()} disclaimer={false} capabilities={{ retryCall: true }} />,
    (a) => seen.push(a),
  );
  const failure = screen.getByTestId("call-failure");
  assert.ok(
    failure.textContent?.includes("GitHub is not connected for the person this run acts for"),
  );
  assert.ok(!failure.textContent?.includes("three times"));
  fireEvent.click(within(failure).getByRole("button", { name: call.details }));
  assert.deepEqual(seen.at(-1), {
    id: ActionId.openRun,
    payload: { turnId: "turn-failed", target: "r-reviewer-1" },
  });
  fireEvent.click(
    within(failure).getByRole("button", { name: call.retryLabel("github-reviewer") }),
  );
  assert.deepEqual(seen.at(-1), {
    id: ActionId.retryCall,
    payload: {
      turnId: "turn-failed",
      target: "github-reviewer",
      text: "Check the CI status and open pull requests of urmzd/humagen and urmzd/saige.",
    },
  });
  // Pressed once, it says it is retrying and is not pressed again.
  const retrying = within(failure).getByRole("button", {
    name: call.retryLabel("github-reviewer"),
  });
  assert.equal((retrying as HTMLButtonElement).disabled, true);
  assert.equal(retrying.textContent, call.retrying);
});

test("without a host that can ask again, a failed delegated run offers no Retry", () => {
  show(<AgentMessage turn={failedTurn()} disclaimer={false} />);
  const failure = screen.getByTestId("call-failure");
  assert.equal(
    within(failure).queryByRole("button", { name: call.retryLabel("github-reviewer") }),
    null,
  );
});

test("a connection the run needed is a card whose Connect opens that connection", () => {
  show(<AgentMessage turn={connectTurn()} disclaimer={false} />);
  const card = screen.getByTestId("connect-card");
  assert.ok(card.textContent?.includes(defaultCopy.conversation.connect.title("GitHub")));
  const link = within(card).getByRole("link", {
    name: defaultCopy.conversation.connect.action("GitHub"),
  });
  assert.equal(link.getAttribute("href"), "/settings/plugins/github");
  cleanup();
  // A host can send it elsewhere.
  show(
    <ConnectLinkContext.Provider value={(p) => `/connect/${p}`}>
      <AgentMessage turn={connectTurn()} disclaimer={false} />
    </ConnectLinkContext.Provider>,
  );
  assert.equal(
    screen
      .getByRole("link", { name: defaultCopy.conversation.connect.action("GitHub") })
      .getAttribute("href"),
    "/connect/github",
  );
});

test("a turn that needed no connection, or is still working, shows no card", () => {
  show(<AgentMessage turn={failedTurn()} disclaimer={false} />);
  assert.equal(screen.queryByTestId("connect-card"), null);
  cleanup();
  show(<AgentMessage turn={{ ...connectTurn(), status: "working" }} disclaimer={false} />);
  assert.equal(screen.queryByTestId("connect-card"), null);
});

test("pull requests, issues and commits in an answer are chips with their mark", () => {
  const { container } = show(<AgentMessage turn={referencesTurn()} disclaimer={false} />);
  const refs = [...container.querySelectorAll("a[data-github-ref]")].map((a) => [
    a.getAttribute("data-github-ref"),
    a.textContent,
    a.getAttribute("href"),
  ]);
  assert.deepEqual(refs, [
    ["ref", "urmzd/humagen#461", "https://github.com/urmzd/humagen/issues/461"],
    ["pull", "urmzd/humagen#461", "https://github.com/urmzd/humagen/pull/461"],
    ["issue", "urmzd/humagen#402", "https://github.com/urmzd/humagen/issues/402"],
    [
      "commit",
      "urmzd/humagen@9f2c4e1",
      "https://github.com/urmzd/humagen/commit/9f2c4e1a7b3d5c6e8f0a1b2c3d4e5f6a7b8c9d0e",
    ],
    ["ref", "urmzd/humagen#461", "https://github.com/urmzd/humagen/issues/461"],
  ]);
  assert.ok(
    screen.getByRole("link", {
      name: defaultCopy.conversation.githubRef.pull("urmzd/humagen#461"),
    }),
  );
  // Inline code is left as code.
  assert.ok(
    [...container.querySelectorAll("code")].some((c) => c.textContent === "pnpm welcome-copy"),
  );
});

test("an open step card shows a call's input as a table and its command with Copy", () => {
  show(<AgentMessage turn={toolsTurn()} disclaimer={false} />);
  const group = screen.getByTestId("step-group");
  fireEvent.click(within(group).getAllByRole("button")[0] as HTMLElement);
  const card = screen.getByTestId("step-card");
  assert.equal(within(card).getAllByTestId("tool-row").length, 3);
  const search = card.querySelector('[data-tool="tool_search"]') as HTMLElement;
  fireEvent.click(within(search).getAllByRole("button")[0] as HTMLElement);
  const table = within(search).getByTestId("input-table");
  assert.deepEqual(
    [...table.querySelectorAll("dt")].map((d) => d.textContent),
    ["Query", "Max results"],
  );
  assert.ok(!search.textContent?.includes('"max_results"'));
  const bash = card.querySelector('[data-tool="bash"]') as HTMLElement;
  fireEvent.click(within(bash).getAllByRole("button")[0] as HTMLElement);
  const block = within(bash).getByTestId("command-block");
  assert.ok(block.textContent?.includes("git status --short"));
  assert.ok(within(block).getByRole("button", { name: line.copyCommand }));
  // A finished call says how long it took.
  assert.ok(bash.textContent?.includes("1.2s"));
});

test("a running command counts its time beside a working mark and shows what it runs", () => {
  const now = Date.parse("2026-11-20T12:00:00Z");
  show(
    <NowContext.Provider value={now}>
      <AgentMessage turn={runningTurn(now, 112_000)} disclaimer={false} />
    </NowContext.Provider>,
  );
  const group = screen.getByTestId("step-group");
  assert.ok(within(group).getAllByTestId("working-mark").length > 0);
  assert.deepEqual(
    within(group)
      .getAllByTestId("elapsed")
      .map((e) => e.textContent),
    ["1m 52s", "1m 52s"],
  );
  assert.ok(within(group).getByTestId("command-block").textContent?.includes("golangci-lint"));
});
