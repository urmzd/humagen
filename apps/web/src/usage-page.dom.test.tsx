import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { Group, Summary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { Usage } from "./Usage.tsx";

const words = defaultCopy.usagePage;
const DAY = 86_400_000;
let rendered = false;
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (rendered) cleanup();
  rendered = false;
  net?.restore();
  net = undefined;
});

function group(name: string): Group {
  return {
    name,
    runs: 4,
    completed: 2,
    completed_alone: 1,
    needed_person: 1,
    human_help_unknown: 1,
    failed: 1,
    stopped: 1,
    active: 0,
    avg_ms: 1200,
    handoffs_out: 2,
    last_failure_run: "failed-run",
    last_failure_at: "2026-10-20T12:00:00Z",
    p50_ms: 1000,
    p95_ms: 1400,
    usage: { model_calls: 4, tokens: 90, spend: 100, unpriced_calls: 0, other_currency_runs: 0 },
  };
}
function summary(query: URLSearchParams, agent = "overview-agent"): Summary {
  const since = query.get("since") ?? "2026-10-20T00:00:00.000Z";
  const until = query.get("until") ?? "2026-10-26T12:00:00.000Z";
  const buckets: Summary["buckets"] = [];
  for (let at = Date.parse(since); at < Date.parse(until); at += DAY)
    buckets.push({
      start: new Date(at).toISOString(),
      completed: 2,
      completed_alone: 1,
      needed_person: 1,
      human_help_unknown: 1,
      failed: 1,
      stopped: 1,
      active: 0,
    });
  return {
    since,
    until,
    bucket_ns: DAY * 1_000_000,
    total: group("total"),
    buckets,
    by_agent: [group(agent)],
    by_origin: [],
    slowest: [],
    delegated: 2,
  };
}
function serve(analytics: (query: URLSearchParams) => unknown) {
  net = stubFetch({ "GET /api/analytics": (_, query) => analytics(query) });
  return net;
}
const requests = () =>
  (net?.seen ?? [])
    .filter((request) => request.path.startsWith("/api/analytics?"))
    .map((request) => new URL(request.path, "http://localhost").searchParams);
const chart = () =>
  within(screen.getByRole("group", { name: words.chartLabel })).getAllByRole("button");
const dayQuery = (query: URLSearchParams) =>
  Date.parse(query.get("until") ?? "") - Date.parse(query.get("since") ?? "") <= DAY;

test("Usage requests bounded UTC ranges with 24h buckets and switches chart/table without another query", async () => {
  serve((query) => summary(query));
  rendered = true;
  render(<Usage agents={[]} tick={0} onOpenAgent={() => {}} onOpenRun={() => {}} />);
  await screen.findByRole("button", { name: "overview-agent" });
  for (const days of [14, 7, 30] as const) {
    if (days !== 14) {
      fireEvent.click(screen.getByRole("button", { name: words.days(days) }));
      await waitFor(() => assert.equal(requests().length, days === 7 ? 2 : 3));
      await screen.findByRole("button", { name: "overview-agent" });
    }
    const query = requests().at(-1)!;
    const since = new Date(query.get("since")!);
    const until = new Date(query.get("until")!);
    assert.equal(query.get("bucket"), "24h");
    assert.equal(query.has("window"), false);
    assert.equal(since.getUTCHours() + since.getUTCMinutes() + since.getUTCSeconds(), 0);
    const today = Date.UTC(until.getUTCFullYear(), until.getUTCMonth(), until.getUTCDate());
    assert.equal(since.getTime(), today - (days - 1) * DAY);
    assert.ok(until.getTime() - since.getTime() <= 30 * DAY);
    assert.equal(
      screen.getByRole("button", { name: words.days(days) }).getAttribute("aria-pressed"),
      "true",
    );
  }
  const count = requests().length;
  fireEvent.click(screen.getByRole("button", { name: words.table }));
  assert.equal(screen.queryByRole("group", { name: words.chartLabel }), null);
  assert.ok(screen.getByRole("table", { name: words.chartTitle }));
  assert.ok(
    screen.getAllByRole("button", { name: /4 runs, 1 completed without help, 1 needed a person/ })
      .length > 0,
  );
  fireEvent.click(screen.getByRole("button", { name: words.chart }));
  assert.equal(chart().length, 30);
  assert.equal(requests().length, count);
});

test("Usage keyboard and pointer day selection request exact UTC intervals and open real agent/failure targets", async () => {
  serve((query) =>
    summary(
      query,
      dayQuery(query) ? `daily-${query.get("since")!.slice(0, 10)}` : "overview-agent",
    ),
  );
  const opened: string[] = [];
  rendered = true;
  render(
    <Usage
      agents={[]}
      tick={0}
      onOpenAgent={(name) => opened.push(`agent:${name}`)}
      onOpenRun={(id) => opened.push(`run:${id}`)}
    />,
  );
  await screen.findByRole("button", { name: "overview-agent" });
  const overview = requests()[0]!;
  const first = Date.parse(overview.get("since")!);
  const select = async (index: number, key?: string) => {
    const buttons = chart();
    if (key)
      fireEvent.keyDown(
        document.activeElement === buttons[0] ||
          buttons.includes(document.activeElement as HTMLButtonElement)
          ? document.activeElement!
          : buttons[0]!,
        { key },
      );
    else fireEvent.click(buttons[index]!);
    const start = new Date(first + index * DAY).toISOString();
    const name = `daily-${start.slice(0, 10)}`;
    await screen.findByRole("button", { name });
    const query = requests().at(-1)!;
    assert.equal(query.get("since"), start);
    assert.equal(
      query.get("until"),
      new Date(
        Math.min(first + (index + 1) * DAY, Date.parse(overview.get("until")!)),
      ).toISOString(),
    );
    assert.equal(query.get("bucket"), "24h");
    assert.equal(chart()[index]!.getAttribute("aria-pressed"), "true");
    if (key) assert.ok(document.activeElement === chart()[index]);
    return name;
  };
  chart()[0]!.focus();
  await select(1, "ArrowRight");
  await select(0, "ArrowLeft");
  await select(13, "End");
  await select(0, "Home");
  const name = await select(3);
  fireEvent.click(screen.getByRole("button", { name }));
  fireEvent.click(screen.getByRole("button", { name: `${words.openFailure}: ${name}` }));
  assert.deepEqual(opened, [`agent:${name}`, "run:failed-run"]);
  fireEvent.click(screen.getByRole("button", { name: words.clearDay }));
  await screen.findByRole("button", { name: "overview-agent" });
});

test("Usage load and selected-day failures show retry instead of a false empty result", async () => {
  let rangeFails = true;
  let dailyFails = true;
  serve((query) => {
    if ((dayQuery(query) && dailyFails) || (!dayQuery(query) && rangeFails))
      return new Response(JSON.stringify({ error: "fixture unavailable" }), { status: 503 });
    return summary(query, dayQuery(query) ? "daily-recovered" : "overview-recovered");
  });
  rendered = true;
  render(<Usage agents={[]} tick={0} onOpenAgent={() => {}} onOpenRun={() => {}} />);
  await screen.findByText(words.loadFailed);
  assert.equal(screen.queryByText(words.noData), null);
  rangeFails = false;
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  await screen.findByRole("button", { name: "overview-recovered" });
  fireEvent.click(chart()[0]!);
  await screen.findByText(words.dayFailed);
  assert.equal(screen.queryByRole("button", { name: "overview-recovered" }), null);
  assert.equal(screen.queryByText(words.noData), null);
  dailyFails = false;
  fireEvent.click(screen.getByRole("button", { name: words.retry }));
  await screen.findByRole("button", { name: "daily-recovered" });
  assert.equal(screen.queryByRole("alert"), null);
});

test("Usage ignores stale range and selected-day responses", async () => {
  let releaseRange: ((value: Summary) => void) | undefined;
  let heldRange: URLSearchParams | undefined;
  let releaseDay: ((value: Summary) => void) | undefined;
  let heldDay: URLSearchParams | undefined;
  serve((query) => {
    if (!heldRange) {
      heldRange = new URLSearchParams(query);
      return new Promise<Summary>((resolve) => {
        releaseRange = resolve;
      });
    }
    if (dayQuery(query) && !heldDay) {
      heldDay = new URLSearchParams(query);
      return new Promise<Summary>((resolve) => {
        releaseDay = resolve;
      });
    }
    return summary(query, dayQuery(query) ? "current-day" : "current-range");
  });
  rendered = true;
  render(<Usage agents={[]} tick={0} onOpenAgent={() => {}} onOpenRun={() => {}} />);
  await waitFor(() => assert.ok(releaseRange));
  fireEvent.click(screen.getByRole("button", { name: words.days(7) }));
  await screen.findByRole("button", { name: "current-range" });
  await act(async () => {
    releaseRange!(summary(heldRange!, "stale-range"));
  });
  assert.equal(screen.queryByRole("button", { name: "stale-range" }), null);
  assert.equal(chart().length, 7);
  fireEvent.click(chart()[0]!);
  await waitFor(() => assert.ok(releaseDay));
  fireEvent.click(chart()[1]!);
  await screen.findByRole("button", { name: "current-day" });
  await act(async () => {
    releaseDay!(summary(heldDay!, "stale-day"));
  });
  assert.equal(screen.queryByRole("button", { name: "stale-day" }), null);
  assert.ok(screen.getByRole("button", { name: "current-day" }));
});
