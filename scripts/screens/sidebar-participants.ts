import assert from "node:assert/strict";
import type { Page } from "playwright-core";

type Call = <T>(method: string, path: string, body?: unknown) => Promise<T>;

/** The roster is a presentation fixture; pin saves and reloads use the native API. */
export function sidebarParticipants(call: Call, thread: string) {
  let preferences: Record<string, unknown>;
  let removeRoster = async () => {};
  return {
    name: "sidebar-participants",
    path: "/settings/profile",
    // This fixture checks the drawer's bounds directly; page-content gutters
    // do not apply to a full-viewport dismissal target or compact navigation.
    rules: { target: 24 },
    act: async (page: Page) => {
      const saved = await call<Record<string, unknown>>("GET", "/api/preferences");
      preferences = Object.fromEntries(
        ["display_name", "theme", "accent", "pinned", "pinned_projects"].map((field) => [
          field,
          saved[field],
        ]),
      );
      await page.route(/\/api\/threads(?:\?|$)/, async (route) => {
        const response = await route.fetch();
        const body = await response.json();
        for (const row of body.threads ?? []) {
          if (row.id !== thread) continue;
          row.title = "Draft the launch checklist with the team";
          row.agents = ["lead", "helper"];
          row.people = ["dev", "mira", "jon", "sam"];
        }
        await route.fulfill({ response, json: body });
      });
      removeRoster = () => page.unroute(/\/api\/threads(?:\?|$)/);
      const phone = (page.viewportSize()?.width ?? 0) < 768;
      const open = async () => {
        const button = page.getByRole("button", { name: /^Open sidebar/ });
        await page
          .getByRole("button", { name: "Close sidebar", exact: true })
          .or(button)
          .first()
          .waitFor();
        if (phone && (await button.isVisible())) await button.click();
      };
      await page.reload();
      await open();
      const navigation = page.getByRole("complementary", { name: "Navigation" });
      await navigation.getByRole("group", { name: "Agents: lead, helper" }).first().waitFor();
      await navigation.getByRole("group", { name: /^People: .*mira, jon, sam$/ }).waitFor();
      const projectPin = navigation.getByRole("button", {
        name: "Pin project Launch",
        exact: true,
      });
      await projectPin.scrollIntoViewIfNeeded();
      await projectPin.click();
      const conversationPin = navigation.getByRole("button", {
        name: "Manage Draft the launch checklist with the team",
        exact: true,
      });
      await conversationPin.scrollIntoViewIfNeeded();
      await conversationPin.click();
      await page.getByRole("menuitem", { name: /^Pin\b/ }).click();
      const pinned = navigation.getByRole("region", { name: "Pinned", exact: true });
      await pinned
        .getByRole("button", {
          name: "Manage Draft the launch checklist with the team",
          exact: true,
        })
        .waitFor();
      await page.reload();
      await open();
      await pinned.getByRole("link", { name: "Launch", exact: true }).waitFor();
      await pinned.getByRole("button", { name: "Unpin project Launch", exact: true }).click();
      const unpinConversation = pinned.getByRole("button", {
        name: "Manage Draft the launch checklist with the team",
        exact: true,
      });
      await unpinConversation.waitFor();
      await unpinConversation.click();
      await page.getByRole("menuitem", { name: /^Unpin\b/ }).waitFor();
      await page.keyboard.press("Escape");
      await navigation.getByRole("button", { name: "Pin project Launch", exact: true }).click();
      await pinned.getByRole("link", { name: "Launch", exact: true }).waitFor();
      await pinned.scrollIntoViewIfNeeded();
      assert.equal(
        await navigation.evaluate((element) => element.scrollWidth <= element.clientWidth),
        true,
      );
      const people = pinned.getByRole("group", { name: /^People: .*mira, jon, sam$/ });
      assert.ok((await people.textContent())?.includes("4 people"));
      assert.equal(
        await people.evaluate((element) => {
          const row = element.closest("button")?.getBoundingClientRect();
          if (!row) return false;
          return [...element.children].every((child) => {
            const box = child.getBoundingClientRect();
            return (
              child.classList.contains("sr-only") ||
              (box.left >= row.left && box.right <= row.right)
            );
          });
        }),
        true,
        "Participant avatars and their overflow count fit the conversation row",
      );
      const pin = await unpinConversation.boundingBox();
      assert.ok(pin && pin.width >= (phone ? 44 : 32) && pin.height >= (phone ? 44 : 32));
    },
    after: async () => {
      await removeRoster();
      await call("PUT", "/api/preferences", preferences);
    },
  };
}
