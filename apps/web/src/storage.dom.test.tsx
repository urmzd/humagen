import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within as inside } from "@testing-library/react";
import { Storage } from "./Storage.tsx";
import { memoryStorage, sampleFiles, sampleUse } from "./storage.fixtures.ts";
import type { Stored, StorageTransport } from "./storage.ts";

afterEach(cleanup);

const noop = () => {};
const page = (via: StorageTransport, canDelete = true) =>
  render(<Storage signal={0} agents={[]} projects={[]} canDelete={canDelete} onOpenRun={noop} onOpenThread={noop} via={via} />);
const titles = () => screen.queryAllByTestId("stored-row").map((r) => r.querySelector("span span")?.textContent);

test("delete asks first: Keep It changes nothing, Delete Version removes the file and its bytes from the totals", async () => {
  const via = memoryStorage();
  let removed: string[] = [];
  const counted: StorageTransport = { ...via, remove: (id) => ((removed = [...removed, id]), via.remove(id)) };
  page(counted);
  fireEvent.click(await screen.findByText("launch-plan.md"));
  const detail = await screen.findByTestId("stored-detail");

  fireEvent.click(inside(detail).getByRole("button", { name: "Delete" }));
  assert.ok(inside(detail).getByText("Delete version 3 of this artifact? This cannot be undone."));
  fireEvent.click(inside(detail).getByRole("button", { name: "Keep It" }));
  assert.ok(!inside(detail).queryByText(/cannot be undone/));
  assert.deepEqual(removed, []);

  fireEvent.click(inside(detail).getByRole("button", { name: "Delete" }));
  fireEvent.click(inside(detail).getByRole("button", { name: "Delete Version" }));
  await waitFor(() => assert.ok(!screen.queryByTestId("stored-detail")));
  assert.deepEqual(removed, ["a1"]);
  await waitFor(() => assert.ok(!titles().includes("launch-plan.md")));
  assert.equal(titles().length, sampleFiles().length - 1);
  await waitFor(() => assert.match(screen.getByTestId("storage-use").textContent ?? "", /3 artifacts/));
});

test("a refused delete keeps the file and says why", async () => {
  const via = memoryStorage();
  page({ ...via, remove: async () => Promise.reject(new Error("You may not delete files.")) });
  fireEvent.click(await screen.findByText("refunds-q3.xlsx"));
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  fireEvent.click(screen.getByRole("button", { name: "Delete Version" }));
  assert.equal((await screen.findByRole("alert")).textContent, "The artifact could not be deleted. You may not delete files.");
  assert.ok(screen.getByTestId("stored-detail"));
  assert.ok(titles().includes("refunds-q3.xlsx"));
});

test("without the right to delete there is no Delete", async () => {
  page(memoryStorage(), false);
  fireEvent.click(await screen.findByText("launch-plan.md"));
  await screen.findByTestId("stored-detail");
  assert.ok(!screen.queryByRole("button", { name: "Delete" }));
});

test("See more loads the next page from the cursor until there is none", async () => {
  const many: Stored[] = Array.from({ length: 65 }, (_, i) => ({
    ...sampleFiles()[0],
    id: `f${i}`,
    title: `f${String(i).padStart(2, "0")}.md`,
    created_at: new Date(Date.UTC(2026, 0, 1, 0, i)).toISOString(),
  })) as Stored[];
  const via = memoryStorage(many, sampleUse());
  const asked: (string | undefined)[] = [];
  page({ ...via, list: (q) => (asked.push(q.before), via.list(q)) });
  await waitFor(() => assert.equal(titles().length, 30));
  assert.equal(titles()[0], "f64.md");

  fireEvent.click(screen.getByRole("button", { name: "See more" }));
  await waitFor(() => assert.equal(titles().length, 60));
  fireEvent.click(screen.getByRole("button", { name: "See more" }));
  await waitFor(() => assert.equal(titles().length, 65));
  assert.ok(!screen.queryByRole("button", { name: "See more" }));
  assert.deepEqual(asked, [undefined, "f35", "f5"]);
  assert.equal(new Set(titles()).size, 65, "no file is shown twice");
  assert.equal(titles()[64], "f00.md");
});

test("a failed page offers to try again, and trying again loads it", async () => {
  const many: Stored[] = Array.from({ length: 40 }, (_, i) => ({ ...sampleFiles()[0], id: `g${i}`, title: `g${i}.md`, created_at: new Date(Date.UTC(2026, 0, 1, 0, i)).toISOString() })) as Stored[];
  const via = memoryStorage(many, sampleUse());
  let fail = true;
  page({ ...via, list: (q) => (q.before && fail ? ((fail = false), Promise.reject(new Error("down"))) : via.list(q)) });
  await waitFor(() => assert.equal(titles().length, 30));
  fireEvent.click(screen.getByRole("button", { name: "See more" }));
  fireEvent.click(await screen.findByRole("button", { name: "Could not load. Try again" }));
  await waitFor(() => assert.equal(titles().length, 40));
});

test("over its limit, the page says agents cannot publish", async () => {
  page(memoryStorage(sampleFiles(), sampleUse({ limit_bytes: 1_000 })));
  assert.match((await screen.findByRole("alert")).textContent ?? "", /over its limit/);
});

test("a selected artifact keeps Open in Workspace, Download and Close available together", async () => {
  const opened:Stored[]=[];
  render(<Storage signal={0} agents={[]} projects={[]} canDelete onOpenRun={noop} onOpenThread={noop} onOpenArtifact={file=>opened.push(file)} via={memoryStorage()} />);
  fireEvent.click(await screen.findByText("launch-plan.md"));
  const detail=await screen.findByTestId("stored-detail");
  const actions=detail.querySelector("header");
  assert.ok(actions);
  fireEvent.click(inside(actions).getByRole("button",{name:"Open in Workspace"}));
  assert.equal(opened.length,1);
  assert.equal(opened[0]?.title,"launch-plan.md");
  assert.ok(inside(actions).getByRole("link",{name:"Download"}).getAttribute("href")?.includes(opened[0]!.id));
  fireEvent.click(inside(actions).getByRole("button",{name:"Close"}));
  await waitFor(()=>assert.ok(!screen.queryByTestId("stored-detail")));
  assert.ok(screen.getByText("launch-plan.md"));
});
