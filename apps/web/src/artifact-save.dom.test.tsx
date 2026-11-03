import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { ChangeReview } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ArtifactCompare } from "./Deliverables.tsx";
import { stubFetch } from "./dom.ts";
import { reviewPins } from "./review-pins.ts";
import { SaveAsArtifact } from "./SaveAsArtifact.tsx";
import type { Stored } from "./storage.ts";

const words = defaultCopy.workspace;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const stored = (id: string, version: number): Stored => ({
  id,
  title: "plan.md",
  kind: "document",
  mime: "text/markdown",
  size: 20,
  sha256: "a".repeat(64),
  version,
  run: "",
  thread: "thread-1",
  agent: "",
  created_by: "ana",
  created_at: "2026-11-01T10:00:00Z",
});

test("Save as Artifact saves a message by its source and hands back the version", async () => {
  const bodies: unknown[] = [];
  net = stubFetch({
    "POST /api/threads/thread-1/artifacts": (body) => {
      bodies.push(body);
      return stored("art-3", 3);
    },
  });
  const saved: string[] = [];
  render(
    <SaveAsArtifact
      thread="thread-1"
      what={{ source: "run", id: "r-1" }}
      className="x"
      onSaved={(a) => saved.push(a.id)}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: words.save.action }));
  await waitFor(() => assert.deepEqual(saved, ["art-3"]));
  assert.deepEqual(bodies, [{ source: "run", id: "r-1" }]);
  assert.ok(screen.getByText(words.save.saved(3)));
});

test("a refused save says why and opens nothing", async () => {
  net = stubFetch({
    "POST /api/threads/thread-1/artifacts": () =>
      new Response(JSON.stringify({ error: "storage limit reached" }), { status: 400 }),
  });
  const saved: string[] = [];
  render(
    <SaveAsArtifact
      thread="thread-1"
      what={{ source: "scratchpad", id: "sp-1" }}
      className="x"
      label
      onSaved={(a) => saved.push(a.id)}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: words.save.action }));
  assert.ok(await screen.findByText("storage limit reached"));
  assert.deepEqual(saved, []);
});

test("two versions compare side by side on a wide screen, older on the left", async () => {
  const match = window.matchMedia;
  window.matchMedia = ((query: string) => ({
    matches: query.includes("min-width"),
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
  try {
    net = stubFetch({
      "GET /api/artifacts/art-1/content": () => new Response("# Plan\nship tuesday\n"),
      "GET /api/artifacts/art-2/content": () => new Response("# Plan\nship wednesday\n"),
    });
    render(<ArtifactCompare a={stored("art-2", 2)} b={stored("art-1", 1)} onClose={() => {}} />);
    assert.ok(await screen.findByRole("region", { name: words.compare.title(1, 2) }));
    const table = await screen.findByRole("region", { name: /Diff for plan\.md|plan\.md/ });
    await waitFor(() => assert.ok(document.querySelector('[data-layout="split"]')));
    const cells = [...table.querySelectorAll("td")].map((td) => td.textContent ?? "");
    assert.ok(cells.some((c) => c.includes("ship tuesday")));
    assert.ok(cells.some((c) => c.includes("ship wednesday")));
  } finally {
    window.matchMedia = match;
  }
});

test("on a phone the diff is one column, and review findings pin to their lines", () => {
  const match = window.matchMedia;
  window.matchMedia = (() => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
  try {
    const pins = reviewPins(
      JSON.stringify({
        format: "humagen.github.review.v1",
        findings: [
          {
            severity: "warning",
            title: "Untrimmed",
            evidence: [{ observation: "name as given", source: "a.ts:2" }],
          },
        ],
      }),
    );
    render(<ChangeReview before={"one\ntwo\n"} after={"one\nTWO\n"} path="a.ts" pins={pins} />);
    assert.equal(document.querySelector('[data-layout="split"]'), null);
    const pin = document.querySelector('[data-slot="review-pin"]');
    assert.ok(pin?.textContent?.includes("Untrimmed: name as given"));
  } finally {
    window.matchMedia = match;
  }
});
