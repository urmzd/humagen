import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
/** Reproducible library/personal states on the screens server, never a live workspace. */
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
type Screen = {
  name: string;
  path: string;
  act?: (page: Page) => Promise<void>;
  after?: () => Promise<void>;
};
type Note = { id: string; text: string; status: string };
const remembered = "Release decisions need a clear owner and a review date.";
const heading = (page: Page, title: string) =>
  page.getByRole("heading", { name: title, exact: true }).first().waitFor();

export function libraryScreens(call: Call): Screen[] {
  let originalPersonal: Record<string, unknown> | undefined;
  let digestMessages: { id: string; subject: string }[] | undefined;
  async function memory() {
    const { notes } = await call<{ notes: Note[] }>("GET", "/api/me/memory");
    return (
      notes.find((note) => note.text === remembered) ??
      (await call<Note>("POST", "/api/me/memory", { text: remembered, kind: "decision" }))
    );
  }
  async function suggestion() {
    const note = await memory();
    const { notes } = await call<{ notes: Note[] }>("GET", "/api/projects/~/memory");
    if (!notes.some((other) => other.text === remembered))
      await call("POST", "/api/memory/promote", {
        scope: "personal",
        id: note.id,
        destination: "~",
      });
  }
  return [
    {
      name: "library-sources-new",
      path: "/library/sources",
      act: async (page) => {
        await page.getByRole("button", { name: "New Source", exact: true }).click();
        const drawer = page.getByRole("dialog", {
          name: defaultCopy.sourceCatalog.title,
          exact: true,
        });
        await drawer.waitFor();
        for (const title of [
          defaultCopy.sourceCatalog.manual,
          defaultCopy.sourceCatalog.drive,
          defaultCopy.sourceCatalog.notion,
        ]) {
          await drawer.getByRole("button", { name: new RegExp(`^${title}`) }).waitFor();
        }
        for (const title of ["Snowflake", "BigQuery", "Slack"])
          await drawer.getByRole("heading", { name: title, exact: true }).waitFor();
        assert.equal(
          await drawer
            .getByText(defaultCopy.sourceCatalog.connectorRequired, { exact: true })
            .count(),
          3,
          "Unconfigured connectors are shown honestly",
        );
        const close = await drawer
          .getByRole("button", { name: `Close ${defaultCopy.sourceCatalog.title}`, exact: true })
          .boundingBox();
        assert.ok(
          close && close.y >= 0 && close.y + close.height <= (page.viewportSize()?.height ?? 0),
          "The source catalog keeps its close control in the viewport",
        );
      },
    },
    {
      name: "library-sources-scheduled",
      path: "/library/sources",
      act: async (page) => {
        const name = "screen-scheduled-source";
        await page.getByRole("button", { name: "New Source", exact: true }).click();
        const drawer = page.getByRole("dialog", {
          name: defaultCopy.sourceCatalog.title,
          exact: true,
        });
        await drawer.getByRole("button", { name: /^Google Drive/ }).click();
        const form = drawer.getByRole("form", {
          name: defaultCopy.sourceOperations.newSource,
          exact: true,
        });
        await form.getByLabel(defaultCopy.sourceOperations.name, { exact: true }).fill(name);
        await form
          .getByLabel(defaultCopy.sourceOperations.origin, { exact: true })
          .fill("screen-folder");
        const mode = form.getByLabel(defaultCopy.sourceOperations.mode, { exact: true });
        assert.equal(
          await mode.inputValue(),
          "live",
          "Provider sources default to current-reader queries",
        );
        assert.equal(
          await form.getByLabel(defaultCopy.sourceCatalog.cadence, { exact: true }).count(),
          0,
        );
        await mode.selectOption("indexed");
        await form
          .getByLabel(defaultCopy.sourceCatalog.cadence, { exact: true })
          .selectOption("scheduled");
        await form.getByLabel(defaultCopy.sourceCatalog.everyMinutes, { exact: true }).fill("90");
        const saved = page.waitForResponse(
          (response) =>
            response.request().method() === "POST" &&
            new URL(response.url()).pathname === "/api/knowledge",
        );
        await form
          .getByRole("button", { name: defaultCopy.sourceOperations.create, exact: true })
          .click();
        assert.equal((await saved).status(), 201, "Native source creation succeeded");
        await heading(page, name);
        const { sources } = await call<{
          sources: {
            name: string;
            lifecycle?: { provider: string; mode: string; sync_every_seconds: number };
          }[];
        }>("GET", "/api/knowledge");
        const source = sources.find((item) => item.name === name);
        assert.ok(source?.lifecycle);
        assert.equal(source.lifecycle.provider, "drive");
        assert.equal(source.lifecycle.mode, "indexed");
        assert.equal(source.lifecycle.sync_every_seconds, 90 * 60);
      },
      after: async () => {
        await call("DELETE", "/api/knowledge/screen-scheduled-source");
      },
    },
    {
      name: "library-agent-defaults",
      path: "/agents/lead/context",
      act: async (page) => {
        await heading(page, "Default skills");
        await page.getByRole("button", { name: "Save Default Skills", exact: true }).waitFor();
      },
    },
    { name: "library-skills", path: "/library/skills", act: (page) => heading(page, "Skills") },
    {
      name: "library-skills-draft",
      path: "/library/skills?scope=workspace",
      act: async (page) => {
        const { skills } = await call<{ skills: { id: string; name: string }[] }>(
          "GET",
          "/api/library/skills?scope=workspace",
        );
        const existing = skills.find((skill) => skill.name === "screen-brief");
        const skill =
          existing ??
          (await call<{ id: string }>("POST", "/api/library/skills", {
            scope: { kind: "workspace" },
            definition: {
              name: "screen-brief",
              description: "Turn a project update into a concise decision brief",
              instructions: "State the decision, owner, next action and review date.",
              inputs: "A project update",
              outputs: "A concise brief",
              tools: [],
            },
            version: 0,
          }));
        await page.goto(
          new URL(
            `/library/skills?scope=workspace&skill=${encodeURIComponent(skill.id)}`,
            page.url(),
          ).href,
        );
        await page.getByRole("tab", { name: "Overview", exact: true }).waitFor();
        await page.getByRole("heading", { name: "Used By", exact: true }).waitFor();
        const font = await page.evaluate(async () => {
          await document.fonts.ready;
          const surface = document.querySelector(".aui-stack");
          return {
            family: surface ? getComputedStyle(surface).fontFamily : "",
            loaded: [...document.fonts].some(
              (face) => face.family.includes("IBM Plex Sans Variable") && face.status === "loaded",
            ),
          };
        });
        assert.ok(
          font.loaded && font.family.includes("IBM Plex Sans Variable"),
          "shared presentation uses the loaded IBM Plex Sans Variable face",
        );
      },
    },
    {
      name: "library-skills-import",
      path: "/library/skills?scope=workspace",
      act: async (page) => {
        const temporary = mkdtempSync(join(tmpdir(), "humagen-skill-import-"));
        const folder = join(temporary, "release-import");
        mkdirSync(join(folder, "references"), { recursive: true });
        writeFileSync(
          join(folder, "SKILL.md"),
          "---\nname: release-import\ndescription: Summarize a release decision\n---\nRead references/style.md before writing the brief.\n",
        );
        writeFileSync(
          join(folder, "references/style.md"),
          "Keep the résumé concise and name the review date.\n",
        );
        try {
          await page.waitForFunction(() => {
            const field = document.querySelector<HTMLInputElement>("input[webkitdirectory]");
            return field && !field.disabled;
          });
          await page.locator("input[webkitdirectory]").setInputFiles(folder);
          await page.getByLabel("File Contents", { exact: true }).waitFor();
          await page.getByRole("button", { name: "Save Draft", exact: true }).click();
          await page.getByText("Saved revision", { exact: true }).waitFor();
          const { skills } = await call<{ skills: { id: string; name: string }[] }>(
            "GET",
            "/api/library/skills?scope=workspace",
          );
          const saved = skills.find((skill) => skill.name === "release-import");
          assert.ok(saved);
          const { revisions } = await call<{
            revisions: { files: { path: string; data: string }[] }[];
          }>("GET", `/api/library/skills/${encodeURIComponent(saved.id)}`);
          const reference = revisions
            .at(-1)
            ?.files.find((file) => file.path === "references/style.md");
          assert.ok(reference);
          assert.equal(
            Buffer.from(reference.data, "base64").toString("utf8"),
            "Keep the résumé concise and name the review date.\n",
          );
          const [download] = await Promise.all([
            page.waitForEvent("download"),
            page.getByRole("button", { name: "Download Folder", exact: true }).click(),
          ]);
          assert.equal(download.suggestedFilename(), "release-import.zip");
          assert.equal(await download.failure(), null);
        } finally {
          rmSync(temporary, { recursive: true, force: true });
        }
      },
    },
    {
      name: "library-skills-contents",
      path: "/library/skills?scope=workspace",
      act: async (page) => {
        const { skills } = await call<{ skills: { id: string; name: string }[] }>(
          "GET",
          "/api/library/skills?scope=workspace",
        );
        const skill =
          skills.find((skill) => skill.name === "screen-package") ??
          (await call<{ id: string }>("POST", "/api/library/skills", {
            scope: { kind: "workspace" },
            version: 0,
            definition: {
              name: "screen-package",
              description: "A portable release brief",
              tools: [],
              files: [
                {
                  path: "SKILL.md",
                  data: Buffer.from(
                    "---\nname: screen-package\ndescription: A portable release brief\n---\nRead references/style.md before drafting the release brief.\n",
                  ).toString("base64"),
                },
                {
                  path: "references/style.md",
                  data: Buffer.from(
                    "# Brief style\n\nName the decision, owner and review date.\n",
                  ).toString("base64"),
                },
                {
                  path: "scripts/check.py",
                  data: Buffer.from("print('Review the release brief')\n").toString("base64"),
                },
              ],
            },
          }));
        await page.goto(
          new URL(
            `/library/skills?scope=workspace&skill=${encodeURIComponent(skill.id)}&tab=contents&file=references%2Fstyle.md`,
            page.url(),
          ).href,
        );
        await page.getByRole("heading", { name: "Brief style", exact: true }).waitFor();
        await page.getByRole("tab", { name: "Contents", exact: true }).focus();
        await page.keyboard.press("ArrowRight");
        await page.getByRole("tab", { name: "Versions", exact: true, selected: true }).waitFor();
        await page.keyboard.press("ArrowLeft");
        await page.getByRole("tab", { name: "Contents", exact: true, selected: true }).waitFor();
        const files = page.getByRole("button", { name: "Skill Files", exact: true });
        if (await files.isVisible()) {
          await files.click();
          const drawer = page.getByRole("dialog", { name: "Skill Files", exact: true });
          await drawer.getByRole("button", { name: "SKILL.md", exact: true }).click();
          await drawer.waitFor({ state: "hidden" });
          assert.equal(await files.evaluate((element) => element === document.activeElement), true);
          await files.click();
          await drawer.getByRole("button", { name: "style.md", exact: true }).click();
          await page.getByRole("heading", { name: "Brief style", exact: true }).waitFor();
        }
        await page.getByRole("button", { name: "Source", exact: true }).click();
        await page.getByLabel("File Contents", { exact: true }).waitFor();
        await page.evaluate(() => document.fonts.ready);
        assert.ok(
          await page
            .getByLabel("File Contents", { exact: true })
            .evaluate((element) => getComputedStyle(element).fontFamily.includes("IBM Plex Mono")),
        );
      },
    },
    {
      name: "library-memory-chat",
      path: "/library/memory",
      act: async (page) => {
        await page.getByRole("link", { name: "Chat With Memory", exact: true }).click();
        const me = await call<{ personal_project: string }>("GET", "/api/me");
        assert.equal(new URL(page.url()).searchParams.get("project"), me.personal_project);
        assert.equal(new URL(page.url()).searchParams.has("channel"), false);
        await page.getByRole("button", { name: "Select project: Personal", exact: true }).waitFor();
        const draft = await page.locator("textarea").first().inputValue();
        assert.ok(draft.includes("Help me review the confirmed memory"));
      },
    },
    {
      name: "library-memory-scope",
      path: "/library/memory",
      act: async (page) => {
        await page.getByRole("heading", { name: "Personal", exact: true }).waitFor();
        const picker = page.getByRole("button", { name: "Memory Chains", exact: true });
        if (await picker.isVisible()) {
          await picker.click();
          await page
            .getByRole("dialog", { name: "Memory Chains", exact: true })
            .getByRole("button", { name: /^Company/ })
            .click();
        } else {
          await page
            .getByRole("navigation", { name: "Memory Chains", exact: true })
            .getByRole("button", { name: /^Company/ })
            .click();
        }
        await page.waitForURL(/scope=workspace/);
        await page.getByRole("heading", { name: "Company", exact: true }).waitFor();
        assert.equal(
          await page.locator("main h1").evaluate((element) => element === document.activeElement),
          true,
        );
      },
    },
    {
      name: "library-memory-entry",
      path: "/library/memory",
      act: async (page) => {
        const note = await memory();
        await page.goto(
          new URL(
            `/library/memory?scope=personal&file=${encodeURIComponent(`${note.id}.md`)}`,
            page.url(),
          ).href,
        );
        await page.getByText(remembered, { exact: true }).last().waitFor();
        await page.getByRole("button", { name: "Back to Entries", exact: true }).waitFor();
      },
    },
    {
      name: "library-memory-personal",
      path: "/library/memory",
      act: async (page) => {
        await memory();
        await page.reload();
        await page.getByText(remembered, { exact: true }).waitFor();
      },
    },
    {
      name: "library-memory-correction",
      path: "/library/memory",
      act: async (page) => {
        await memory();
        await page.reload();
        await page.getByText(remembered, { exact: true }).waitFor();
        await page.getByRole("button", { name: "Edit", exact: true }).first().click();
        await page.getByRole("button", { name: "Save Correction", exact: true }).waitFor();
      },
    },
    {
      name: "library-memory-review",
      path: "/library/memory?scope=workspace",
      act: async (page) => {
        await suggestion();
        await page.goto(new URL("/library/memory?scope=workspace", page.url()).href);
        await page.getByText(remembered, { exact: true }).waitFor();
        await page.getByRole("button", { name: "Keep", exact: true }).first().waitFor();
      },
    },
    {
      name: "library-memory-promotion",
      path: "/library/memory",
      act: async (page) => {
        await memory();
        await page.reload();
        await page.getByText(remembered, { exact: true }).waitFor();
        await page.getByText(remembered, { exact: true }).click();
        await page.locator("summary").filter({ hasText: "Propose to Project" }).first().click();
        await page.getByLabel("Destination Project", { exact: true }).first().waitFor();
      },
    },
    {
      name: "audit-expanded",
      path: "/operations/audit",
      act: async (page) => {
        await suggestion();
        await page.reload();
        await heading(page, "Audit");
        const event = page
          .locator("main ol > li")
          .filter({ has: page.getByRole("heading", { name: "Memory promote", exact: true }) })
          .first();
        await event
          .locator("summary")
          .filter({ hasText: defaultCopy.audit.details })
          .first()
          .click();
        await event.locator("summary").filter({ hasText: "After" }).first().click();
      },
    },
    {
      name: "audit-calendar",
      path: "/operations/audit",
      act: async (page) => {
        await suggestion();
        await page.reload();
        await heading(page, "Audit");
        const copy = defaultCopy.audit;
        const me = await call<{ principal: { subject: string } }>("GET", "/api/me");
        await page.getByLabel(copy.actor, { exact: true }).fill(me.principal.subject);
        await page.getByLabel(copy.domain, { exact: true }).selectOption("memory");
        const trigger = page.getByRole("button", { name: copy.dateRange, exact: true });
        await trigger.click();
        const calendar = page.getByRole("dialog", { name: copy.dateRange, exact: true });
        await calendar.waitFor();
        const day = calendar.locator(".rdp-day_button").first();
        await day.focus();
        const before = await page.evaluate(() =>
          document.activeElement?.getAttribute("aria-label"),
        );
        await page.keyboard.press("ArrowRight");
        assert.notEqual(
          await page.evaluate(() => document.activeElement?.getAttribute("aria-label")),
          before,
          "Calendar dates support keyboard movement",
        );
        await page.keyboard.press("Space");
        assert.ok((await calendar.getByLabel(copy.from, { exact: true }).inputValue()).length > 0);
        const from = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
        const to = new Date().toISOString().slice(0, 10);
        await calendar.getByLabel(copy.from, { exact: true }).fill(from);
        await calendar.getByLabel(`${copy.from} ${copy.time}`, { exact: true }).fill("00:00");
        await calendar.getByLabel(copy.to, { exact: true }).fill(to);
        await calendar.getByLabel(`${copy.to} ${copy.time}`, { exact: true }).fill("23:59");
        await page.keyboard.press("Escape");
        await calendar.waitFor({ state: "hidden" });
        await page.waitForFunction(
          (label) => document.activeElement?.getAttribute("aria-label") === label,
          copy.dateRange,
          { timeout: 1000 },
        );
        const request = page.waitForResponse(
          (response) => new URL(response.url()).pathname === "/api/audit",
        );
        await page.getByRole("button", { name: copy.apply, exact: true }).click();
        const applied = await request;
        assert.equal(applied.status(), 200);
        const params = new URL(applied.url()).searchParams;
        assert.equal(params.get("actor"), me.principal.subject);
        assert.equal(params.get("resource"), "memory");
        assert.equal(params.get("from"), `${from}T00:00:00.000Z`);
        assert.equal(params.get("to"), `${to}T23:59:00.000Z`);
        const event = page
          .locator("main ol > li")
          .filter({ has: page.getByRole("heading", { name: "Memory promote", exact: true }) })
          .first();
        await event.locator("summary").filter({ hasText: copy.details }).first().click();
        await event.getByText(copy.eventID, { exact: true }).waitFor();
        const downloaded = page.waitForEvent("download");
        await page.getByRole("button", { name: copy.export, exact: true }).click();
        const file = await downloaded;
        assert.equal(file.suggestedFilename(), "humagen-audit.csv");
        const path = await file.path();
        assert.ok(path);
        const csv = readFileSync(path, "utf8");
        assert.ok(csv.includes("memory.promote"), "CSV exports the actual filtered events");
        await trigger.click();
        await calendar.waitFor();
        assert.ok(
          await calendar.locator(".rdp-selected").count(),
          "Selected dates remain visible in the calendar",
        );
        const box = await calendar.boundingBox();
        assert.ok(
          box && box.y >= 0 && box.y + box.height <= (page.viewportSize()?.height ?? 0) + 1,
          "The calendar popup stays within the viewport and scrolls its own long controls",
        );
      },
    },
    { name: "you-profile", path: "/you", act: (page) => heading(page, "Profile") },
    {
      name: "you-notifications",
      path: "/you/notifications",
      act: (page) => heading(page, "Notifications"),
    },
    {
      name: "you-privacy",
      path: "/you/privacy",
      act: async (page) => {
        await heading(page, "Privacy");
        const copy = defaultCopy.personalPreferences;
        const before = await call<{ personal?: Record<string, unknown> }>(
          "GET",
          "/api/preferences",
        );
        originalPersonal = before.personal ?? {};
        const remember = page.getByRole("switch", { name: copy.remember, exact: true });
        const inProjects = page.getByRole("switch", { name: copy.personalMemory, exact: true });
        await remember.waitFor();
        await remember.click();
        await inProjects.click();
        await page.getByRole("button", { name: copy.save, exact: true }).click();
        await page.getByRole("status").filter({ hasText: copy.saved }).waitFor();
        const after = await call<{
          personal: { remember: boolean; use_personal_memory_in_projects: boolean };
        }>("GET", "/api/preferences");
        assert.equal(after.personal.remember, originalPersonal.remember !== true);
        assert.equal(
          after.personal.use_personal_memory_in_projects,
          originalPersonal.use_personal_memory_in_projects !== true,
        );
        await page.reload();
        await remember.waitFor();
        assert.equal(await remember.getAttribute("aria-checked"), String(after.personal.remember));
        assert.equal(
          await inProjects.getAttribute("aria-checked"),
          String(after.personal.use_personal_memory_in_projects),
        );
      },
      after: async () => {
        if (originalPersonal) {
          const current = await call<{ display_name: string; theme: string; accent: string }>(
            "GET",
            "/api/preferences",
          );
          await call("PUT", "/api/preferences", {
            display_name: current.display_name,
            theme: current.theme,
            accent: current.accent,
            personal: originalPersonal,
          });
        }
        originalPersonal = undefined;
      },
    },
    { name: "you-agents", path: "/you/agents", act: (page) => heading(page, "My Agents") },

    {
      name: "you-agent-review",
      path: "/you/agents",
      act: async (page) => {
        const [me, list] = await Promise.all([
          call<{ personal_project: string }>("GET", "/api/me"),
          call<{ agents: { spec: { name: string } }[]; homes: Record<string, string> }>(
            "GET",
            "/api/agents",
          ),
        ]);
        const name = "screen-personal-helper";
        if (!list.agents.some((row) => row.spec.name === name))
          await call("POST", "/api/agents", {
            name,
            description: "A helper for the developer's internal work",
            provider: "offline",
            model: "offline",
            system_prompt: "Summarize internal project updates concisely.",
          });
        const home = me.personal_project.replace(/^~/, "");
        if (list.homes[name] !== home)
          await call("PUT", `/api/agents/${name}/home`, { project: home });
        await page.reload();
        await page.getByRole("heading", { name, exact: true }).waitFor();
        await page.locator("summary").filter({ hasText: "Add to a Project" }).first().click();
        await page
          .getByLabel("Destination Project", { exact: true })
          .first()
          .selectOption("Launch");
        await page.getByRole("button", { name: "Review Audience", exact: true }).click();
        await page.getByRole("button", { name: "Move Agent to Project", exact: true }).waitFor();
      },
    },
    ...[false, true].map(
      (failed): Screen => ({
        name: failed ? "notifications-error" : "notifications-empty",
        path: "/notifications",
        act: async (page) => {
          await page.route("**/api/me/notifications", (route) =>
            route.fulfill(
              failed
                ? { status: 503, json: { error: "Delivery temporarily unavailable" } }
                : { json: { notifications: [] } },
            ),
          );
          if (!failed) {
            await page.route(/\/api\/inbox(?:\?.*)?$/, (route) =>
              route.fulfill({ json: { messages: [], next: null, open: 0, unread: 0, updates: 0 } }),
            );
            await page.route("**/api/inbox/count", (route) =>
              route.fulfill({ json: { open: 0, unread: 0, updates: 0 } }),
            );
          }
          await page.reload();
          await heading(page, "Notifications");
          await page.getByRole("link", { name: "Notification Preferences", exact: true }).waitFor();
          if (failed) await page.getByRole("button", { name: "Try Again", exact: true }).waitFor();
          else await page.getByText(defaultCopy.mailbox.empty, { exact: true }).waitFor();
        },
      }),
    ),
    {
      name: "notifications-hub",
      path: "/notifications",
      act: async (page) => {
        await heading(page, "Notifications");
        const navigation = page.getByRole("navigation", { name: "Notifications", exact: true });
        const feed = navigation.getByRole("link", {
          name: defaultCopy.navigation.notifications,
          exact: true,
        });
        const rules = navigation.getByRole("link", {
          name: defaultCopy.navigation.alertRules,
          exact: true,
        });
        assert.equal(await feed.getAttribute("href"), "/notifications");
        assert.equal(await feed.getAttribute("aria-current"), "page");
        assert.equal(await rules.getAttribute("href"), "/monitor/rules");
        const sidebar = page.getByRole("complementary", { name: "Navigation", exact: true });
        if (!(await sidebar.isVisible()))
          await page.getByRole("button", { name: /^Open sidebar/ }).click();
        const sections = sidebar.getByRole("navigation", { name: "Sections", exact: true });
        const bell = sections.getByRole("link", { name: /^Notifications/ });
        assert.equal(await bell.count(), 1, "Notifications has one sidebar destination");
        assert.equal(await sections.getByRole("link", { name: /^Needs You/ }).count(), 0);
        const { open } = await call<{ open: number }>("GET", "/api/inbox/count");
        if (open > 0)
          await sections
            .getByRole("link", { name: new RegExp(`^Notifications, ${open} need`) })
            .waitFor();
        assert.equal(await bell.getAttribute("href"), "/notifications");
        await bell.click();
        await page.waitForURL(/\/notifications$/);
        await rules.click();
        await page.waitForURL(/\/monitor\/rules$/);
        assert.equal(await rules.getAttribute("aria-current"), "page");
        await feed.click();
        await page.waitForURL(/\/notifications$/);
        await page.goto(new URL("/inbox", page.url()).href);
        await heading(page, "Notifications");
        assert.equal(
          await feed.getAttribute("aria-current"),
          "page",
          "Legacy inbox links reach the unified feed",
        );
        const query = page.getByRole("searchbox", {
          name: defaultCopy.inboxControls.search,
          exact: true,
        });
        await query.fill("No matching update in this collection");
        assert.equal(await query.inputValue(), "No matching update in this collection");
        await page.getByText(defaultCopy.mailbox.empty, { exact: true }).waitFor();
        await query.fill("");
        const filter = page.getByRole("button", {
          name: defaultCopy.inboxControls.notificationFilter,
          exact: true,
        });
        await filter.click();
        const panel = page.getByRole("dialog", {
          name: defaultCopy.inboxControls.notificationFilter,
          exact: true,
        });
        await panel
          .getByLabel(defaultCopy.inboxControls.status, { exact: true })
          .selectOption("open");
        await panel
          .getByLabel(defaultCopy.inboxControls.type, { exact: true })
          .selectOption("failure");
        await panel
          .getByRole("button", { name: defaultCopy.inboxControls.done, exact: true })
          .click();
        await page.waitForFunction(
          (label) => document.activeElement?.getAttribute("aria-label") === label,
          defaultCopy.inboxControls.notificationFilter,
          { timeout: 1000 },
        );
        assert.equal(new URL(page.url()).searchParams.get("kind"), "failure");
        await filter.click();
        await panel
          .getByRole("button", { name: defaultCopy.inboxControls.reset, exact: true })
          .click();
        await panel
          .getByRole("button", { name: defaultCopy.inboxControls.done, exact: true })
          .click();
        assert.equal(new URL(page.url()).searchParams.get("kind"), null);
      },
    },
    {
      name: "notifications-digest",
      path: "/notifications",
      act: async (page) => {
        // Delivery timing uses a reproducible projection; every displayed
        // message still exists in the native inbox and retains its own actions.
        digestMessages ??= await Promise.all(
          ["Review the project decision", "Confirm the suggested memory"].map((subject) =>
            call<{ id: string; subject: string }>("POST", "/api/projects/Launch/announcements", {
              subject,
              text: "A retained project update for the review.",
            }),
          ),
        );
        await page.route("**/api/me/notifications", (route) =>
          route.fulfill({
            json: {
              notifications: [
                {
                  id: "ntf-screen",
                  mode: "digest",
                  at: "2026-10-26T12:00:00Z",
                  items: digestMessages?.map((message) => ({
                    inbox: message.id,
                    kind: "announcement",
                    subject: message.subject,
                    at: "2026-10-26T11:00:00Z",
                  })),
                },
              ],
            },
          }),
        );
        await page.reload();
        await heading(page, "Notifications");
        await page
          .locator("main")
          .getByText("Review the project decision", { exact: true })
          .first()
          .waitFor();
        await page
          .locator("main")
          .getByText("Review the project decision", { exact: true })
          .first()
          .click();
        await page
          .getByRole("button", { name: defaultCopy.notifications.dismissDigest(2), exact: true })
          .waitFor();
      },
    },
  ];
}
