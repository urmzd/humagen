/** A deterministic local model, never a replacement for Humagen's API or tools.
 * Requests still pass through the real OpenAI adapter, runtime, sandbox and publication path.
 */
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { REVIEW_ANSWER, REVIEW_ASK } from "./review.ts";
import {
  REVIEW_ANSWER as SESSION_REVIEW_ANSWER,
  REVIEW_ASK as SESSION_REVIEW_ASK,
} from "./session-fixtures.ts";

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
type RevisionModel = {
  title?: string;
  paragraphs?: string[];
  slides?: typeof original.slides;
  rows?: string[][];
  sources?: string[];
};
function revisionModel(content: unknown): RevisionModel {
  const text = plain(content);
  const object = JSON.parse(text);
  if (object.slides || object.paragraphs || object.rows) return object;
  for (const key of ["result", "output", "content"]) {
    if (object[key]) return revisionModel(object[key]);
  }
  throw new Error("Actual file_read did not return a native revision model");
}
type Call = { name: string; args: Record<string, unknown> };
type Reply = { text?: string; calls?: Call[] };

const PLAN = "# Launch plan\n\n- Keep November 12\n- Confirm the owner\n";
const PLAN_REVISED =
  "# Launch plan\n\n- Keep November 12\n- Developer owns the briefing\n- Review the draft on Monday\n";

/**
 * A turn over four model steps, each with words before its tool calls, so
 * the console's timeline has words, runs of calls and a file written twice to
 * draw in order (scripts/screens: conversation-timeline).
 */
const multiStep: Reply[] = [
  {
    text: "I'll draft the plan in the scratchpad first.",
    calls: [{ name: "file_write", args: { path: "plan.md", content: PLAN } }],
  },
  {
    text: "\n\nThe draft is there. Reading it back with everything else in the folder.",
    calls: [
      { name: "file_read", args: { path: "plan.md" } },
      { name: "file_list", args: { path: "." } },
    ],
  },
  {
    text: "\n\nThe owner is missing, so I'm adding it.",
    calls: [{ name: "file_write", args: { path: "plan.md", content: PLAN_REVISED } }],
  },
  {
    text: "\n\nThe plan now keeps the November 12 date, names Developer as the owner and sets a Monday review.",
  },
];

function multiStepReply(request: ModelRequest, lastUser: number): Reply {
  const step = request.messages.slice(lastUser + 1).filter((m) => m.role === "assistant").length;
  return multiStep[Math.min(step, multiStep.length - 1)] as Reply;
}

export function fixtureResponse(request: ModelRequest) {
  const lastUser = request.messages.findLastIndex((message) => message.role === "user");
  if (/multi-step plan/i.test(plain(request.messages[lastUser]?.content))) {
    const reply = multiStepReply(request, lastUser);
    const step = request.messages.slice(lastUser + 1).filter((m) => m.role === "assistant").length;
    return {
      lead: reply.text && reply.calls ? { role: "assistant", content: reply.text } : undefined,
      delta: reply.calls
        ? {
            role: "assistant",
            tool_calls: reply.calls.map((c, index) => ({
              index,
              id: `multi-${step}-${index}`,
              type: "function",
              function: { name: c.name, arguments: JSON.stringify(c.args) },
            })),
          }
        : { role: "assistant", content: reply.text },
      finish: reply.calls ? "tool_calls" : "stop",
    };
  }
  const current = request.messages.slice(lastUser + 1);
  const asks = plain(request.messages[lastUser]?.content);
  // The review card's agent answers at once with its structured review (review.ts).
  if (asks.includes(SESSION_REVIEW_ASK))
    return { delta: { role: "assistant", content: SESSION_REVIEW_ANSWER }, finish: "stop" };
  if (asks.includes(REVIEW_ASK))
    return { delta: { role: "assistant", content: REVIEW_ANSWER }, finish: "stop" };
  const revising = /Requested changes:|please revise/i.test(asks);
  const results = current.filter((message) => message.role === "tool");
  const step = results.length;
  let tool: string | undefined, args: Record<string, unknown> | undefined;
  let model: RevisionModel = structuredClone(original);
  if (revising) {
    const source = /artifact:([A-Za-z0-9_-]+)@(\d+)/.exec(asks);
    assert.ok(source, "Revision must carry the exact immutable source capability");
    if (step === 0) {
      tool = "file_read";
      args = { path: `artifact:${source[1]}@${source[2]}` };
    } else {
      model = revisionModel(results[0]?.content);
      if (model.slides) {
        assert.equal(model.slides[0]?.title, "Launch decision");
        const selected = model.slides[1];
        assert.ok(selected);
        selected.body = ["Developer owns the briefing", "Review the draft before sharing"];
        selected.notes =
          "Owner confirmed for this revision. The launch date and original evidence remain unchanged.";
      } else {
        assert.equal(model.rows?.[1]?.[1], "Developer");
        const row = model.rows?.[1];
        assert.ok(row);
        row[1] = "Confirmed Developer";
      }
    }
  }
  const documentRequested =
    /native docx evidence table|native-evidence\.docx/i.test(asks) ||
    (revising && !!model.paragraphs);
  const spreadsheetRequested =
    /native xlsx owner sheet|native-owners\.xlsx/i.test(asks) ||
    (revising && !model.slides && !model.paragraphs && !!model.rows);
  const filename = documentRequested
    ? "native-evidence.docx"
    : spreadsheetRequested
      ? "native-owners.xlsx"
      : "native-launch.pptx";
  const authored = revising
    ? model
    : documentRequested
      ? {
          title: "Native evidence brief",
          paragraphs: ["Review the evidence before publication."],
          rows: [
            ["Decision", "Owner"],
            ["Keep the launch date", "Developer"],
          ],
          sources: ["https://example.test/launch-decision"],
        }
      : spreadsheetRequested
        ? {
            rows: [
              ["Action", "Owner"],
              ["Review evidence", "Developer"],
            ],
          }
        : model;
  const offset = revising ? 1 : 0;
  if (step === offset) {
    tool = "file_write";
    args = { path: filename, format: "native", content: JSON.stringify(authored) };
  } else if (step === offset + 1) {
    tool = "publish_artifact";
    args = {
      path: filename,
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
      const output = fixtureResponse(request);
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      const chunk = (delta: unknown, finish_reason?: string) =>
        JSON.stringify({
          id: "screens-native",
          object: "chat.completion.chunk",
          model: "screens-native",
          choices: [{ index: 0, delta, finish_reason }],
        });
      if ("lead" in output && output.lead) res.write(`data: ${chunk(output.lead)}\n\n`);
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
