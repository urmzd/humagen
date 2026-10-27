import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;

/** Review genuine isolated archive records through the production console. */
export function conversationDeletions(call: Call) {
  let page: Page;
  let waiting: string;
  let due: string;
  const words = defaultCopy.deletions;
  return {
    name: "conversation-deletions",
    path: "/settings/deletions",
    act: async (current: Page) => {
      page = current;
      const start = async (title: string, deleteNow: boolean) => {
        const created = await call<{ thread: { id: string } }>("POST", "/api/threads", {
          project: "Launch",
          title,
          visibility: "private",
          text: title,
          post_only: true,
        });
        await call("POST", `/api/threads/${created.thread.id}/archive`, { delete_now: deleteNow });
        return created.thread.id;
      };
      waiting = await start("Archived launch draft", false);
      due = await start("Launch draft awaiting deletion approval", true);
      await page.reload();
      const pending = page
        .getByRole("listitem")
        .filter({ has: page.getByText("Archived launch draft", { exact: true }) });
      await pending.waitFor();
      assert.equal(
        await pending.getByRole("button", { name: words.approve, exact: true }).isDisabled(),
        true,
      );
      const ready = page.getByRole("listitem").filter({
        has: page.getByText("Launch draft awaiting deletion approval", { exact: true }),
      });
      assert.equal(
        await ready.getByRole("button", { name: words.approve, exact: true }).isEnabled(),
        true,
      );
    },
    after: async () => {
      const ready = page.getByRole("listitem").filter({
        has: page.getByText("Launch draft awaiting deletion approval", { exact: true }),
      });
      await ready.getByRole("button", { name: words.approve, exact: true }).click();
      await ready.waitFor({ state: "detached" });
      const response = await page.request.get(`${new URL(page.url()).origin}/api/threads/${due}`);
      assert.equal(response.status(), 404, "Approved deletion hides the native conversation");
      const pending = page
        .getByRole("listitem")
        .filter({ has: page.getByText("Archived launch draft", { exact: true }) });
      await pending.getByRole("button", { name: words.restore, exact: true }).click();
      await page.getByText(words.empty, { exact: true }).waitFor();
      const restored = await call<{ thread: { id: string } }>("GET", `/api/threads/${waiting}`);
      assert.equal(restored.thread.id, waiting);
    },
  };
}
