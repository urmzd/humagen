import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import type { Citation } from "@humagen/core";
import * as fixtures from "@humagen/core/fixtures";
import { Markdown, Sources, Task } from "@humagen/primitives";
import {
  CitationProvenance,
  CitationRegistry,
  TurnMetaContext,
  WebSurface,
} from "@humagen/render-web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

const labels = defaultCopy.citation;
const url = "https://example.com/shared-page";
const indexed: Citation = {
  id: "[1]",
  title: "Indexed Runbook",
  url,
  mode: "indexed",
  copyAt: "2026-10-20T12:00:00Z",
  refreshFailed: true,
};
const live: Citation = { id: "[1]", title: "Live Page", url, mode: "live" };
afterEach(cleanup);

test("source references show recorded copy freshness, live retrieval and explicit unknown history", () => {
  const result = render(
    <WebSurface>
      <Sources
        labels={labels}
        items={[indexed, { ...live, id: "[2]" }, { id: "old", title: "Historical Source" }]}
      />
    </WebSurface>,
  );
  assert.ok(screen.getByText("Indexed Copy"));
  assert.ok(screen.getByText("Live Results"));
  assert.ok(screen.getByText("Source Mode Unknown"));
  assert.ok(screen.getByText("Freshness Unknown"));
  assert.ok(screen.getByText("Retrieved for This Turn"));
  assert.ok(screen.getByText(/^Last Good Copy:/));
  assert.equal(result.container.querySelector("time")?.getAttribute("datetime"), indexed.copyAt);
});

test("unproven history and malformed copy dates never gain current freshness", () => {
  const result = render(
    <>
      <CitationProvenance
        citation={{ id: "old", title: "Old", copyAt: "2026-10-20T12:00:00Z", refreshFailed: true }}
        labels={labels}
      />
      <CitationProvenance
        citation={{ id: "bad", title: "Bad date", mode: "indexed", copyAt: "yesterday" }}
        labels={labels}
      />
    </>,
  );
  assert.ok(screen.getByText("Source Mode Unknown"));
  assert.equal(screen.getAllByText("Freshness Unknown").length, 2);
  assert.ok(result.container.querySelector("time") === null);
  assert.ok(screen.queryByText(/Last Good Copy/) === null);
});

test("registry isolates the same URL by immutable turn and never borrows an ambiguous record", () => {
  const registry = new CitationRegistry();
  const old = [indexed],
    current = [live];
  registry.register(old, labels, "older-turn");
  registry.register(current, labels, "newer-turn");
  assert.equal(registry.get(url, "older-turn")?.citation, indexed);
  assert.equal(registry.get(url, "newer-turn")?.citation, live);
  assert.equal(registry.get(url), undefined);
  registry.unregister("older-turn", old);
  assert.equal(registry.get(url, "older-turn"), undefined);
  assert.equal(registry.get(url), undefined);
});

function References({ old = true }: { old?: boolean }) {
  return (
    <WebSurface>
      <div data-testid="older">
        <TurnMetaContext.Provider value={{ ...fixtures.doneTurn, id: "older-turn" }}>
          <Markdown text={`Recorded answer [[1]](${url})`} />
          <Sources labels={labels} scope="older-turn" items={old ? [indexed] : []} />
        </TurnMetaContext.Provider>
      </div>
      <div data-testid="newer">
        <TurnMetaContext.Provider value={{ ...fixtures.doneTurn, id: "newer-turn" }}>
          <Markdown text={`New answer [[1]](${url})`} />
          <Sources labels={labels} scope="newer-turn" items={[live]} />
        </TurnMetaContext.Provider>
      </div>
    </WebSurface>
  );
}

test("inline hover and list use the same recorded facts even when another turn shares the URL", async () => {
  const result = render(<References />);
  const older = screen.getByTestId("older");
  const newer = screen.getByTestId("newer");
  const oldLink = within(older).getByRole("link", { name: "Source 1: Indexed Runbook" });
  fireEvent.focus(oldLink);
  const oldTip = await within(older).findByRole("tooltip");
  assert.ok(within(oldTip).getByText("Indexed Copy"));
  assert.ok(within(oldTip).getByText(/^Last Good Copy:/));
  assert.equal(oldTip.querySelector("time")?.getAttribute("datetime"), indexed.copyAt);
  assert.ok(within(oldTip).queryByText("Live Results") === null);
  fireEvent.blur(oldLink);
  await waitFor(() => assert.ok(within(older).queryByRole("tooltip") === null));
  fireEvent.focus(within(newer).getByRole("link", { name: "Source 1: Live Page" }));
  const newTip = await within(newer).findByRole("tooltip");
  assert.ok(within(newTip).getByText("Live Results"));
  assert.ok(within(newTip).queryByText(/^Last Good Copy:/) === null);
  result.rerender(<References old={false} />);
  const revoked = within(screen.getByTestId("older")).getByRole("link", { name: "Source 1" });
  fireEvent.focus(revoked);
  await waitFor(() =>
    assert.ok(within(screen.getByTestId("older")).queryByRole("tooltip") === null),
  );
});

test("expanded tool source references show the same retained-copy warning", async () => {
  render(
    <WebSurface>
      <Task
        id="tool"
        title="Read Runbook"
        status="complete"
        sources={[indexed]}
        sourceLabels={labels}
      />
    </WebSurface>,
  );
  fireEvent.click(screen.getByRole("button", { name: /Read Runbook/ }));
  assert.ok(await screen.findByText("Indexed Copy"));
  assert.ok(screen.getByText(/^Last Good Copy:/));
});

test("two citations from one turn sharing a URL keep their own numbered evidence", () => {
  const registry = new CitationRegistry();
  registry.register([indexed, live], labels, "one-turn");
  assert.equal(registry.get(url, "one-turn", 1)?.citation, indexed);
  assert.equal(registry.get(url, "one-turn", 2)?.citation, live);
  assert.equal(registry.get(url, "one-turn"), undefined);
});
