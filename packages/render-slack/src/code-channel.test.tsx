import { defaultCopy, TurnView } from "@humagen/components";
import { ActionId, decodeAction } from "@humagen/core";
import { doneTurn, failedTurn, streamingTurn } from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { renderToSlack } from "./index";

type Button = { type: string; action_id?: string; value?: string; text?: { text: string } };
type Blk = { type: string; block_id?: string; elements?: Button[] };
const blocksOf = (el: React.ReactElement) => renderToSlack(el).blocks as Blk[];
const offer = (blocks: Blk[]) => blocks.find((b) => b.block_id?.startsWith("code-channel:"));

describe("Continue in a Code Channel", () => {
  it("is left out unless the host says it can open one", () => {
    const plain = blocksOf(<TurnView turn={doneTurn} />);
    expect(offer(plain)).toBeUndefined();
    expect(blocksOf(<TurnView turn={doneTurn} capabilities={{}} />)).toEqual(plain);
    expect(blocksOf(<TurnView turn={doneTurn} capabilities={{ codeChannel: false }} />)).toEqual(
      plain,
    );
  });

  it("is one button under a finished answer, naming the answered turn", () => {
    const plain = blocksOf(<TurnView turn={doneTurn} />);
    const out = renderToSlack(<TurnView turn={doneTurn} capabilities={{ codeChannel: true }} />);
    expect(out.warnings).toEqual([]);
    const blocks = out.blocks as Blk[];
    const block = offer(blocks);
    expect(block?.type).toBe("actions");
    expect(block?.elements).toHaveLength(1);
    const button = block?.elements?.[0];
    expect(button?.text?.text).toBe(defaultCopy.continueInCodeChannel);
    expect(button?.action_id?.startsWith(ActionId.codeChannel)).toBe(true);
    expect(decodeAction(String(button?.value))).toEqual({
      turnId: doneTurn.id,
      target: doneTurn.id,
    });
    // Nothing else moves: the answer is the same with the one block added.
    expect(blocks.filter((b) => b !== block)).toEqual(plain);
    // The same in the layout a streamed answer is closed with.
    expect(
      offer(blocksOf(<TurnView turn={doneTurn} capabilities={{ codeChannel: true }} streamed />)),
    ).toEqual(block);
  });

  it("is not offered while a turn is working or after it failed", () => {
    for (const turn of [streamingTurn, failedTurn]) {
      expect(
        offer(blocksOf(<TurnView turn={turn} capabilities={{ codeChannel: true }} />)),
      ).toBeUndefined();
    }
  });
});
