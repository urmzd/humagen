import { strFromU8, unzipSync } from "fflate";

export type Slide = { title: string; paragraphs: string[]; notes: string[] };
export type FilePreview =
  | { kind: "document"; paragraphs: string[]; truncated: boolean }
  | { kind: "presentation"; slides: Slide[]; truncated: boolean }
  | { kind: "table"; rows: string[][]; truncated: boolean }
  | { kind: "text"; text: string; markdown: boolean }
  | { kind: "none" };

export const MAX_PREVIEW_BYTES = 8 * 1024 * 1024;
const MAX_EXPANDED = 16 * 1024 * 1024;
const MAX_XML = 2 * 1024 * 1024;
const MAX_TEXT = 160_000;
const MAX_SLIDES = 80;
const MAX_ROWS = 150;
const MAX_COLUMNS = 40;
const DRAWING = "http://schemas.openxmlformats.org/drawingml/2006/main";
const WORD = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
const REL = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";

function xml(bytes: Uint8Array | undefined): Document {
  if (!bytes || bytes.length > MAX_XML)
    throw new Error("The file's preview data is missing or exceeds the preview limit.");
  const value = strFromU8(bytes);
  if (/<!DOCTYPE|<!ENTITY/i.test(value))
    throw new Error("This file contains XML declarations that cannot be previewed.");
  const doc = new DOMParser().parseFromString(value, "application/xml");
  if (doc.getElementsByTagName("parsererror").length)
    throw new Error("The file's preview data could not be read.");
  return doc;
}

function paragraphs(doc: Document | Element, namespace: string): string[] {
  return [...doc.getElementsByTagNameNS(namespace, "p")]
    .map((p) =>
      [...p.getElementsByTagNameNS(namespace, "t")].map((t) => t.textContent ?? "").join(""),
    )
    .filter(Boolean);
}

function path(base: string, target: string): string {
  const parts: string[] = [];
  for (const part of (target.startsWith("/") ? target.slice(1) : `${base}/${target}`).split("/")) {
    if (part === "..") parts.pop();
    else if (part && part !== ".") parts.push(part);
  }
  return parts.join("/");
}

function relationships(bytes: Uint8Array | undefined) {
  if (!bytes) return [];
  return [...xml(bytes).getElementsByTagNameNS("*", "Relationship")]
    .filter((node) => node.getAttribute("TargetMode") !== "External")
    .map((node) => ({
      id: node.getAttribute("Id"),
      target: node.getAttribute("Target") ?? "",
      type: node.getAttribute("Type") ?? "",
    }));
}

/** Read a bounded part of a CSV, including quoted commas, quotes and newlines. */
export function csvPreview(text: string): Extract<FilePreview, { kind: "table" }> {
  const rows: string[][] = [];
  let row: string[] = [],
    cell = "",
    quoted = false,
    truncated = false;
  const input = text.replace(/^\uFEFF/, "");
  const addCell = () => {
    if (row.length < MAX_COLUMNS) row.push(cell);
    else truncated = true;
    cell = "";
  };
  for (let i = 0; i < input.length; i++) {
    const ch = input[i];
    if (ch === '"') {
      if (quoted && input[i + 1] === '"') {
        cell += '"';
        i++;
      } else if (quoted || cell.length === 0) quoted = !quoted;
      else cell += ch;
    } else if (!quoted && ch === ",") addCell();
    else if (!quoted && (ch === "\r" || ch === "\n")) {
      if (ch === "\r" && input[i + 1] === "\n") i++;
      addCell();
      rows.push(row);
      row = [];
      if (rows.length >= MAX_ROWS) {
        truncated ||= i < input.length - 1;
        return { kind: "table", rows, truncated };
      }
    } else if (cell.length < 16_000) cell += ch;
    else truncated = true;
  }
  if (quoted) throw new Error("The CSV has an unfinished quoted field.");
  if (cell || row.length) {
    addCell();
    rows.push(row);
  }
  return { kind: "table", rows, truncated };
}

/** Parse data only. Office XML, formulas, links and macros are never executed. */
export function previewOf(bytes: Uint8Array, title: string, mime: string): FilePreview {
  if (bytes.length > MAX_PREVIEW_BYTES)
    throw new Error(
      "Preview is limited to files of 8 MB. Download the original to view the whole file.",
    );
  const ext = title.split(".").at(-1)?.toLowerCase();
  if (ext === "csv" || mime === "text/csv") return csvPreview(new TextDecoder().decode(bytes));
  if (ext === "pptx" || ext === "docx" || ext === "xlsx") {
    let expanded = 0;
    const files = unzipSync(bytes, {
      filter: (file) => {
        if (!/\.(xml|rels)$/i.test(file.name)) return false;
        expanded += file.originalSize;
        if (expanded > MAX_EXPANDED || file.originalSize > MAX_XML)
          throw new Error(
            "The file exceeds the Office preview limit. Download the original to view it.",
          );
        return true;
      },
    });
    if (ext === "docx") {
      const all = paragraphs(xml(files["word/document.xml"]), WORD);
      let count = 0;
      const kept = all
        .filter((p) => {
          count += p.length;
          return count <= MAX_TEXT;
        })
        .slice(0, 500);
      return { kind: "document", paragraphs: kept, truncated: kept.length < all.length };
    }
    if (ext === "pptx") {
      const rels = relationships(files["ppt/_rels/presentation.xml.rels"]);
      const order = [...xml(files["ppt/presentation.xml"]).getElementsByTagNameNS("*", "sldId")]
        .map((node) => rels.find((rel) => rel.id === node.getAttributeNS(REL, "id")))
        .filter((rel) => rel !== undefined);
      const slides = order.slice(0, MAX_SLIDES).map((rel): Slide => {
        const name = path("ppt", rel.target);
        const doc = xml(files[name]);
        const shapes = [...doc.getElementsByTagNameNS("*", "sp")]
          .map((shape) => ({
            lines: paragraphs(shape, DRAWING),
            score: [...shape.getElementsByTagNameNS("*", "ph")].some((p) =>
              /^(title|ctrTitle)$/.test(p.getAttribute("type") ?? ""),
            )
              ? 100_000
              : Math.max(
                  0,
                  ...[
                    ...shape.getElementsByTagNameNS(DRAWING, "rPr"),
                    ...shape.getElementsByTagNameNS(DRAWING, "defRPr"),
                  ].map((run) => Number(run.getAttribute("sz") ?? 0)),
                ),
          }))
          .filter((shape) => shape.lines.length > 0);
        const titleShape = [...shapes].sort((a, b) => b.score - a.score)[0];
        const rest = shapes.filter((shape) => shape !== titleShape).flatMap((shape) => shape.lines);
        const parent = name.slice(0, name.lastIndexOf("/"));
        const file = name.slice(name.lastIndexOf("/") + 1);
        const notesRel = relationships(files[`${parent}/_rels/${file}.rels`]).find((item) =>
          item.type.endsWith("/notesSlide"),
        );
        const notes = notesRel
          ? paragraphs(xml(files[path(parent, notesRel.target)]), DRAWING)
          : [];
        return {
          title: titleShape?.lines.join(" ") ?? "Untitled slide",
          paragraphs: rest.map((line) => line.slice(0, 8_000)),
          notes,
        };
      });
      if (!slides.length) throw new Error("No slides could be read from this presentation.");
      return { kind: "presentation", slides, truncated: order.length > MAX_SLIDES };
    }
    const shared = files["xl/sharedStrings.xml"]
      ? [...xml(files["xl/sharedStrings.xml"]).getElementsByTagNameNS("*", "si")].map((node) =>
          [...node.getElementsByTagNameNS("*", "t")].map((part) => part.textContent ?? "").join(""),
        )
      : [];
    const book = xml(files["xl/workbook.xml"]);
    const first = book.getElementsByTagNameNS("*", "sheet")[0];
    const sheetRel = relationships(files["xl/_rels/workbook.xml.rels"]).find(
      (rel) => rel.id === first?.getAttributeNS(REL, "id"),
    );
    if (!sheetRel) throw new Error("No worksheet could be read from this spreadsheet.");
    const allRows = [...xml(files[path("xl", sheetRel.target)]).getElementsByTagNameNS("*", "row")];
    let truncated = allRows.length > MAX_ROWS;
    const rows = allRows.slice(0, MAX_ROWS).map((node) => {
      const row: string[] = [];
      for (const cell of node.getElementsByTagNameNS("*", "c")) {
        const letters = cell.getAttribute("r")?.match(/^[A-Z]+/)?.[0] ?? "A";
        const column =
          [...letters].reduce((n, letter) => n * 26 + letter.charCodeAt(0) - 64, 0) - 1;
        if (column >= MAX_COLUMNS) {
          truncated = true;
          continue;
        }
        while (row.length <= column) row.push("");
        const value =
          cell.getElementsByTagNameNS("*", "v")[0]?.textContent ??
          cell.getElementsByTagNameNS("*", "t")[0]?.textContent ??
          "";
        row[column] = (
          cell.getAttribute("t") === "s" ? (shared[Number(value)] ?? "") : value
        ).slice(0, 16_000);
      }
      return row;
    });
    return { kind: "table", rows, truncated };
  }
  if (
    mime.startsWith("text/") ||
    mime === "application/json" ||
    /^(md|txt|json|yaml|yml|log|ts|js|py|go)$/.test(ext ?? "")
  ) {
    // HTML and SVG are shown as source, never inserted into the application.
    const text = new TextDecoder().decode(bytes).slice(0, MAX_TEXT);
    return { kind: "text", text, markdown: mime === "text/markdown" || ext === "md" };
  }
  return { kind: "none" };
}

/** A readable response is bounded even when a server omits or misstates its size. */
export async function readPreviewBytes(response: Response): Promise<Uint8Array> {
  if (!response.ok) throw new Error("The artifact content could not be loaded.");
  if (!response.body) return new Uint8Array();
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > MAX_PREVIEW_BYTES)
        throw new Error(
          "Preview is limited to files of 8 MB. Download the original to view the whole file.",
        );
      chunks.push(value);
    }
  } finally {
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.length;
  }
  return bytes;
}
