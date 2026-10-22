import { TurnView } from "@humagen/components";
import { ActionId, decodeAction, type Turn } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { Callout, Markdown, Only, Text, useSurface } from "@humagen/primitives";
import { describe, expect, it } from "vitest";
import {
  actionIdBase,
  decodeFeedbackModal,
  feedbackModal,
  LIMITS,
  renderToSlack,
  toMrkdwn,
} from "./index";

describe("renderToSlack", () => {
  it.each([
    ["streaming", fx.streamingTurn],
    ["done", fx.doneTurn],
    ["waiting", fx.waitingTurn],
    ["failed", fx.failedTurn],
  ])("renders the %s turn", (_name, turn) => {
    const out = renderToSlack(<TurnView turn={turn} />);
    expect(out.warnings).toEqual([]);
    expect(out.text.length).toBeGreaterThan(0);
    const { pages, ...message } = out;
    expect(pages).toEqual([out.blocks]);
    expect(message).toMatchSnapshot();
  });

  it("ends every turn with references, the one link, then the disclaimer", () => {
    const isTurn = (v: unknown): v is Turn =>
      typeof v === "object" && v !== null && "tasks" in v && "agent" in v;
    const turns = Object.entries(fx).filter((e): e is [string, Turn] => isTurn(e[1]));
    expect(turns.length).toBeGreaterThan(20);
    for (const [name, turn] of turns) {
      for (const streamed of [false, true]) {
        const blocks =
          renderToSlack(<TurnView turn={turn} streamed={streamed} />).pages.at(-1) ?? [];
        const [link, disclaimer] = blocks.slice(-2) as {
          type: string;
          elements: { text: string }[];
        }[];
        const where = `${name}${streamed ? " (streamed)" : ""}`;
        expect(disclaimer, where).toEqual({
          type: "context",
          elements: [{ type: "mrkdwn", text: expect.stringContaining("is AI") }],
        });
        expect(link?.type, where).toBe("context");
        expect(link?.elements, where).toHaveLength(1);
        // The block is the session link and nothing else: no label, no prefix.
        expect(link?.elements[0]?.text, where).toBe(`<${turn.sessionUrl}|${turn.sessionUrl}>`);
        // The link appears nowhere else in the message.
        expect(JSON.stringify(blocks).split(`|${turn.sessionUrl}>`), where).toHaveLength(2);
        if (turn.citations.length > 0) {
          expect(JSON.stringify(blocks.at(-3)), where).toMatch(/"\[\d+\] /);
        }
      }
    }
  });

  it("falls back to the source's page when there is no session, and to nothing when there is neither", () => {
    const page = "https://example.com/console/payments";
    const sourceOnly = renderToSlack(<TurnView turn={{ ...fx.doneTurn, sessionUrl: undefined }} />);
    expect(sourceOnly.blocks.at(-2)).toEqual({
      type: "context",
      elements: [{ type: "mrkdwn", text: `<${page}|${page}>` }],
    });
    const neither = renderToSlack(
      <TurnView turn={{ ...fx.doneTurn, sessionUrl: undefined, source: undefined }} />,
    );
    expect(JSON.stringify(neither.blocks.at(-1))).toContain("is AI");
    expect(JSON.stringify(neither.blocks.at(-2))).toContain("[1] ");
  });

  it("emits approval buttons that decode to the shared action contract", () => {
    const { blocks } = renderToSlack(<TurnView turn={fx.waitingTurn} />);
    const actions = blocks.find((b) => b.type === "actions") as unknown as {
      elements: { action_id: string; value: string; confirm?: unknown; style?: string }[];
    };
    const byId = (id: string) => actions.elements.find((e) => actionIdBase(e.action_id) === id);
    const approve = byId(ActionId.approve);
    const cancel = byId(ActionId.deny);
    expect(actions.elements.map((e) => actionIdBase(e.action_id))).toEqual([
      ActionId.approve,
      ActionId.edit,
      ActionId.ask,
      ActionId.deny,
    ]);
    // Declining is the safe choice, so it stays unstyled.
    expect(cancel).not.toHaveProperty("style");
    expect(decodeAction(approve?.value ?? "")).toEqual({ turnId: "turn-1", target: "a1" });
    expect(approve?.confirm).toBeDefined(); // destructive
  });

  it("uses an alert block only in modals", () => {
    const callout = <Callout tone="warn" title="Heads up" text="Read-only mode." />;
    expect(renderToSlack(callout).blocks[0]?.type).toBe("section");
    expect(renderToSlack(callout, { placement: "modal" }).blocks[0]).toEqual({
      type: "alert",
      text: { type: "mrkdwn", text: "*Heads up*: Read-only mode." },
      level: "warning",
    });
  });

  it("enforces the block limit with a visible note", () => {
    const many = Array.from({ length: 60 }, (_, i) => `line ${i}`).map((line) => (
      <Text key={line} text={line} />
    ));
    const out = renderToSlack(many);
    expect(out.blocks).toHaveLength(LIMITS.blocks.message);
    expect(out.warnings[0]).toMatch(/exceed 50/);
  });

  it("continues in follow-up messages instead of dropping blocks", () => {
    const many = Array.from({ length: 120 }, (_, i) => `line ${i}`).map((line) => (
      <Text key={line} text={line} />
    ));
    const out = renderToSlack(many);
    expect(out.pages).toHaveLength(3);
    expect(out.pages[0]).toBe(out.blocks);
    for (const page of out.pages) expect(page.length).toBeLessThanOrEqual(LIMITS.blocks.message);
    const sections = out.pages.flat().filter((b) => b.type === "section");
    expect(sections).toHaveLength(120);
    expect(renderToSlack(many.slice(0, 50)).pages).toHaveLength(1);
  });

  it("closes with the source and session once, then the disclaimer", () => {
    const session = "https://example.com/sessions/turn-1";
    const { pages, warnings } = renderToSlack(<TurnView turn={fx.longTurn} />);
    expect(pages.length).toBeGreaterThan(1);
    expect(warnings).toHaveLength(1);
    // Once, on the last page only, directly above the disclaimer.
    const link = `<${session}|${session}>`;
    expect(JSON.stringify(pages).split(link)).toHaveLength(2);
    expect(JSON.stringify(pages.slice(0, -1))).not.toContain(link);
    expect(pages.at(-1)?.at(-2)).toEqual({
      type: "context",
      elements: [{ type: "mrkdwn", text: link }],
    });
    expect(pages.at(-1)?.at(-1)).toEqual({
      type: "context",
      elements: [
        {
          type: "mrkdwn",
          text: "Assistant is AI and can make mistakes. Double-check its answers before you rely on them.",
        },
      ],
    });
    // Streamed or not, finished or failed: the link is there, after everything but the disclaimer.
    const streamed = renderToSlack(<TurnView turn={fx.doneTurn} streamed />).blocks;
    expect(JSON.stringify(streamed.at(-2))).toContain(link);
    for (const turn of [fx.failedTurn, fx.stoppedTurn, fx.waitingTurn]) {
      const { blocks } = renderToSlack(<TurnView turn={turn} streamed />);
      const at = blocks.findIndex((b) => JSON.stringify(b).includes(link));
      expect(at).toBeGreaterThanOrEqual(blocks.length - 2);
      expect(JSON.stringify(blocks).split(link)).toHaveLength(2);
    }
    expect(JSON.stringify(streamed.at(-1))).toContain("Assistant is AI");
    // A host's own wording replaces the default.
    const custom = renderToSlack(<TurnView turn={{ ...fx.doneTurn, disclaimer: "Beta." }} />);
    expect(JSON.stringify(custom.blocks.at(-1))).toContain("Beta.");
  });

  it("shares the markdown budget across blocks", () => {
    const big = "x".repeat(8000);
    const out = renderToSlack(
      <>
        <Markdown text={big} />
        <Markdown text={big} />
      </>,
    );
    const total = out.blocks.reduce((n, b) => n + String(b.text ?? "").length, 0);
    expect(total).toBeLessThanOrEqual(LIMITS.markdownTotal);
    expect(out.warnings).toHaveLength(1);
  });

  it("honours Only and useSurface", () => {
    function Probe() {
      return <Text text={`on ${useSurface().surface}`} />;
    }
    const out = renderToSlack(
      <>
        <Only surface="web">
          <Text text="web only" />
        </Only>
        <Probe />
      </>,
    );
    expect(out.blocks).toEqual([{ type: "section", text: { type: "mrkdwn", text: "on slack" } }]);
  });
});

describe("feedbackModal", () => {
  it("ignores malformed modal metadata without losing the event handler", () => {
    for (const private_metadata of [
      "{",
      "null",
      "[]",
      '{"turnId":"t","rating":"invalid"}',
      '{"turnId":4,"rating":"up"}',
    ]) {
      expect(
        decodeFeedbackModal({
          callback_id: "aui.feedback.modal",
          private_metadata,
          state: { values: { comment: { value: { value: "hello" } } } },
        }),
      ).toBeNull();
    }
  });

  it("carries the turn and rating, and asks only optional questions", () => {
    const view = feedbackModal({ turnId: "turn-1", rating: "down" });
    expect(JSON.parse(view.private_metadata)).toEqual({ turnId: "turn-1", rating: "down" });
    expect(view.title.text).toBe("What went wrong?");
    expect(view.blocks.at(-1)).toMatchObject({ element: { max_length: 3000 } });
    expect(view.close.text).toBe("Skip");
    expect(view.blocks.map((b) => [b.block_id, b.optional])).toEqual([
      ["reason", true],
      ["comment", true],
    ]);
    // A thumbs-up has nothing to choose a reason for.
    expect(feedbackModal({ turnId: "t", rating: "up" }).blocks.map((b) => b.block_id)).toEqual([
      "comment",
    ]);
  });

  it("decodes a submission into the comment action, and an empty one into nothing", () => {
    const view = feedbackModal({ turnId: "turn-1", rating: "down" });
    const submitted = {
      ...view,
      state: {
        values: {
          reason: { value: { selected_option: { value: "wrong" } } },
          comment: { value: { value: " Wrong deploy " } },
        },
      },
    };
    expect(decodeFeedbackModal(submitted)).toEqual({
      id: ActionId.feedbackComment,
      payload: { turnId: "turn-1", target: "down", text: "Wrong deploy", option: "wrong" },
    });
    const empty = { ...view, state: { values: { comment: { value: { value: null } } } } };
    expect(decodeFeedbackModal(empty)).toBeNull();
    expect(decodeFeedbackModal({ callback_id: "something.else" })).toBeNull();
  });
});

describe("toMrkdwn", () => {
  it("converts the standard subset", () => {
    expect(toMrkdwn("**bold** *it* ~~gone~~ [docs](https://x.dev/a?b=1&c=2) <tag>")).toBe(
      "*bold* _it_ ~gone~ <https://x.dev/a?b=1&c=2|docs> &lt;tag&gt;",
    );
  });
});
