import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
/** Reproducible library/personal states on the screens server, never a live workspace. */
import type { Page } from "playwright-core";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
type Screen = { name: string; path: string; act?: (page: Page) => Promise<void> };
type Note = { id: string; text: string; status: string };
const remembered = "Release decisions need a clear owner and a review date.";
const heading = (page: Page, title: string) =>
  page.getByRole("heading", { name: title, exact: true }).first().waitFor();

export function libraryScreens(call: Call): Screen[] {
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
              (face) => face.family.includes("Figtree Variable") && face.status === "loaded",
            ),
          };
        });
        assert.ok(
          font.loaded && font.family.includes("Figtree Variable"),
          "shared presentation uses the loaded Figtree Variable face",
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
            .at(-1)!
            .files.find((file) => file.path === "references/style.md");
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
        await page.getByRole("button", { name: "Source", exact: true }).click();
        await page.getByLabel("File Contents", { exact: true }).waitFor();
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
        await page.getByRole("heading", { name: "memory.promote", exact: true }).first().waitFor();
        await page.locator("summary").filter({ hasText: "After" }).first().click();
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
      act: (page) => heading(page, "Privacy and Defaults"),
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
          await page.reload();
          await heading(page, "Notifications");
          await page.getByRole("link", { name: "Notification Preferences", exact: true }).waitFor();
          if (failed) await page.getByRole("button", { name: "Try Again", exact: true }).waitFor();
          else await page.getByText("No notifications ready.", { exact: true }).waitFor();
        },
      }),
    ),
    {
      name: "notifications-digest",
      path: "/notifications",
      act: async (page) => {
        // The worker's deterministic journey tests cover real timing. This
        // retained delivery fixture makes its expanded rendering reproducible.
        await page.route("**/api/me/notifications", (route) =>
          route.fulfill({
            json: {
              notifications: [
                {
                  id: "ntf-screen",
                  mode: "digest",
                  at: "2026-10-26T12:00:00Z",
                  items: [
                    {
                      inbox: "in-screen",
                      kind: "mention",
                      subject: "Review the project decision",
                      at: "2026-10-26T11:00:00Z",
                    },
                    {
                      inbox: "in-screen-two",
                      kind: "memory",
                      subject: "Confirm the suggested memory",
                      at: "2026-10-26T11:15:00Z",
                    },
                  ],
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
          .waitFor();
      },
    },
  ];
}
