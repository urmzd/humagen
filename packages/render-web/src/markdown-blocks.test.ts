import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { describe, expect, it } from "vitest";
import { markdownBlocks } from "./markdown-blocks";

const html = (text: string) =>
  renderToStaticMarkup(createElement(ReactMarkdown, { remarkPlugins: [remarkGfm] }, text));

/** The pieces drawn one after another, as `Markdown` draws them. */
const pieces = (text: string) => markdownBlocks(text).map(html).join("\n");

const documents: Record<string, string> = {
  "headings, paragraphs, a fence and a table": [
    "## One",
    "",
    "A paragraph with `code` and a [link](https://example.com).",
    "",
    "- a",
    "- b",
    "",
    "```ts",
    "const a = 1;",
    "",
    "# not a heading",
    "```",
    "",
    "| name | value |",
    "|------|-------|",
    "| a | 1 |",
    "",
    "## Two",
    "",
    "Last.",
  ].join("\n"),
  "a loose list stays one list": "- a\n\n- b\n\n- c\n\nAfter.",
  "an ordered list keeps counting": "1. a\n\n2. b\n\n   still b\n\n3. c",
  "a list item holding a fence and a paragraph": [
    "- item",
    "",
    "  ```sh",
    "  echo hi",
    "",
    "  echo again",
    "  ```",
    "",
    "  more of the item",
    "",
    "Outside.",
  ].join("\n"),
  "a quote, then a paragraph, then a quote": "> one\n> two\n\nBetween.\n\n> three",
  "a tilde fence holding backticks": "~~~\n```\n\ntext\n~~~\n\nAfter.",
  "an unclosed fence": "Before.\n\n```\ncode\n\nmore code",
  "a setext heading and a rule": "Title\n=====\n\nBody.\n\n---\n\nEnd.",
  "indented code after a paragraph": "Para.\n\n    code\n\n    more\n\nEnd.",
  "a reference link defined at the end": "See [the docs][d].\n\nMore.\n\n[d]: https://example.com",
  "a footnote": "A claim.[^1]\n\nMore.\n\n[^1]: The source.",
  "an html comment across blank lines": "Before.\n\n<!--\n\n# hidden\n\n-->\n\nAfter.",
  "emphasis and strong at the margin": "**Note:** one.\n\n*two* is next.\n\n***\n\nthree",
  "inline code that looks like a fence": "Start.\n\n```not a fence``` here.\n\nEnd.",
};

describe("markdownBlocks", () => {
  for (const [name, text] of Object.entries(documents)) {
    it(`draws the same in pieces: ${name}`, () => {
      expect(markdownBlocks(text).join("")).toBe(text);
      expect(pieces(text)).toBe(html(text));
    });
    it(`draws the same in pieces at every length while written: ${name}`, () => {
      for (let n = 1; n < text.length; n++) {
        const partial = text.slice(0, n);
        expect(pieces(partial), `first ${n} characters`).toBe(html(partial));
      }
    });
  }

  it("cuts a long answer, so only its end parses again", () => {
    const text = documents["headings, paragraphs, a fence and a table"] as string;
    const blocks = markdownBlocks(text);
    expect(blocks.length).toBeGreaterThan(4);
    // What is settled does not change as more is written after it.
    const longer = markdownBlocks(`${text}\n\nAnd more.`);
    expect(longer.slice(0, blocks.length - 1)).toEqual(blocks.slice(0, -1));
  });

  it("leaves text whole when a definition reaches across it", () => {
    expect(markdownBlocks("[a][b]\n\nMore.\n\n[b]: https://example.com")).toHaveLength(1);
  });
});
