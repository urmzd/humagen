import assert from "node:assert/strict";
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";

// What Home loads is what its files import, so the pages read on demand
// (pages.ts) stay out of it only while nothing Home draws imports one of
// them directly. This walks the imports the bundler follows, from the
// console's entry through the linked humagen packages, and fails when a lazy
// page or one of its heavy dependencies is reached without an `import()`.

const src = dirname(fileURLToPath(import.meta.url));
const repo = resolve(src, "../../..");

/** The file a linked humagen package answers a specifier with, from its exports. */
function packageFile(name: string, sub: string | undefined): string | null {
  const dir = join(repo, "packages", name);
  if (!existsSync(join(dir, "package.json"))) return null;
  const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8")) as {
    exports?: Record<string, string | { default?: string; import?: string }>;
    main?: string;
  };
  const exports = pkg.exports ?? { ".": pkg.main ?? "./src/index.ts" };
  const key = sub ? `./${sub}` : ".";
  let target = exports[key];
  if (!target)
    for (const [pattern, to] of Object.entries(exports)) {
      const [before, after] = pattern.split("*");
      if (after === undefined || before === undefined || typeof to !== "string") continue;
      if (key.startsWith(before) && key.endsWith(after))
        target = to.replace("*", key.slice(before.length, key.length - after.length));
    }
  if (target && typeof target === "object") target = target.default ?? target.import;
  return typeof target === "string" ? join(dir, target) : null;
}

/** Where a specifier leads: a file, or `npm:<package>` for a dependency. */
function resolveImport(from: string, spec: string): string | null {
  if (spec.startsWith(".")) {
    const base = resolve(dirname(from), spec);
    for (const file of [
      base,
      `${base}.ts`,
      `${base}.tsx`,
      join(base, "index.ts"),
      join(base, "index.tsx"),
    ])
      if (existsSync(file) && statSync(file).isFile()) return file;
    return null;
  }
  const own = spec.match(/^@humagen\/([^/]+)(?:\/(.*))?$/);
  if (own) return packageFile(own[1] ?? "", own[2]);
  return `npm:${spec.startsWith("@") ? spec.split("/").slice(0, 2).join("/") : spec.split("/")[0]}`;
}

/** The specifiers a file imports when it loads, and those it imports on demand. */
function importsOf(file: string): { now: string[]; later: string[] } {
  const source = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest);
  const now: string[] = [];
  const later: string[] = [];
  for (const statement of source.statements) {
    // `import type` is erased. `import { type A }` is kept as an import of the
    // module (verbatimModuleSyntax), so it counts.
    if (ts.isImportDeclaration(statement) && ts.isStringLiteral(statement.moduleSpecifier)) {
      if (!statement.importClause?.isTypeOnly) now.push(statement.moduleSpecifier.text);
    } else if (
      ts.isExportDeclaration(statement) &&
      statement.moduleSpecifier &&
      ts.isStringLiteral(statement.moduleSpecifier) &&
      !statement.isTypeOnly
    )
      now.push(statement.moduleSpecifier.text);
  }
  const visit = (node: ts.Node) => {
    if (
      ts.isCallExpression(node) &&
      node.expression.kind === ts.SyntaxKind.ImportKeyword &&
      node.arguments[0] &&
      ts.isStringLiteral(node.arguments[0])
    )
      later.push(node.arguments[0].text);
    ts.forEachChild(node, visit);
  };
  visit(source);
  return { now, later };
}

/** Every file and dependency loaded with `entries`, each with the file that imported it. */
function loadedWith(entries: string[]): Map<string, string> {
  const seen = new Map<string, string>();
  const walk = (file: string, via: string) => {
    if (seen.has(file)) return;
    seen.set(file, via);
    if (file.startsWith("npm:") || !/\.tsx?$/.test(file)) return;
    for (const spec of importsOf(file).now) {
      const to = resolveImport(file, spec);
      if (to) walk(to, file);
    }
  };
  for (const entry of entries) walk(entry, "entry");
  return seen;
}

/** How a file came to be loaded, from the entry down, for a failure's message. */
function chain(seen: Map<string, string>, file: string): string {
  const steps: string[] = [];
  for (let at: string | undefined = file; at && at !== "entry"; at = seen.get(at))
    steps.unshift(at.replace(`${repo}/`, ""));
  return steps.join(" -> ");
}

/** The most source of the console's own that Home may load, in bytes: about a tenth over what it loads now. */
const HOME_SOURCE_BUDGET = 870_500;

// main.tsx reads the console, the update prompt and the API once the branding is in.
const main = join(src, "main.tsx");
const home = loadedWith([
  main,
  ...importsOf(main).later.flatMap((spec) => resolveImport(main, spec) ?? []),
]);
const lazy = [
  ...new Set(
    importsOf(join(src, "pages.ts")).later.flatMap(
      (spec) => resolveImport(join(src, "pages.ts"), spec) ?? [],
    ),
  ),
];

test("Home's entry holds the shell, the sidebar, the composer and the session line", () => {
  for (const file of [
    "App.tsx",
    "Sidebar.tsx",
    "Chat.tsx",
    "Composer.tsx",
    "HomePage.tsx",
    "SessionCenter.tsx",
    "pages.ts",
  ])
    assert.ok(home.has(join(src, file)), `${file} is not loaded with Home`);
});

test("no page read on demand is imported by what Home loads", () => {
  assert.ok(lazy.length >= 30, `pages.ts names ${lazy.length} files`);
  for (const file of lazy)
    assert.ok(!home.has(file), `Home loads a page it should read on demand: ${chain(home, file)}`);
});

test("the settings pages, the conversation and the page-only views stay out of Home", () => {
  for (const file of [
    "Settings.tsx",
    "Room.tsx",
    "ChatConversation.tsx",
    "Workspace.tsx",
    "Deliverables.tsx",
    "ResponseActivity.tsx",
    "InboxPage.tsx",
    "Billing.tsx",
    "Analytics.tsx",
    "ComposerLight.tsx",
  ])
    assert.ok(!home.has(join(src, file)), `Home loads ${file}: ${chain(home, join(src, file))}`);
});

test("the composer's light and the date picker are not dependencies of Home", () => {
  for (const dependency of ["metal-fx", "voice-glow", "react-day-picker"])
    assert.ok(
      !home.has(`npm:${dependency}`),
      `Home loads ${dependency}: ${chain(home, `npm:${dependency}`)}`,
    );
});

test("Home's own source stays within its budget", () => {
  // The console's own files loaded with Home, as written. A new page imported
  // straight into the shell shows here even before it is listed in pages.ts:
  // read it on demand (pages.ts) rather than raising the budget.
  const own = [...home.keys()].filter((file) => file.startsWith(`${src}/`));
  const bytes = own.reduce((sum, file) => sum + statSync(file).size, 0);
  assert.ok(
    bytes < HOME_SOURCE_BUDGET,
    `Home loads ${bytes} bytes of apps/web/src in ${own.length} files; the budget is ${HOME_SOURCE_BUDGET}`,
  );
});

test("an import made directly would be caught", () => {
  // The walk itself: a page reached only through pages.ts is loaded once it is an entry.
  const room = join(src, "Room.tsx");
  assert.ok(loadedWith([room]).has(join(src, "Workspace.tsx")));
  assert.ok(loadedWith([join(src, "ComposerLight.tsx")]).has("npm:metal-fx"));
});
