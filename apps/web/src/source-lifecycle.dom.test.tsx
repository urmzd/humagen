import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { Source } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { SourceLifecycle } from "./SourceLifecycle.tsx";

const at = "2026-10-01T09:00:00Z";
const source: Source = {
  name: "runbooks",
  documents: 1,
  passages: 1,
  excluded: 0,
  variants: [],
  updated_at: at,
  lifecycle: {
    mode: "indexed",
    provider: "drive",
    origin: "folder1",
    owner: "ana",
    status: "needs_attention",
    cause: "auth_expired",
    last_good_at: at,
    connected_at: at,
    stop_after_seconds: 86400,
    attempts: [
      { id: "attempt1", started_at: at, finished_at: at, status: "failed", cause: "auth_expired" },
    ],
  },
};
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net?.restore();
  net = undefined;
});

test("saving a source reference does not claim verified provider access", async () => {
  let changed = 0;
  net = stubFetch({
    "GET /api/knowledge/runbooks/dependencies": () => ({ agents: [], projects: [] }),
    "PUT /api/knowledge/runbooks/credential": (body) => {
      assert.deepEqual(body, { ref: "op://People/ana/drive" });
      return source;
    },
  });
  render(<SourceLifecycle source={source} canWrite={true} onChanged={() => changed++} />);
  await screen.findByText("No visible declarations or recorded uses in this window");
  fireEvent.change(screen.getByLabelText("Your credential reference"), {
    target: { value: "op://People/ana/drive" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Reference" }));
  await screen.findByText("Your reference is saved. Refresh or search to verify access.");
  assert.equal(changed, 1);
  assert.ok(screen.getByText("Needs attention"));
  assert.equal(screen.queryByText("In sync"), null);
  assert.equal(
    net!.seen.some((request) => request.path.endsWith("/sync")),
    false,
  );
});

test("source refresh shows pending and failure states without discarding the last copy", async () => {
  let fail!: (value: Response) => void;
  net = stubFetch({
    "GET /api/knowledge/runbooks/dependencies": () => ({ agents: [], projects: [] }),
    "POST /api/knowledge/runbooks/sync": () =>
      new Promise<Response>((resolve) => {
        fail = resolve;
      }),
  });
  render(<SourceLifecycle source={source} canWrite={true} onChanged={() => undefined} />);
  fireEvent.click(screen.getByRole("button", { name: "Refresh Copy" }));
  await screen.findByText("Request in progress");
  assert.equal(
    (screen.getByRole("button", { name: "Refresh Copy" }) as HTMLButtonElement).disabled,
    true,
  );
  fail(new Response(JSON.stringify({ error: "The source is not available." }), { status: 503 }));
  await screen.findByText("The source is not available.");
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: "Refresh Copy" }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  assert.ok(screen.getByText(/^Last good copy:/));
  assert.ok(screen.getByText("Needs attention"));
});

test("source readers cannot configure or refresh workspace copies", async () => {
  net = stubFetch({
    "GET /api/knowledge/runbooks/dependencies": () => ({
      agents: ["helper"],
      projects: ["Launch"],
    }),
  });
  render(<SourceLifecycle source={source} canWrite={false} onChanged={() => undefined} />);
  assert.equal(screen.queryByText("Configure Source"), null);
  assert.equal(screen.queryByRole("button", { name: "Refresh Copy" }), null);
  assert.equal(
    (await screen.findByRole("link", { name: "Agents: helper" })).getAttribute("href"),
    "/agents/helper",
  );
});

test("an available manual source does not claim its retained documents are missing", async () => {
  net = stubFetch({
    "GET /api/knowledge/runbooks/dependencies": () => ({ agents: [], projects: [] }),
  });
  render(<SourceLifecycle source={{ ...source, lifecycle: undefined }} canWrite={false} onChanged={() => undefined} />);
  await screen.findByText("No visible declarations or recorded uses in this window");
  assert.equal(screen.queryByText("No successful copy yet") === null, true);
  assert.ok(screen.getByText("Manually added documents; no external refresh"));
});

test("Used By links recorded conversations and publishing runs without inventing skill or memory dependencies", async () => {
  net = stubFetch({
    "GET /api/knowledge/runbooks/dependencies": () => ({
      agents: [], projects: [],
      conversations: [{ id: "th-visible", title: "Launch planning", relation: "retrieved_in" }],
      deliverables: [{ id: "art-visible", title: "Launch brief.md", relation: "published_by_retrieving_run" }],
      unsupported: ["skills", "memory"],
    }),
  });
  render(<SourceLifecycle source={source} canWrite={false} onChanged={() => undefined} />);
  assert.equal((await screen.findByRole("link", { name: "Conversations: Launch planning" })).getAttribute("href"), "/conversations/th-visible");
  assert.equal(screen.getByRole("link", { name: "Deliverables: Launch brief.md" }).getAttribute("href"), "/artifacts/art-visible");
  assert.ok(screen.getByText("Published by a run that retrieved this source. Content dependency is not inferred."));
  assert.ok(screen.getByText(/Skill-to-source relationships are not recorded/));
  assert.ok(screen.getByText(/Memory-to-source relationships are not recorded/));
});

test("Used By renders authoritative adapter skill and memory links with origin-run details omitted", async () => {
  net = stubFetch({
    "GET /api/knowledge/runbooks/dependencies": () => ({
      agents: [], projects: [], conversations: [], deliverables: [], unsupported: [],
      skills: [{ id: "skill", title: "Release Review", relation: "recorded_origin_run", path: "/library/skills/skill" }],
      memory: [{ id: "memory", title: "Supplier note", relation: "recorded_origin_run", path: "/projects/Launch/context" }],
    }),
  });
  render(<SourceLifecycle source={source} canWrite={false} onChanged={() => undefined} />);
  assert.equal((await screen.findByRole("link", { name: "Skills: Release Review" })).getAttribute("href"), "/library/skills/skill");
  assert.equal(screen.getByRole("link", { name: "Memory: Supplier note" }).getAttribute("href"), "/projects/Launch/context");
  assert.equal(screen.queryByText(/Skill-to-source relationships are not recorded/) === null, true);
});
