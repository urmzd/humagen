import assert from "node:assert/strict";
import { test } from "node:test";
import type { PersonContext } from "./api.ts";
import {
  credentialWords,
  hostOf,
  MAX_TOOL_SERVERS,
  toolServerProblem,
  withoutToolServer,
  withToolServer,
} from "./toolServers.ts";

const starts = ["op://People/lee/"];
const ctx: PersonContext = {
  run: { preferences: { model: "offline/echo" } },
  tool_servers: [{ name: "notes", url: "https://mcp.example.com/notes" }],
  version: 3,
};

test("a server the server would refuse is refused sooner, in the same terms", () => {
  const ok = { name: "issues", url: "https://issues.example.com/mcp" };
  assert.equal(toolServerProblem(ok, ctx.tool_servers, starts), null);
  assert.match(toolServerProblem({ ...ok, name: "Issues" }, [], starts) ?? "", /lower case/);
  assert.match(
    toolServerProblem({ ...ok, name: "notes" }, ctx.tool_servers, starts) ?? "",
    /already/,
  );
  assert.match(
    toolServerProblem({ ...ok, url: "http://issues.example.com" }, [], starts) ?? "",
    /https/,
  );
  assert.match(
    toolServerProblem({ ...ok, url: "https://a:b@issues.example.com" }, [], starts) ?? "",
    /no user/,
  );
  assert.match(toolServerProblem({ ...ok, url: "not a url" }, [], starts) ?? "", /https/);
  assert.match(
    toolServerProblem({ ...ok, credential: "op://Shared/team/issues" }, [], starts) ?? "",
    /not somewhere/,
  );
  assert.equal(
    toolServerProblem({ ...ok, credential: "op://People/lee/issues" }, [], starts),
    null,
  );
  const full = Array.from({ length: MAX_TOOL_SERVERS }, (_, i) => ({
    name: `s${i}`,
    url: "https://x.example",
  }));
  assert.match(toolServerProblem(ok, full, starts) ?? "", /at most 16/);
});

test("adding and removing keep the run settings and send the version read", () => {
  const added = withToolServer(ctx, {
    name: "issues",
    url: " https://issues.example.com/mcp ",
    credential: " op://People/lee/issues ",
  });
  assert.deepEqual(added, {
    run: ctx.run,
    tool_servers: [
      { name: "notes", url: "https://mcp.example.com/notes" },
      {
        name: "issues",
        url: "https://issues.example.com/mcp",
        credential: "op://People/lee/issues",
      },
    ],
    version: 3,
  });
  // An empty reference is left out, not sent as "".
  assert.ok(
    !(
      "credential" in
      (withToolServer(ctx, { name: "x", url: "https://x.example", credential: "" })
        .tool_servers?.[1] ?? {})
    ),
  );
  assert.deepEqual(withoutToolServer(ctx, "notes"), { run: ctx.run, tool_servers: [], version: 3 });
});

test("a server is shown by its host, and its reference but never a secret", () => {
  assert.equal(hostOf("https://MCP.Example.com:8443/x"), "mcp.example.com");
  assert.equal(hostOf("nonsense"), "nonsense");
  assert.match(
    credentialWords({ name: "a", url: "https://a", credential: "op://People/lee/a" }),
    /reads op:\/\/People\/lee\/a/,
  );
  assert.equal(credentialWords({ name: "a", url: "https://a" }), "Reached without a credential.");
});
