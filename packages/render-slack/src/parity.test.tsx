import {
  AgentList,
  defaultCopy,
  FailureAlert,
  MessageView,
  RequestView,
  TurnView,
} from "@vultus/components";
import { ActionId, decodeAction, type InboxMessage } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { Actions, Button, Checkboxes } from "@vultus/primitives";
import { describe, expect, it } from "vitest";
import {
  AMOUNT_MODAL,
  amountModal,
  askForm,
  decodeAmountModal,
  decodeAskForm,
  ENTRY,
  renderToSlack,
} from "./index";

const types = (blocks: { type: string }[]) => blocks.map((b) => b.type);
const mail = (id: string) => fx.mailMessages.find((m) => m.id === id) as InboxMessage;

describe("empty containers", () => {
  it("leaves out an actions block with nothing in it", () => {
    const out = renderToSlack(
      <>
        <Actions blockId="none">{false}</Actions>
        <Actions blockId="one">
          <Button
            label="Go"
            action={{ id: ActionId.retry, payload: { turnId: "t", target: "t" } }}
          />
        </Actions>
      </>,
    );
    expect(out.blocks).toEqual([expect.objectContaining({ type: "actions", block_id: "one" })]);
  });

  it("leaves out checkboxes with nothing to tick", () => {
    const out = renderToSlack(
      <Checkboxes name="g" options={[{ id: "a", label: "A", disabled: true }]} selected={[]} />,
    );
    expect(out.blocks).toEqual([]);
  });

  it("never emits an empty actions block for a limit with nothing offered as a button", () => {
    const base = mail("m-limit");
    const onlyRaise: InboxMessage = {
      ...base,
      parts: base.parts.map((p) => ({ ...p, allowed: [ActionId.raiseBudget] })),
    };
    for (const message of [base, onlyRaise, ...fx.mailMessages]) {
      for (const block of renderToSlack(<MessageView message={message} />).blocks) {
        if (block.type === "actions")
          expect((block.elements as unknown[]).length).toBeGreaterThan(0);
      }
    }
  });
});

describe("a limit", () => {
  const raiseButton = (blocks: Record<string, unknown>[]) =>
    blocks.find((b) =>
      (b.accessory as { action_id?: string } | undefined)?.action_id?.startsWith(
        ActionId.raiseBudget,
      ),
    ) as unknown as
      | { text: { text: string }; accessory: { value: string; text: { text: string } } }
      | undefined;

  it("offers Acknowledge and, for a host that opens amountModal, a raise to a typed amount", () => {
    const { blocks } = renderToSlack(<MessageView message={mail("m-limit")} />, {
      amountForm: true,
    });
    const json = JSON.stringify(blocks);
    expect(json).toContain(`"${defaultCopy.acknowledge}"`);
    expect(json).not.toContain(defaultCopy.allowOnce);
    const raise = raiseButton(blocks);
    expect(raise?.text.text).toContain(defaultCopy.newLimit);
    expect(raise?.text.text).toContain("More than $250.00");
    expect(raise?.accessory.text.text).toBe(defaultCopy.raiseLimit);
    // The click carries no amount: the host collects it in amountModal.
    expect(decodeAction(raise?.accessory.value ?? "")).toEqual({
      turnId: "m-limit",
      target: "spend",
    });
  });

  it("shows no Raise Limit button to a host that doesn't open the form", () => {
    const { blocks } = renderToSlack(<MessageView message={mail("m-limit")} />);
    expect(raiseButton(blocks)).toBeUndefined();
    expect(JSON.stringify(blocks)).toContain(`"${defaultCopy.acknowledge}"`);
  });

  it("offers no typed amount beside the amounts a part offers", () => {
    const { blocks } = renderToSlack(<MessageView message={mail("m-budget")} />, {
      amountForm: true,
    });
    expect(JSON.stringify(blocks)).not.toContain(defaultCopy.newLimit);
    expect(JSON.stringify(blocks)).toContain(defaultCopy.raiseTo("$75"));
  });

  it("collects the amount in a modal and sends the same action with it", () => {
    const action = { id: ActionId.raiseBudget, payload: { turnId: "m-limit", target: "spend" } };
    const view = amountModal({ action, unit: "usd", above: 250, max: 1000 });
    expect(view.callback_id).toBe(AMOUNT_MODAL);
    expect(view.blocks[0]?.label.text).toBe(defaultCopy.amountLabel(defaultCopy.newLimit, "usd"));
    expect(view.blocks[0]?.element).toMatchObject({
      type: "number_input",
      is_decimal_allowed: true,
      min_value: "250.01",
      max_value: "1000.00",
    });
    const submit = (value: string, v: { private_metadata?: string } = view) =>
      decodeAmountModal({ ...view, ...v, state: { values: { amount: { value: { value } } } } });
    expect(submit("400")).toEqual({ ...action, payload: { ...action.payload, option: "400" } });
    expect(submit("400.5")?.payload.option).toBe("400.5");
    for (const bad of ["250", "", "400.555", "1e3", "0x200", "-300", "+300", "1000.01", "abc"]) {
      expect(submit(bad), bad).toBeNull();
    }
    expect(submit("400", { private_metadata: "{not json" })).toBeNull();
    expect(submit("400", { private_metadata: '{"id":1}' })).toBeNull();
    expect(decodeAmountModal({ callback_id: "other" })).toBeNull();
    const runs = amountModal({ action, unit: "runs", above: 200 });
    expect(runs.blocks[0]?.element).toMatchObject({ is_decimal_allowed: false, min_value: "201" });
    const counted = (value: string) =>
      decodeAmountModal({ ...runs, state: { values: { amount: { value: { value } } } } });
    expect(counted("300")?.payload.option).toBe("300");
    expect(counted("300.5")).toBeNull();
  });
});

describe("askForm", () => {
  it("is the shared form, in copy's words, and its submission decodes", () => {
    const view = askForm();
    expect(view.callback_id).toBe(ENTRY.form);
    expect(view.title.text).toBe(defaultCopy.askForm.title);
    expect(view.blocks[0]?.label.text).toBe(defaultCopy.askForm.label);
    expect(view.submit.text).toBe(defaultCopy.askForm.send);
    expect(view.close.text).toBe(defaultCopy.askForm.cancel);
    const sent = decodeAskForm({
      ...view,
      state: { values: { text: { value: { value: "  why 5xx?  " } } } },
    });
    expect(sent).toEqual({ text: "why 5xx?", meta: { kind: "ask" } });
    expect(
      decodeAskForm({ ...view, state: { values: { text: { value: { value: " " } } } } }),
    ).toBeNull();
    const edit = askForm({ title: "Edit", meta: { kind: "edit", turnId: "t" }, initial: "draft" });
    expect(edit.blocks[0]?.element).toMatchObject({ initial_value: "draft" });
    expect(JSON.parse(edit.private_metadata)).toEqual({ kind: "edit", turnId: "t" });
  });
});

describe("RequestView", () => {
  it("names the person, keeps mentions, and escapes everything else they typed", () => {
    const named = renderToSlack(<RequestView by="Dana" text="is <b> & 5 > 4?" />);
    expect(named.blocks).toEqual([
      { type: "section", text: { type: "mrkdwn", text: "*Dana*: is &lt;b&gt; &amp; 5 &gt; 4?" } },
    ]);
    expect(named.text).toBe("Dana: is &lt;b&gt; &amp; 5 &gt; 4?");
    const mention = renderToSlack(
      <RequestView by="<@U0SANDBOX>" text="ask <@U0OTHER|dana> in <#C0GENERAL>" />,
    );
    expect(mention.blocks[0]?.text).toEqual({
      type: "mrkdwn",
      text: "<@U0SANDBOX>: ask <@U0OTHER|dana> in <#C0GENERAL>",
    });
  });

  it.each([
    "<!channel>",
    "<!here>",
    "<!everyone>",
    "<!subteam^S0TEAM>",
    "<!subteam^S0TEAM|@oncall>",
    "<https://evil.example.com|your bank>",
    "<mailto:a@example.com|mail>",
  ])("never lets %s ping or link, in the words, the name or the fallback", (token) => {
    const out = renderToSlack(<RequestView by={token} text={`hi ${token}`} />);
    const shown = JSON.stringify(out.blocks);
    expect(shown).not.toContain(token);
    expect(out.text).not.toContain(token);
    expect(out.text).not.toMatch(/<[!h]/);
  });

  it("keeps a name's formatting characters from breaking the bold", () => {
    const out = renderToSlack(<RequestView by="a*b_c" text="x" />);
    expect((out.blocks[0]?.text as { text: string }).text).toMatch(/^\*a[^*_]b[^*_]c\*: x$/);
  });

  it("escapes what documents bring into sources and code titles", () => {
    const out = renderToSlack(
      <TurnView
        turn={{
          ...fx.doneTurn,
          citations: [
            {
              id: "c",
              title: "<!channel> notes",
              source: "<!here>",
              url: "https://example.com/a?b=1&c=2",
            },
          ],
        }}
      />,
    );
    const json = JSON.stringify(out.blocks);
    expect(json).not.toContain("<!channel>");
    expect(json).not.toContain("<!here>");
    expect(json).toContain("<https://example.com/a?b=1&amp;c=2|&lt;!channel&gt; notes>");
  });
});

describe("AgentList", () => {
  it("lists each agent by handle with what it is for", () => {
    const { blocks, warnings } = renderToSlack(<AgentList agents={fx.agentList} />);
    expect(warnings).toEqual([]);
    const text = JSON.stringify(blocks);
    expect(text).toContain("`@oncall`");
    expect(text).toContain(defaultCopy.agentList.noDescription);
    expect(text).toContain(defaultCopy.agentList.hint);
    expect(renderToSlack(<AgentList agents={[]} />).text).toBe(defaultCopy.agentList.empty);
  });
});

describe("a run followed quietly", () => {
  it("says it is still working, in the final layout too, and only while it runs", () => {
    const line = {
      type: "context",
      elements: [{ type: "mrkdwn", text: defaultCopy.stillWorking("Assistant") }],
    };
    for (const streamed of [false, true]) {
      expect(
        renderToSlack(<TurnView turn={fx.quietTurn} streamed={streamed} />).blocks,
      ).toContainEqual(line);
    }
    expect(renderToSlack(<TurnView turn={fx.streamingTurn} />).blocks).not.toContainEqual(line);
    const ended = { ...fx.quietTurn, status: "done" as const };
    expect(renderToSlack(<TurnView turn={ended} />).blocks).not.toContainEqual(line);
  });
});

describe("feedback and report", () => {
  it("says a rating already given, since the buttons can't show it", () => {
    const { blocks } = renderToSlack(<TurnView turn={fx.ratedTurn} />);
    const i = types(blocks).indexOf("context_actions");
    expect(blocks[i + 1]).toEqual({
      type: "context",
      elements: [{ type: "mrkdwn", text: defaultCopy.rated.up }],
    });
    const unrated = renderToSlack(<TurnView turn={fx.doneTurn} />).blocks;
    expect(JSON.stringify(unrated)).not.toContain(defaultCopy.rated.up);
  });

  it("leaves out feedback and Report Issue when the host can't act on them", () => {
    const done = renderToSlack(<TurnView turn={fx.doneTurn} capabilities={{ feedback: false }} />);
    expect(types(done.blocks)).not.toContain("context_actions");
    const internal = { ...fx.failures.internal, retryable: true };
    const failed = { ...fx.failedTurn, failure: internal };
    const withReport = JSON.stringify(renderToSlack(<TurnView turn={failed} />).blocks);
    expect(withReport).toContain(defaultCopy.report);
    const without = JSON.stringify(
      renderToSlack(<TurnView turn={failed} capabilities={{ report: false }} />).blocks,
    );
    expect(without).not.toContain(defaultCopy.report);
    expect(without).toContain(defaultCopy.retry);
    // Not retryable and no report: nothing to offer, and no empty row.
    const bare = renderToSlack(
      <FailureAlert turnId="t" failure={{ ...internal, retryable: false }} report={false} />,
    );
    expect(types(bare.blocks)).not.toContain("actions");
  });
});
