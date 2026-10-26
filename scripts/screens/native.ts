/** A deterministic local model, never a replacement for Humagen's API or tools.
 * Requests still pass through the real OpenAI adapter, runtime, sandbox and publication path.
 */
import assert from "node:assert/strict";
import { createServer } from "node:http";

interface Message {
  role: string;
  content?: unknown;
  tool_calls?: { function?: { name?: string; arguments?: string } }[];
}
interface ModelRequest {
  messages: Message[];
}
const original = {
  title: "Native launch briefing",
  slides: [
    {
      title: "Launch decision",
      body: ["Keep the November 12 launch date", "Confirm the briefing owner"],
      notes: "The date is unchanged. Ask the owner before publication.",
      sources: ["https://example.test/launch-decision"],
    },
    {
      title: "Owner and next action",
      body: ["Assign a briefing owner", "Review the draft before sharing"],
      notes: "The launch owner must be confirmed by a person.",
      sources: ["https://example.test/launch-owner"],
    },
  ],
};

function plain(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content))
    return content.map((part) => (typeof part?.text === "string" ? part.text : "")).join("\n");
  return JSON.stringify(content ?? "");
}
function revisionModel(content: unknown): typeof original {
  const text = plain(content);
  const object = JSON.parse(text);
  if (object.slides) return object;
  for (const key of ["result", "output", "content"]) {
    if (object[key]) return revisionModel(object[key]);
  }
  throw new Error("Actual file_read did not return a native revision model");
}
function response(request: ModelRequest) {
  const lastUser = request.messages.findLastIndex((message) => message.role === "user");
  const current = request.messages.slice(lastUser + 1);
  const asks = plain(request.messages[lastUser]?.content);
  const revising = /Requested changes:|please revise/i.test(asks);
  const results = current.filter((message) => message.role === "tool");
  const step = results.length;
  let tool: string | undefined, args: Record<string, unknown> | undefined;
  let model = structuredClone(original);
  if (revising) {
    const source = /artifact:([A-Za-z0-9_-]+)@(\d+)/.exec(asks);
    assert.ok(source, "Revision must carry the exact immutable source capability");
    if (step === 0) {
      tool = "file_read";
      args = { path: `artifact:${source[1]}@${source[2]}` };
    } else {
      model = revisionModel(results[0]?.content);
      assert.equal(model.slides[0]?.title, "Launch decision");
      const selected = model.slides[1];
      assert.ok(selected);
      selected.body = ["Developer owns the briefing", "Review the draft before sharing"];
      selected.notes =
        "Owner confirmed for this revision. The launch date and original evidence remain unchanged.";
    }
  }
  const offset = revising ? 1 : 0;
  if (step === offset) {
    tool = "file_write";
    args = { path: "native-launch.pptx", format: "native", content: JSON.stringify(model) };
  } else if (step === offset + 1) {
    tool = "publish_artifact";
    args = {
      path: "native-launch.pptx",
      description: revising
        ? "Native briefing with the revised owner"
        : "Native briefing generated through the actual sandbox tools",
    };
  } else if (step === offset + 2) {
    tool = "record_outcome";
    args = {
      kind: "recommendation",
      summary: revising
        ? "The native briefing now names its owner."
        : "Review the launch owner before publishing.",
      decisions: JSON.stringify(["Keep the current launch date"]),
      next_action: "Confirm who owns the briefing",
    };
  }
  if (tool)
    return {
      delta: {
        role: "assistant",
        tool_calls: [
          {
            index: 0,
            id: `native-${offset}-${step}`,
            type: "function",
            function: { name: tool, arguments: JSON.stringify(args) },
          },
        ],
      },
      finish: "tool_calls",
    };
  return {
    delta: {
      role: "assistant",
      content: revising
        ? "Published the revised native briefing with its original evidence retained."
        : "Published the editable native launch briefing.",
    },
    finish: "stop",
  };
}

export async function startNativeModel(): Promise<{ base: string; close: () => Promise<void> }> {
  const server = createServer(async (req, res) => {
    try {
      if (req.method !== "POST" || !req.url?.endsWith("/chat/completions")) {
        res.writeHead(404).end();
        return;
      }
      const chunks: Buffer[] = [];
      let size = 0;
      for await (const chunk of req) {
        const bytes = Buffer.from(chunk);
        size += bytes.length;
        if (size > 2 << 20) throw new Error("Fixture model request exceeds its bound");
        chunks.push(bytes);
      }
      const request = JSON.parse(Buffer.concat(chunks).toString()) as ModelRequest;
      const output = response(request);
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      const chunk = (delta: unknown, finish_reason?: string) =>
        JSON.stringify({
          id: "screens-native",
          object: "chat.completion.chunk",
          model: "screens-native",
          choices: [{ index: 0, delta, finish_reason }],
        });
      res.write(`data: ${chunk(output.delta)}\n\n`);
      res.write(`data: ${chunk({}, output.finish)}\n\n`);
      res.end("data: [DONE]\n\n");
    } catch (error) {
      res.writeHead(500, { "Content-Type": "application/json" }).end(
        JSON.stringify({
          error: {
            message: error instanceof Error ? error.message : "Fixture model failed",
            type: "fixture_error",
          },
        }),
      );
    }
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  return {
    base: `http://127.0.0.1:${address.port}/v1`,
    close: () =>
      new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      ),
  };
}
