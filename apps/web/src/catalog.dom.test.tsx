import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Catalog } from "./Catalog.tsx";
import { stubFetch } from "./dom.ts";

afterEach(cleanup);
const page = {
  connectors: [
    {
      name: "warehouse",
      system: "snowflake",
      mine: true,
      entries: 1,
      generation: 1,
      capabilities: ["catalog", "live"],
    },
  ],
  entries: [
    {
      system: "snowflake",
      kind: "table",
      path: "DATA.PUBLIC.CUSTOMERS",
      connector: "warehouse",
      seen_at: "2026-11-03T00:00:00Z",
      schema: [{ name: "ID", type: "NUMBER", description: "Customer identifier" }],
    },
  ],
};
test("catalog browses schema and reloads after owner publishes", async () => {
  const { seen: calls, restore } = stubFetch({
    "GET /api/catalog": () => page,
    "POST /api/catalog/connectors/warehouse/publish": () => page.connectors[0],
  });
  try {
    render(<Catalog canWrite />);
    await screen.findByText("DATA.PUBLIC.CUSTOMERS");
    assert.ok(screen.getByText("Customer identifier"));
    fireEvent.click(screen.getByRole("button", { name: defaultCopy.catalog.publish }));
    await waitFor(() => assert.ok(calls.some((call) => call.path.includes("/warehouse/publish"))));
    await waitFor(() =>
      assert.equal(
        calls.filter((call) => call.method === "GET" && call.path.split("?")[0] === "/api/catalog")
          .length,
        2,
      ),
    );
  } finally {
    restore();
  }
});
test("catalog hides owner actions for read-only readers", async () => {
  const { restore } = stubFetch({ "GET /api/catalog": () => page });
  try {
    render(<Catalog canWrite={false} />);
    await screen.findByText("DATA.PUBLIC.CUSTOMERS");
    assert.equal(screen.queryByRole("button", { name: defaultCopy.catalog.publish }), null);
    assert.equal(screen.queryByRole("button", { name: defaultCopy.catalog.crawl }), null);
  } finally {
    restore();
  }
});
test("catalog search submits an addressable query", async () => {
  const { restore } = stubFetch({ "GET /api/catalog": () => page });
  let query = "";
  try {
    render(
      <Catalog
        canWrite={false}
        onFilter={(value) => {
          query = value;
        }}
      />,
    );
    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "customer" } });
    fireEvent.click(screen.getByRole("button", { name: defaultCopy.catalog.search }));
    assert.equal(query, "customer");
  } finally {
    restore();
  }
});
test("catalog reports request failures", async () => {
  const { restore } = stubFetch({ "GET /api/catalog": () => new Response("{}", { status: 503 }) });
  try {
    render(<Catalog canWrite={false} />);
    await screen.findByText(defaultCopy.catalog.failed);
  } finally {
    restore();
  }
});

test("an empty catalog leads to the canonical connection setup", async () => {
  const { restore } = stubFetch({ "GET /api/catalog": () => ({ connectors: [], entries: [] }) });
  try {
    render(<Catalog canWrite />);
    const link = await screen.findByRole("link", { name: "Open Connections" });
    assert.equal(link.getAttribute("href"), "/connections");
    assert.ok(screen.getByText(defaultCopy.catalog.emptyHint));
  } finally {
    restore();
  }
});

test("New Source groups storage capability and configures indexed collection", async () => {
  const { SourceCatalog } = await import("./SourceCatalog.tsx");
  const { ConnectSource } = await import("./Integrations.tsx");
  let provider = "";
  const picker = render(
    <SourceCatalog
      onSelect={(value) => {
        provider = value;
      }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: /Google Cloud Storage/ }));
  assert.equal(provider, "gcs");
  picker.unmount();
  render(<ConnectSource initialProvider="gcs" onCreated={() => {}} onCancel={() => {}} />);
  const mode = screen.getByRole("combobox", {
    name: defaultCopy.sourceOperations.mode,
  }) as HTMLSelectElement;
  assert.equal(mode.value, "indexed");
  assert.equal(mode.options.length, 1);
});
