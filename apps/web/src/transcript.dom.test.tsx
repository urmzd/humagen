import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { failureOf } from "@humagen/adapter/run";
import { defaultCopy } from "@humagen/components";
import { ActionId, emptyTurn, type Turn, type UiAction } from "@humagen/core";
import { GitHubRefKindsContext, githubRefKinds, NowContext, WebSurface } from "@humagen/render-web";
import {
  AgentMessage,
  AgentNameContext,
  ConnectLinkContext,
  type RetryCall,
  RetryCallContext,
} from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import {
  connectTurn,
  failedTurn,
  referencesTurn,
  retriedTurn,
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

test("a failed answer says what happened in one plain sentence, the runtime's words behind Show details", () => {
  const raw =
    'durable: max attempts exhausted: runtime: run r1: agent "github-reviewer" version 1.0.0: platform: model provider: @github-reviewer needs a model provider its models name (anthropic); an administrator enables one in Settings, under Model providers';
  const turn: Turn = {
    ...emptyTurn("r-provider", { name: "github-reviewer" }),
    status: "failed",
    failure: failureOf(raw, "r-provider"),
  };
  const words = defaultCopy.runFailure;
  show(
    <AgentNameContext.Provider value={(n) => (n === "github-reviewer" ? "Github Reviewer" : n)}>
      <AgentMessage turn={turn} disclaimer={false} />
    </AgentNameContext.Provider>,
  );
  // Who can fix it is an admin, so it is a warning, not an alarm.
  const alert = document.querySelector<HTMLElement>('[data-aui="callout"]') as HTMLElement;
  assert.equal(alert.getAttribute("data-tone"), "warn");
  assert.ok(alert.textContent?.includes(words.couldNotRun("Github Reviewer")));
  assert.ok(alert.textContent?.includes(words.because.no_model_provider));
  // The runtime's words wait behind Show details.
  assert.ok(!document.body.textContent?.includes("max attempts exhausted"));
  assert.equal(screen.queryByTestId("callout-details"), null);
  fireEvent.click(within(alert).getByRole("button", { name: line.showDetails }));
  assert.equal(screen.getByTestId("callout-details").textContent, raw);
  fireEvent.click(within(alert).getByRole("button", { name: line.hideDetails }));
  assert.equal(screen.queryByTestId("callout-details"), null);
});

test("an error no cause matches falls back to a plain sentence, with its words behind Show details", () => {
  const turn: Turn = {
    ...emptyTurn("r-odd", { name: "helper" }),
    status: "failed",
    failure: failureOf("agent: step 2\nprovider: 529 overloaded", "r-odd"),
  };
  show(<AgentMessage turn={turn} disclaimer={false} />);
  const alert = screen.getByRole("alert");
  assert.ok(alert.textContent?.includes(defaultCopy.runFailure.couldNotFinish("helper")));
  assert.ok(alert.textContent?.includes(defaultCopy.runFailure.because.unknown));
  assert.ok(!alert.textContent?.includes("overloaded"));
  fireEvent.click(
    within(alert).getByRole("button", { name: defaultCopy.conversation.toolLine.showDetails }),
  );
  assert.match(screen.getByTestId("callout-details").textContent ?? "", /529 overloaded/);
});

test("a failure a later call recovered reads as retried, quietly, with its reason kept", () => {
  show(<AgentMessage turn={retriedTurn()} disclaimer={false} />);
  const group = screen.getByTestId("step-group");
  assert.ok(group.textContent?.includes("Checked 2 sources"));
  assert.equal(within(group).getByTestId("retried-count").textContent, `|${line.retried(1)}`);
  // Nothing failed that still stands: no failed count, and no alarm color on the line.
  assert.equal(within(group).queryByTestId("failed-count"), null);
  assert.equal(group.querySelector(".text-aui-bad"), null);
  // Opened, the attempt is there, said as retried, and its reason waits behind Show Details.
  fireEvent.click(within(group).getByRole("button", { expanded: false }));
  const rows = screen.getAllByTestId("tool-row");
  const retried = rows.find((r) => r.getAttribute("data-retried") === "true") as HTMLElement;
  assert.ok(retried);
  assert.equal(retried.getAttribute("data-failed"), null);
  assert.ok(within(retried).getByText(line.retriedRow));
  assert.ok(within(retried).getByTestId("failure-reason").textContent?.includes("undeliverable"));
  assert.equal(retried.querySelector(".text-aui-bad"), null);
  fireEvent.click(within(retried).getByRole("button", { name: line.showDetails }));
  assert.ok(within(retried).getByText(/urmzd\/humagen/, { selector: "pre, code, span, p, div" }));
});

test("a failure no later call recovered stays a failed count", () => {
  const turn = retriedTurn();
  turn.tasks = [...turn.tasks].reverse();
  show(<AgentMessage turn={turn} disclaimer={false} />);
  const group = screen.getByTestId("step-group");
  assert.ok(within(group).getByTestId("failed-count"));
  assert.equal(within(group).queryByTestId("retried-count"), null);
});

test("a failed delegated run says why, opens its run, and Retry asks the agent again", async () => {
  const seen: UiAction[] = [];
  const asked: RetryCall[] = [];
  show(
    <RetryCallContext.Provider
      value={{
        ask: async (c) => {
          asked.push(c);
        },
      }}
    >
      <AgentMessage turn={failedTurn()} disclaimer={false} />
    </RetryCallContext.Provider>,
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
  const retry = () =>
    within(failure).getByRole("button", {
      name: `${call.retry} github-reviewer`,
    }) as HTMLButtonElement;
  fireEvent.click(retry());
  assert.deepEqual(asked, [
    {
      turnId: "turn-failed",
      agent: "github-reviewer",
      text: "Check the CI status and open pull requests of urmzd/humagen and urmzd/saige.",
    },
  ]);
  // Posted, it says so and is not pressed again.
  const sent = () =>
    within(failure).getByRole("button", {
      name: `${call.retried} github-reviewer`,
    }) as HTMLButtonElement;
  await waitFor(() => assert.equal(sent().disabled, true));
});

test("a Retry the host could not post is ready to press again", async () => {
  let attempts = 0;
  show(
    <RetryCallContext.Provider
      value={{
        ask: () => {
          attempts++;
          return Promise.reject(new Error("503 Service Unavailable"));
        },
      }}
    >
      <AgentMessage turn={failedTurn()} disclaimer={false} />
    </RetryCallContext.Provider>,
  );
  const retry = () =>
    screen.getByRole("button", { name: `${call.retry} github-reviewer` }) as HTMLButtonElement;
  fireEvent.click(retry());
  await waitFor(() => assert.equal(retry().disabled, false));
  assert.equal(retry().textContent, `${call.retry} github-reviewer`);
  fireEvent.click(retry());
  await waitFor(() => assert.equal(attempts, 2));
});

test("a call the host could not post again offers no Retry", () => {
  show(
    <RetryCallContext.Provider value={{ ask: async () => {}, allows: () => false }}>
      <AgentMessage turn={failedTurn()} disclaimer={false} />
    </RetryCallContext.Provider>,
  );
  assert.equal(screen.queryByRole("button", { name: `${call.retry} github-reviewer` }), null);
  assert.ok(screen.getByTestId("call-failure"));
});

test("a link whose text names another reference stays a plain link", () => {
  const { container } = show(
    <AgentMessage
      turn={{
        ...referencesTurn(),
        text: "See [urmzd/humagen#7](https://github.com/evil/repo/pull/8) and [the fix](https://github.com/a/b/pull/9).",
      }}
      disclaimer={false}
    />,
  );
  const chips = [...container.querySelectorAll("a[data-github-ref]")].map((a) => a.textContent);
  assert.deepEqual(chips, ["the fix"]);
  assert.equal(
    screen.getByRole("link", { name: "urmzd/humagen#7" }).getAttribute("href"),
    "https://github.com/evil/repo/pull/8",
  );
});

test("without a host that can ask again, a failed delegated run offers no Retry", () => {
  show(<AgentMessage turn={failedTurn()} disclaimer={false} />);
  const failure = screen.getByTestId("call-failure");
  assert.equal(
    within(failure).queryByRole("button", { name: `${call.retry} github-reviewer` }),
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
  // #461 is a pull request by its address, so every chip for it is one, written out or not.
  assert.deepEqual(refs, [
    ["pull", "urmzd/humagen#461", "https://github.com/urmzd/humagen/issues/461"],
    ["pull", "urmzd/humagen#461", "https://github.com/urmzd/humagen/pull/461"],
    ["issue", "urmzd/humagen#402", "https://github.com/urmzd/humagen/issues/402"],
    [
      "commit",
      "urmzd/humagen@9f2c4e1",
      "https://github.com/urmzd/humagen/commit/9f2c4e1a7b3d5c6e8f0a1b2c3d4e5f6a7b8c9d0e",
    ],
    ["pull", "urmzd/humagen#461", "https://github.com/urmzd/humagen/issues/461"],
  ]);
  assert.equal(
    screen.getAllByRole("link", {
      name: defaultCopy.conversation.githubRef.pull("urmzd/humagen#461"),
    }).length,
    3,
  );
  // Inline code is left as code.
  assert.ok(
    [...container.querySelectorAll("code")].some((c) => c.textContent === "pnpm welcome-copy"),
  );
});

test("one number reads one way: known from anywhere in the answer, else one neutral mark", () => {
  const kinds = (text: string) => {
    const { container } = show(
      <AgentMessage turn={{ ...referencesTurn(), text }} disclaimer={false} />,
    );
    const out = [...container.querySelectorAll("a[data-github-ref]")].map((a) => [
      a.textContent,
      a.getAttribute("data-github-ref"),
      a.querySelector("svg")?.getAttribute("data-ref-kind"),
    ]);
    cleanup();
    return out;
  };
  // Named as an issue after it is first written out: the earlier chip is an issue too.
  assert.deepEqual(
    kinds("See a/b#7, then a/b#7 again, tracked at https://github.com/a/b/issues/7."),
    [
      ["a/b#7", "issue", "issue"],
      ["a/b#7", "issue", "issue"],
      ["a/b#7", "issue", "issue"],
    ],
  );
  // An issue's address and a pull request's for one number: GitHub sends the first on to the second.
  assert.deepEqual(
    kinds("https://github.com/a/b/issues/8 is https://github.com/a/b/pull/8, and a/b#8."),
    [
      ["a/b#8", "pull", "pull"],
      ["a/b#8", "pull", "pull"],
      ["a/b#8", "pull", "pull"],
    ],
  );
  // Nothing says what a/b#9 is: every chip for it has the same neutral mark.
  assert.deepEqual(kinds("a/b#9 and a/b#9."), [
    ["a/b#9", "ref", "ref"],
    ["a/b#9", "ref", "ref"],
  ]);
});

test("a reference is one however its owner and repository are cased", () => {
  const { container } = show(
    <AgentMessage
      turn={{
        ...referencesTurn(),
        text: "URMZD/Humagen#471 is https://github.com/urmzd/humagen/pull/471.",
      }}
      disclaimer={false}
    />,
  );
  assert.deepEqual(
    [...container.querySelectorAll("a[data-github-ref]")].map((a) =>
      a.getAttribute("data-github-ref"),
    ),
    ["pull", "pull"],
  );
  // As written, not as normalised.
  assert.equal(container.querySelector("a[data-github-ref]")?.textContent, "URMZD/Humagen#471");
});

test("a session's pull request reads as one in a later answer that only writes its number", () => {
  const { container } = render(
    <GitHubRefKindsContext.Provider value={githubRefKinds("https://github.com/a/b/pull/5")}>
      <WebSurface>
        <AgentMessage turn={{ ...referencesTurn(), text: "Merged a/b#5." }} disclaimer={false} />
      </WebSurface>
    </GitHubRefKindsContext.Provider>,
  );
  assert.equal(
    container.querySelector("a[data-github-ref]")?.getAttribute("data-github-ref"),
    "pull",
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
