import { describe, expect, it } from "vitest";
import { decodeReplyModal, REPLY_MODAL, replyModal } from "./index";

type View = ReturnType<typeof replyModal>;
const submitted = (view: View, value: string | null) => ({
  callback_id: view.callback_id,
  private_metadata: view.private_metadata,
  state: { values: { reply: { value: { value } } } },
});

describe("replyModal", () => {
  it("is one text field, with what it answers as ids in its metadata", () => {
    const view = replyModal(
      { inbox: "in-7", part: "p1" },
      { context: "Dana mentioned you in #renewals" },
    );
    expect(view.callback_id).toBe(REPLY_MODAL);
    expect(JSON.parse(view.private_metadata)).toEqual({ inbox: "in-7", part: "p1" });
    const inputs = view.blocks.filter((b) => (b as { type: string }).type === "input");
    expect(inputs).toHaveLength(1);
    expect(view.title.text.length).toBeLessThanOrEqual(24);
  });

  it("answers a question with its own label", () => {
    const view = replyModal({ run: "r1", question: "q1" });
    expect(JSON.parse(view.private_metadata)).toEqual({ run: "r1", question: "q1" });
    expect(JSON.stringify(view.blocks)).toContain("Your answer");
  });

  it("gives back the words and the target on submit", () => {
    const view = replyModal({ run: "r1", question: "q1" });
    expect(decodeReplyModal(submitted(view, "  Ship it.  "))).toEqual({
      text: "Ship it.",
      target: { run: "r1", question: "q1" },
    });
  });

  it("refuses a submission that is empty, not its own, or names no exact target", () => {
    const view = replyModal({ inbox: "in-7", part: "p1" });
    expect(decodeReplyModal(submitted(view, "   "))).toBeNull();
    expect(decodeReplyModal(submitted(view, null))).toBeNull();
    expect(decodeReplyModal({ ...submitted(view, "hi"), callback_id: "aui_form" })).toBeNull();
    for (const meta of [
      "",
      "not json",
      "[]",
      '{"inbox":"in-7"}',
      '{"inbox":"in-7","part":"p1","run":"r1"}',
      '{"thread":"th-1","message":"m-7"}',
      '{"run":"r1","question":7}',
    ]) {
      expect(decodeReplyModal({ ...submitted(view, "hi"), private_metadata: meta })).toBeNull();
    }
  });

  it("refuses to build a modal for no target", () => {
    expect(() => replyModal({ inbox: "", part: "p1" })).toThrow(RangeError);
  });
});
