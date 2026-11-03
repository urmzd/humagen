import assert from "node:assert/strict";
import { test } from "node:test";
import { WebSurface } from "@humagen/render-web";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { GitHubPicker, GitHubTab } from "./GitHubTab.tsx";

test("pull tab shows CI and pinned findings, and issue tab shows labels and comments", async () => {
  const subject = {
    number: 12,
    title: "Repair lease",
    state: "open",
    body: "**Evidence**",
    html_url: "https://github.com/acme/api/pull/12",
  };
  const net = stubFetch({
    "GET /api/github/pull": () => ({
      subject,
      files: [
        { filename: "a.go", status: "modified", patch: "@@ -1 +1 @@\n-old\n+new", blob_url: "" },
      ],
      comments: [],
      workflows: [
        { id: 1, name: "Tests", status: "completed", conclusion: "failure", html_url: "" },
      ],
    }),
    "GET /api/github/issue": () => ({
      subject,
      labels: ["bug"],
      comments: [{ id: 1, body: "Regression confirmed", user: { login: "ada" }, html_url: "" }],
    }),
  });
  try {
    render(
      <WebSurface>
        <GitHubTab
          tab={{ kind: "review", ref: "acme/api#12" }}
          review={JSON.stringify({
            format: "humagen.github.review.v1",
            findings: [
              {
                severity: "error",
                title: "Lost lease",
                evidence: [{ source: "a.go:1", observation: "Close first" }],
              },
            ],
          })}
        />
      </WebSurface>,
    );
    await screen.findByText("Repair lease #12");
    assert.ok(screen.getByText("Tests"));
    assert.ok(screen.getByText(/failure/));
    fireEvent.click(screen.getByRole("button", { name: /Lost lease/ }));
    await waitFor(() => assert.ok(document.querySelector('[data-slot="review-pin"]')));
    cleanup();
    render(
      <WebSurface>
        <GitHubTab tab={{ kind: "issue", ref: "acme/api#12" }} />
      </WebSurface>,
    );
    await screen.findByText("bug");
    assert.ok(screen.getByText("Regression confirmed"));
  } finally {
    cleanup();
    net.restore();
  }
});
test("GitHub picker requires a repository for a number", () => {
  let opened = "";
  render(
    <GitHubPicker
      close={() => {}}
      onOpen={(t) => {
        opened = t.ref ?? "";
      }}
    />,
  );
  try {
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "acme/api" } });
    fireEvent.change(screen.getByLabelText("GitHub URL or #number"), { target: { value: "#12" } });
    fireEvent.click(screen.getByRole("button", { name: "Open GitHub Tab" }));
    assert.equal(opened, "acme/api#12");
  } finally {
    cleanup();
  }
});

test("Open Review in a conversation room opens its pull evidence pane", async () => {
  const { ThreadRoom } = await import("./Room.tsx");
  const { reviewReportJSON } = await import("@humagen/core/fixtures");
  const { stubEventSource } = await import("./dom.ts");
  const report = JSON.parse(reviewReportJSON);
  report.summary += " https://github.com/acme/api/pull/12";
  const at = "2026-11-01T00:00:00Z";
  const initial = {
    thread: {
      id: "th-review",
      project: "",
      channel: "",
      visibility: "private",
      participants: [],
      version: 1,
      created_by: "ada",
      created_at: at,
      updated_at: at,
    },
    turns: [
      {
        kind: "run",
        run: {
          id: "review-run",
          agent: "reviewer",
          thread: "th-review",
          status: "completed",
          answer: JSON.stringify(report),
          text: "Review",
          created_at: at,
          updated_at: at,
        },
      },
    ],
    next: null,
    answers: { agent: "reviewer", from: "project" },
  } as unknown as import("./api.ts").ThreadPage;
  const net = stubFetch({
    "GET /api/github/pull": () => ({
      subject: {
        title: "Review evidence",
        number: 12,
        state: "open",
        body: "",
        html_url: "https://github.com/acme/api/pull/12",
      },
      files: [],
      comments: [],
      workflows: [],
    }),
    "GET /api/threads/th-review": () => initial,
  });
  const restore = stubEventSource();
  try {
    render(
      <ThreadRoom
        id="th-review"
        me="ada"
        initial={initial}
        tick={0}
        agents={[]}
        projects={[]}
        root={null}
        workspaceName="Acme"
        canRun={false}
        onOpenChannel={() => {}}
        onChanged={() => {}}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Open Review" }));
    await screen.findByRole("heading", { name: "Review evidence #12" });
    assert.ok(screen.getByText("No workflow runs reported for this head."));
  } finally {
    cleanup();
    restore();
    net.restore();
  }
});
