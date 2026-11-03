import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;
type Screen = {
  name: string;
  path: string;
  act: (page: Page) => Promise<void>;
};
type File = { path: string; content: string; revision: string };

const copy = defaultCopy.workspaceFiles;
/** The Workspace opens from Conversation Settings. */
const openWorkspace = async (page: Page) => {
  await page.getByRole("button", { name: defaultCopy.conversation.settings, exact: true }).click();
  await page.getByRole("button", { name: defaultCopy.workspace.menu, exact: true }).click();
};
const greeting = 'export function greeting(name: string) {\n  return "Hello, " + name;\n}\n';
const changedGreeting =
  'export function greeting(name: string) {\n  return "Welcome, " + name.trim();\n}\n';
const patch = [
  "diff --git a/src/greeting.ts b/src/greeting.ts",
  "--- a/src/greeting.ts",
  "+++ b/src/greeting.ts",
  "@@ -1,3 +1,3 @@",
  " export function greeting(name: string) {",
  '-  return "Hello, " + name;',
  '+  return "Welcome, " + name.trim();',
  " }",
  "diff --git a/src/greeting.test.ts b/src/greeting.test.ts",
  "--- a/src/greeting.test.ts",
  "+++ b/src/greeting.test.ts",
  "@@ -1 +1,2 @@",
  '-assert.equal(greeting("Ada"), "Hello, Ada");',
  '+assert.equal(greeting("Ada"), "Welcome, Ada");',
  '+assert.equal(greeting(" Ada "), "Welcome, Ada");',
  "",
].join("\n");

/** Real scratch-file APIs and saved publications, without intercepted responses. */
export function ideScreens(call: Call): Screen[] {
  const prepare = async (page: Page, file: string, tab = "edit") => {
    const posted = await call<{ thread: { id: string }; runs: unknown[] }>("POST", "/api/threads", {
      project: "Launch",
      text: "File workspace for the launch review.",
      visibility: "private",
    });
    assert.equal(posted.runs.length, 0, "Preparing files must not start an agent");
    const thread = posted.thread.id;
    const endpoint = `/api/threads/${encodeURIComponent(thread)}/files`;
    for (const [path, content] of [
      ["src/greeting.ts", greeting],
      [
        "scratchpads/plan.md",
        "# Launch review\n\n- Review the greeting change\n- Check whitespace handling\n- Publish the agreed result\n",
      ],
      ["review.diff", patch],
      [
        "preview.html",
        '<!doctype html><html><head><style>body{font:18px system-ui;padding:24px;color:#172033}h1{font-size:30px}button{padding:12px}</style></head><body><h1>Launch readiness</h1><p>The reviewed greeting is ready for the team.</p><button>View Release Notes</button><script>document.body.dataset.executed="yes"</script><img src="https://example.invalid/workspace-preview-probe.png"></body></html>',
      ],
    ]) {
      await call("PUT", `${endpoint}/content`, { path, content, revision: "" });
    }
    await page.goto(
      new URL(
        `/conversations/${thread}/workspace?${new URLSearchParams({ file, tab })}`,
        page.url(),
      ).href,
    );
    await page.getByRole("dialog", { name: copy.title, exact: true }).waitFor();
    return { thread, endpoint };
  };
  const savedFile = (endpoint: string, path: string) =>
    call<File>("GET", `${endpoint}/content?${new URLSearchParams({ path })}`);
  const screens: Screen[] = [
    {
      name: "ide-editor",
      path: "/",
      act: async (page) => {
        const { endpoint } = await prepare(page, "src/greeting.ts");
        const editor = page.getByRole("textbox", { name: "Edit src/greeting.ts", exact: true });
        await editor.fill(changedGreeting);
        await page.getByRole("button", { name: /^Save/ }).first().click();
        await page.waitForFunction(() => !document.body.textContent?.includes("Unsaved"));
        assert.equal((await savedFile(endpoint, "src/greeting.ts")).content, changedGreeting);
        await page.reload();
        await editor.waitFor();
        assert.equal(await editor.inputValue(), changedGreeting, "Saved code survives reload");
        await editor.fill(`${changedGreeting}\n// Ready for a teammate to review.\n`);
        await page.getByRole("button", { name: copy.changes, exact: true }).click();
        await page.getByRole("region", { name: "Change review", exact: true }).waitFor();
        await page.getByRole("button", { name: copy.edit, exact: true }).click();
        await editor.waitFor();
      },
    },
    {
      name: "ide-patch-review",
      path: "/",
      act: async (page) => {
        await prepare(page, "review.diff", "changes");
        const review = page.getByRole("region", { name: "Change review", exact: true });
        await review.waitFor();
        assert.match(await review.innerText(), /2 files/);
        await page
          .getByRole("button", { name: "Comment on src/greeting.ts, new line 2", exact: true })
          .click();
        await page
          .getByRole("textbox", { name: "Comment on src/greeting.ts, new line 2", exact: true })
          .fill("Please keep a regression test for leading and trailing whitespace.");
        await page.getByRole("button", { name: "Add To Draft", exact: true }).click();
        const message = page.locator('form[aria-label="Message"] textarea');
        await page.waitForFunction(() =>
          document
            .querySelector<HTMLTextAreaElement>('form[aria-label="Message"] textarea')
            ?.value.includes("regression test"),
        );
        assert.match(await message.inputValue(), /src\/greeting\.ts/);
        assert.equal(
          await page.getByRole("article", { name: /^Message from / }).count(),
          1,
          "Review feedback stays unsent",
        );
        await openWorkspace(page);
        await page.getByRole("button", { name: copy.changes, exact: true }).click();
        await review.waitFor();
      },
    },
    {
      name: "ide-scratchpad-preview",
      path: "/",
      act: async (page) => {
        const { endpoint } = await prepare(page, "scratchpads/plan.md", "preview");
        await page.getByRole("button", { name: copy.newFile, exact: true }).click();
        await page.getByLabel(copy.path, { exact: true }).fill("notes.md");
        await page.getByRole("button", { name: copy.create, exact: true }).click();
        await page.getByRole("button", { name: copy.edit, exact: true }).click();
        const scratch = page.getByRole("textbox", {
          name: "Edit notes.md",
          exact: true,
        });
        await scratch.fill(
          "# Launch review\n\nThe greeting handles surrounding whitespace. Ready for publication.\n",
        );
        await page.getByRole("button", { name: /^Save/ }).first().click();
        await page.waitForFunction(() => !document.body.textContent?.includes("Unsaved"));
        assert.match((await savedFile(endpoint, "notes.md")).content, /Ready for publication/);
        await page.getByRole("button", { name: copy.preview, exact: true }).click();
        await page.getByRole("heading", { name: "Launch review", exact: true }).waitFor();
        await page.getByRole("button", { name: copy.publish, exact: true }).click();
        await page.getByRole("button", { name: copy.openPublished, exact: true }).waitFor();
        await page.getByRole("button", { name: copy.openPublished, exact: true }).click();
        await page.getByRole("link", { name: "Download Original", exact: true }).waitFor();
        assert.match(page.url(), /\/deliverables\//, "Publication opens its immutable artifact");
        await page.getByRole("button", { name: "Close notes.md", exact: true }).click();
        await openWorkspace(page);
        await page.getByRole("button", { name: copy.preview, exact: true }).click();
        await page.getByRole("heading", { name: "Launch review", exact: true }).waitFor();
      },
    },
    {
      name: "ide-html-preview",
      path: "/",
      act: async (page) => {
        const requests: string[] = [];
        const observe = (request: { url(): string }) => {
          if (request.url().includes("workspace-preview-probe")) requests.push(request.url());
        };
        page.on("request", observe);
        try {
          await prepare(page, "preview.html", "preview");
          const iframe = page.locator('iframe[title="Preview: preview.html"]');
          await page
            .frameLocator('iframe[title="Preview: preview.html"]')
            .getByRole("heading", { name: "Launch readiness", exact: true })
            .waitFor();
          assert.equal(await iframe.getAttribute("sandbox"), "");
          const frame = await (await iframe.elementHandle())?.contentFrame();
          assert.ok(frame);
          assert.equal(
            await frame.locator("body").getAttribute("data-executed"),
            null,
            "Preview scripts cannot run",
          );
          assert.deepEqual(requests, [], "Preview content cannot request network resources");
        } finally {
          page.off("request", observe);
        }
      },
    },
    {
      name: "ide-stale-edit",
      path: "/",
      act: async (page) => {
        const { endpoint } = await prepare(page, "src/greeting.ts");
        const editor = page.getByRole("textbox", { name: "Edit src/greeting.ts", exact: true });
        await editor.fill(changedGreeting);
        const prior = await savedFile(endpoint, "src/greeting.ts");
        await call("PUT", `${endpoint}/content`, {
          ...prior,
          content: `${greeting}// Teammate edit\n`,
        });
        await page.getByRole("button", { name: /^Save/ }).first().click();
        await page.getByText(copy.conflict, { exact: true }).first().waitFor();
        assert.equal(
          await editor.inputValue(),
          changedGreeting,
          "A conflict preserves unsaved code",
        );
        assert.match((await savedFile(endpoint, "src/greeting.ts")).content, /Teammate edit/);
      },
    },
  ];
  return [
    ...screens,
    ...screens
      .filter((screen) => screen.name === "ide-editor" || screen.name === "ide-patch-review")
      .map((screen) => ({
        ...screen,
        name: `${screen.name}-focused`,
        act: async (page: Page) => {
          await screen.act(page);
          await page
            .locator(
              screen.name === "ide-editor"
                ? '[data-slot="code-editor"] textarea'
                : '[data-slot="change-review"]',
            )
            .scrollIntoViewIfNeeded();
        },
      })),
  ];
}
