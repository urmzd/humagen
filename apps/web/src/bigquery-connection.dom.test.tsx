import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { BigQueryConnection } from "./BigQueryConnection.tsx";
import { stubFetch } from "./dom.ts";

afterEach(cleanup);
const copy = defaultCopy.bigQueryConnection;
const setup = {
  oauth_available: true,
  connected: true,
  adc: [],
  credential_refs: [],
  oauth_connectors: [],
};
const configured = {
  name: "warehouse",
  system: "bigquery",
  mine: true,
  capabilities: ["catalog", "live"],
  entries: 0,
  generation: 0,
};

test("BigQuery saves a private connection with bounded datasets and requires verification before publication", async () => {
  let installed = false;
  const { seen, restore } = stubFetch({
    "GET /api/catalog/connectors/bigquery/setup": () => setup,
    "GET /api/catalog": () => ({ connectors: installed ? [configured] : [], entries: [] }),
    "POST /api/catalog/connectors/bigquery": () => {
      installed = true;
      return configured;
    },
  });
  try {
    render(<BigQueryConnection canWrite onCancel={() => {}} />);
    await screen.findByText(copy.empty);
    fireEvent.change(screen.getByLabelText(copy.cloudProject), {
      target: { value: "cloud-project" },
    });
    fireEvent.change(screen.getByLabelText(copy.datasets, { exact: false }), {
      target: { value: "sales" },
    });
    fireEvent.click(screen.getByRole("button", { name: copy.save }));
    await screen.findByText(copy.saved);
    const value = seen.find((call) => call.method === "POST")?.body as Record<string, unknown>;
    assert.equal(value.owner, undefined);
    assert.equal(value.tenant, undefined);
    assert.deepEqual(value.datasets, ["sales"]);
    assert.deepEqual(value.credential, { kind: "oauth" });
    assert.equal(
      seen.some((call) => call.path.endsWith("/crawl") || call.path.endsWith("/publish")),
      false,
    );
    fireEvent.click(screen.getByRole("button", { name: copy.reload }));
    await screen.findByRole("button", { name: copy.verify });
    assert.equal(
      (screen.getByRole("button", { name: copy.publish }) as HTMLButtonElement).disabled,
      true,
    );
    assert.ok(screen.getByText(copy.unchecked));
  } finally {
    restore();
  }
});

test("BigQuery verification failure retains the previous successful count and requires a new check before publishing", async () => {
  const previous = { ...configured, entries: 7, generation: 2, crawled_at: "2026-11-01T12:00:00Z" };
  let fail = true;
  let published = false;
  let verified = 0;
  const { seen, restore } = stubFetch({
    "GET /api/catalog/connectors/bigquery/setup": () => setup,
    "GET /api/catalog": () => ({ connectors: [previous], entries: [] }),
    "POST /api/catalog/connectors/warehouse/crawl": () =>
      fail
        ? new Response("{}", { status: 502 })
        : { ...previous, entries: 9, generation: 3, crawled_at: "2026-11-07T12:00:00Z" },
    "POST /api/catalog/connectors/warehouse/publish": () => {
      published = true;
      return { ...previous, entries: 9, published: { by: "alice", at: "2026-11-07T12:01:00Z" } };
    },
  });
  try {
    render(
      <BigQueryConnection
        canWrite
        onCancel={() => {}}
        onVerified={() => {
          verified++;
        }}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: copy.verify }));
    await screen.findByText(copy.verifyFailed);
    assert.ok(screen.getByText("7"));
    assert.equal(document.querySelector("time")?.dateTime, previous.crawled_at);
    assert.equal(
      (screen.getByRole("button", { name: copy.publish }) as HTMLButtonElement).disabled,
      true,
    );
    assert.equal(verified, 0);
    fireEvent.click(screen.getByRole("button", { name: copy.reload }));
    await waitFor(() =>
      assert.equal(
        (screen.getByRole("button", { name: copy.reload }) as HTMLButtonElement).disabled,
        false,
      ),
    );
    assert.ok(screen.getByText(copy.checkFailed));
    assert.equal(
      (screen.getByRole("button", { name: copy.publish }) as HTMLButtonElement).disabled,
      true,
    );
    fail = false;
    fireEvent.click(screen.getByRole("button", { name: copy.verify }));
    await screen.findByText(copy.verified);
    assert.equal(verified, 1);
    assert.ok(screen.getByText("9"));
    assert.equal(document.querySelector("time")?.dateTime, "2026-11-07T12:00:00Z");
    assert.equal(published, false);
    fireEvent.click(screen.getByRole("button", { name: copy.publish }));
    await screen.findByText(copy.published);
    assert.equal(published, true);
    assert.equal(seen.filter((call) => call.path.endsWith("/crawl")).length, 2);
  } finally {
    restore();
  }
});

test("BigQuery readers cannot configure, verify or publish someone else's connection", async () => {
  const { seen, restore } = stubFetch({
    "GET /api/catalog": () => ({ connectors: [{ ...configured, mine: false }], entries: [] }),
  });
  try {
    render(<BigQueryConnection canWrite={false} onCancel={() => {}} />);
    await screen.findByText(copy.readOnly);
    assert.equal(screen.queryByRole("button", { name: copy.save }), null);
    assert.equal(screen.queryByRole("button", { name: copy.verify }), null);
    assert.equal(screen.queryByRole("button", { name: copy.publish }), null);
    assert.deepEqual(
      seen.map((call) => call.path),
      ["/api/catalog"],
    );
  } finally {
    restore();
  }
});

test("BigQuery reload failure removes stale controls until a successful reload", async () => {
  let fail = false;
  const { restore } = stubFetch({
    "GET /api/catalog/connectors/bigquery/setup": () => setup,
    "GET /api/catalog": () =>
      fail ? new Response("{}", { status: 503 }) : { connectors: [configured], entries: [] },
  });
  try {
    render(<BigQueryConnection canWrite onCancel={() => {}} />);
    await screen.findByRole("button", { name: copy.verify });
    fail = true;
    fireEvent.click(screen.getByRole("button", { name: copy.reload }));
    await screen.findByText(copy.loadFailed);
    assert.equal(screen.queryByRole("button", { name: copy.verify }), null);
    fail = false;
    fireEvent.click(screen.getByRole("button", { name: copy.reload }));
    await screen.findByRole("button", { name: copy.verify });
    assert.equal(screen.queryByText(copy.loadFailed), null);
  } finally {
    restore();
  }
});

test("BigQuery requires explicit Google authorization and hides unauthorized deployment fallbacks", async () => {
  let authorized = 0;
  const { seen, restore } = stubFetch({
    "GET /api/catalog/connectors/bigquery/setup": () => ({ ...setup, connected: false }),
    "GET /api/catalog": () => ({ connectors: [], entries: [] }),
  });
  try {
    render(
      <BigQueryConnection
        canWrite
        onCancel={() => {}}
        onAuthorize={() => {
          authorized++;
        }}
      />,
    );
    await screen.findByText(copy.connectFirst);
    assert.equal(
      (screen.getByRole("button", { name: copy.save }) as HTMLButtonElement).disabled,
      true,
    );
    assert.equal(screen.queryByRole("option", { name: copy.defaultCredentials }), null);
    assert.equal(screen.queryByRole("option", { name: copy.reference }), null);
    fireEvent.click(screen.getByRole("button", { name: copy.connectGoogle }));
    assert.equal(authorized, 1);
    assert.equal(
      seen.some((call) => call.method === "POST"),
      false,
    );
  } finally {
    restore();
  }
});

test("BigQuery save failure keeps entered datasets and never reports metadata verification", async () => {
  const { restore } = stubFetch({
    "GET /api/catalog/connectors/bigquery/setup": () => setup,
    "GET /api/catalog": () => ({ connectors: [], entries: [] }),
    "POST /api/catalog/connectors/bigquery": () => new Response("{}", { status: 409 }),
  });
  try {
    render(<BigQueryConnection canWrite onCancel={() => {}} />);
    await screen.findByText(copy.empty);
    fireEvent.change(screen.getByLabelText(copy.cloudProject), {
      target: { value: "cloud-project" },
    });
    fireEvent.change(screen.getByLabelText(copy.datasets, { exact: false }), {
      target: { value: "sales" },
    });
    fireEvent.click(screen.getByRole("button", { name: copy.save }));
    await screen.findByText(copy.saveFailed);
    assert.equal(
      (screen.getByLabelText(copy.datasets, { exact: false }) as HTMLTextAreaElement).value,
      "sales",
    );
    assert.equal(screen.queryByText(copy.saved), null);
    assert.equal(screen.queryByText(copy.verified), null);
  } finally {
    restore();
  }
});

test("BigQuery renewed consent is bound explicitly without automatically publishing or verifying", async () => {
  const { seen, restore } = stubFetch({
    "GET /api/catalog/connectors/bigquery/setup": () => ({
      ...setup,
      oauth_connectors: [configured.name],
    }),
    "GET /api/catalog": () => ({ connectors: [configured], entries: [] }),
    "POST /api/catalog/connectors/warehouse/reconnect": () => configured,
  });
  try {
    render(<BigQueryConnection canWrite onCancel={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: copy.useGoogleAccount }));
    await screen.findByText(copy.reconnected);
    assert.deepEqual(
      seen.filter((call) => call.method === "POST").map((call) => call.path),
      ["/api/catalog/connectors/warehouse/reconnect"],
    );
    assert.equal(
      (screen.getByRole("button", { name: copy.publish }) as HTMLButtonElement).disabled,
      true,
    );
  } finally {
    restore();
  }
});
