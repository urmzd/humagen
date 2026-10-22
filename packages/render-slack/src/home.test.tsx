import { Home } from "@warp/components";
import { ActionId, ANY_AGENT, HOME_ID, type HomeModel } from "@warp/core";
import * as fx from "@warp/core/fixtures";
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
      notifications: {
        items: Array.from({ length: 60 }, (_, i) => ({
          kind: "inbox" as const,
          id: `w${i}`,
          from: "docs-bot",
          summary: "wants to post a reply in #support.",
          at: "2026-10-13T12:00:00Z",
        })),
      },
    };
    const { view, warnings } = homeView(<Home home={long} />);
    expect(view.blocks.length).toBeLessThanOrEqual(LIMITS.blocks.home);
    for (const b of view.blocks.filter((b) => b.type === "header")) {
      expect((b.text as { text: string }).text.length).toBeLessThanOrEqual(LIMITS.header);
    }
    for (const e of elements(view.blocks).filter((e) => e.type === "button")) {
      expect(e.text?.text.length).toBeLessThanOrEqual(LIMITS.buttonText);
    }
    // Eight rows, then a count: the rest live in the inbox.
    expect(JSON.stringify(view.blocks)).toContain("52 more in the inbox");
    expect(warnings).toEqual(["header clipped from 214 to 150 chars"]);
  });

  it("shows three buttons, and Ask a Question asks for words in a form", () => {
    const { view } = homeView(<Home home={fx.home} />);
    const start = view.blocks.find((b) => b.block_id === "home:start");
    const buttons = (start?.elements as Element[]).map((e) => e.text?.text);
    expect(buttons).toEqual(["Ask a Question", "Build an Agent", "Search Context"]);
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
    expect(JSON.stringify(view.blocks)).toContain("Nothing needs you right now");
    expect(elements(view.blocks).some((e) => e.action_id?.startsWith(ActionId.open))).toBe(false);
  });
});

/** The section text of each notification row, in order. */
function notificationRows(blocks: Block[]): string[] {
  const start = blocks.findIndex(
    (b) => b.type === "header" && (b.text as { text: string }).text === "Needs you",
  );
  const rows: string[] = [];
  for (const b of blocks.slice(start + 1)) {
    if (b.type !== "section") break;
    rows.push((b.text as { text: string }).text);
  }
  return rows;
}

describe("Home notifications on Slack", () => {
  it("orders critical alerts, then the inbox, then other alerts, newest first", () => {
    const rows = notificationRows(homeView(<Home home={fx.homes.default} />).view.blocks);
    expect(rows.map((r) => r.split("\n")[0])).toEqual([
      "*deploy-failures* fired: a run of deployer failed.",
      "*docs-bot* wants to post a reply in #support.",
      "*author* drafted a change to the triage agent.",
      "*slow-runs* fired: a run of triage took 94s.",
    ]);
  });

  it("gives each row the item's console address, written out, and says severity in words", () => {
    const [alert, inbox] = notificationRows(homeView(<Home home={fx.homes.default} />).view.blocks);
    expect(alert).toContain("Critical alert · <!date^");
    // Slack escapes "&" in what it shows, and shows it as "&".
    const url = "https://example.com/console/?view=alerts&alert=al-deploy-failed";
    expect(alert).toContain(`<${url}|${url.replace("&", "&amp;")}>`);
    expect(inbox).toContain(
      "<https://example.com/console/?view=inbox&message=msg-docs-reply|https://example.com/console/?view=inbox&amp;message=msg-docs-reply>",
    );
    // A linked row needs no button: the address is the way in.
    expect(
      elements(homeView(<Home home={fx.homes.default} />).view.blocks).some((e) =>
        e.action_id?.startsWith(ActionId.open),
      ),
    ).toBe(false);
  });

  it("shows items without a link when the host has no public address, and Review stays for the inbox", () => {
    const { view } = homeView(<Home home={fx.homes.unlinked} />);
    const rows = notificationRows(view.blocks);
    expect(rows).toHaveLength(4);
    for (const r of rows) expect(r).not.toContain("<http");
    const reviews = elements(view.blocks).filter((e) => e.action_id?.startsWith(ActionId.open));
    expect(
      reviews.map(
        (e) => decodeSlackAction(e as { action_id: string; value: string })?.payload.target,
      ),
    ).toEqual(["msg-docs-reply", "msg-triage-revision"]);
  });

  it("never links a relative address, which Slack could not open", () => {
    const relative: HomeModel = {
      ...fx.home,
      notifications: {
        items: fx.home.notifications.items.map((n) => ({
          ...n,
          url: `/?view=inbox&message=${n.id}`,
        })),
        inbox: { total: 20, url: "/?view=inbox" },
      },
    };
    const text = JSON.stringify(homeView(<Home home={relative} />).view.blocks);
    expect(text).not.toContain("/?view=");
    expect(text).toContain("18 more in the inbox");
  });

  it("caps the list and links the rest of each kind to its page", () => {
    const { view, warnings } = homeView(<Home home={fx.homes.overflow} />);
    expect(warnings).toEqual([]);
    expect(notificationRows(view.blocks)).toHaveLength(8);
    const text = JSON.stringify(view.blocks);
    expect(text).toContain("<https://example.com/console/?view=inbox|8 more in the inbox>");
    expect(text).toContain("<https://example.com/console/?view=alerts|4 more open alerts>");
  });

  it("shows only alerts when the inbox is clear", () => {
    const rows = notificationRows(homeView(<Home home={fx.homes.alertsOnly} />).view.blocks);
    expect(rows.every((r) => r.includes("alert ·"))).toBe(true);
    expect(rows).toHaveLength(2);
  });

  it("keeps the greeting as Slack's header block", () => {
    const [first] = homeView(<Home home={fx.home} />).view.blocks.filter(
      (b) => b.type === "header",
    );
    expect((first?.text as { text: string }).text).toBe("Hi Ada, I'm Warp.");
  });
});

/** The section blocks under a heading, up to the next heading or divider. */
function under(blocks: Block[], heading: string): Block[] {
  const start = blocks.findIndex(
    (b) => b.type === "header" && (b.text as { text: string }).text === heading,
  );
  if (start === -1) return [];
  const out: Block[] = [];
  for (const b of blocks.slice(start + 1)) {
    if (b.type === "header" || b.type === "divider") break;
    out.push(b);
  }
  return out;
}

const textOf = (b: Block) =>
  b.type === "section"
    ? (b.text as { text: string }).text
    : ((b.elements as { text: string }[] | undefined) ?? []).map((e) => e.text).join(" ");

describe("What's new and Your projects on Slack", () => {
  it("shows what's new unread first, saying so in words, each with an Open button to where it lives", () => {
    const rows = under(homeView(<Home home={fx.home} />).view.blocks, "What's new");
    expect(rows.map((r) => textOf(r).split("\n")[0])).toEqual([
      "*The Field offsite moves to Lisbon, in May*",
      "Weekly recap: three renewals closed",
    ]);
    expect(textOf(rows[0] as Block)).toContain("Unread · Posted in Field engineering · <!date^");
    expect(textOf(rows[1] as Block)).not.toContain("Unread");
    const open = rows[0]?.accessory as { text: { text: string }; url: string; action_id: string };
    expect(open.text.text).toBe("Open");
    expect(open.url).toBe("https://example.com/console/projects/Field?message=m-offsite");
    // The link is Slack's to open: the click carries nothing for the app to do.
    expect(decodeSlackAction(open)).toBeNull();
  });

  it("shows the person's projects, the busiest first, with their kind and where they sit", () => {
    const rows = under(homeView(<Home home={fx.home} />).view.blocks, "Your projects");
    expect(rows.map((r) => textOf(r).split("\n"))).toEqual([
      ["*Field engineering*", expect.stringMatching(/^Team · 1 running · active <!date\^\d+\^/)],
      ["*Globex*", expect.stringMatching(/^Account · in Field engineering · active <!date/)],
      ["*Launch*", "Team"],
    ]);
    expect((rows[2]?.accessory as { url: string }).url).toBe(
      "https://example.com/console/projects/Launch",
    );
  });

  it("shows five announcements and eight projects at most, and counts the rest of the projects", () => {
    const { view, warnings } = homeView(<Home home={fx.homes.overflow} />);
    expect(warnings).toEqual([]);
    expect(under(view.blocks, "What's new")).toHaveLength(5);
    const projects = under(view.blocks, "Your projects");
    expect(projects.filter((b) => b.type === "section")).toHaveLength(8);
    expect(textOf(projects.at(-1) as Block)).toBe(
      "<https://example.com/console/projects|4 more in Projects>",
    );
    // The two projects where nothing happened come last.
    expect(JSON.stringify(projects)).not.toContain("Account 11");
  });

  it("says when nothing is new and the person is in no project", () => {
    const blocks = homeView(<Home home={fx.homes.empty} />).view.blocks;
    expect(under(blocks, "What's new").map(textOf)).toEqual(["Nothing new from your projects."]);
    expect(under(blocks, "Your projects").map(textOf)).toEqual(["You are not in any project yet."]);
  });

  it("offers no Open button where the host gave no absolute address, and leaves out a section it leaves out", () => {
    const blocks = homeView(<Home home={fx.homes.unlinked} />).view.blocks;
    for (const b of [...under(blocks, "What's new"), ...under(blocks, "Your projects")]) {
      expect(b.accessory).toBeUndefined();
    }
    const relative: HomeModel = {
      ...fx.home,
      updates: fx.home.updates?.map((u) => ({ ...u, url: `/projects/${u.project}` })),
      projects: undefined,
    };
    const shown = homeView(<Home home={relative} />).view.blocks;
    expect(JSON.stringify(shown)).not.toContain('"/projects/');
    expect(under(shown, "Your projects")).toEqual([]);
    expect(
      shown.some((b) => (b.text as { text?: string } | undefined)?.text === "Your projects"),
    ).toBe(false);
  });
});

describe("the Home the Slack app shows a person it reads for", () => {
  const { view, warnings } = homeView(<Home home={fx.homes.readFor} />);

  it("starts a question or an agent there, and searches in the console", () => {
    expect(warnings).toEqual([]);
    const start = view.blocks.find((b) => b.block_id === "home:start");
    const [ask, build, search] = start?.elements as (Element & { url?: string; value?: string })[];
    expect(decodeSlackAction(ask as { action_id: string; value: string })?.id).toBe(
      ActionId.homeAsk,
    );
    expect(decodeSlackAction(build as { action_id: string; value: string })).toEqual({
      id: ActionId.homeStart,
      payload: { turnId: HOME_ID, target: "build" },
    });
    expect(search?.text?.text).toBe("Search Context");
    expect(search?.url).toBe("https://example.com/console/context");
    expect(search?.value).toBeUndefined();
  });

  it("shows no access it cannot know and no connections, only the way to the console", () => {
    const text = JSON.stringify(view.blocks);
    expect(text).not.toContain("Signed in as");
    expect(text).not.toContain("Connections");
    expect(textOf(view.blocks.at(-1) as Block)).toBe(
      "<https://example.com/console|Open the console> for runs, schedules and access.",
    );
    expect(view.blocks.filter((b) => b.type === "divider").length).toBe(4);
  });

  it("stays within the Home budget with every section full", () => {
    const full: HomeModel = {
      ...fx.homes.readFor,
      notifications: fx.homes.overflow.notifications,
      updates: fx.homes.overflow.updates,
      projects: fx.homes.overflow.projects,
    };
    const { view, warnings } = homeView(<Home home={full} />);
    expect(warnings).toEqual([]);
    expect(view.blocks.length).toBeLessThanOrEqual(LIMITS.blocks.home);
  });
});
