import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
type Screen = {
  name: string;
  path: string;
  act?: (page: Page) => Promise<void>;
  after?: () => Promise<void>;
};
/** Canonical workspace states use the real isolated screens API and production host. */
export function workspaceScreens(call: Call): Screen[] {
  const setup = defaultCopy.agentSetup;
  const heading = (page: Page, title: string) =>
    page.getByRole("heading", { name: title, exact: true }).first().waitFor();
  const prepare = async (page: Page) => {
    await heading(page, setup.title);
    const { providers } = await call<{ providers: { name: string; declared: boolean }[] }>(
      "GET",
      "/api/model-providers",
    );
    const { agents } = await call<{ agents: { spec: { provider: string; model: string } }[] }>(
      "GET",
      "/api/agents",
    );
    const real = agents.find(
      (agent) =>
        agent.spec.provider !== "offline" &&
        providers.some((provider) => provider.declared && provider.name === agent.spec.provider),
    );
    assert.ok(real, "Screens must seed an actual registered model adapter for wizard states");
    await page.getByLabel(setup.name, { exact: false }).fill("screen-reviewed-agent");
    await page.getByLabel(setup.provider, { exact: false }).selectOption(real.spec.provider);
    await page.getByLabel(setup.model, { exact: true }).fill(real.spec.model);
  };
  let priorControl:
    | { paused: boolean; version: number; manager?: string; stop_rules?: string }
    | undefined;
  const nativeFormat = (format: "docx" | "xlsx"): Screen => ({
    name: `native-${format}-production`,
    path: "/",
    act: async (page) => {
      const posted = await call<{ thread: { id: string }; runs: { id: string }[] }>(
        "POST",
        "/api/threads",
        {
          project: "Launch",
          visibility: "private",
          text:
            format === "docx"
              ? "@native-writer Create a native DOCX evidence table."
              : "@native-writer Create a native XLSX owner sheet.",
        },
      );
      assert.equal(posted.runs.length, 1);
      const run = posted.runs[0];
      assert.ok(run);
      for (let attempt = 0; attempt < 100; attempt++) {
        const record = await call<{ status: string }>("GET", `/api/runs/${run.id}`);
        if (record.status === "completed") break;
        assert.ok(
          record.status !== "failed" && record.status !== "stopped",
          `Native ${format} production failed`,
        );
        assert.ok(attempt < 99, "Native production did not finish");
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      const { artifacts } = await call<{
        artifacts: { id: string; title: string; version: number }[];
      }>(
        "GET",
        `/api/artifacts?${new URLSearchParams({ thread: posted.thread.id, limit: "100" })}`,
      );
      const file = artifacts.find((item) => item.title.endsWith(`.${format}`));
      assert.ok(file, "Actual native tool output must be retained");
      assert.equal(file.version, 1);
      await page.goto(
        new URL(`/conversations/${posted.thread.id}/deliverables/${file.id}`, page.url()).href,
      );
      await page.getByRole("cell", { name: "Developer", exact: true }).waitFor();
      const original = page.waitForEvent("download");
      await page.getByRole("link", { name: "Download Original", exact: true }).click();
      const office = await original;
      const officePath = await office.path();
      assert.ok(officePath);
      assert.equal(readFileSync(officePath).subarray(0, 2).toString(), "PK");
      const download = page.waitForEvent("download");
      await page.getByRole("button", { name: "Download PDF", exact: true }).click();
      const pdf = await download;
      const pdfPath = await pdf.path();
      assert.ok(pdfPath);
      assert.equal(readFileSync(pdfPath).subarray(0, 5).toString(), "%PDF-");
      const originalVersion = await call<{ sha256: string }>("GET", `/api/artifacts/${file.id}`);
      await page.getByRole("button", { name: "Request Changes", exact: true }).click();
      const composer = page.locator('form[aria-label="Message"] textarea');
      await composer.waitFor();
      const draft = await composer.inputValue();
      assert.ok(draft.includes(`artifact:${file.id}@1`));
      await composer.fill(
        `${draft}Confirm the owner as Confirmed Developer and preserve the rest.`,
      );
      await page.getByRole("button", { name: "Send", exact: true }).click();
      let revised: { id: string; title: string; version: number } | undefined;
      for (let attempt = 0; attempt < 100; attempt++) {
        const latest = await call<{ artifacts: { id: string; title: string; version: number }[] }>(
          "GET",
          `/api/artifacts?${new URLSearchParams({ thread: posted.thread.id, limit: "100" })}`,
        );
        revised = latest.artifacts.find((item) => item.title === file.title && item.version === 2);
        if (revised) break;
        assert.ok(attempt < 99, "Native revision did not publish");
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      assert.ok(revised);
      assert.notEqual(revised.id, file.id);
      assert.equal(
        (await call<{ sha256: string }>("GET", `/api/artifacts/${file.id}`)).sha256,
        originalVersion.sha256,
      );
      await page.goto(
        new URL(`/conversations/${posted.thread.id}/deliverables/${revised.id}`, page.url()).href,
      );
      await page.getByRole("cell", { name: "Confirmed Developer", exact: true }).waitFor();
    },
  });
  return [
    nativeFormat("docx"),
    nativeFormat("xlsx"),
    {
      name: "workspace-home-announcement",
      path: "/",
      act: async (page) => {
        await page.getByRole("heading", { level: 1 }).waitFor();
        const me = await call<{ welcome_announcement?: string }>("GET", "/api/me");
        assert.ok(me.welcome_announcement, "Home must use its real retained announcement identity");
      },
    },
    {
      name: "workspace-find-help",
      path: "/",
      act: async (page) => {
        await page.getByText(defaultCopy.home.findHelp.title, { exact: true }).click();
        await page.getByLabel(defaultCopy.home.findHelp.label).fill("launch plan");
        await page
          .getByRole("button", { name: defaultCopy.home.findHelp.search, exact: true })
          .click();
        await page
          .getByRole("button", { name: defaultCopy.home.findHelp.open, exact: true })
          .first()
          .waitFor();
      },
    },
    { name: "agent-setup-role", path: "/agents/new/setup", act: prepare },
    {
      name: "agent-setup-permissions",
      path: "/agents/new/setup",
      act: async (page) => {
        await prepare(page);
        await page.getByRole("button", { name: setup.next, exact: true }).click();
        await page.getByText(setup.toolsHint, { exact: true }).waitFor();
      },
    },
    {
      name: "agent-setup-review",
      path: "/agents/new/setup",
      act: async (page) => {
        await prepare(page);
        await page.getByRole("button", { name: setup.next, exact: true }).click();
        await page.getByRole("button", { name: setup.next, exact: true }).click();
        await page.getByRole("button", { name: setup.save, exact: true }).waitFor();
      },
    },
    {
      name: "agents-operational-states",
      path: "/agents",
      act: async (page) => {
        await page.getByLabel(defaultCopy.agentOperations.filter).waitFor();
        await page
          .getByRole("button", { name: defaultCopy.agentOperations.pauseAll, exact: true })
          .waitFor();
      },
    },
    {
      name: "agent-controls-paused",
      path: "/agents/lead/control",
      act: async (page) => {
        const previous = await call<NonNullable<typeof priorControl>>(
          "GET",
          "/api/agents/lead/control",
        );
        priorControl = previous;
        await call("PUT", "/api/agents/lead/control", {
          version: previous.version,
          paused: true,
          manager: previous.manager,
          stop_rules: previous.stop_rules,
        });
        await page.reload();
        await page
          .getByRole("button", { name: defaultCopy.operations.resume, exact: true })
          .waitFor();
      },
      after: async () => {
        if (priorControl) {
          const current = await call<{ version: number }>("GET", "/api/agents/lead/control");
          await call("PUT", "/api/agents/lead/control", {
            paused: priorControl.paused,
            manager: priorControl.manager,
            stop_rules: priorControl.stop_rules,
            version: current.version,
          });
          priorControl = undefined;
        }
      },
    },
    {
      name: "operations-context-health",
      path: "/operations/context",
      act: (page) => heading(page, defaultCopy.sourceOperations.health),
    },
    {
      name: "operations-integrations",
      path: "/operations/integrations",
      act: (page) => heading(page, defaultCopy.sourceOperations.integrations),
    },
    {
      name: "operations-action-policies",
      path: "/operations/policies",
      act: async (page) => {
        await heading(page, defaultCopy.operations.policies);
        await page.getByLabel(defaultCopy.operations.categories.write_files).waitFor();
      },
    },
  ];
}
