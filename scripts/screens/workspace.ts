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
  const projectPickerScreen: Screen = {
    name: "home-project-picker",
    path: "/",
    act: async (page) => {
      const input = page.locator('form[aria-label="Message"] textarea');
      await input.fill("Keep this draft when changing projects.");
      const picker = page.getByRole("button", { name: /^Select project: Personal/ });
      assert.equal(await picker.getAttribute("aria-haspopup"), "dialog");
      assert.equal(
        await picker.locator("svg").count(),
        1,
        "a drawer trigger has no dropdown caret",
      );
      await picker.click();
      const drawer = page.getByRole("dialog", {
        name: defaultCopy.chat.chooseProject,
        exact: true,
      });
      await drawer.waitFor();
      const project = drawer
        .getByRole("list", { name: "Projects" })
        .getByRole("button")
        .filter({ has: page.getByText("Launch", { exact: true }) });
      await project.click();
      assert.equal(await input.inputValue(), "Keep this draft when changing projects.");
      await page.getByRole("button", { name: "Select project: Launch", exact: true }).waitFor();
      await input.fill("");
      const suggestions = page.getByRole("list", { name: defaultCopy.chat.suggestions });
      await suggestions.waitFor();
      assert.equal(await suggestions.getByRole("button").count(), 3);
      const beforeTyping = await suggestions.boundingBox();
      await input.fill("Prepare the launch brief.");
      const afterTyping = await suggestions.boundingBox();
      assert.ok(beforeTyping && afterTyping, "Starters remain visible while composing");
      assert.ok(
        Math.abs(beforeTyping.y - afterTyping.y) < 1 &&
          Math.abs(beforeTyping.height - afterTyping.height) < 1,
        "Typing a short draft does not move or collapse the starter suggestions",
      );
      for (const choice of await suggestions.getByRole("button").all()) {
        const box = await choice.boundingBox();
        assert.ok(
          box && box.x >= 0 && box.x + box.width <= (page.viewportSize()?.width ?? 0),
          "Every starter fits the viewport",
        );
      }
      await page.getByRole("button", { name: "Select project: Launch", exact: true }).click();
      await drawer.waitFor();
    },
  };

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
  let welcomeAnnouncement: string | undefined;
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
        const record: { status: string } = await call<{ status: string }>(
          "GET",
          `/api/runs/${run.id}`,
        );
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
      // The existing composer stays mounted while its requested revision seed
      // is applied in a React effect. Wait for that exact immutable capability.
      await page.waitForFunction(
        (expected) =>
          (
            document.querySelector(
              'form[aria-label="Message"] textarea',
            ) as HTMLTextAreaElement | null
          )?.value.includes(expected),
        `artifact:${file.id}@1`,
      );
      const draft = await composer.inputValue();
      assert.ok(draft.includes(`artifact:${file.id}@1`));
      await composer.fill(
        `${draft}Confirm the owner as Confirmed Developer and preserve the rest.`,
      );
      await page.getByRole("button", { name: "Send message", exact: true }).click();
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
    projectPickerScreen,
    {
      name: "workspace-sidebar-pin",
      path: "/",
      act: async (page) => {
        const sidebar = page.getByRole("complementary", { name: "Navigation", exact: true });
        const reveal = async () => {
          if (!(await sidebar.isVisible()))
            await page.getByRole("button", { name: /^Open sidebar/ }).click();
        };
        await reveal();
        // Conversation rows pin from their ⋯ menu; focus returns to that trigger.
        const row = sidebar.locator("div[data-pin-thread]").first();
        await row.waitFor();
        const thread = await row.getAttribute("data-pin-thread");
        assert.ok(thread);
        const control = sidebar.locator(`div[data-pin-thread="${thread}"] button[aria-haspopup]`);
        const choose = async (item: RegExp) => {
          await control.focus();
          await page.keyboard.press("Enter");
          await page.getByRole("menuitem", { name: item }).first().focus();
          const saved = page.waitForResponse(
            (response) =>
              response.request().method() === "PUT" &&
              new URL(response.url()).pathname === "/api/preferences",
          );
          await page.keyboard.press("Enter");
          assert.equal((await saved).status(), 200);
        };
        const focused = () => control.evaluate((element) => element === document.activeElement);
        await choose(/^Pin\b/);
        await sidebar
          .getByRole("region", { name: "Pinned", exact: true })
          .locator(`div[data-pin-thread="${thread}"]`)
          .waitFor();
        await page.waitForFunction(
          (id) =>
            document.querySelector(`div[data-pin-thread="${id}"] button[aria-haspopup]`) ===
            document.activeElement,
          thread,
          { timeout: 1000 },
        );
        assert.equal(await focused(), true, "Keyboard focus follows the pinned row");
        assert.ok(
          (await call<{ pinned: string[] }>("GET", "/api/preferences")).pinned.includes(thread),
        );
        await page.reload();
        await reveal();
        await control.waitFor();
        await choose(/^Unpin\b/);
        await page.waitForFunction(
          (id) =>
            document.querySelector(`div[data-pin-thread="${id}"] button[aria-haspopup]`) ===
            document.activeElement,
          thread,
          { timeout: 1000 },
        );
        assert.equal(await focused(), true, "Keyboard focus follows the unpinned row");
        assert.equal(
          (await call<{ pinned: string[] }>("GET", "/api/preferences")).pinned.includes(thread),
          false,
        );
        if ((page.viewportSize()?.width ?? 0) < 768)
          await sidebar.getByRole("button", { name: "Close sidebar", exact: true }).click();
      },
    },
    {
      name: "workspace-motion",
      path: "/",
      act: async (page) => {
        await page.emulateMedia({ reducedMotion: "no-preference" });
        const picker = page.getByRole("button", { name: /^Select project: Personal/ });
        const drawer = page.getByRole("dialog", {
          name: defaultCopy.chat.chooseProject,
          exact: true,
        });
        await picker.click();
        await drawer.waitFor();
        const moving = await drawer.evaluate((element) => ({
          properties: getComputedStyle(element).transitionProperty,
          duration: getComputedStyle(element).transitionDuration,
        }));
        assert.ok(moving.properties.includes("translate"));
        assert.ok(moving.duration.split(",").some((duration) => parseFloat(duration) > 0));
        await page.keyboard.press("Escape");
        await drawer.waitFor({ state: "hidden" });
        assert.equal(await picker.evaluate((element) => element === document.activeElement), true);
        // Reopening before the outgoing shell is removed cancels that removal.
        await picker.click();
        await drawer.waitFor();
        await page.waitForTimeout(240);
        assert.equal(await page.locator(".humagen-inspector[open]").count(), 1);
        await page.keyboard.press("Escape");
        await drawer.waitFor({ state: "hidden" });
        await page.emulateMedia({ reducedMotion: "reduce" });
        await picker.click();
        await drawer.waitFor();
        assert.equal(
          await drawer.evaluate((element) => getComputedStyle(element).transitionProperty),
          "none",
          "Reduced motion removes transitions even when a duration token remains defined",
        );
        await page.keyboard.press("Escape");
        await drawer.waitFor({ state: "hidden" });
        await picker.click();
        await drawer.waitFor();
      },
    },
    nativeFormat("docx"),
    nativeFormat("xlsx"),
    {
      name: "workspace-home-announcement",
      path: "/",
      act: async (page) => {
        await page.getByRole("heading", { level: 1 }).waitFor();
        const me = await call<{ welcome_announcement?: string }>("GET", "/api/me");
        assert.ok(me.welcome_announcement, "Home must use its real retained announcement identity");
        welcomeAnnouncement = me.welcome_announcement;
        // The shared runner intentionally dismisses initial announcements.
        // Restore this native projection for its own browser state.
        await call("POST", `/api/inbox/${welcomeAnnouncement}/unhide`, {});
        await page.reload();
        const banner = page.getByRole("region", { name: defaultCopy.home.banner.label });
        await banner.waitFor();
        const hide = banner.getByRole("button", {
          name: defaultCopy.home.banner.hide,
          exact: true,
        });
        assert.equal(await hide.locator("svg").count(), 1, "Hide uses an accessible icon control");
        await banner
          .getByRole("button", { name: defaultCopy.home.banner.open, exact: true })
          .waitFor();
        assert.equal(
          await banner
            .getByRole("button", { name: defaultCopy.home.banner.markRead, exact: true })
            .count(),
          0,
          "The announcement exposes one reading action",
        );
      },
      after: async () => {
        if (welcomeAnnouncement)
          await call("POST", `/api/inbox/${welcomeAnnouncement}/dismiss`, {});
        welcomeAnnouncement = undefined;
      },
    },
    ...[false, true].map(
      (choose): Screen => ({
        name: choose ? "workspace-find-help-selection" : "workspace-find-help",
        path: "/",
        act: async (page) => {
          const c = defaultCopy.home.findHelp;
          const composer = page.locator('form[aria-label="Message"] textarea');
          const draft = "Keep this message while I choose the right agent.";
          await composer.fill(draft);
          assert.equal(await page.getByRole("region", { name: c.title, exact: true }).count(), 0);
          await page
            .getByRole("button", { name: "Add files, agents or a project", exact: true })
            .click();
          await page
            .getByRole("dialog", { name: defaultCopy.chat.menu, exact: true })
            .getByRole("button", { name: /^Agents\b/ })
            .click();
          await page
            .getByRole("dialog", { name: defaultCopy.chat.menu, exact: true })
            .getByRole("button", { name: c.title, exact: true })
            .click();
          let posts = 0;
          const requested = (request: import("playwright-core").Request) => {
            if (request.method() === "POST" && new URL(request.url()).pathname === "/api/threads")
              posts++;
          };
          page.on("request", requested);
          try {
            await page.getByLabel(c.label).fill("launch plan");
            await page.getByLabel(c.label).press("Enter");
            const match = page.getByRole("button", { name: c.choose, exact: true }).first();
            await match.waitFor();
            if (choose) {
              await match.click();
              await page
                .getByRole("list", { name: "Selected for this conversation", exact: true })
                .waitFor();
              assert.equal(
                await page
                  .getByRole("dialog", { name: defaultCopy.chat.menu, exact: true })
                  .count(),
                0,
              );
            }
            assert.equal(await composer.inputValue(), draft);
            assert.equal(posts, 0, "Discovery must not submit the current conversation");
          } finally {
            page.off("request", requested);
          }
        },
      }),
    ),
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
      name: "workspace-conversation-instructions",
      path: "/",
      act: async (page) => {
        const result = await call<{ threads: { id: string }[] }>(
          "GET",
          "/api/threads?project=Launch&limit=20",
        );
        const thread = result.threads[0];
        assert.ok(thread, "The real seed must retain a readable Launch conversation");
        await page.goto(
          new URL(
            `/conversations/${encodeURIComponent(thread.id)}/context/instructions`,
            page.url(),
          ).href,
        );
        const preview = defaultCopy.projects.context.preview;
        await page
          .getByRole("tab", { name: defaultCopy.conversation.instructionsTab, exact: true })
          .waitFor();
        await page.getByLabel(preview.agent, { exact: true }).fill("lead");
        await page.getByRole("button", { name: preview.submit, exact: true }).click();
        await page
          .getByRole("region", { name: preview.title, exact: true })
          .locator("ol > li")
          .first()
          .waitFor();
      },
    },
    {
      name: "operations-context-health-empty",
      path: "/operations/context",
      act: async (page) => {
        await page.route("**/api/knowledge", (route) =>
          route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({ sources: [] }),
          }),
        );
        await page.reload();
        await page.getByText("No sources to monitor yet", { exact: true }).waitFor();
        await page.getByRole("link", { name: "Add Sources", exact: true }).waitFor();
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
