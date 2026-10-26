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
        await page.getByLabel("Name", { exact: true }).waitFor();
        await page.getByRole("button", { name: "Test Skill", exact: true }).waitFor();
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
    {
      name: "you-notification-digest",
      path: "/you/notifications",
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
        await page.locator("main summary").filter({ hasText: "Notifications" }).first().click();
        await page
          .locator("main")
          .getByText("Review the project decision", { exact: true })
          .waitFor();
      },
    },
  ];
}
