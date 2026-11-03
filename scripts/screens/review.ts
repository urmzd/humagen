/**
 * The review card: an agent on the deterministic local model answers with a
 * structured review (`humagen.github.review.v1`), fenced as models do, through
 * the real run, thread and console path. One screen shows the card, the other
 * Ask Follow-up putting the agent and the finding's prompt in the box.
 */
import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
type Screen = { name: string; path: string; act?: (page: Page) => Promise<void> };

const SHA = "316c56f81540c3eaca02184173df322a48aebdd9";
const blob = (path: string, lines: string) =>
  `https://github.com/urmzd/humagen/blob/${SHA}/${path}#${lines}`;

/** What the review agent is asked; native.ts answers it with REVIEW_ANSWER. */
export const REVIEW_ASK = "Review the fixture pull request urmzd/humagen#212.";

const FOLLOW_UP = "How does the 250ms refresh gap behave under a burst of unknown webhooks?";

export const REVIEW_ANSWER = `\`\`\`json\n${JSON.stringify(
  {
    format: "humagen.github.review.v1",
    summary: `Adds horizontal scaling for clients: webhook clients sit on every replica and Socket Mode apps are leased with Postgres advisory locks. Reviewed https://github.com/urmzd/humagen/pull/212 at ${SHA}.`,
    comments: [
      "Concurrency tests cover the change counter and multi-replica leases.",
      "CI for the reviewed SHA passed; no load test was run.",
    ],
    findings: [
      {
        severity: "error",
        title: "Lease is released before the socket closes",
        suggestion:
          "Close the Socket Mode connection, then release the advisory lock, so two replicas never hold the same app.",
        evidence: [
          {
            observation: "`unlock` runs in a defer registered before `conn.Close`.",
            source: blob("internal/app/slack.go", "L298-L304"),
          },
        ],
        prompt: "Show the order of unlock and close in the lease loop and propose the fix.",
      },
      {
        severity: "warning",
        title: "Refresh gap is fixed at 250ms",
        suggestion: "Make the gap a spec field with 250ms as its default.",
        evidence: [
          {
            observation: "An unknown webhook triggers a sync at most once every 250ms.",
            source: blob("internal/platform/client_sync.go", "L30"),
          },
          {
            observation: "The race test passes with the fixed gap.",
            source: "go test -race ./internal/platform -run TestClientSync",
          },
        ],
        prompt: FOLLOW_UP,
      },
      {
        severity: "info",
        title: "Generation tracking is race-safe",
        suggestion: "Keep the compare-and-set on the generation counter.",
        evidence: [
          {
            observation: "`seen` only advances when it was exactly n-1.",
            source: blob("pkg/clients/generations.go", "L52"),
          },
        ],
        prompt: "Explain why the generation counter cannot skip a change.",
      },
    ],
    related: [
      {
        title: "PostgreSQL advisory locks",
        url: "https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS",
      },
    ],
  },
  null,
  2,
)}\n\`\`\``;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** The conversation, made once, on the first screen that needs it. */
function reviewThread(call: Call): () => Promise<string> {
  let thread: Promise<string> | undefined;
  const make = async (): Promise<string> => {
    await call("POST", "/api/agents", {
      name: "github-reviewer",
      description: "Deterministic local model fixture for the review card",
      model: "screens-native",
      provider: "screens-native",
      system_prompt: "Return the structured review the fixture model gives.",
      tools: ["file_read"],
    });
    const posted = await call<{ thread: { id: string }; runs: { id: string }[] }>(
      "POST",
      "/api/threads",
      { project: "Launch", visibility: "private", text: `@github-reviewer ${REVIEW_ASK}` },
    );
    assert.equal(posted.runs.length, 1);
    const id = posted.runs[0]?.id ?? "";
    for (let i = 0; i < 100; i++) {
      const r = await call<{ status: string }>("GET", `/api/runs/${id}`);
      if (r.status === "completed") return posted.thread.id;
      if (r.status === "failed" || r.status === "stopped")
        throw new Error(`review run ${r.status}`);
      await sleep(100);
    }
    throw new Error("the review run did not end");
  };
  return () => {
    thread ??= make();
    return thread;
  };
}

/** Scrolls the card's top under the page's floating controls, where a reader starts it. */
const toCard = (page: Page) =>
  card(page).evaluate((el) => {
    el.scrollIntoView({ block: "start" });
    let box = el.parentElement;
    while (box && !/(auto|scroll)/.test(getComputedStyle(box).overflowY)) box = box.parentElement;
    box?.scrollBy(0, -96);
  });

const card = (page: Page) =>
  page.getByRole("region", { name: defaultCopy.reviewReport.title, exact: true }).last();

export function reviewScreens(call: Call): Screen[] {
  const thread = reviewThread(call);
  const open = async (page: Page) => {
    await page.goto(new URL(`/conversations/${await thread()}`, page.url()).href);
    await card(page).waitFor();
    assert.equal(await page.getByText('"format"').count(), 0, "the review is a card, not JSON");
  };
  return [
    {
      name: "conversation-review-card",
      path: "/",
      act: async (page) => {
        await open(page);
        const counts = card(page).getByRole("list", { name: defaultCopy.reviewReport.severity });
        assert.deepEqual(await counts.getByRole("listitem").allInnerTexts(), [
          "1 error",
          "1 warning",
          "1 info",
        ]);
        await card(page).getByRole("button", { name: SHOW_LEASE }).waitFor();
        await toCard(page);
      },
    },
    {
      name: "conversation-review-follow-up",
      path: "/",
      act: async (page) => {
        await open(page);
        await card(page)
          .getByRole("button", {
            name: defaultCopy.reviewReport.followUpLabel("Refresh gap is fixed at 250ms"),
          })
          .click();
        const box = page.locator("form[aria-label='Message'] textarea");
        await page.waitForFunction(
          (want) =>
            (
              document.querySelector("form[aria-label='Message'] textarea") as HTMLTextAreaElement
            )?.value.startsWith(want),
          `@github-reviewer ${FOLLOW_UP}`,
        );
        assert.equal(await box.inputValue(), `@github-reviewer ${FOLLOW_UP} `);
      },
    },
    {
      // The fixture's repository is not one a screens workspace has a GitHub
      // client for, so the source route's answer is given here: the lines a
      // client would read at that commit. The card, the request it makes
      // and how it draws the answer are the console's own.
      name: "conversation-review-source",
      path: "/",
      act: async (page) => {
        await page.route("**/api/github/source?*", (route) => {
          const q = new URL(route.request().url()).searchParams;
          assert.equal(q.get("sha"), SHA);
          assert.equal(q.get("file"), "internal/app/slack.go");
          const first = Number(q.get("start")) - 3;
          route.fulfill({
            contentType: "application/json",
            body: JSON.stringify({
              repo: q.get("repo"),
              sha: SHA,
              path: q.get("file"),
              first,
              start: Number(q.get("start")),
              end: Number(q.get("end")),
              total: 420,
              lines: LEASE_LINES,
              url: blob("internal/app/slack.go", "L298-L304"),
            }),
          });
        });
        await open(page);
        const chip = card(page).getByRole("button", { name: SHOW_LEASE });
        await chip.click();
        const excerpt = page.locator("[data-aui='source-excerpt']");
        await excerpt.getByText("unlock()").first().waitFor();
        assert.equal(await excerpt.locator("tr[data-cited]").count(), 7);
        await excerpt.evaluate((el) => el.scrollIntoView({ block: "center" }));
      },
    },
  ];
}

const SHOW_LEASE = `${defaultCopy.reviewReport.source.show} internal/app/slack.go:298-304`;

/** internal/app/slack.go:295-307 as the fixture review cites it. */
const LEASE_LINES = [
  "func (l *lease) hold(ctx context.Context, app string) error {",
  "\tconn, err := l.dial(ctx, app)",
  "\tif err != nil {",
  "\t\treturn err",
  "\t}",
  "\tunlock, err := l.lock(ctx, app)",
  "\tif err != nil {",
  "\t\treturn err",
  "\t}",
  "\tdefer unlock() // released before the socket closes",
  "\tdefer conn.Close()",
  "\treturn conn.Serve(ctx)",
  "}",
];
