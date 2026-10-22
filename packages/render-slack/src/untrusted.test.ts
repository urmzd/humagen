import { describe, expect, it } from "vitest";
import { escapeMentions } from "./index";

describe("escapeMentions", () => {
  it("turns every way of pinging people into plain words", () => {
    for (const [text, want] of [
      ["<!here> we shipped", "@here we shipped"],
      ["<!channel|channel> and <!everyone>", "@channel and @everyone"],
      ["ask <!subteam^S0123|@oncall>", "ask @oncall"],
      ["ask <!subteam^S0123>", "ask @S0123"],
      ["thanks <@U0DANA|dana> and <@W0BOB>", "thanks @dana and @W0BOB"],
    ] as const) {
      expect(escapeMentions(text)).toBe(want);
    }
  });

  it("leaves nothing Slack would read as a mention", () => {
    const out = escapeMentions("<!here><!channel><!everyone><!subteam^S1|@a><@U1><@W2|x> done");
    expect(out).not.toMatch(/<[!@]/);
  });

  it("keeps what pings nobody", () => {
    const text = "see <#C0123|general>, due <!date^1700000000^{date}|Nov 14>, a < b > c & d";
    expect(escapeMentions(text)).toBe(text);
  });
});
