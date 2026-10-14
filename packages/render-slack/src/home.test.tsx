import { Home } from "@vultus/components";
import { ActionId, ANY_AGENT, HOME_ID, type HomeModel } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { describe, expect, it } from "vitest";
import { type Block, decodeSlackAction, homeView, LIMITS } from "./index";

type Element = Record<string, unknown> & {
  action_id?: string;
  text?: { text: string };
  options?: { value: string }[];
};

/** Every interactive element in a view: actions-block elements and section accessories. */
function elements(blocks: Block[]): Element[] {
  return blocks.flatMap((b) => [
    ...((b.elements as Element[] | undefined) ?? []).filter((e) => e.action_id),
    ...(b.accessory ? [b.accessory as Element] : []),
  ]);
}

describe("homeView", () => {
  it.each(Object.entries(fx.homes))("renders the %s Home tab", (_name, home) => {
    const { view, warnings } = homeView(<Home home={home} />);
    expect(warnings).toEqual([]);
    expect(view.type).toBe("home");
    expect(view.blocks.length).toBeLessThanOrEqual(LIMITS.blocks.home);
    expect(view).toMatchSnapshot();
  });

  it("keeps action ids unique across the view and within Slack's limits", () => {
    for (const home of Object.values(fx.homes)) {
      const ids = elements(homeView(<Home home={home} />).view.blocks).map((e) => e.action_id);
      expect(new Set(ids).size).toBe(ids.length);
      for (const id of ids) expect((id as string).length).toBeLessThanOrEqual(LIMITS.actionId);
    }
  });

  it("keeps header and button text within limits", () => {
    const long: HomeModel = {
      ...fx.home,
      person: { id: "U1", name: "A".repeat(200) },
      waiting: Array.from({ length: 60 }, (_, i) => ({
        id: `w${i}`,
        from: "docs-bot",
        summary: "wants to post a reply in #support.",
      })),
    };
    const { view, warnings } = homeView(<Home home={long} />);
    expect(view.blocks.length).toBeLessThanOrEqual(LIMITS.blocks.home);
    for (const b of view.blocks.filter((b) => b.type === "header")) {
      expect((b.text as { text: string }).text.length).toBeLessThanOrEqual(LIMITS.header);
    }
    for (const e of elements(view.blocks).filter((e) => e.type === "button")) {
      expect(e.text?.text.length).toBeLessThanOrEqual(LIMITS.buttonText);
    }
    // Ten rows, then a count: the rest live in the inbox.
    expect(JSON.stringify(view.blocks)).toContain("50 more in the inbox");
    expect(warnings).toEqual(["header clipped from 219 to 150 chars"]);
  });

  it("shows three buttons, and Ask a Question asks for words in a form", () => {
    const { view } = homeView(<Home home={fx.home} />);
    const start = view.blocks.find((b) => b.block_id === "home:start");
    const buttons = (start?.elements as Element[]).map((e) => e.text?.text);
    expect(buttons).toEqual(["Ask a Question", "Build an Agent", "Search Knowledge"]);
    const ask = (start?.elements as Element[])[0] as { action_id: string; value: string };
    expect(decodeSlackAction(ask)).toEqual({
      id: ActionId.homeAsk,
      payload: { turnId: HOME_ID, target: "ask" },
    });
  });

  it("decodes the answered-by select to the agent that was picked", () => {
    const { view } = homeView(<Home home={fx.home} />);
    const select = elements(view.blocks).find((e) => e.type === "static_select");
    expect(select?.initial_option).toEqual(select?.options?.[0]);
    const triage = select?.options?.find((o) => o.value.includes('"triage"'));
    const ui = decodeSlackAction({
      action_id: select?.action_id as string,
      selected_option: triage as { value: string },
    });
    expect(ui).toEqual({
      id: ActionId.homeAnsweredBy,
      payload: { turnId: HOME_ID, target: "answered_by", option: "triage" },
    });
    expect(JSON.parse(select?.options?.[0]?.value ?? "{}").p.option).toBe(ANY_AGENT);
  });

  it("offers Connect only to a viewer who may connect", () => {
    const connects = (home: HomeModel) =>
      elements(homeView(<Home home={home} />).view.blocks)
        .filter((e) => e.action_id?.startsWith(ActionId.homeConnect))
        .map((e) => e.text?.text);
    expect(connects(fx.homes.default)).toEqual(["Connect", "Connect an Account"]);
    expect(connects(fx.homes.noConnect)).toEqual(["Connect an Account"]);
    expect(connects(fx.homes.noGrants)).toEqual([]);
    expect(JSON.stringify(homeView(<Home home={fx.homes.noConnect} />).view)).toContain(
      "Not connected. An admin can add it.",
    );
  });

  it("says when nothing needs the person", () => {
    const { view } = homeView(<Home home={fx.homes.empty} />);
    expect(JSON.stringify(view.blocks)).toContain("Nothing needs you right now.");
    expect(elements(view.blocks).some((e) => e.action_id?.startsWith(ActionId.open))).toBe(false);
  });
});
