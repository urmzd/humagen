import { describe, expect, it } from "vitest";
import { escapeAnnouncement, escapeSlackText } from "./mrkdwn";

describe("escapeAnnouncement", () => {
  it.each([
    "<!channel>",
    "<!here>",
    "<!everyone>",
    "<!subteam^S0TEAM>",
    "<!subteam^S0TEAM|@oncall>",
    "<@U0DANA>",
    "<@W0DANA|dana>",
    "<#C0GENERAL>",
    "<https://evil.example.com|your bank>",
  ])("shows %s as typed and pings nobody", (token) => {
    const out = escapeAnnouncement(`Heads up ${token} for the renewal`);
    expect(out).not.toContain(token);
    expect(out).not.toMatch(/<[!@#h]/);
    expect(out).toContain("&lt;");
  });

  it("breaks a bare @here, @channel or @everyone, in any case", () => {
    for (const word of ["@here", "@channel", "@everyone", "@HERE"]) {
      const out = escapeAnnouncement(`${word} the offsite moved`);
      expect(out).not.toContain(word);
      expect(out.replaceAll("​", "")).toBe(`${word} the offsite moved`);
    }
    // A handle that only starts like one is left alone.
    expect(escapeAnnouncement("ask @heresy-bot")).toBe("ask @heresy-bot");
  });

  it("is stricter than typed text, which keeps user mentions", () => {
    expect(escapeSlackText("ask <@U0DANA>")).toBe("ask <@U0DANA>");
    expect(escapeAnnouncement("ask <@U0DANA>")).toBe("ask &lt;@U0DANA&gt;");
    expect(escapeAnnouncement("5 > 4 & 3 < 4")).toBe("5 &gt; 4 &amp; 3 &lt; 4");
  });
});
