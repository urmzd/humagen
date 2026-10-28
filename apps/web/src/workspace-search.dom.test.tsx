import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { QuickSwitcher } from "./QuickSwitcher.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (net) {
    cleanup();
    net.restore();
    net = undefined;
  }
});
const words = defaultCopy.projectSwitcher;

test("workspace search requests indexed content and opens exact message, file and source destinations", async () => {
  let query: URLSearchParams | undefined;
  net = stubFetch({
    "GET /api/discovery": (_body, q) => {
      query = q;
      return {
        items: [
          {
            kind: "message",
            id: "th-a:m-2",
            thread: "th-a",
            message: "m-2",
            title: "Launch evidence",
            snippet: "Saved content inside the second message",
          },
          {
            kind: "file",
            id: "art-a",
            title: "evidence.docx",
            snippet: "Extracted document content",
          },
          {
            kind: "source",
            id: "reference:doc-a",
            source: "reference",
            title: "Retained reference",
          },
        ],
      };
    },
  });
  const opened: unknown[] = [];
  render(<QuickSwitcher onClose={() => {}} onGo={(route) => opened.push(route)} />);
  const message = await screen.findByRole("link", { name: /^Launch evidence/ });
  assert.equal(query?.get("content"), "true");
  assert.equal(query?.get("limit"), "20");
  assert.equal(message.getAttribute("href"), "/conversations/th-a?message=m-2");
  assert.equal(
    screen.getByRole("link", { name: /^evidence.docx/ }).getAttribute("href"),
    "/artifacts/art-a",
  );
  fireEvent.click(screen.getByRole("link", { name: /^Retained reference/ }));
  assert.deepEqual(opened, [{ page: "context", source: "reference", tab: "documents" }]);
  fireEvent.click(screen.getByRole("button", { name: words.files }));
  await screen.findByText("Extracted document content");
  assert.equal(query?.get("kinds"), "file");
});

test("workspace search reports a refused request without showing stale destinations", async () => {
  net = stubFetch({
    "GET /api/discovery": () => new Response(JSON.stringify({ error: "denied" }), { status: 403 }),
  });
  render(<QuickSwitcher onClose={() => {}} onGo={() => {}} />);
  assert.ok(await screen.findByRole("alert"));
  assert.ok(screen.getByText(words.failed));
  assert.equal(screen.queryByRole("link"), null);
});
