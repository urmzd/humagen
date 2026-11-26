import { describe, expect, it } from "vitest";
import { cn } from "./utils";

describe("cn", () => {
  it("lets a theme size replace a size and keeps the color beside it", () => {
    expect(cn("text-sm text-muted-foreground", "text-aui-sm")).toBe(
      "text-muted-foreground text-aui-sm",
    );
    expect(cn("text-aui-fg", "text-aui-xs")).toBe("text-aui-fg text-aui-xs");
    expect(cn("text-aui-sm", "text-aui-muted")).toBe("text-aui-sm text-aui-muted");
  });

  it("lets a theme radius and a theme shadow replace the ones before them", () => {
    expect(cn("rounded-md", "rounded-aui-lg")).toBe("rounded-aui-lg");
    expect(cn("shadow-xs", "shadow-aui-pop")).toBe("shadow-aui-pop");
  });
});
