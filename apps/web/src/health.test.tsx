import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { HealthComponent } from "../../../packages/client/src/generated/types.ts";
import { grouped, HealthView, uptime } from "./Health.tsx";

const at = (i: number) => new Date(Date.UTC(2026, 9, 29, 12, 0, i)).toISOString();
const comp = (over: Partial<HealthComponent>): HealthComponent => ({
  id: "api",
  name: "API",
  group: "Platform",
  status: "operational",
  latency_ms: 0,
  history: [],
  ...over,
});

test("uptime counts every check that was not down, and says nothing before the first", () => {
  assert.equal(uptime({ history: [] }), null);
  assert.equal(
    uptime({
      history: [
        { at: at(0), status: "operational" },
        { at: at(1), status: "degraded" },
        { at: at(2), status: "down" },
      ],
    }),
    66,
  );
});

test("components keep the server's order inside each group", () => {
  const list = [
    comp({ id: "api" }),
    comp({ id: "provider:anthropic", group: "Model providers" }),
    comp({ id: "bus", name: "Event bus" }),
  ];
  assert.deepEqual(
    grouped(list).map(([g, cs]) => [g, cs.map((c) => c.id)]),
    [
      ["Platform", ["api", "bus"]],
      ["Model providers", ["provider:anthropic"]],
    ],
  );
});

test("a deployment with no model provider reads as an outage", () => {
  const html = renderToStaticMarkup(
    <HealthView
      report={{
        status: "down",
        checked_at: at(0),
        components: [
          comp({ history: [{ at: at(0), status: "operational" }] }),
          comp({
            id: "provider:none",
            name: "Model providers",
            group: "Model providers",
            status: "down",
            detail: "None set up, so agents cannot answer.",
            history: [{ at: at(0), status: "down" }],
          }),
        ],
      }}
    />,
  );
  assert.match(html, /Some systems are down/);
  assert.match(html, /None set up, so agents cannot answer\./);
  assert.match(html, /0% working over the last 1 check/);
});
