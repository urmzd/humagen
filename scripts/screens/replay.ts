import assert from "node:assert/strict";
import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

/** DES-058: the real finished-turn picker and its saved fork, at both widths. */
export function replayScreens(thread: string) {
  return [
    {
      name: "conversation-replay-agent",
      path: `/conversations/${thread}`,
      act: async (page: Page) => {
        const button = page
          .getByRole("button", { name: defaultCopy.forking.replay, exact: true })
          .last();
        await button.waitFor({ state: "attached" });
        // Message actions become visible on focus on phones as well as desktop.
        await button.focus();
        await button.click();
        const picker = page.getByRole("menu", { name: defaultCopy.forking.replay });
        await picker
          .getByRole("menuitem", { name: defaultCopy.forking.replayWith("lead") })
          .waitFor();
        assert.ok(await picker.getByRole("menuitem").count());
      },
    },
  ];
}
