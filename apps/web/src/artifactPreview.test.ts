import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { after, test } from "node:test";
import { DOMParser as XMLParser } from "@xmldom/xmldom";
import { strToU8, zipSync } from "fflate";
import { csvPreview, MAX_PREVIEW_BYTES, previewOf, readPreviewBytes } from "./artifactPreview.ts";

const previous = globalThis.DOMParser;
globalThis.DOMParser = XMLParser as unknown as typeof DOMParser;
after(() => {
  globalThis.DOMParser = previous;
});

test("DOCX previews keep actual table rows in document order and ignore retained-model phantom rows", () => {
  const w = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
  const p = (text: string) => `<w:p><w:r><w:t>${text}</w:t></w:r></w:p>`;
  const preview = previewOf(
    zipSync({
      "word/document.xml": strToU8(
        `<w:document xmlns:w="${w}"><w:body>${p("Report")}<w:tbl><w:tr><w:tc>${p("Check")}</w:tc><w:tc>${p("Result")}</w:tc></w:tr><w:tr><w:tc>${p("Document")}</w:tc><w:tc>${p("Completed")}</w:tc></w:tr></w:tbl>${p("After the table")}</w:body></w:document>`,
      ),
      "humagen/revision.json": strToU8('{"rows":[["PHANTOM TABLE"]]}'),
    }),
    "report.docx",
    "application/octet-stream",
  );
  assert.equal(preview.kind, "document");
  if (preview.kind !== "document") return;
  assert.deepEqual(preview.paragraphs, ["Report", "After the table"]);
  assert.deepEqual(preview.blocks, [
    { kind: "paragraph", text: "Report" },
    {
      kind: "table",
      rows: [
        ["Check", "Result"],
        ["Document", "Completed"],
      ],
    },
    { kind: "paragraph", text: "After the table" },
  ]);
  assert.equal(preview.truncated, false);
});

test("DOCX table previews bound rows and columns and disclose truncation", () => {
  const rows = Array.from(
    { length: 170 },
    () =>
      `<w:tr>${Array.from({ length: 45 }, () => "<w:tc><w:p><w:r><w:t>Cell</w:t></w:r></w:p></w:tc>").join("")}</w:tr>`,
  ).join("");
  const preview = previewOf(
    zipSync({
      "word/document.xml": strToU8(
        `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>${rows}</w:tbl></w:body></w:document>`,
      ),
    }),
    "large.docx",
    "application/octet-stream",
  );
  if (preview.kind !== "document") return assert.fail("document preview expected");
  const table = preview.blocks?.find((block) => block.kind === "table");
  assert.ok(table?.kind === "table");
  assert.equal(table.rows.length, 150);
  assert.equal(table.rows[0]?.length, 40);
  assert.equal(preview.truncated, true);
});

test("CSV preserves quoted delimiters, quotes, newlines and empty cells", () => {
  assert.deepEqual(csvPreview('\uFEFFName,Note,Empty\r\nDana,"a, b\nsecond ""line""",\r\n').rows, [
    ["Name", "Note", "Empty"],
    ["Dana", 'a, b\nsecond "line"', ""],
  ]);
  assert.throws(() => csvPreview('name,"unfinished'), /unfinished/);
});

test("a large table is bounded and tells the viewer it is shortened", () => {
  const preview = csvPreview(
    Array.from({ length: 200 }, () => Array(50).fill("value").join(",")).join("\n"),
  );
  assert.equal(preview.rows.length, 150);
  assert.equal(preview.rows[0]?.length, 40);
  assert.equal(preview.truncated, true);
});

test("the editable sample decks expose six slides, correct titles and speaker notes", async () => {
  for (const audience of ["leadership", "project-team"]) {
    const bytes = await readFile(
      new URL(
        `../../../docs/design/humagen-workspace/exports/Launch-briefing-${audience}.pptx`,
        import.meta.url,
      ),
    );
    const preview = previewOf(
      bytes,
      "briefing.pptx",
      "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    );
    assert.equal(preview.kind, "presentation");
    if (preview.kind !== "presentation") return;
    assert.equal(preview.slides.length, 6);
    assert.match(
      preview.slides[0]?.title ?? "",
      audience === "leadership" ? /controlled shift/ : /launch plan/,
    );
    assert.ok(preview.slides[3]?.notes.some((note) => note.includes("No notices were sent")));
    assert.equal(preview.truncated, false);
  }
});

test("the saved DOCX is read as text, including its title and decisions", async () => {
  const bytes = await readFile(
    new URL("../../../docs/design/humagen-workspace/exports/Launch-plan-v4.docx", import.meta.url),
  );
  const preview = previewOf(
    bytes,
    "Launch-plan.docx",
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  );
  assert.equal(preview.kind, "document");
  if (preview.kind === "document") {
    assert.equal(preview.paragraphs[0], "Launch plan");
    assert.match(preview.paragraphs.join("\n"), /separate delivery approval/);
  }
});

test("Office relationships determine slide order and exclude outside relationships", () => {
  const p = "http://schemas.openxmlformats.org/presentationml/2006/main";
  const a = "http://schemas.openxmlformats.org/drawingml/2006/main";
  const r = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
  const slide = (title: string) =>
    `<p:sld xmlns:p="${p}" xmlns:a="${a}"><p:sp><a:p><a:r><a:rPr sz="3000"/><a:t>${title}</a:t></a:r></a:p></p:sp></p:sld>`;
  const bytes = zipSync({
    "ppt/presentation.xml": strToU8(
      `<p:presentation xmlns:p="${p}" xmlns:r="${r}"><p:sldIdLst><p:sldId id="1" r:id="second"/><p:sldId id="2" r:id="first"/></p:sldIdLst></p:presentation>`,
    ),
    "ppt/_rels/presentation.xml.rels": strToU8(
      '<Relationships><Relationship Id="first" Target="slides/slide1.xml"/><Relationship Id="second" Target="slides/slide2.xml"/><Relationship Id="outside" Target="https://example.invalid/slide.xml" TargetMode="External"/></Relationships>',
    ),
    "ppt/slides/slide1.xml": strToU8(slide("First")),
    "ppt/slides/slide2.xml": strToU8(slide("Second")),
  });
  const preview = previewOf(bytes, "ordered.pptx", "application/octet-stream");
  assert.equal(preview.kind, "presentation");
  if (preview.kind === "presentation")
    assert.deepEqual(
      preview.slides.map((s) => s.title),
      ["Second", "First"],
    );
});

test("an XLSX follows the workbook's first sheet and preserves empty columns", () => {
  const bytes = zipSync({
    "xl/workbook.xml": strToU8(
      '<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet r:id="data" name="Data"/></sheets></workbook>',
    ),
    "xl/_rels/workbook.xml.rels": strToU8(
      '<Relationships><Relationship Id="data" Target="worksheets/sheet7.xml"/></Relationships>',
    ),
    "xl/sharedStrings.xml": strToU8("<sst><si><t>Name</t></si><si><t>Dana</t></si></sst>"),
    "xl/worksheets/sheet7.xml": strToU8(
      '<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="inlineStr"><is><t>Value</t></is></c></row><row r="2"><c r="A2" t="s"><v>1</v></c><c r="C2"><f>1+1</f><v>2</v></c></row></sheetData></worksheet>',
    ),
  });
  const preview = previewOf(bytes, "values.xlsx", "application/octet-stream");
  assert.equal(preview.kind, "table");
  if (preview.kind === "table")
    assert.deepEqual(preview.rows, [
      ["Name", "", "Value"],
      ["Dana", "", "2"],
    ]);
});

test("Office expansion, XML entities and oversize input fail before rendering", () => {
  assert.throws(
    () => previewOf(new Uint8Array(MAX_PREVIEW_BYTES + 1), "big.csv", "text/csv"),
    /8 MB/,
  );
  assert.throws(
    () =>
      previewOf(
        zipSync({ "word/document.xml": strToU8("x".repeat(2 * 1024 * 1024 + 1)) }),
        "large.docx",
        "application/octet-stream",
      ),
    /preview limit/,
  );
  assert.throws(
    () =>
      previewOf(
        zipSync({
          "word/document.xml": strToU8(
            '<!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]><x>&e;</x>',
          ),
        }),
        "entity.docx",
        "application/octet-stream",
      ),
    /XML declarations/,
  );
});

test("HTML is displayed as source text and never inserted into the document", () => {
  const preview = previewOf(
    strToU8('<script>throw new Error("run")</script><img src="https://example.invalid/private">'),
    "report.html",
    "text/html",
  );
  assert.equal(preview.kind, "text");
  if (preview.kind === "text") assert.equal(preview.markdown, false);
  if (preview.kind === "text") assert.match(preview.text, /<script>/);
});

test("a content stream is bounded when its declared length is absent", async () => {
  let cancelled = false;
  const response = new Response(
    new ReadableStream({
      start(controller) {
        controller.enqueue(new Uint8Array(MAX_PREVIEW_BYTES));
        controller.enqueue(new Uint8Array(1));
      },
      cancel() {
        cancelled = true;
      },
    }),
  );
  await assert.rejects(readPreviewBytes(response), /8 MB/);
  assert.equal(cancelled, true);
});
