// Keeps the shared browser open for as long as this container runs.
//
// The Playwright MCP server closes its one browser when the last client
// disconnects: a Humagen server that restarts, or a rollout of every pod,
// would take the open page and every session cookie with it. This is a
// client that never leaves. It connects to the server beside it, attaches to
// the browser with one harmless call, holds its stream open and answers the
// server's pings: the server drops a client that does not answer one within
// five seconds. If the connection ends all the same, it connects again at
// once.
//
// It speaks MCP streamable HTTP with node's own http module, so the image
// carries nothing more for it.

import http from "node:http";

const port = Number(process.env.HUMAGEN_BROWSER_MCP_PORT || 8931);
// The server answers only for the names it is told (entrypoint.sh). The
// first of them is as good as any: this request never leaves the container.
const host = (process.env.HUMAGEN_BROWSER_ALLOWED_HOSTS || `localhost:${port}`).split(",")[0].trim();
const wait = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/** One request to the server; resolves with its status, headers and body. */
function send(method, body, session, { stream = false } = {}) {
  return new Promise((resolve, reject) => {
    const data = body ? JSON.stringify(body) : undefined;
    const req = http.request(
      {
        host: "127.0.0.1",
        port,
        path: "/mcp",
        method,
        headers: {
          Host: host,
          Accept: "application/json, text/event-stream",
          ...(data ? { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) } : {}),
          ...(session ? { "Mcp-Session-Id": session } : {}),
        },
      },
      (res) => {
        if (stream) return resolve(res);
        let text = "";
        res.setEncoding("utf8");
        res.on("data", (chunk) => {
          text += chunk;
        });
        res.on("end", () => resolve({ status: res.statusCode, headers: res.headers, text }));
      },
    );
    req.on("error", reject);
    req.end(data);
  });
}

async function hold() {
  const hello = await send("POST", {
    jsonrpc: "2.0",
    id: 1,
    method: "initialize",
    params: { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "humagen-browser-anchor", version: "1" } },
  });
  const session = hello.headers["mcp-session-id"];
  if (hello.status !== 200 || !session) throw new Error(`initialize answered ${hello.status}`);
  await send("POST", { jsonrpc: "2.0", method: "notifications/initialized" }, session);
  // Attach to the browser: listing tabs opens it and changes nothing.
  const tabs = await send("POST", { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "browser_tabs", arguments: { action: "list" } } }, session);
  if (tabs.status !== 200) throw new Error(`browser_tabs answered ${tabs.status}`);
  console.error("anchor: holding the browser open");
  // The stream stays open until the server goes or drops this client. The
  // server's requests arrive on it as events; a ping is answered, and nothing
  // else is asked of a client that offers nothing.
  const stream = await send("GET", undefined, session, { stream: true });
  if (stream.statusCode !== 200) throw new Error(`the stream answered ${stream.statusCode}`);
  await new Promise((resolve) => {
    let buffer = "";
    stream.setEncoding("utf8");
    stream.on("data", (chunk) => {
      buffer = (buffer + chunk).replace(/\r\n/g, "\n");
      for (let end = buffer.indexOf("\n\n"); end !== -1; end = buffer.indexOf("\n\n")) {
        const event = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        const data = event
          .split("\n")
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.slice(5).trimStart())
          .join("\n");
        if (!data) continue;
        let message;
        try {
          message = JSON.parse(data);
        } catch {
          continue;
        }
        if (message.id === undefined || !message.method) continue;
        const answer =
          message.method === "ping"
            ? { jsonrpc: "2.0", id: message.id, result: {} }
            : { jsonrpc: "2.0", id: message.id, error: { code: -32601, message: "Method not found" } };
        send("POST", answer, session).catch(() => {});
      }
    });
    stream.on("end", resolve);
    stream.on("error", resolve);
    stream.on("close", resolve);
  });
}

for (;;) {
  try {
    await hold();
    // At once: while this client is away, the last other one to leave
    // closes the browser.
    console.error("anchor: the connection ended; connecting again");
  } catch (error) {
    console.error(`anchor: ${error.message}; trying again`);
    await wait(1000);
  }
}
