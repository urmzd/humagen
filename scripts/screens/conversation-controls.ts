import type { Page } from "playwright-core";
import { defaultCopy } from "../../packages/components/src/copy.ts";

/** Reveal panel actions through the compact menu only when the header hides them. */
export async function revealConversationControls(page: Page): Promise<void> {
  const overview = page.getByRole("button", { name: "Overview", exact: true });
  const settings = page.getByRole("button", {
    name: defaultCopy.conversation.settings,
    exact: true,
  });
  await overview.or(settings).first().waitFor();
  if (!(await overview.isVisible())) await settings.click();
}
