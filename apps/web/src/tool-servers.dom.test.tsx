import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { PersonContext, PersonContextInput } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { YourToolServers } from "./ToolServers.tsx";

// The loader imports every DOM file into one process, so the hook acts only
// on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const starts = ["op://People/lee/"];

function serve(initial: PersonContext) {
  let cur = initial;
  return {
    "GET /api/me/context": () => cur,
    "PUT /api/me/context": (body: unknown) => {
      const input = body as PersonContextInput;
      if (input.version !== cur.version)
        return new Response(JSON.stringify({ error: "changed since it was read" }), {
          status: 409,
        });
      cur = {
        ...cur,
        run: input.run ?? {},
        tool_servers: input.tool_servers ?? [],
        version: cur.version + 1,
      };
      return cur;
    },
  };
}

test("a person adds a server by its address and a reference, and the write keeps their run settings", async () => {
  net = stubFetch(
    serve({ run: { preferences: { model: "offline/echo" } }, tool_servers: [], version: 2 }),
  );
  render(<YourToolServers references={starts} />);
  assert.ok(await screen.findByText("No tool servers"));
  fireEvent.click(screen.getByRole("button", { name: "Add Tool Server" }));
  const form = await screen.findByRole("form", { name: "New tool server" });
  fireEvent.change(screen.getByLabelText(/^Name/), { target: { value: "issues" } });
  fireEvent.change(screen.getByLabelText(/^Address/), {
    target: { value: "https://issues.example.com/mcp" },
  });
  fireEvent.change(screen.getByLabelText(/^Credential reference/), {
    target: { value: "op://People/lee/issues" },
  });
  fireEvent.submit(form);
  assert.ok(await screen.findByRole("listitem", { name: "issues" }));
  assert.ok(screen.getByText(/reads op:\/\/People\/lee\/issues/));
  const put = net.seen.find((r) => r.method === "PUT");
  assert.deepEqual(put?.body, {
    run: { preferences: { model: "offline/echo" } },
    tool_servers: [
      {
        name: "issues",
        url: "https://issues.example.com/mcp",
        credential: "op://People/lee/issues",
      },
    ],
    version: 2,
  });
});

test("a reference outside the person's own starts is refused before anything is sent", async () => {
  net = stubFetch(serve({ run: {}, tool_servers: [], version: 0 }));
  render(<YourToolServers references={starts} />);
  fireEvent.click(await screen.findByRole("button", { name: "Add Tool Server" }));
  const form = await screen.findByRole("form", { name: "New tool server" });
  fireEvent.change(screen.getByLabelText(/^Name/), { target: { value: "issues" } });
  fireEvent.change(screen.getByLabelText(/^Address/), {
    target: { value: "https://issues.example.com/mcp" },
  });
  fireEvent.change(screen.getByLabelText(/^Credential reference/), {
    target: { value: "op://Shared/team/x" },
  });
  fireEvent.submit(form);
  assert.ok(await screen.findByText(/not somewhere this workspace lets references live/));
  assert.ok(!net.seen.some((r) => r.method === "PUT"));
});

test("a person removes a server, and one who may not change them sees them with nothing to press", async () => {
  const one: PersonContext = {
    run: {},
    tool_servers: [{ name: "notes", url: "https://mcp.example.com/notes" }],
    version: 4,
  };
  net = stubFetch(serve(one));
  const { unmount } = render(<YourToolServers references={starts} />);
  fireEvent.click(await screen.findByRole("button", { name: "Remove notes" }));
  assert.ok(await screen.findByText("No tool servers"));
  assert.deepEqual(net.seen.find((r) => r.method === "PUT")?.body, {
    run: {},
    tool_servers: [],
    version: 4,
  });
  unmount();
  net.restore();

  net = stubFetch(serve(one));
  render(<YourToolServers references={starts} canConnect={false} />);
  assert.ok(await screen.findByRole("listitem", { name: "notes" }));
  assert.ok(screen.getByText("Reached without a credential."));
  for (const name of ["Add Tool Server", "Remove notes"])
    assert.ok(!screen.queryByRole("button", { name }), `${name} is offered`);
  await waitFor(() => assert.ok(!net?.seen.some((r) => r.method !== "GET")));
});
