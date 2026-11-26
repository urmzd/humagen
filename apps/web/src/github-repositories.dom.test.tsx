import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { WebSurface } from "@humagen/render-web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { GitHubRepository } from "../../../packages/client/src/generated/types.ts";
import { stubFetch } from "./dom.ts";
import { GitHubRepositories } from "./GitHubRepositories.tsx";
import type { Route } from "./route.ts";

const words = defaultCopy.githubRepos;

afterEach(() => cleanup());

const off = { opened: false, pushes: false };

function repo(name: string, extra: Partial<GitHubRepository> = {}): GitHubRepository {
  return {
    repository: name,
    version: 0,
    installed: true,
    auto_review: off,
    agent: "lead",
    cross_surface_reads: true,
    ...extra,
  };
}

/** A server holding one app's repositories, which follow the app until one is set. */
function server() {
  const repos: Record<string, GitHubRepository> = {
    "acme/api": repo("acme/api"),
    "acme/site": repo("acme/site", {
      version: 2,
      auto_review: { opened: true, pushes: false },
      agent: "reviewer",
      settings: {
        repository: "acme/site",
        auto_review: { opened: true, pushes: false },
        agent: "reviewer",
        updated_at: "2026-11-22T00:00:00Z",
      },
    }),
  };
  const net = stubFetch({
    "GET /api/clients/gh/repositories": () => ({
      repositories: Object.values(repos),
      defaults: { auto_review: off, agent: "lead" },
      listed: true,
    }),
    "GET /api/clients/gh/repositories/acme/api": () => repos["acme/api"],
    "PUT /api/clients/gh/repositories/acme/api": (body) => {
      const b = body as {
        version: number;
        agent: string;
        instructions: string;
        reads: string[];
        cross_surface_reads: boolean;
        auto_review?: { opened: boolean; pushes: boolean };
      };
      if (b.version !== repos["acme/api"]?.version)
        return new Response(
          JSON.stringify({
            error: "these settings changed since they were read; reload and try again",
          }),
          {
            status: 409,
          },
        );
      repos["acme/api"] = repo("acme/api", {
        version: b.version + 1,
        auto_review: b.auto_review ?? off,
        agent: b.agent || "lead",
        cross_surface_reads: b.cross_surface_reads,
        settings: {
          repository: "acme/api",
          auto_review: b.auto_review,
          agent: b.agent || undefined,
          instructions: b.instructions || undefined,
          reads: b.reads as ("pull_diff" | "linked_issues")[],
          cross_surface_reads: b.cross_surface_reads ? undefined : false,
          updated_at: "2026-11-22T00:00:00Z",
        },
      });
      return repos["acme/api"];
    },
    "DELETE /api/clients/gh/repositories/acme/api": () => {
      repos["acme/api"] = repo("acme/api");
      return repos["acme/api"];
    },
    "GET /api/clients": () => ({
      clients: [
        { id: "gh", name: "GitHub", surface: "github", agents: ["*"], default_agent: "lead" },
      ],
      caps: { max: 10, max_per_surface: 10 },
    }),
    "GET /api/agents": () => ({
      agents: [{ spec: { name: "lead" } }, { spec: { name: "reviewer" } }],
    }),
  });
  return { net, repos };
}

test("the repositories list shows what applies in each, and a row opens its settings", async () => {
  const { net } = server();
  const opened: Route[] = [];
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" canWrite onNavigate={(r) => opened.push(r)} />
      </WebSurface>,
    );
    // Loading draws skeleton rows, never bare text.
    assert.ok(screen.getByTestId("skeleton"));
    const table = await screen.findByRole("table", { name: words.title });
    const rows = within(table).getAllByRole("row");
    assert.equal(rows.length, 3, "a header and two repositories");
    const site = rows.find((r) => r.textContent?.includes("acme/site"));
    assert.ok(site?.textContent?.includes(words.custom), "a repository with settings says so");
    assert.ok(site?.textContent?.includes(words.review.opened));
    assert.ok(site?.textContent?.includes("@reviewer"));
    const api = rows.find((r) => r.textContent?.includes("acme/api"));
    assert.ok(api?.textContent?.includes(words.review.off));
    assert.ok(!api?.textContent?.includes(words.custom));
    // The path names the app, so two GitHub apps are told apart.
    assert.ok(await screen.findByRole("button", { name: "GitHub" }));
    const link = within(api as HTMLElement).getByRole("link", { name: "acme/api" });
    assert.equal(link.getAttribute("href"), "/settings/apps/github/gh/repositories/acme/api");
    fireEvent.click(link);
    assert.deepEqual(opened, [{ page: "repositories", client: "gh", repo: "acme/api" }]);
  } finally {
    net.restore();
  }
});

test("an app with no repositories says so with one action", async () => {
  const net = stubFetch({
    "GET /api/clients/gh/repositories": () => ({
      repositories: [],
      defaults: { auto_review: off },
      listed: true,
    }),
  });
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" canWrite onNavigate={() => {}} />
      </WebSurface>,
    );
    await screen.findByText(words.empty);
    assert.ok(screen.getByRole("button", { name: words.checkAgain }));
    assert.equal(screen.queryByRole("table"), null);
  } finally {
    net.restore();
  }
});

test("a repository's settings are saved with the version read, and reset to the app's defaults", async () => {
  const { net, repos } = server();
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" repo="acme/api" canWrite onNavigate={() => {}} />
      </WebSurface>,
    );
    const form = await screen.findByRole("form", { name: "acme/api" });
    assert.ok(await screen.findByRole("button", { name: "GitHub" }));
    const review = within(form).getByLabelText(words.form.review) as HTMLSelectElement;
    // The app's own default is the first choice, and says what it is.
    await waitFor(() =>
      assert.equal(review.options[0]?.textContent, words.follow(words.review.off)),
    );
    fireEvent.change(review, { target: { value: "both" } });
    const agent = within(form).getByLabelText(words.form.agent) as HTMLSelectElement;
    await waitFor(() => assert.ok([...agent.options].some((o) => o.value === "reviewer")));
    fireEvent.change(agent, { target: { value: "reviewer" } });
    fireEvent.change(within(form).getByLabelText(words.form.instructions), {
      target: { value: "Run make check." },
    });
    fireEvent.click(within(form).getByLabelText(words.form.readDiff));
    fireEvent.click(within(form).getByLabelText(new RegExp(`^${words.form.crossSurface}`)));
    fireEvent.click(within(form).getByRole("button", { name: words.form.save }));
    await waitFor(() => assert.equal(repos["acme/api"]?.version, 1));
    const put = net.seen.find((s) => s.method === "PUT");
    assert.deepEqual(put?.body, {
      version: 0,
      auto_review: { opened: true, pushes: true },
      agent: "reviewer",
      instructions: "Run make check.",
      reads: ["pull_diff"],
      cross_surface_reads: false,
    });
    const reset = await within(form).findByRole("button", { name: words.form.reset });
    fireEvent.click(reset);
    await waitFor(() =>
      assert.ok(net.seen.some((s) => s.method === "DELETE" && s.path.endsWith("?version=1"))),
    );
    await waitFor(() =>
      assert.equal(within(form).queryByRole("button", { name: words.form.reset }), null),
    );
    assert.equal((within(form).getByLabelText(words.form.review) as HTMLSelectElement).value, "");
  } finally {
    net.restore();
  }
});

test("a save keeps a repository's approve setting, which the form does not show", async () => {
  const kept = repo("acme/site", {
    version: 3,
    settings: { repository: "acme/site", approve: "never", updated_at: "2026-11-22T00:00:00Z" },
  });
  const net = stubFetch({
    "GET /api/clients/gh/repositories": () => ({
      repositories: [kept],
      defaults: { auto_review: off },
      listed: true,
    }),
    "GET /api/clients/gh/repositories/acme/site": () => kept,
    "PUT /api/clients/gh/repositories/acme/site": () => ({ ...kept, version: 4 }),
    "GET /api/clients": () => ({
      clients: [{ id: "gh", name: "GitHub", surface: "github", agents: ["*"] }],
      caps: { max: 10, max_per_surface: 10 },
    }),
    "GET /api/agents": () => ({ agents: [] }),
  });
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" repo="acme/site" canWrite onNavigate={() => {}} />
      </WebSurface>,
    );
    const form = await screen.findByRole("form", { name: "acme/site" });
    fireEvent.click(within(form).getByRole("button", { name: words.form.save }));
    await waitFor(() => assert.ok(net.seen.some((s) => s.method === "PUT")));
    const put = net.seen.find((s) => s.method === "PUT");
    assert.equal((put?.body as { approve?: string }).approve, "never");
  } finally {
    net.restore();
  }
});

test("a write over a change made since is not lost: the person is told to reload", async () => {
  const { net, repos } = server();
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" repo="acme/api" canWrite onNavigate={() => {}} />
      </WebSurface>,
    );
    const form = await screen.findByRole("form", { name: "acme/api" });
    // Someone else saved the repository after it was read.
    repos["acme/api"] = repo("acme/api", { version: 5 });
    fireEvent.click(within(form).getByRole("button", { name: words.form.save }));
    await screen.findByText(words.form.conflict);
    assert.ok(screen.getByRole("button", { name: words.form.reload }));
  } finally {
    net.restore();
  }
});

test("someone who may not change apps reads a repository's settings without controls", async () => {
  const { net } = server();
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" repo="acme/api" canWrite={false} onNavigate={() => {}} />
      </WebSurface>,
    );
    const form = await screen.findByRole("form", { name: "acme/api" });
    assert.ok(screen.getByText(words.form.readOnly));
    assert.equal(within(form).queryByRole("button", { name: words.form.save }), null);
    assert.ok(form.querySelector("fieldset")?.disabled, "the controls are disabled");
  } finally {
    net.restore();
  }
});

test("a repository another app keeps to its own runs says which, and the list says so too", async () => {
  const kept = repo("acme/infra", { cross_surface_reads: false, kept_by: ["GitHub Two"] });
  const net = stubFetch({
    "GET /api/clients/gh/repositories": () => ({
      repositories: [kept],
      defaults: { auto_review: off },
      listed: true,
    }),
    "GET /api/clients/gh/repositories/acme/infra": () => kept,
    "GET /api/clients": () => ({
      clients: [{ id: "gh", name: "GitHub", surface: "github", agents: ["*"] }],
      caps: { max: 10, max_per_surface: 10 },
    }),
    "GET /api/agents": () => ({ agents: [] }),
  });
  try {
    render(
      <WebSurface>
        <GitHubRepositories client="gh" canWrite onNavigate={() => {}} />
      </WebSurface>,
    );
    const table = await screen.findByRole("table", { name: words.title });
    assert.ok(within(table).getByText(words.ownRuns));
    cleanup();
    render(
      <WebSurface>
        <GitHubRepositories client="gh" repo="acme/infra" canWrite onNavigate={() => {}} />
      </WebSurface>,
    );
    await screen.findByRole("form", { name: "acme/infra" });
    await screen.findByText(words.form.keptBy(["GitHub Two"]));
    // This app's own switch shows its own setting, which is on.
    const own = screen.getByLabelText(
      new RegExp(`^${words.form.crossSurface}`),
    ) as HTMLInputElement;
    assert.equal(own.checked, true);
  } finally {
    net.restore();
  }
});
