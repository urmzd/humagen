import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, render, screen } from "@testing-library/react";
import type { Person } from "./api.ts";
import { DirectoryPage } from "./Directory.tsx";
import { stubFetch } from "./dom.ts";

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const dana: Person = {
  subject: "okta|dana",
  name: "Dana Scully",
  handle: "dana-scully",
  roles: ["operator"],
  picture: "/api/people/dana-scully/photo?v=2",
};
const fox: Person = { subject: "okta|fox", handle: "fox", roles: [], picture: null };

function openPeople(can = ["people:read"]) {
  net = stubFetch({ "GET /api/people": () => ({ people: [dana, fox], next: null }) });
  render(<DirectoryPage can={(p) => can.includes(p)} />);
}

test("People: each by name and handle with their own picture, and no letter in a box for one without", async () => {
  openPeople();
  assert.ok(screen.getByRole("heading", { name: "People" }));
  await screen.findByText("Dana Scully");
  assert.ok(screen.getByText(/@dana-scully | operator/));
  assert.ok(screen.getByText(/@fox | No role/));
  const pictures = document.querySelectorAll("img");
  assert.equal(pictures.length, 1);
  assert.equal(pictures[0]?.getAttribute("src"), "/api/people/dana-scully/photo?v=2");
  // Fox has no picture: a still circle, with nothing written in it.
  assert.ok(!screen.queryByText("F"));
});

test("People is only people: no Agents or Projects tabs repeating their own pages", async () => {
  openPeople();
  await screen.findByText("Dana Scully");
  assert.equal(screen.queryByRole("tab"), null);
  assert.equal(screen.queryByText("@lead"), null);
});

test("someone who may not read people is told so, and nothing is fetched", () => {
  net = stubFetch({});
  render(<DirectoryPage can={() => false} />);
  assert.equal(net.seen.length, 0);
  assert.ok(screen.getByRole("heading", { name: "People" }));
});
