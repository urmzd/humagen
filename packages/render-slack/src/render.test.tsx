import { TurnView } from "@agent-ui/components";
import { ActionId, decodeAction } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { Callout, Markdown, Only, Text, useSurface } from "@agent-ui/primitives";
import { describe, expect, it } from "vitest";
import { actionIdBase, LIMITS, renderToSlack, toMrkdwn } from "./index";

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
    expect(out).toMatchSnapshot();
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

describe("toMrkdwn", () => {
  it("converts the standard subset", () => {
    expect(toMrkdwn("**bold** *it* ~~gone~~ [docs](https://x.dev/a?b=1&c=2) <tag>")).toBe(
      "*bold* _it_ ~gone~ <https://x.dev/a?b=1&c=2|docs> &lt;tag&gt;",
    );
  });
});
