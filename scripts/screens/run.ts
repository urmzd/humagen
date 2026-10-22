/**
 * Captures every main console screen at phone and desktop width, and fails
 * when one breaks the layout checks in checks.ts.
 *
 * It starts warp on a free loopback port with the development sign-in, the
 * seeded agents on the offline provider and state in memory, adds a little
 * more through the API (announcements, a failed run whose alert waits in the
 * inbox, a schedule), and serves the built console from apps/web/dist. Run it
 * with `make screens`, which builds both first.
 *
 * Settings, all optional: WARP_BIN (the warp binary), WARP_WEB (the built
 * console), SCREENS_OUT (where screenshots go, .screens at the root),
 * CHROMIUM (a Chromium or Chrome to use instead of the first one found),
 * SCREENS_ONLY (a comma-separated list of screen names).
 */
import { type ChildProcess, spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { type Browser, chromium, type Page } from "playwright-core";
import { collect, DESKTOP, judge, PHONE, type Problem, type Rules } from "./checks.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "../..");
const bin = resolve(process.env.WARP_BIN ?? join(repo, "bin/warp"));
const web = resolve(process.env.WARP_WEB ?? join(repo, "apps/web/dist"));
const out = resolve(process.env.SCREENS_OUT ?? join(repo, ".screens"));
const only = (process.env.SCREENS_ONLY ?? "").split(",").filter(Boolean);

interface Viewport {
  name: "phone" | "desktop";
  width: number;
  height: number;
  rules: Rules;
}

const VIEWPORTS: Viewport[] = [
  { name: "phone", width: 390, height: 844, rules: PHONE },
  { name: "desktop", width: 1280, height: 800, rules: DESKTOP },
];

interface Screen {
  name: string;
  path: string;
}

/** Every main screen: each page, and each tab of the pages that have them. */
function screens(ids: { task: string; artifact: string; schedule: string }): Screen[] {
  const list: Screen[] = [
    { name: "home", path: "/" },
    { name: "task", path: `/tasks/${ids.task}` },
    { name: "projects", path: "/projects" },
    ...[
      "inbox",
      "overview",
      "context",
      "agents",
      "channels",
      "schedules",
      "people",
      "limits",
      "history",
    ].map((tab) => ({
      name: `project-${tab}`,
      path: tab === "inbox" ? "/projects/Launch" : `/projects/Launch/${tab}`,
    })),
    { name: "workspace", path: "/projects/~" },
    { name: "inbox", path: "/inbox" },
    { name: "schedules", path: "/schedules" },
    { name: "schedule", path: `/schedules/${ids.schedule}` },
    { name: "monitor", path: "/monitor" },
    { name: "monitor-rules", path: "/monitor/rules" },
    { name: "context", path: "/context" },
    { name: "agents", path: "/agents" },
    ...[
      "prompt",
      "model",
      "access",
      "queue",
      "context",
      "memory",
      "evals",
      "alerts",
      "revisions",
    ].map((tab) => ({
      name: `agent-${tab}`,
      path: tab === "prompt" ? "/agents/lead" : `/agents/lead/${tab}`,
    })),
    { name: "agent-new", path: "/agents/new" },
    { name: "artifacts", path: "/artifacts" },
    { name: "artifact", path: `/artifacts/${ids.artifact}` },
    { name: "connections", path: "/connections" },
    ...["people", "agents", "projects"].map((tab) => ({
      name: `directory-${tab}`,
      path: tab === "people" ? "/directory" : `/directory/${tab}`,
    })),
    ...[
      "profile",
      "appearance",
      "workspace",
      "routing",
      "models",
      "limits",
      "connections",
      "access",
    ].map((s) => ({
      name: `settings-${s}`,
      path: `/settings/${s}`,
    })),
  ];
  return only.length ? list.filter((s) => only.includes(s.name)) : list;
}

/** The Chromium to drive: one already installed, never a download. */
function chromiumPath(): string {
  const given = process.env.CHROMIUM;
  if (given) return given;
  const pw = process.env.PLAYWRIGHT_BROWSERS_PATH ?? "/opt/pw-browsers";
  const candidates = [
    join(pw, "chromium"),
    ...(existsSync(pw)
      ? readdirSync(pw)
          .filter((d) => /^chromium-\d+$/.test(d))
          .map((d) => join(pw, d, "chrome-linux/chrome"))
      : []),
    "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable",
    "/usr/bin/chromium",
    "/usr/bin/chromium-browser",
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  ];
  const found = candidates.find((c) => existsSync(c));
  if (!found)
    throw new Error("No Chromium found. Install Chrome or Chromium, or set CHROMIUM to one.");
  return found;
}

function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const s = createServer();
    s.once("error", fail);
    s.listen(0, "127.0.0.1", () => {
      const a = s.address();
      s.close(() => (a && typeof a === "object" ? ok(a.port) : fail(new Error("no port"))));
    });
  });
}

const sleep = (ms: number) => new Promise((ok) => setTimeout(ok, ms));

/** warp with seeded agents, no sign-in and nothing kept, in a directory of its own. */
async function startWarp(port: number): Promise<{ base: string; proc: ChildProcess; dir: string }> {
  if (!existsSync(bin))
    throw new Error(`No warp binary at ${bin}. Run make build, or set WARP_BIN.`);
  if (!existsSync(join(web, "index.html")))
    throw new Error(
      `No built console at ${web}. Run pnpm --filter @warp/console build, or set WARP_WEB.`,
    );
  const dir = mkdtempSync(join(tmpdir(), "warp-screens-"));
  const env = { ...process.env };
  // A model key would let the agents seeded to fail succeed.
  for (const k of Object.keys(env)) if (/_API_KEY$/.test(k)) delete env[k];
  const proc = spawn(
    bin,
    [
      "serve",
      "-auth=dev",
      "-seed",
      "-state=memory",
      "-storage=memory",
      `-addr=127.0.0.1:${port}`,
      `-web=${web}`,
      "-builtin-provider=offline",
      "-builtin-model=offline",
    ],
    { cwd: dir, env, stdio: ["ignore", "pipe", "pipe"] },
  );
  let log = "";
  const keep = (d: Buffer) => {
    log += d;
  };
  proc.stdout?.on("data", keep);
  proc.stderr?.on("data", keep);
  const base = `http://127.0.0.1:${port}`;
  for (let i = 0; i < 100; i++) {
    if (proc.exitCode !== null) throw new Error(`warp exited with ${proc.exitCode}:\n${log}`);
    try {
      const r = await fetch(`${base}/api/me`);
      if (r.ok) return { base, proc, dir };
    } catch {
      // Not listening yet.
    }
    await sleep(100);
  }
  proc.kill();
  throw new Error(`warp did not start:\n${log}`);
}

/** One call to the API, failing loudly: a seed that did not land would make every screen lie. */
function api(base: string) {
  return async <T>(method: string, path: string, body?: unknown): Promise<T> => {
    const r = await fetch(`${base}${path}`, {
      method,
      headers: { "content-type": "application/json", origin: base },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    const text = await r.text();
    if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${text}`);
    return (text ? JSON.parse(text) : undefined) as T;
  };
}

type Call = ReturnType<typeof api>;
interface Inbox {
  messages: { id: string }[];
}

async function emptied(call: Call): Promise<void> {
  let quiet = 0;
  for (let i = 0; i < 100 && quiet < 5; i++) {
    const page = await call<Inbox & { open: number }>("GET", "/api/inbox?limit=50&state=open");
    for (const m of page.messages) await call("POST", `/api/inbox/${m.id}/dismiss`, {});
    quiet = page.messages.length === 0 && page.open === 0 ? quiet + 1 : 0;
    await sleep(200);
  }
  if (quiet < 5) throw new Error("the inbox did not stay empty");
}

/** Ids the screen list is checked against before anything is seeded. */
const ids0 = { task: "", artifact: "", schedule: "" };

async function waitFor(ok: () => Promise<boolean>, failure: string): Promise<void> {
  for (let i = 0; i < 100; i++) {
    if (await ok()) return;
    await sleep(100);
  }
  throw new Error(failure);
}

async function settled(call: Call, id: string): Promise<void> {
  for (let i = 0; i < 100; i++) {
    const r = await call<{ status: string }>("GET", `/api/runs/${id}`);
    if (r.status === "completed" || r.status === "failed" || r.status === "stopped") return;
    await sleep(100);
  }
  throw new Error(`run ${id} did not end`);
}

/** Capture one screen, then measure it. */
async function shoot(
  page: Page,
  base: string,
  vp: Viewport,
  s: Screen,
): Promise<{ problems: Problem[]; full: boolean }> {
  await page.goto(`${base}${s.path}`, { waitUntil: "domcontentloaded" });
  // Ready when the page has drawn its title and the loading placeholders are gone.
  await page.waitForSelector("h1", { timeout: 15_000 }).catch(() => {});
  await page
    .waitForFunction(
      () => !document.querySelector("[aria-busy='true'], .warp-skeleton"),
      undefined,
      { timeout: 5_000 },
    )
    .catch(() => {});
  await page.evaluate(() => document.fonts.ready);
  await sleep(300);
  const file = join(out, vp.name, `${s.name}.png`);
  await page.screenshot({ path: file });
  const problems = judge(await page.evaluate(collect), vp.rules);
  // The page scrolls inside its own column, so a screenshot of the window
  // shows only the first screen. For review, the window is made as tall as
  // the column's content once more, and that is kept beside it.
  const extra = await page.evaluate(() => {
    let most = 0;
    for (const el of document.querySelectorAll("main *")) {
      const st = getComputedStyle(el);
      if (st.overflowY !== "auto" && st.overflowY !== "scroll") continue;
      most = Math.max(most, el.scrollHeight - el.clientHeight);
    }
    return most;
  });
  if (extra > 8) {
    await page.setViewportSize({ width: vp.width, height: Math.min(vp.height + extra, 8000) });
    await sleep(200);
    await page.screenshot({ path: join(out, vp.name, `${s.name}.full.png`) });
    await page.setViewportSize({ width: vp.width, height: vp.height });
  }
  return { problems, full: extra > 8 };
}

interface Result {
  screen: string;
  path: string;
  viewport: Viewport["name"];
  /** The first screen, as a person sees it on arrival. */
  file: string;
  /** The whole page, when it scrolls. */
  full?: string;
  problems: Problem[];
}

async function capture(
  browser: Browser,
  base: string,
  list: Screen[],
  tag = "",
): Promise<Result[]> {
  const results: Result[] = [];
  for (const vp of VIEWPORTS) {
    mkdirSync(join(out, vp.name), { recursive: true });
    const phone = vp.name === "phone";
    const context = await browser.newContext({
      viewport: { width: vp.width, height: vp.height },
      deviceScaleFactor: phone ? 2 : 1,
      isMobile: phone,
      hasTouch: phone,
      reducedMotion: "reduce",
      colorScheme: "light",
      locale: "en-US",
      timezoneId: "UTC",
    });
    const page = await context.newPage();
    for (const s of list) {
      const screen = `${s.name}${tag}`;
      const { problems, full } = await shoot(page, base, vp, { ...s, name: screen });
      results.push({
        screen,
        path: s.path,
        viewport: vp.name,
        file: `${vp.name}/${screen}.png`,
        ...(full ? { full: `${vp.name}/${screen}.full.png` } : {}),
        problems,
      });
      const mark = problems.length ? `FAIL ${problems.length}` : "ok";
      console.log(`${mark.padEnd(7)} ${vp.name.padEnd(8)} ${screen}`);
    }
    await context.close();
  }
  return results;
}

function report(results: Result[]): void {
  writeFileSync(join(out, "report.json"), `${JSON.stringify(results, null, 2)}\n`);
  const esc = (t: string) => t.replace(/&/g, "&amp;").replace(/</g, "&lt;");
  const names = [...new Set(results.map((r) => r.screen))];
  const rows = names
    .map((name) => {
      const cells = VIEWPORTS.map((vp) => {
        const r = results.find((x) => x.screen === name && x.viewport === vp.name);
        if (!r) return "<td></td>";
        const list = r.problems.map((p) => `<li>${p.check}: ${esc(p.detail)}</li>`).join("");
        const whole = r.full ? `<br><a href="${r.full}">The whole page</a>` : "";
        return `<td><a href="${r.file}"><img src="${r.file}" loading="lazy" width="${vp.name === "phone" ? 195 : 400}"></a>${whole}${list ? `<ul>${list}</ul>` : ""}</td>`;
      }).join("");
      return `<tr><th>${esc(name)}</th>${cells}</tr>`;
    })
    .join("\n");
  writeFileSync(
    join(out, "index.html"),
    `<!doctype html><meta charset="utf-8"><title>Console screens</title><style>body{font:14px system-ui;margin:16px}th{text-align:left;vertical-align:top;padding-right:12px}td{vertical-align:top;padding:0 12px 24px 0}img{border:1px solid #ccc}li{color:#b00020;max-width:400px}</style><table>\n${rows}\n</table>\n`,
  );
}

async function main(): Promise<void> {
  // A mistyped name would otherwise capture nothing and pass.
  const unknown = only.filter((n) => n !== "home-new" && !screens(ids0).some((s) => s.name === n));
  if (unknown.length) throw new Error(`SCREENS_ONLY names no screen: ${unknown.join(", ")}`);
  // Only a folder this script wrote, or a new one, is cleared.
  if (existsSync(out) && readdirSync(out).length > 0 && !existsSync(join(out, "report.json"))) {
    throw new Error(
      `${out} is not empty and holds no report.json, so it is not cleared. Set SCREENS_OUT to another folder.`,
    );
  }
  rmSync(out, { recursive: true, force: true });
  mkdirSync(out, { recursive: true });
  const executablePath = chromiumPath();
  const { base, proc, dir } = await startWarp(await freePort());
  let browser: Browser | undefined;
  try {
    browser = await chromium.launch({ executablePath, args: ["--no-sandbox"] });
    const call = api(base);
    const results: Result[] = [];

    // A person with nothing waiting and nothing new: Home's welcome. The
    // seeded files reach the inbox just after warp starts listening, so it
    // is cleared until it stays empty.
    await emptied(call);
    if (only.length === 0 || only.includes("home-new")) {
      results.push(...(await capture(browser, base, [{ name: "home", path: "/" }], "-new")));
    }

    // Something in every list: announcements, a conversation, a failed run
    // whose alert waits on the person, and a schedule.
    await call("POST", "/api/projects", {
      name: "Support",
      description: "Customer tickets and escalations",
    });
    await call("POST", "/api/projects/~/announcements", {
      subject: "Office closed on Friday",
      text: "Agents keep running.",
    });
    await call("POST", "/api/projects/Launch/announcements", {
      subject: "Launch moves to November 4, after the smoke tests on every supported platform",
      text: "One more week for the smoke tests.",
    });
    // Agents on a provider with no key fail, and the seeded rule raises an
    // alert for each into the inbox: what needs the person on Home.
    const broken = [
      { name: "triage", description: "Sorts new tickets.", text: "Sort the backlog" },
      { name: "intake", description: "Reads the support queue.", text: "Read the new tickets" },
      {
        name: "billing",
        description: "Answers invoice questions.",
        text: "Check the open invoices",
      },
    ];
    for (const a of broken) {
      await call("POST", "/api/agents", {
        name: a.name,
        description: a.description,
        model: "unavailable",
        provider: "anthropic",
        system_prompt: a.description,
      });
    }
    const ran = await call<{ id: string; thread?: string }>("POST", "/api/runs", {
      agent: "lead",
      text: "Summarize the launch plan",
    });
    await settled(call, ran.id);
    for (const a of broken) {
      const r = await call<{ id: string }>("POST", "/api/runs", { agent: a.name, text: a.text });
      await settled(call, r.id);
    }
    // Each failure's alert reaches the inbox after its run ends.
    await waitFor(
      async () => (await call<{ open: number }>("GET", "/api/inbox/count")).open >= broken.length,
      "the alerts did not reach the inbox",
    );
    await call("POST", "/api/schedules", {
      name: "weekly-summary",
      expression: "0 9 * * 1",
      agent: "lead",
      text: "Summarize the week",
      description: "A summary every Monday morning",
    });
    const files = await call<{ artifacts?: { id: string }[] }>("GET", "/api/artifacts");
    const ids = {
      task: ran.thread ?? ran.id,
      artifact: files.artifacts?.[0]?.id ?? "art-1",
      schedule: "weekly-summary",
    };
    results.push(...(await capture(browser, base, screens(ids))));

    report(results);
    const failing = results.filter((r) => r.problems.length > 0);
    console.log(`\n${results.length} screenshots in ${out}, index.html shows them side by side.`);
    if (failing.length) {
      console.log(`\n${failing.length} failed a layout check:`);
      for (const r of failing) {
        for (const p of r.problems)
          console.log(`  ${r.viewport} ${r.screen} (${r.path}): ${p.check}: ${p.detail}`);
      }
      process.exitCode = 1;
    }
  } finally {
    await browser?.close();
    proc.kill();
    rmSync(dir, { recursive: true, force: true });
  }
}

main().catch((e) => {
  console.error(e instanceof Error ? e.message : e);
  process.exit(1);
});
