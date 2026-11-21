import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import type { HomeNotification, HomeUpdate } from "@humagen/core";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ProjectView } from "./api.ts";
import { FindHelp } from "./FindHelp.tsx";
import { HomeBoxes, HomePage, HomeProjects } from "./HomePage.tsx";
import type { Route } from "./route.ts";

// The loader runs every DOM file in one process, so this cleans up only after its own tests.
let rendered = false;
afterEach(() => {
  if (!rendered) return;
  cleanup();
  rendered = false;
});

const failed: HomeNotification = {
  kind: "inbox",
  id: "m-5",
  from: "any-run-fails",
  summary: "triage failed",
  at: "2026-10-21T11:00:00Z",
};
const posted: HomeUpdate = {
  id: "m-4",
  project: "Launch",
  projectTitle: "Launch",
  subject: "Launch moves to November 4",
  from: { kind: "person", name: "ana" },
  at: "2026-10-21T10:00:00Z",
  unread: true,
};

test("Home's rows open what they name, and each box's text control goes to the rest in the inbox", () => {
  const did: string[] = [];
  rendered = true;
  render(
    <HomeBoxes
      notifications={{ state: "ready", value: { items: [failed], inbox: { total: 3 } } }}
      updates={{ state: "ready", value: [posted] }}
      onOpen={(n) => did.push(`open ${n.id}`)}
      onInbox={() => did.push("inbox")}
      onUpdate={(u) => did.push(`update ${u.id}`)}
      onUpdates={() => did.push("updates")}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: /triage failed/ }));
  fireEvent.click(screen.getByRole("button", { name: /Launch moves to November 4/ }));
  fireEvent.click(screen.getByRole("button", { name: "Open Notifications" }));
  fireEvent.click(screen.getByRole("button", { name: "See All" }));
  assert.deepEqual(did, ["open m-5", "update m-4", "inbox", "updates"]);
  // Each box is a region named by its heading.
  assert.ok(screen.getByRole("region", { name: /Needs you/ }));
  assert.ok(screen.getByRole("region", { name: "What's new" }));
});

const project = (name: string, title: string, day: number, personal_owner?: string) =>
  ({
    name,
    title,
    kind: "team",
    created_at: `2026-11-${day}T00:00:00Z`,
    personal_owner,
  }) as ProjectView;

test("Home opens the three highest-ranked readable projects", () => {
  rendered = true;
  const opened: Route[] = [];
  render(
    <HomeProjects
      projects={[
        project("", "Workspace", 24),
        project("~", "Workspace alias", 24),
        project("personal-dev", "Personal", 24),
        project("personal-peer", "Other Personal", 24, "peer"),
        project("docs", "Docs refresh", 11),
        project("launch", "Launch", 20),
        project("support", "Support", 21),
        project("older", "Backlog", 19),
      ]}
      pinned={["docs"]}
      personal="personal-dev"
      go={(route) => opened.push(route)}
    />,
  );

  const section = screen.getByRole("region", { name: "Projects" });
  const links = Array.from(section.querySelectorAll("a"));
  assert.deepEqual(
    links.map((link) => link.textContent),
    ["Docs refresh", "Support", "Launch"],
  );
  const launch = screen.getByRole("link", { name: "Launch" });
  assert.equal(launch.getAttribute("href"), "/projects/launch");
  fireEvent.click(launch);
  assert.deepEqual(opened, [{ page: "project", name: "launch" }]);
});

test("Home omits the Projects section while awaiting readable work projects", () => {
  rendered = true;
  const go = () => {};
  const { rerender } = render(<HomeProjects projects={null} go={go} />);
  assert.equal(screen.queryByRole("region", { name: "Projects" }), null);
  rerender(<HomeProjects projects={[]} go={go} />);
  assert.equal(screen.queryByRole("region", { name: "Projects" }), null);
  rerender(<HomeProjects projects={[project("automatic", "Personal", 20, "you")]} go={go} />);
  assert.equal(screen.queryByRole("region", { name: "Projects" }), null);
});

test("announcement actions use the retained inbox callbacks", () => {
  rendered = true;
  const did: string[] = [];
  render(
    <HomePage
      greeting="Hello"
      canRun
      hasAgents
      banner={{
        id: "m-1",
        source: "Company",
        title: "A retained announcement",
        text: "Saved copy",
        unread: true,
        onRead: () => did.push("read"),
        onHide: () => did.push("hide"),
        onOpen: () => did.push("open"),
      }}
    >
      <form aria-label="Message" />
    </HomePage>,
  );
  const hide = screen.getByRole("button", { name: "Hide for Me" });
  assert.ok(hide.querySelector("svg"));
  fireEvent.click(hide);
  fireEvent.click(screen.getByRole("button", { name: "Open Announcement" }));
  assert.deepEqual(did, ["hide", "open"]);
  assert.equal(screen.queryByRole("button", { name: "Mark as Read" }), null);
});

test("a pending announcement action prevents hiding again", () => {
  rendered = true;
  let hidden = false;
  render(
    <HomePage
      greeting="Hello"
      canRun
      hasAgents
      banner={{
        id: "m-1",
        source: "Company",
        title: "A retained announcement",
        text: "Saved copy",
        unread: true,
        busy: true,
        onRead: () => {},
        onHide: () => {
          hidden = true;
        },
      }}
    >
      <form aria-label="Message" />
    </HomePage>,
  );
  const hide = screen.getByRole("button", { name: "Hide for Me" }) as HTMLButtonElement;
  assert.equal(hide.disabled, true);
  fireEvent.click(hide);
  assert.equal(hidden, false);
});

test("agent and project discovery searches without starting work and choosing is explicit", async () => {
  rendered = true;
  const c = defaultCopy.home.findHelp;
  const did: string[] = [];
  const result = {
    project: "Field",
    title: "Field engineering",
    agent: "helper",
    reason: "Matches your description",
  };
  render(
    <FindHelp
      onSearch={async (query) => {
        did.push(`search:${query}`);
        return [result];
      }}
      onChoose={(match) => did.push(`choose:${match.project}:${match.agent}`)}
    />,
  );
  assert.equal(
    document.querySelector("form"),
    null,
    "the picker content must not introduce another form",
  );
  fireEvent.change(screen.getByLabelText(c.label), { target: { value: "renewal" } });
  fireEvent.click(screen.getByRole("button", { name: c.search }));
  await screen.findByText("Field engineering");
  assert.deepEqual(did, ["search:renewal"]);
  fireEvent.click(screen.getByRole("button", { name: c.choose }));
  assert.deepEqual(did, ["search:renewal", "choose:Field:helper"]);
});

test("agent and project discovery exposes a real failed request as a retryable error", async () => {
  rendered = true;
  const c = defaultCopy.home.findHelp;
  render(
    <FindHelp
      onSearch={async () => {
        throw new Error("failed");
      }}
      onChoose={() => {}}
    />,
  );
  fireEvent.change(screen.getByLabelText(c.label), { target: { value: "renewal" } });
  fireEvent.click(screen.getByRole("button", { name: c.search }));
  assert.match((await screen.findByRole("alert")).textContent ?? "", /Try again/);
});
