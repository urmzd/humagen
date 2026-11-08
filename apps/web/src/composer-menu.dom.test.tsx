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
import { PlusMenu, type Selection } from "./Plus.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

test("the composer menu offers Agents and Skills while the persistent folder owns project selection", async () => {
  net = stubFetch({
    "GET /api/projects/personal/channels": () => ({ channels: [{ name: "general" }] }),
  });
  render(
    <PlusMenu
      agents={[{ name: "helper" }]}
      skills={[]}
      onSkill={() => {}}
      projects={[]}
      personalProject="personal"
      selection={{ project: "personal", channel: "general" }}
      onChange={() => {}}
      onAttach={() => {}}
      onClose={() => {}}
    />,
  );
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  for (const name of ["Agents", "Skills"])
    assert.ok(within(menu).getByRole("button", { name: new RegExp(`^${name}\\b`) }));
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

test("Skills search Enter cannot submit work and Escape restores category focus before closing", async () => {
  net = stubFetch({});
  let closed = 0;
  const chosen: string[] = [];
  render(
    <form
      aria-label="Message"
      onSubmit={(event) => {
        event.preventDefault();
        throw new Error("search submitted the message");
      }}
    >
      <PlusMenu
        agents={[]}
        skills={[{ name: "review", description: "Review a change" }]}
        onSkill={(name) => chosen.push(name)}
        projects={[]}
        selection={{ project: null, channel: "general" }}
        onChange={() => {}}
        onAttach={() => {}}
        onClose={() => {
          closed++;
        }}
      />
    </form>,
  );
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Skills\b/ }));
  const search = await within(menu).findByRole("searchbox");
  await waitFor(() => assert.equal(document.activeElement === search, true));
  fireEvent.change(search, { target: { value: "no-such-skill" } });
  const allowed = fireEvent.keyDown(search, { key: "Enter" });
  assert.equal(allowed, false, "the search consumes Enter before the message form can submit");
  assert.deepEqual(chosen, []);
  assert.equal(closed, 0);
  fireEvent.keyDown(search, { key: "Escape" });
  assert.equal((search as HTMLInputElement).value, "", "Escape clears a nonempty filter first");
  assert.equal(document.activeElement === search, true);
  fireEvent.keyDown(search, { key: "Escape" });
  await waitFor(() =>
    assert.equal(
      document.activeElement?.textContent?.startsWith("Skills"),
      true,
      `${document.activeElement?.tagName}: ${document.activeElement?.textContent?.slice(0, 100)}`,
    ),
  );
  assert.equal(closed, 0);
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  assert.equal(closed, 1);
});

test("Skills loading and failure states never offer a stale skill choice", async () => {
  net = stubFetch({});
  const common = {
    agents: [],
    projects: [],
    selection: { project: null, channel: "general" } as Selection,
    onChange: () => {},
    onAttach: () => {},
    onClose: () => {},
    onSkill: () => {},
  };
  const { rerender } = render(<PlusMenu {...common} skills={null} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Skills\b/ }));
  assert.ok(
    within(menu).getByRole("status").textContent?.includes(defaultCopy.chat.picker.skillsLoading),
  );
  rerender(<PlusMenu {...common} skills={[{ name: "old-secret" }]} skillsFailed />);
  assert.ok(
    within(menu).getByRole("alert").textContent?.includes(defaultCopy.chat.picker.skillsFailed),
  );
  assert.equal(!!within(menu).queryByRole("button", { name: /^old-secret/ }), false);
  rerender(<PlusMenu {...common} skills={[]} />);
  assert.ok(
    within(menu).getByRole("status").textContent?.includes(defaultCopy.chat.picker.noSkills),
  );
});

for (const category of ["Agents", "Skills"] as const) {
  test(`${category} search keeps native caret movement when ArrowLeft is pressed`, async () => {
    net = stubFetch({});
    let closed = 0;
    render(
      <PlusMenu
        agents={[{ name: "helper", description: "Review changes" }]}
        skills={[{ name: "review", description: "Review a change" }]}
        onSkill={() => {}}
        projects={[]}
        selection={{ project: null, channel: "general" }}
        onChange={() => {}}
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
