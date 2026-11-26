import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { useCompletion } from "./completion.ts";
import { stubFetch } from "./dom.ts";
import { PlusMenu } from "./Plus.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

test("a conversation's + menu offers files and Agents, never Skills or projects", async () => {
  net = stubFetch({
    "GET /api/projects/personal/channels": () => ({ channels: [{ name: "general" }] }),
  });
  render(<PlusMenu agents={[{ name: "helper" }]} onAttach={() => {}} onClose={() => {}} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  assert.ok(within(menu).getByRole("button", { name: /^Agents\b/ }));
  assert.equal(
    within(menu).queryByRole("button", { name: /^Skills\b/ }),
    null,
    "skills are typed as /",
  );
  assert.equal(within(menu).queryByRole("button", { name: /^Projects\b/ }), null);
  assert.equal(within(menu).queryByRole("radio", { name: "#general" }), null);
  assert.equal(net.seen.length, 0, "the menu does not read legacy channels");
  assert.equal(
    !!within(menu).queryByRole("checkbox", { name: /^helper/ }),
    false,
    "agent choices live inside Agents",
  );
});

test("project skill options clear while a new project's skills load and stay empty after denial", async () => {
  let answer: (value: unknown) => void = () => {};
  net = stubFetch({
    "GET /api/projects/Globex/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/projects/Vault/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/skills": (_, query) =>
      query.get("project") === "Globex"
        ? {
            skills: [
              {
                name: "review",
                description: "Review the release",
                from: "Globex",
                argument_hint: "",
              },
            ],
          }
        : new Promise((resolve) => {
            answer = resolve;
          }),
    "GET /api/mentions": () => ({ kinds: [] }),
  });
  const { result, rerender } = renderHook(
    ({ project }) => useCompletion(project, [], {}, [], "ada"),
    { initialProps: { project: "Globex" } },
  );
  await waitFor(() => assert.equal(result.current.skills?.[0]?.name, "review"));
  rerender({ project: "Vault" });
  const pendingSkills = result.current.skills;
  assert.equal(pendingSkills, null, "the previous project's skills disappear immediately");
  assert.equal(
    result.current.options.some((option) => option.kind === "skill"),
    false,
  );
  await act(async () => answer(new Response(null, { status: 403 })));
  await waitFor(() => assert.equal(result.current.skillsFailed, true));
  assert.equal(result.current.skills?.length ?? 0, 0);
  assert.equal(
    result.current.options.some((option) => option.kind === "skill"),
    false,
  );
});

test("a late skill lookup from the previous project cannot enter the current menu or autocomplete", async () => {
  let answer: (value: unknown) => void = () => {};
  net = stubFetch({
    "GET /api/projects/Globex/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/projects/Vault/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/skills": (_, query) =>
      query.get("project") === "Globex"
        ? new Promise((resolve) => {
            answer = resolve;
          })
        : {
            skills: [
              {
                name: "vault-check",
                description: "Check this project",
                from: "Vault",
                argument_hint: "",
              },
            ],
          },
    "GET /api/mentions": () => ({ kinds: [] }),
  });
  const { result, rerender } = renderHook(
    ({ project }) => useCompletion(project, [], {}, [], "ada"),
    { initialProps: { project: "Globex" } },
  );
  await waitFor(() =>
    assert.ok(net?.seen.some((request) => request.path === "/api/skills?project=Globex")),
  );
  rerender({ project: "Vault" });
  await waitFor(() => assert.equal(result.current.skills?.[0]?.name, "vault-check"));
  await act(async () =>
    answer({
      skills: [
        {
          name: "old-secret",
          description: "Must stay in previous project",
          from: "Globex",
          argument_hint: "",
        },
      ],
    }),
  );
  assert.deepEqual(
    result.current.skills?.map((skill) => skill.name),
    ["vault-check"],
  );
  assert.equal(
    result.current.options.some((option) => option.token.includes("old-secret")),
    false,
  );
});

for (const category of ["Agents"] as const) {
  test(`${category} search keeps native caret movement when ArrowLeft is pressed`, async () => {
    net = stubFetch({});
    let closed = 0;
    render(
      <PlusMenu
        agents={[{ name: "helper", description: "Review changes" }]}
        onAttach={() => {}}
        onClose={() => {
          closed++;
        }}
      />,
    );
    const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
    fireEvent.click(within(menu).getByRole("button", { name: new RegExp(`^${category}\\b`) }));
    const search = (await within(menu).findByRole("searchbox")) as HTMLInputElement;
    fireEvent.change(search, { target: { value: "review" } });
    search.setSelectionRange(4, 4);
    const allowed = fireEvent.keyDown(search, { key: "ArrowLeft" });
    assert.equal(allowed, true, "navigation must not prevent the input's native caret movement");
    assert.equal(document.activeElement === search, true);
    assert.ok(within(menu).getByRole("heading", { name: category }));
    assert.equal(search.value, "review");
    assert.equal(closed, 0);
  });
}

test("a project's agents and skills read before show at once when its bar is drawn again", async () => {
  let reads = 0;
  let answer: (value: unknown) => void = () => {};
  net = stubFetch({
    "GET /api/projects/Globex/agents": () => {
      reads++;
      // The second read waits, as on a slow link: what was read before stands in.
      return reads === 1
        ? {
            available: { all: true, names: [] },
            default_resolved: { agent: "lead", from: "project:Globex" },
          }
        : new Promise((resolve) => {
            answer = resolve;
          });
    },
    "GET /api/skills": () => ({
      skills: [{ name: "review", description: "Review", from: "Globex", argument_hint: "" }],
    }),
    "GET /api/mentions": () => ({ kinds: [] }),
  });
  const first = renderHook(() => useCompletion("Globex", [], {}, [], "ada"));
  await waitFor(() => assert.equal(first.result.current.skills?.[0]?.name, "review"));
  await waitFor(() => assert.equal(first.result.current.defaultAgent, "lead"));
  first.unmount();
  // Home again: nothing says loading, and the reads go out again behind it.
  const again = renderHook(() => useCompletion("Globex", [], {}, [], "ada"));
  assert.equal(again.result.current.agentsLoading, false);
  assert.equal(again.result.current.defaultAgent, "lead");
  assert.equal(again.result.current.skills?.[0]?.name, "review");
  await waitFor(() => assert.equal(reads, 2));
  await act(async () =>
    answer({
      available: { all: true, names: [] },
      default_resolved: { agent: "writer", from: "project:Globex" },
    }),
  );
  await waitFor(() => assert.equal(again.result.current.defaultAgent, "writer"));
  again.unmount();
});
