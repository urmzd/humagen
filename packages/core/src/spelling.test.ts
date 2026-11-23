import { describe, expect, it } from "vitest";
import { spelled } from "./spelling";

describe("spelled", () => {
  it("gives a brand its owner's capitals, whatever the case it arrives in", () => {
    expect(spelled("github")).toBe("GitHub");
    expect(spelled("GITHUB")).toBe("GitHub");
    expect(spelled("bigquery")).toBe("BigQuery");
    expect(spelled("slack")).toBe("Slack");
  });

  it("writes a known short word in capitals", () => {
    expect(spelled("qa")).toBe("QA");
    expect(spelled("Api")).toBe("API");
  });

  it("leaves an ordinary word to its caller", () => {
    expect(spelled("reviewer")).toBeUndefined();
    expect(spelled("git")).toBeUndefined();
  });
});
