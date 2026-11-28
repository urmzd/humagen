import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { webImpls } from "./impls";
import { MentionContext, splitMentions } from "./mentions";

const names = (text: string) =>
  (splitMentions(text) ?? []).filter((n) => n.type === "link").map((n) => n.children?.[0]?.value);

describe("an agent's `@name` in its own words", () => {
  it("is found at the start, after a space and after a bracket", () => {
    expect(names("@lead asked @humagen-writer (and @ops_2).")).toEqual([
      "@lead",
      "@humagen-writer",
      "@ops_2",
    ]);
  });

  it("is not found in an address, a commit reference or a scoped package", () => {
    expect(splitMentions("write to ada@example.com")).toBeUndefined();
    expect(splitMentions("see urmzd/humagen@abc1234")).toBeUndefined();
    expect(splitMentions("install npm/@types/node")).toBeUndefined();
    expect(splitMentions("a lone @ and @@twice")).toBeUndefined();
  });

  it("is not found in a scoped package, a file name or the host of an address, though the name is an agent's", () => {
    expect(splitMentions("install @humagen/core and @types/node")).toBeUndefined();
    expect(splitMentions("see @humagen.md")).toBeUndefined();
    expect(splitMentions("@example.com")).toBeUndefined();
    expect(splitMentions("@ada@example.com")).toBeUndefined();
  });

  it("ends at the name: a full stop, a comma or a hyphen after it is not part of it", () => {
    expect(names("Ask @writer. Then @lead, or @ops- next")).toEqual(["@writer", "@lead", "@ops"]);
  });

  it("keeps the text around it", () => {
    expect(
      splitMentions("Bringing in @writer now")?.map((n) => n.value ?? n.children?.[0]?.value),
    ).toEqual(["Bringing in ", "@writer", " now"]);
  });

  const draw = (text: string, host?: (name: string) => string | undefined) =>
    renderToStaticMarkup(
      createElement(
        MentionContext.Provider,
        { value: host ?? null },
        createElement(webImpls.Markdown, { text }),
      ),
    );

  it("is drawn by the host in a paragraph, and left as text in code and in a link", () => {
    const host = (name: string) => (name === "writer" ? "[[Writer]]" : undefined);
    const out = draw(
      "Bringing in @writer and @nobody. Run `@writer` or [@writer](https://example.com).",
      host,
    );
    expect(out).toContain("Bringing in [[Writer]] and @nobody.");
    expect(out).toContain("<code>@writer</code>");
    expect(out).toContain('href="https://example.com"');
    expect(out.match(/\[\[Writer\]\]/g)?.length).toBe(1);
  });

  it("stays text without a host", () => {
    const out = draw("Bringing in @writer.");
    expect(out).toContain("Bringing in @writer.");
    expect(out).not.toContain("<a");
  });
});
