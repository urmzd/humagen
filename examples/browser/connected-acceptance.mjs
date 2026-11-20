import { createRequire } from "node:module";
import { join, resolve, dirname } from "node:path";
import { tmpdir } from "node:os";
import { createServer } from "node:http";
import { randomBytes, createHash } from "node:crypto";
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  readdirSync,
  mkdtempSync,
  existsSync,
  renameSync,
} from "node:fs";
import { spawn } from "node:child_process";
// Opt-in: this runs a real model through Humagen with the native profile in
// humagen.yaml, on the provider key in your environment (ANTHROPIC_API_KEY),
// and consumes that account's usage.
if (process.env.HUMAGEN_BROWSER_LIVE !== "1")
  throw new Error("Set HUMAGEN_BROWSER_LIVE=1 to run the real authenticated fixture acceptance.");
if (!process.env.HUMAGEN_BROWSER_TOOL_DIR)
  throw new Error(
    "Set HUMAGEN_BROWSER_TOOL_DIR to a prefix containing npm-installed @playwright/mcp@0.0.85.",
  );
const tools = resolve(process.env.HUMAGEN_BROWSER_TOOL_DIR);
const compareUpload = process.env.HUMAGEN_BROWSER_COMPARE_UPLOAD === "1";
const metadata = join(tools, "node_modules/@playwright/mcp/package.json");
if (JSON.parse(readFileSync(metadata, "utf8")).version !== "0.0.85")
  throw new Error("Acceptance requires the pinned @playwright/mcp@0.0.85.");
const require = createRequire(metadata);
const { chromium } = require("playwright-core");
const mcpCLI = join(dirname(metadata), "cli.js");
const root = process.env.HUMAGEN_BROWSER_CAPTURE_DIR
  ? resolve(process.env.HUMAGEN_BROWSER_CAPTURE_DIR)
  : mkdtempSync(join(tmpdir(), "humagen-browser-acceptance-"));
mkdirSync(root, { recursive: true });
mkdirSync(join(root, "downloads"), { recursive: true });
mkdirSync(join(root, "artifacts"), { recursive: true });
mkdirSync(join(root, "verified-downloads"), { recursive: true });
console.log("Acceptance artifacts:", root);
async function freePort() {
  const probe = createServer();
  await new Promise((r) => probe.listen(0, "127.0.0.1", r));
  const port = probe.address().port;
  await new Promise((r) => probe.close(r));
  return port;
}
async function waitURL(url) {
  for (let i = 0; i < 200; i++) {
    if (humagen?.launchError) throw humagen.launchError;
    if (humagen && humagen.exitCode !== null)
      throw new Error("Humagen exited before becoming ready; inspect " + root + "/server.log");
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("Service did not become ready: " + url);
}
async function stopProcess(child) {
  if (!child || child.launchError || child.exitCode !== null) return;
  const exited = new Promise((r) => child.once("exit", r));
  child.kill("SIGTERM");
  await Promise.race([
    exited,
    new Promise((r) => {
      const timer = setTimeout(r, 5000);
      timer.unref();
    }),
  ]);
  if (child.exitCode === null) {
    child.kill("SIGKILL");
    await exited;
  }
}
function startProcess(command, args, options) {
  const child = spawn(command, args, options);
  child.once("error", (error) => {
    child.launchError = error;
  });
  return child;
}
const mcpPort = await freePort(),
  apiPort = await freePort();
if (mcpPort === apiPort) throw new Error("Ephemeral port collision; rerun the acceptance.");
const tenant = "browser-fixture-" + randomBytes(8).toString("hex");
let humagen;
const cookie = randomBytes(24).toString("hex"),
  password = randomBytes(20).toString("hex");
let record = "",
  changed = false,
  downloads = 0,
  loginCount = 0;
let base = "";
const api = "http://127.0.0.1:" + apiPort;
async function sessionRun(session, previous = new Set()) {
  for (let i = 0; i < 240; i++) {
    const response = await fetch(api + "/api/sessions/" + session);
    if (!response.ok) throw new Error(await response.text());
    const view = await response.json();
    const run = view.messages.map((m) => m.run).find((r) => r && !previous.has(r.id));
    if (run && ["completed", "failed", "stopped"].includes(run.status)) return run;
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error("Session did not complete: " + session);
}
// Observe the exact bytes the MCP worker returns to Humagen, rather than
// inferring inline images from the run's text events or a saved screenshot path.
const wireImages = [];
const wireScreenshots = [];
const wireErrors = [];
const bridge = createServer(async (req, res) => {
  const controller = new AbortController();
  res.on("close", () => controller.abort());
  try {
    let body = "";
    for await (const part of req) {
      body += part;
      if (body.length > 1024 * 1024) throw new Error("MCP request exceeds fixture bound");
    }
    const request = body ? JSON.parse(body) : undefined;
    const call = request?.method === "tools/call" ? request.params : undefined;
    if (call?.name === "browser_take_screenshot") wireScreenshots.push(call.arguments ?? {});
    const headers = {};
    for (const [key, value] of Object.entries(req.headers))
      if (!["host", "connection", "content-length"].includes(key) && value !== undefined)
        headers[key] = value;
    const response = await fetch("http://127.0.0.1:" + mcpPort + req.url, {
      method: req.method,
      headers,
      ...(body ? { body } : {}),
      signal: controller.signal,
    });
    const responseHeaders = {};
    for (const [key, value] of response.headers)
      if (!["connection", "transfer-encoding", "content-length", "content-encoding"].includes(key))
        responseHeaders[key] = value;
    res.writeHead(response.status, responseHeaders);
    let payload = "";
    const inspect = (raw) => {
      if (!raw.trim()) return;
      const message = JSON.parse(raw);
      if (!call || message.id !== request.id) return;
      for (const content of message.result?.content ?? []) {
        if (content.type !== "image") continue;
        const bytes = Buffer.from(content.data, "base64");
        if (
          content.mimeType !== "image/png" ||
          bytes.length < 24 ||
          !bytes.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))
        )
          throw new Error("Screenshot result is not a nonempty PNG image block");
        wireImages.push({
          tool: call.name,
          bytes: bytes.length,
          width: bytes.readUInt32BE(16),
          height: bytes.readUInt32BE(20),
          sha256: createHash("sha256").update(bytes).digest("hex"),
          fixtureChanged: changed,
        });
      }
    };
    for await (const chunk of response.body ?? []) {
      // Forward unchanged while retaining only the bounded response for parsing.
      res.write(chunk);
      if (call) {
        payload += Buffer.from(chunk).toString("utf8");
        if (payload.length > 8 * 1024 * 1024) throw new Error("MCP response exceeds fixture bound");
      }
    }
    if (call) {
      if ((response.headers.get("content-type") ?? "").includes("text/event-stream")) {
        for (const line of payload.split(/\r?\n/))
          if (line.startsWith("data:")) inspect(line.slice(5));
      } else inspect(payload);
    }
    res.end();
  } catch (error) {
    if (!controller.signal.aborted) {
      wireErrors.push(String(error));
      if (!res.headersSent) res.writeHead(502);
      res.end();
    }
  }
});
await new Promise((r) => bridge.listen(0, "127.0.0.1", r));
const bridgePort = bridge.address().port;
const server = createServer(async (req, res) => {
  const url = new URL(req.url, base);
  const auth = req.headers.cookie?.includes("fixture_session=" + cookie);
  if (url.pathname === "/login" && req.method === "POST") {
    let body = "";
    for await (const part of req) body += part;
    const data = new URLSearchParams(body);
    if (data.get("user") !== "fixture" || data.get("password") !== password) {
      res.writeHead(403).end("Refused");
      return;
    }
    loginCount++;
    res
      .writeHead(303, {
        "Set-Cookie": `fixture_session=${cookie}; HttpOnly; SameSite=Strict; Path=/`,
        Location: "/portal",
      })
      .end();
    return;
  }
  if (!auth) {
    res
      .writeHead(200, { "Content-Type": "text/html" })
      .end(
        '<title>Fixture sign in</title><form method="post" action="/login"><label>User <input name="user"></label><label>Password <input name="password" type="password"></label><button>Sign in</button></form>',
      );
    return;
  }
  if (url.pathname === "/record.csv") {
    downloads++;
    res
      .writeHead(200, {
        "Content-Type": "text/csv",
        "Content-Disposition": `attachment; filename="${record}.csv"`,
      })
      .end(`record,value\n${record},42\n`);
    return;
  }
  if (url.pathname === "/approve" && req.method === "POST") {
    changed = true;
    res.writeHead(303, { Location: "/portal" }).end();
    return;
  }
  res
    .writeHead(200, { "Content-Type": "text/html" })
    .end(
      `<!doctype html><title>Connected browser fixture</title><meta name="viewport" content="width=device-width, initial-scale=1"><style>:root{color-scheme:light dark}*{box-sizing:border-box}body{margin:0;padding:24px;font:18px/1.5 system-ui;background:light-dark(#fafafa,#171717);color:light-dark(#171717,#fafafa)}main{max-width:900px;margin:auto}h1{font-size:30px;line-height:1.2}button,a{display:inline-block;font:inherit;min-height:44px;padding:10px 14px;margin:8px 0}a{color:light-dark(#1256b0,#8ec2ff)}.panels{display:flex;gap:16px;margin:20px 0}.panels div{height:120px;flex:1;border-radius:12px}.one{background:${compareUpload && changed ? "#16803c" : "#ffdf00"}}.two{background:${compareUpload && changed ? "#ff8c00" : "#800080"}}</style><main><p>Authorized fixture account</p><h1>${changed ? "Fixture approved" : "Signed-in records"}</h1><p>Record: <strong>${record}</strong></p><p>The two panels have no text labels. Inspect their pixels.</p><div class="panels" aria-hidden="true"><div class="one"></div><div class="two"></div></div><form action="/approve" method="post"><button>Approve fixture</button></form><a href="/record.csv" download>Download selected record</a></main>`,
    );
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
base = "http://127.0.0.1:" + server.address().port;
let chrome, browser, context, page, worker, endpoint;
const results = [];
async function startWorker() {
  worker = startProcess(
    process.execPath,
    [
      mcpCLI,
      "--port",
      String(mcpPort),
      "--host",
      "127.0.0.1",
      "--cdp-endpoint",
      endpoint,
      "--output-dir",
      root + "/artifacts",
      "--file-paths",
      "absolute",
      "--shared-browser-context",
      "--no-webmcp",
      "--image-responses",
      "allow",
    ],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  let log = "";
  worker.stdout.on("data", (b) => {
    log += b;
    writeFileSync(root + "/worker.log", log);
  });
  worker.stderr.on("data", (b) => {
    log += b;
    writeFileSync(root + "/worker.log", log);
  });
  for (let i = 0; i < 200 && !log.includes("Listening on"); i++) {
    if (worker.launchError) throw worker.launchError;
    if (worker.exitCode !== null) throw new Error("MCP worker exited: " + log);
    await new Promise((r) => setTimeout(r, 100));
  }
  if (!log.includes("Listening on")) throw new Error("MCP worker did not become ready: " + log);
}
async function stopWorker() {
  await stopProcess(worker);
  worker = undefined;
}
try {
  const userdir = mkdtempSync(root + "/profile-");
  mkdirSync(userdir + "/Default", { recursive: true });
  writeFileSync(
    userdir + "/Default/Preferences",
    JSON.stringify({
      download: {
        default_directory: root + "/downloads",
        prompt_for_download: false,
        directory_upgrade: true,
      },
    }),
  );
  chrome = startProcess(
    process.env.HUMAGEN_BROWSER_CHROME ||
      (process.platform === "darwin"
        ? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        : "chromium"),
    [
      "--headless=new",
      "--remote-debugging-port=0",
      "--user-data-dir=" + userdir,
      "--no-first-run",
      "--no-default-browser-check",
      "about:blank",
    ],
    { stdio: "ignore" },
  );
  for (let i = 0; i < 150 && !existsSync(userdir + "/DevToolsActivePort"); i++) {
    if (chrome.launchError) throw chrome.launchError;
    await new Promise((r) => setTimeout(r, 100));
  }
  const port = readFileSync(userdir + "/DevToolsActivePort", "utf8").split("\n")[0];
  endpoint = "http://127.0.0.1:" + port;
  browser = await chromium.connectOverCDP(endpoint, { noDefaults: true });
  context = browser.contexts()[0];
  page = context.pages()[0] ?? (await context.newPage());

  // The native server accepts the profile's pinned browser tools when it
  // starts, so the worker must already be listening (through the bridge).
  await startWorker();
  let config = readFileSync(new URL("./humagen.yaml", import.meta.url), "utf8");
  for (const [from, to] of [
    ["127.0.0.1:8080", "127.0.0.1:" + apiPort],
    ["http://127.0.0.1:8931/mcp", "http://127.0.0.1:" + bridgePort + "/mcp"],
    ["{fixed: default}", "{fixed: " + tenant + "}"],
    ["metadata: {name: default}", "metadata: {name: " + tenant + "}"],
  ]) {
    if (!config.includes(from)) throw new Error("humagen.yaml no longer contains " + from);
    config = config.replace(from, to);
  }
  writeFileSync(join(root, "humagen.yaml"), config);
  humagen = startProcess(
    process.env.HUMAGEN_BROWSER_BINARY || "humagen",
    ["serve", "-f", join(root, "humagen.yaml")],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  let serverLog = "";
  for (const stream of [humagen.stdout, humagen.stderr])
    stream.on("data", (b) => {
      serverLog += b;
      writeFileSync(join(root, "server.log"), serverLog);
    });
  await waitURL(api + "/api/me");
  const me = await (await fetch(api + "/api/me")).json();
  if (me.principal?.tenant !== tenant)
    throw new Error("The API is not the owned fixture workspace; refusing to submit work.");
  await page.goto(base + "/login");
  await page.getByLabel("User", { exact: true }).fill("fixture");
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL(base + "/portal");
  const target = (await (await context.newCDPSession(page)).send("Target.getTargetInfo")).targetInfo
    .targetId;
  const states = [
    ["light", 390, 844],
    ["dark", 390, 844],
    ["light", 1280, 800],
    ["dark", 1280, 800],
  ];
  for (const [theme, width, height] of states) {
    record = "AUDIT-" + randomBytes(4).toString("hex").toUpperCase();
    changed = false;
    downloads = 0;
    if (readdirSync(root + "/downloads").length !== 0)
      throw new Error("Download directory contains another record or MCP artifact");
    const imageStart = wireImages.length,
      screenshotStart = wireScreenshots.length;
    await page.setViewportSize({ width, height });
    await page.emulateMedia({ colorScheme: theme });
    await page.goto(base + "/portal");
    await page.getByRole("heading", { name: "Signed-in records", exact: true }).waitFor();
    await page.screenshot({
      path: `${root}/before-${theme}-${width}x${height}.png`,
    });
    // Keep the fixture's CDP connection attached: disconnecting clears its
    // viewport/media emulation and makes the paired screenshots incomparable.
    // Chrome was launched independently with its own selected-download folder.
    // The native agent has no tool that reads that folder: this helper checks
    // the downloaded bytes itself.
    const text = `Use only the already signed-in authorized fixture tab at ${base}/portal. List the tabs, inspect its snapshot, and take a screenshot to read the two unlabelled panels' fill colors in left-to-right order. Do not inspect source code. For browser_take_screenshot omit filename entirely and set scale css, so its image bytes are returned instead of only a saved path. Read those returned image pixels directly, do not use browser_evaluate. Then click Approve fixture, inspect the changed page, and click Download selected record. Return the record identifier, colors and changed heading, and say that you did not open the downloaded file. These exact fixture actions are authorized. Do not navigate away from this origin or close any tab.`;
    let created, uploadAnalysis;
    if (compareUpload) {
      const png = readFileSync(`${root}/before-${theme}-${width}x${height}.png`);
      let res = await fetch(api + "/api/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          agent: "browser",
          text: "Analyze only the attached screenshot's actual pixels. Return its record identifier and the two unlabelled panels' fill colors in left-to-right order. Do not use any tool, browser, source code or local file. Do not change anything yet.",
          uploads: [
            {
              name: "original-page.png",
              kind: "image",
              data: png.toString("base64"),
            },
          ],
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      const session = (await res.json()).id;
      const first = await sessionRun(session);
      const firstEvents = await (await fetch(api + "/api/runs/" + first.id + "/events")).json();
      writeFileSync(
        `${root}/upload-state-${theme}-${width}x${height}.json`,
        JSON.stringify(first, null, 2),
      );
      writeFileSync(
        `${root}/upload-events-${theme}-${width}x${height}.json`,
        JSON.stringify(firstEvents, null, 2),
      );
      if (
        first.status !== "completed" ||
        !first.answer?.includes(record) ||
        !first.answer.toLowerCase().includes("yellow") ||
        !first.answer.toLowerCase().includes("purple") ||
        firstEvents.events.some((e) => e.type === "run.delta.tool.call.v1")
      )
        throw new Error(
          "Uploaded pixels were not independently analyzed without tools; inspect upload-state/events",
        );
      uploadAnalysis = {
        session,
        run: first.id,
        answer: first.answer,
        bytes: png.length,
        sha256: createHash("sha256").update(png).digest("hex"),
        toolCalls: 0,
      };
      writeFileSync(
        `${root}/upload-proof-${theme}-${width}x${height}.json`,
        JSON.stringify(uploadAnalysis, null, 2),
      );
      res = await fetch(api + "/api/sessions/" + session + "/messages", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          text:
            text +
            " Now take another screenshot after approving, also without filename and with scale css. Compare its actual panel pixels and heading against the original uploaded screenshot in this conversation. Report each original and changed color in left-to-right order. Do not infer visual changes from the button response, snapshot text or source code.",
        }),
      });
      if (!res.ok) throw new Error(await res.text());
      created = await sessionRun(session, new Set([first.id]));
    } else {
      const res = await fetch(api + "/api/runs", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ agent: "browser", text }),
      });
      if (!res.ok) throw new Error(await res.text());
      created = await res.json();
    }
    let state;
    for (let i = 0; i < 240; i++) {
      state = await (await fetch(api + "/api/runs/" + created.id)).json();
      if (["completed", "failed", "stopped"].includes(state.status)) break;
      await new Promise((r) => setTimeout(r, 1000));
    }
    const events = await (await fetch(api + "/api/runs/" + created.id + "/events")).json();
    const stem = `${theme}-${width}x${height}`;
    writeFileSync(`${root}/state-${stem}.json`, JSON.stringify(state, null, 2));
    writeFileSync(`${root}/events-${stem}.json`, JSON.stringify(events, null, 2));
    const answer = state.answer ?? "";
    if (
      state.status !== "completed" ||
      !answer.includes(record) ||
      !answer.toLowerCase().includes("yellow") ||
      !answer.toLowerCase().includes("purple") ||
      !changed ||
      downloads !== 1
    )
      throw new Error(JSON.stringify({ state: state.status, answer, changed, downloads }));
    const calls = events.events
      .filter((e) => e.type === "run.delta.tool.call.v1")
      .map((e) => e.data.name);
    if (compareUpload && calls.some((name) => /browser_(evaluate|run_code)$/.test(name)))
      throw new Error("Screenshot comparison attempted source-code evaluation");
    if (
      !calls.includes("browser__browser_take_screenshot") ||
      !calls.includes("browser__browser_tabs")
    )
      throw new Error("no screenshot/tab evidence");
    const images = wireImages.slice(imageStart).filter((i) => i.tool === "browser_take_screenshot");
    if (
      compareUpload &&
      (!answer.toLowerCase().includes("green") ||
        !answer.toLowerCase().includes("orange") ||
        !images.some((i) => !i.fixtureChanged && i.width === width && i.height === height) ||
        !images.some((i) => i.fixtureChanged && i.width === width && i.height === height))
    )
      throw new Error("Changed pixels were not captured and compared with the original upload");
    if (compareUpload) {
      const beforeImages = images.filter(
        (i) => !i.fixtureChanged && i.width === width && i.height === height,
      );
      const afterImages = images.filter(
        (i) => i.fixtureChanged && i.width === width && i.height === height,
      );
      if (!afterImages.some((after) => beforeImages.some((before) => before.sha256 !== after.sha256)))
        throw new Error("Before and after inline screenshot bytes did not differ");
    }
    if (
      wireErrors.length ||
      !images.some((i) => i.width === width && i.height === height) ||
      wireScreenshots.slice(screenshotStart).some((args) => args.filename !== undefined)
    )
      throw new Error(
        "Inline screenshot bytes or requested pixel size missing: " +
          JSON.stringify({ images, wireErrors }),
      );
    const files = readdirSync(root + "/downloads").filter((n) => n === record + ".csv");
    if (files.length !== 1) throw new Error("download file missing " + files);
    const data = readFileSync(root + "/downloads/" + files[0]);
    if (data.toString() !== `record,value\n${record},42\n`) throw new Error("wrong download bytes");
    const verifiedDownloadPath = root + "/verified-downloads/" + files[0];
    renameSync(root + "/downloads/" + files[0], verifiedDownloadPath);
    page = context.pages().find((p) => p.url() === base + "/portal");
    if (!page) throw new Error("existing tab missing");
    const cdp = await context.newCDPSession(page);
    await page.getByRole("heading", { name: "Fixture approved", exact: true }).waitFor();
    const display = await page.evaluate(() => ({
      width: innerWidth,
      height: innerHeight,
      dark: matchMedia("(prefers-color-scheme: dark)").matches,
    }));
    if (display.width !== width || display.height !== height || display.dark !== (theme === "dark"))
      throw new Error(
        "Browser changed the requested viewport or theme: " + JSON.stringify(display),
      );
    await page.screenshot({ path: `${root}/after-${stem}.png` });
    const sameTarget = (await cdp.send("Target.getTargetInfo")).targetInfo.targetId === target;
    if (!sameTarget || loginCount !== 1)
      throw new Error("did not retain original authenticated tab");
    results.push({
      theme,
      width,
      height,
      run: created.id,
      record,
      answer,
      calls,
      sameTarget,
      loginCount,
      downloads,
      downloadSHA256: createHash("sha256").update(data).digest("hex"),
      verifiedDownloadPath,
      inlineImages: images,
      ...(compareUpload ? { uploadAnalysis } : {}),
    });
    writeFileSync(root + "/results.json", JSON.stringify(results, null, 2));
    console.log(
      "PASS",
      stem,
      compareUpload
        ? "uploaded pixels without tools, same conversation, real changed screenshot comparison and exact downloaded CSV"
        : "same authenticated target, real model screenshot colors, selected mutation and exact downloaded CSV",
      created.id,
    );
  }
} finally {
  await stopWorker();
  await browser?.close();
  await stopProcess(humagen);
  await stopProcess(chrome);
  bridge.closeAllConnections();
  await new Promise((resolve) => bridge.close(resolve));
  await new Promise((resolve) => server.close(resolve));
}
