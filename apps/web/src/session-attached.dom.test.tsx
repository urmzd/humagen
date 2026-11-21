import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { attachedTo, ConnectorsSection, ShowMoreList } from "./SessionSections.tsx";
import { type SessionSummary, summarize } from "./sessionSummary.ts";

const words = defaultCopy.sessionPanel;

afterEach(() => cleanup());

const output = (kind: SessionSummary["outputs"][number]["kind"], title: string, url?: string) => ({
  key: `${kind}:${title}`,
  kind,
  title,
  type: kind,
  ...(url ? { url } : {}),
});

function withOutputs(outputs: SessionSummary["outputs"]): SessionSummary {
  return { ...summarize([], {}), outputs };
}

test("a session is attached to the Slack thread it is, then the posts and issues it made, never its pull requests", () => {
  const summary = withOutputs([
    output("slack", "Launch update", "https://acme.slack.com/archives/C1/p2"),
    output("issue", "Rounding is off", "https://github.com/acme/billing/issues/7"),
    output("pr", "Fix rounding", "https://github.com/acme/billing/pull/8"),
    output("slack", "No link"),
    output("issue", "Rounding is off", "https://github.com/acme/billing/issues/7"),
  ]);
  const origin = {
    kind: "slack" as const,
    label: "#launch",
    url: "https://acme.slack.com/archives/C1/p1",
  };
  assert.deepEqual(attachedTo(origin, summary), [
    { kind: "slack", label: words.slackThread, url: "https://acme.slack.com/archives/C1/p1" },
    { kind: "slack", label: "Launch update", url: "https://acme.slack.com/archives/C1/p2" },
    { kind: "github", label: "Rounding is off", url: "https://github.com/acme/billing/issues/7" },
  ]);
  assert.deepEqual(attachedTo({ kind: "schedule", label: "nightly" }, withOutputs([])), []);
});

test("Connectors lists where the session is attached, each opening where it lives", async () => {
  const net = stubFetch({ "GET /api/me/credentials": () => ({ providers: [] }) });
  const summary = withOutputs([
    output("issue", "Rounding is off", "https://github.com/acme/billing/issues/7"),
  ]);
  render(
    <ul>
      <ConnectorsSection
        summary={summary}
        origin={{ kind: "slack", label: "#launch", url: "https://acme.slack.com/archives/C1/p1" }}
      />
    </ul>,
  );
  const list = await screen.findByTestId("panel-attached");
  const links = within(list).getAllByRole("link");
  assert.deepEqual(
    links.map((a) => [a.textContent, a.getAttribute("href")]),
    [
      [words.slackThread, "https://acme.slack.com/archives/C1/p1"],
      ["Rounding is off", "https://github.com/acme/billing/issues/7"],
    ],
  );
  net.restore();
});

test("a long list shows five rows, then Show More shows the rest", () => {
  const items = Array.from({ length: 8 }, (_, i) => `row ${i + 1}`);
  render(
    <ul>
      <ShowMoreList items={items}>{(t) => <li key={t}>{t}</li>}</ShowMoreList>
    </ul>,
  );
  assert.equal(screen.getAllByRole("listitem").length, 6);
  fireEvent.click(screen.getByRole("button", { name: words.showMore(3) }));
  assert.equal(screen.getAllByRole("listitem").length, 8);
  assert.equal(screen.queryByTestId("panel-show-more"), null);
});
