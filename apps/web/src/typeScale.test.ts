import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

// Text under 12px is not legible on a phone, and a phone is where the console
// is read most closely. The floor is the type scale's, so it holds at every
// width, and a size written by hand may not go under it either.
const FLOOR_PX = 12;
const ROOT_PX = 16;

const repo = fileURLToPath(new URL("../../..", import.meta.url));
const theme = readFileSync(join(repo, "packages/render-web/src/theme.css"), "utf8");

/** Every source file of the console and the shared web packages, tests aside. */
function sources(): { file: string; text: string }[] {
  const found: { file: string; text: string }[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "node_modules" && entry.name !== "generated") walk(path);
      } else if (/\.(tsx?|css)$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) {
        found.push({ file: path.slice(repo.length), text: readFileSync(path, "utf8") });
      }
    }
  };
  for (const dir of [
    "apps/web/src",
    "packages/web/src",
    "packages/render-web/src",
    "packages/components/src",
  ])
    walk(join(repo, dir));
  return found;
}

const px = (value: number, unit: string) => (unit === "rem" ? value * ROOT_PX : value);

test("type scale: no step is under 12px", () => {
  const steps = [...theme.matchAll(/--text-aui-([a-z0-9]+):\s*([\d.]+)(rem|px);/g)];
  assert.ok(steps.length >= 6, "the type scale was found");
  for (const [, name, value, unit] of steps)
    assert.ok(
      px(Number(value), unit ?? "") >= FLOOR_PX,
      `text-aui-${name} is ${px(Number(value), unit ?? "")}px`,
    );
});

test("type scale: no size written by hand is under 12px", () => {
  const small: string[] = [];
  for (const { file, text } of sources()) {
    const lines = text.split("\n");
    lines.forEach((line, i) => {
      const at = `${file}:${i + 1}`;
      // Tailwind's arbitrary sizes and CSS declarations, in px or rem.
      for (const m of line.matchAll(/(?:text-\[|font-size:\s*)([\d.]+)(px|rem)\b/g))
        if (px(Number(m[1]), m[2] ?? "") < FLOOR_PX) small.push(`${at} ${m[0]}`);
      // A size relative to its parent has no floor of its own: it takes the scale's.
      for (const m of line.matchAll(/(?:text-\[|font-size:\s*)(0?\.\d+)em\b/g))
        small.push(`${at} ${m[0]} (wrap it in max() with var(--text-aui-2xs))`);
      // SVG and inline styles.
      for (const m of line.matchAll(/fontSize[=:]\s*\{?\s*(\d+(?:\.\d+)?)\s*[,}]/g))
        if (Number(m[1]) < FLOOR_PX) small.push(`${at} ${m[0]}`);
    });
  }
  assert.deepEqual(small, []);
});
