import { ApprovalCard, defaultCopy, TurnView } from "@humagen/components";
import { ActionId, decodeAction, type Turn } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { Callout, Markdown, Only, Sources, Text, useSurface } from "@humagen/primitives";
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

  it("links the conversation once, with an arrow at the end of the answer or the disclaimer", () => {
    const isTurn = (v: unknown): v is Turn =>
      typeof v === "object" && v !== null && "tasks" in v && "agent" in v;
    const turns = Object.entries(fx).filter((e): e is [string, Turn] => isTurn(e[1]));
    expect(turns.length).toBeGreaterThan(20);
    for (const [name, turn] of turns) {
      for (const streamed of [false, true]) {
        const out = renderToSlack(<TurnView turn={turn} streamed={streamed} />);
        const blocks = out.pages.at(-1) ?? [];
        const all = JSON.stringify(out.pages);
        const where = `${name}${streamed ? " (streamed)" : ""}`;
        // The old line is gone, and the arrow, as text, links the session exactly once.
        expect(all, where).not.toContain("Open Conversation");
        expect(
          all.split(`<${turn.sessionUrl}|\u2197\uFE0E>`).length - 1,
          where,
        ).toBeLessThanOrEqual(1);
        const inAnswer = all.split(`[\u2197\uFE0E](${turn.sessionUrl})`).length - 1;
        const inFooter = all.split(`<${turn.sessionUrl}|\u2197\uFE0E>`).length - 1;
        expect(inAnswer + inFooter, where).toBe(1);
        // The message still ends with the disclaimer, and no block holds the link alone.
        const disclaimer = blocks.at(-1) as { type: string; elements: { text: string }[] };
        expect(disclaimer.type, where).toBe("context");
        expect(disclaimer.elements, where).toHaveLength(1);
        expect(disclaimer.elements[0]?.text, where).toContain("is AI");
        if (inFooter) {
          expect(disclaimer.elements[0]?.text, where).toMatch(/them\. <[^|]+\|\u2197\uFE0E>$/);
        }
        // Notifications read the fallback text: never a stray arrow.
        expect(out.text, where).not.toContain("\u2197");
        if (turn.citations.length > 0) {
          expect(JSON.stringify(blocks.at(-2)), where).toMatch(/"\[\d+\] /);
        }
      }
    }
  });

  it("ends a finished answer's last line with the arrow, unless it ends in code, a table or a rule", () => {
    const session = "https://example.com/sessions/turn-1";
    const arrow = `[\u2197\uFE0E](${session})`;
    const answer = (text: string, status: Turn["status"] = "done") =>
      renderToSlack(<TurnView turn={{ ...fx.doneTurn, citations: [], status, text }} />);
    const last = (text: string, status?: Turn["status"]) => {
      const { blocks } = answer(text, status);
      return {
        markdown: blocks.find((b) => b.type === "markdown") as { text: string } | undefined,
        footer: JSON.stringify(blocks.at(-1)),
      };
    };
    for (const text of [
      "One line.",
      "Three:\n\n- a\n- b\n- c",
      "# Title\n\nWords [x](https://x.test).",
    ]) {
      const { markdown, footer } = last(text);
      expect(markdown?.text, text).toBe(`${text} ${arrow}`);
      expect(footer, text).not.toContain("\u2197");
    }
    for (const text of [
      "Run:\n\n```bash\nmake\n```",
      "Open fence:\n\n```\nstill code",
      "| a | b |\n| --- | --- |\n| 1 | 2 |",
      "Above\n\n---",
      "Indented:\n\n    code",
      // Found in review: endings a trailing link would change.
      "Summary\n=======",
      "Summary\n--",
      "See [docs][1].\n\n[1]: https://example.test/d",
      "a | b\n--|--\n1 | 2",
      "| a | b\n|---|---\n| 1 | 2",
      "~~~\n```\nstill code",
      "````\n```\nstill code\n```",
    ]) {
      const { markdown, footer } = last(text);
      expect(markdown?.text, text).toBe(text);
      expect(footer, text).toContain(`<${session}|\u2197\uFE0E>`);
    }
    // Trailing blank lines or spaces never leave the arrow on a line of its own.
    for (const text of ["Answer.\n", "Answer.\n\n", "Answer.   ", "Answer.\n  \n"]) {
      expect(last(text).markdown?.text, JSON.stringify(text)).toBe(`Answer. ${arrow}`);
    }
    // An answer cut short by a failure or a stop is not ended with it: the disclaimer is.
    for (const status of ["failed", "stopped", "working"] as const) {
      const { markdown, footer } = last("Cut short", status);
      expect(markdown?.text ?? "", status).not.toContain("\u2197");
      expect(footer, status).toContain(`<${session}|\u2197\uFE0E>`);
    }
    // Streamed, the host decides: it ended the stream with the arrow, or the disclaimer has it.
    const linked = renderToSlack(<TurnView turn={fx.doneTurn} streamed linked />);
    expect(JSON.stringify(linked.blocks)).not.toContain("\u2197");
    const unlinked = renderToSlack(<TurnView turn={fx.doneTurn} streamed />);
    expect(JSON.stringify(unlinked.blocks.at(-1))).toContain(`<${session}|\u2197\uFE0E>`);
    // A queued turn has no disclaimer yet: the arrow is its line.
    const queued = renderToSlack(
      <TurnView turn={{ ...fx.doneTurn, status: "queued", text: "" }} />,
    );
    expect(queued.blocks.at(-1)).toEqual({
      type: "context",
      elements: [{ type: "mrkdwn", text: `<${session}|\u2197\uFE0E>` }],
    });
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

  it("offers a run held at a limit a grant and a stop, with the limit, and nothing else", () => {
    const { blocks, warnings } = renderToSlack(
      <ApprovalCard
        turnId="r1"
        approval={{
          type: "approval",
          id: "q-b",
          summary: "lead is held: the workspace has reached its limit of spend a day.",
          tool: "",
          budget: {
            scope: "the workspace",
            limit: "spend",
            used: "$9.00",
            max: "$5.00",
            grant: "$10.00",
          },
          decision: { kind: "pending" },
        }}
      />,
    );
    expect(warnings).toEqual([]);
    const actions = blocks.find((b) => b.type === "actions") as unknown as {
      elements: { action_id: string; value: string; text: { text: string } }[];
    };
    expect(actions.elements.map((e) => [actionIdBase(e.action_id), e.text.text])).toEqual([
      [ActionId.approve, "Grant $10.00"],
      [ActionId.deny, "Stop Run"],
    ]);
    expect(decodeAction(actions.elements[0]?.value ?? "")).toEqual({ turnId: "r1", target: "q-b" });
    const shown = JSON.stringify(blocks);
    expect(shown).toContain("$5.00 (the workspace)");
    expect(shown).toContain("$9.00");

    const stopped = renderToSlack(
      <ApprovalCard
        turnId="r1"
        approval={{
          type: "approval",
          id: "q-b",
          summary: "lead is held.",
          tool: "",
          budget: {
            scope: "the workspace",
            limit: "spend",
            used: "$9.00",
            max: "$5.00",
            grant: "$10.00",
          },
          decision: {
            kind: "denied",
            by: "Ana",
            at: "2026-10-30T00:00:00Z",
            reason: "Not this month",
          },
        }}
      />,
    );
    expect(JSON.stringify(stopped.blocks)).toContain("Stopped at its limit.");
    expect(JSON.stringify(stopped.blocks)).toContain("Stopped by Ana");
    expect(stopped.blocks.some((b) => b.type === "actions")).toBe(false);
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

  it("never lets the arrow push a posted answer into another message", () => {
    const session = "https://example.com/sessions/turn-1";
    const answer = (text: string) =>
      renderToSlack(<TurnView turn={{ ...fx.doneTurn, citations: [], text }} />);
    const markdown = (out: ReturnType<typeof answer>) =>
      out.pages.flat().filter((b) => b.type === "markdown") as { text: string }[];
    // One paragraph just under the limit: alone it is one message, and so it stays.
    const near = "abcdefghi ".repeat(1200).slice(0, 11_998).trimEnd().replace(/ $/, "x");
    const full = answer(near);
    expect(full.pages).toHaveLength(1);
    expect(markdown(full).map((b) => b.text)).toEqual([near]);
    expect(JSON.stringify(full.blocks.at(-1))).toContain(`<${session}|\u2197\uFE0E>`);
    // An answer with room for it still ends with it.
    const roomy = answer(near.slice(0, 11_000));
    expect(roomy.pages).toHaveLength(1);
    expect(markdown(roomy).at(-1)?.text.endsWith(`[\u2197\uFE0E](${session})`)).toBe(true);
    // No markdown block is ever over the limit.
    for (const b of [...markdown(full), ...markdown(roomy)]) {
      expect(b.text.length).toBeLessThanOrEqual(LIMITS.markdownTotal);
    }
  });

  it("ends a reply split over messages once, on its last page, with the disclaimer last", () => {
    const { pages, warnings } = renderToSlack(<TurnView turn={fx.longTurn} />);
    expect(pages.length).toBeGreaterThan(1);
    expect(warnings).toHaveLength(1);
    // longTurn outgrows one message: the arrow ends the disclaimer, once, on the last page.
    const footer = "<https://example.com/sessions/turn-1|\u2197\uFE0E>";
    expect(JSON.stringify(pages).match(/sessions\/turn-1/g)).toHaveLength(1);
    expect(pages.at(-1)?.at(-1)).toEqual({
      type: "context",
      elements: [
        {
          type: "mrkdwn",
          text: `Assistant is AI and can make mistakes. Double-check its answers before you rely on them. ${footer}`,
        },
      ],
    });
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

it("does not decorate Slack references with private retrieval facts without delivery eligibility", () => {
  const reference = {
    id: "private",
    title: "Existing private reference",
    url: "https://private.example.com/reference",
  };
  const before = renderToSlack(<Sources items={[reference]} labels={defaultCopy.citation} />);
  const enriched = renderToSlack(
    <Sources
      items={[
        { ...reference, mode: "indexed", copyAt: "2026-10-20T12:00:00Z", refreshFailed: true },
      ]}
      labels={defaultCopy.citation}
    />,
  );
  expect(enriched).toEqual(before);
  const text = JSON.stringify(enriched);
  expect(text).not.toContain("2026-10-20");
  expect(text).not.toContain(defaultCopy.citation.lastGoodCopy);
  expect(text).not.toContain(defaultCopy.citation.indexed);
});
