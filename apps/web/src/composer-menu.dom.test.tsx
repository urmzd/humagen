import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { stubFetch } from "./dom.ts";
import { useCompletion } from "./completion.ts";
import { tree } from "./projects.fixtures.ts";
import { PlusMenu, type Selection } from "./Plus.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

test("the composer root groups Agents Projects and Skills without a loose default channel", async () => {
  net = stubFetch({ "GET /api/projects/personal/channels": () => ({ channels: [{ name: "general" }] }) });
  render(<PlusMenu agents={[{ name: "helper" }]} skills={[]} onSkill={() => {}} projects={[]} personalProject="personal" selection={{ agents: [], mode: "best_fit", project: "personal", channel: "general" }} onChange={() => {}} onAttach={() => {}} onClose={() => {}} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  for (const name of ["Agents", "Projects", "Skills"]) assert.ok(within(menu).getByRole("button", { name: new RegExp(`^${name}\\b`) }));
  await waitFor(() => assert.equal(!!within(menu).queryByRole("radio", { name: "#general" }), false));
  assert.equal(!!within(menu).queryByRole("checkbox", { name: /^helper/ }), false, "agent choices live inside Agents");
});


test("project skill options clear while a new project's skills load and stay empty after denial", async () => {
  let answer: (value: unknown) => void = () => {};
  net = stubFetch({
    "GET /api/projects/Globex/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/projects/Vault/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/skills": (_, query) => query.get("project") === "Globex"
      ? { skills: [{ name: "review", description: "Review the release", from: "Globex", argument_hint: "" }] }
      : new Promise(resolve => { answer = resolve; }),
    "GET /api/mentions": () => ({ kinds: [] }),
  });
  const { result, rerender } = renderHook(({ project }) => useCompletion(project, [], {}, [], "ada"), { initialProps: { project: "Globex" } });
  await waitFor(() => assert.equal(result.current.skills?.[0]?.name, "review"));
  rerender({ project: "Vault" });
  const pendingSkills = result.current.skills;
  assert.equal(pendingSkills, null, "the previous project's skills disappear immediately");
  assert.equal(result.current.options.some(option => option.kind === "skill"), false);
  await act(async () => answer(new Response(null, { status: 403 })));
  await waitFor(() => assert.equal(result.current.skillsFailed, true));
  assert.equal(result.current.skills?.length ?? 0, 0);
  assert.equal(result.current.options.some(option => option.kind === "skill"), false);
});

test("a late skill lookup from the previous project cannot enter the current menu or autocomplete", async () => {
  let answer: (value: unknown) => void = () => {};
  net = stubFetch({
    "GET /api/projects/Globex/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/projects/Vault/agents": () => ({ available: { all: true, names: [] } }),
    "GET /api/skills": (_, query) => query.get("project") === "Globex"
      ? new Promise(resolve => { answer = resolve; })
      : { skills: [{ name: "vault-check", description: "Check this project", from: "Vault", argument_hint: "" }] },
    "GET /api/mentions": () => ({ kinds: [] }),
  });
  const { result, rerender } = renderHook(({ project }) => useCompletion(project, [], {}, [], "ada"), { initialProps: { project: "Globex" } });
  await waitFor(() => assert.ok(net?.seen.some(request => request.path === "/api/skills?project=Globex")));
  rerender({ project: "Vault" });
  await waitFor(() => assert.equal(result.current.skills?.[0]?.name, "vault-check"));
  await act(async () => answer({ skills: [{ name: "old-secret", description: "Must stay in previous project", from: "Globex", argument_hint: "" }] }));
  assert.deepEqual(result.current.skills?.map(skill => skill.name), ["vault-check"]);
  assert.equal(result.current.options.some(option => option.token.includes("old-secret")), false);
});


function ProjectMenu({ onChange, onClose, project = "Globex" }: { onChange: (selection: Selection) => void; onClose: () => void; project?: string }) {
  const [open, setOpen] = useState(true);
  const [selection, setSelection] = useState<Selection>({ agents: [], mode: "best_fit", project, channel: "deals" });
  if (!open) return null;
  return <PlusMenu agents={[{ name: "helper" }]} skills={[]} onSkill={() => {}} projects={tree} selection={selection} onChange={next => { setSelection(next); onChange(next); }} onAttach={() => {}} onClose={() => { setOpen(false); onClose(); }} />;
}

test("project selection resets the channel and real choices stay inside the project pane as full rows", async () => {
  const changed: Selection[] = [];
  net = stubFetch({
    "GET /api/projects/Globex/channels": () => ({ channels: [{ name: "general" }, { name: "deals" }] }),
    "GET /api/projects/Initech/channels": () => ({ channels: [{ name: "general" }, { name: "triage" }] }),
  });
  render(<ProjectMenu onChange={next => changed.push(next)} onClose={() => {}} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  assert.equal(!!within(menu).queryByRole("radiogroup"), false);
  fireEvent.click(within(menu).getByRole("button", { name: /^Projects\b/ }));
  fireEvent.click(await within(menu).findByRole("button", { name: /^Initech/ }));
  await waitFor(() => assert.equal(changed.at(-1)?.project, "Initech"));
  assert.equal(changed.at(-1)?.channel, "general");
  const channels = await within(menu).findByRole("radiogroup");
  assert.ok(within(menu).getByRole("heading", { name: defaultCopy.chat.picker.channelsIn("Initech") }), "channels identify their project");
  assert.deepEqual(within(channels).getAllByRole("radio").map(row => row.textContent), ["#general", "#triage"]);
  for (const row of within(channels).getAllByRole("radio")) assert.match(row.className, /(?:^|\s)w-full(?:\s|$)/);
  assert.equal(within(channels).getByRole("radio", { name: "#general" }).getAttribute("aria-checked"), "true");
  fireEvent.click(within(channels).getByRole("radio", { name: "#triage" }));
  assert.equal(changed.at(-1)?.channel, "triage");
  assert.equal(net.seen.filter(request => request.method === "POST").length, 0);
});

test("a project with only General is selected without showing a loose default channel", async () => {
  let closed = 0;
  const changed: Selection[] = [];
  net = stubFetch({
    "GET /api/projects/Initech/channels": () => ({ channels: [{ name: "general" }, { name: "deals" }] }),
    "GET /api/projects/Globex/channels": () => ({ channels: [{ name: "general" }] }),
  });
  render(<ProjectMenu project="Initech" onChange={next => changed.push(next)} onClose={() => { closed++; }} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Projects\b/ }));
  fireEvent.click(within(menu).getByRole("button", { name: /^Globex\s+Account/ }));
  await waitFor(() => assert.equal(closed, 1));
  assert.equal(changed.at(-1)?.channel, "general");
  assert.equal(!!screen.queryByRole("dialog", { name: defaultCopy.chat.menu }), false);
  assert.equal(!!screen.queryByRole("radio", { name: "#general" }), false);
});

test("Skills search Enter cannot submit work and Escape restores category focus before closing", async () => {
  net = stubFetch({});
  let closed = 0;
  const chosen: string[] = [];
  render(<form aria-label="Message" onSubmit={event => { event.preventDefault(); throw new Error("search submitted the message"); }}>
    <PlusMenu agents={[]} skills={[{ name: "review", description: "Review a change" }]} onSkill={name => chosen.push(name)} projects={[]} selection={{ agents: [], mode: "best_fit", project: null, channel: "general" }} onChange={() => {}} onAttach={() => {}} onClose={() => { closed++; }} />
  </form>);
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
  await waitFor(() => assert.equal(document.activeElement?.textContent?.startsWith("Skills"), true, `${document.activeElement?.tagName}: ${document.activeElement?.textContent?.slice(0, 100)}`));
  assert.equal(closed, 0);
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  assert.equal(closed, 1);
});

test("Skills loading and failure states never offer a stale skill choice", async () => {
  net = stubFetch({});
  const common = { agents: [], projects: [], selection: { agents: [], mode: "best_fit", project: null, channel: "general" } as Selection, onChange: () => {}, onAttach: () => {}, onClose: () => {}, onSkill: () => {} };
  const { rerender } = render(<PlusMenu {...common} skills={null} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Skills\b/ }));
  assert.ok(within(menu).getByRole("status").textContent?.includes(defaultCopy.chat.picker.skillsLoading));
  rerender(<PlusMenu {...common} skills={[{ name: "old-secret" }]} skillsFailed />);
  assert.ok(within(menu).getByRole("alert").textContent?.includes(defaultCopy.chat.picker.skillsFailed));
  assert.equal(!!within(menu).queryByRole("button", { name: /^old-secret/ }), false);
  rerender(<PlusMenu {...common} skills={[]} />);
  assert.ok(within(menu).getByRole("status").textContent?.includes(defaultCopy.chat.picker.noSkills));
});

test("a refused channel read exposes no manufactured General choice and retry uses the real read", async () => {
  let refuse = true;
  let reads = 0;
  let closed = 0;
  net = stubFetch({ "GET /api/projects/Globex/channels": () => {
    reads++;
    return refuse ? new Response(null, { status: 403 }) : { channels: [{ name: "general" }, { name: "deals" }] };
  } });
  render(<ProjectMenu onChange={() => {}} onClose={() => { closed++; }} />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Projects\b/ }));
  fireEvent.click(within(menu).getByRole("button", { pressed: true }));
  await within(menu).findByRole("alert");
  assert.equal(!!within(menu).queryByRole("radio"), false);
  assert.equal(closed, 0);
  const before = reads;
  refuse = false;
  fireEvent.click(within(menu).getByRole("button", { name: defaultCopy.chat.picker.retry }));
  await within(menu).findByRole("radio", { name: "#deals" });
  assert.equal(reads, before + 1);
  assert.equal(net.seen.filter(request => request.method === "POST").length, 0);
});

for (const category of ["Agents", "Skills"] as const) {
  test(`${category} search keeps native caret movement when ArrowLeft is pressed`, async () => {
    net = stubFetch({});
    let closed = 0;
    render(<PlusMenu agents={[{ name: "helper", description: "Review changes" }]} skills={[{ name: "review", description: "Review a change" }]} onSkill={() => {}} projects={[]} selection={{ agents: [], mode: "best_fit", project: null, channel: "general" }} onChange={() => {}} onAttach={() => {}} onClose={() => { closed++; }} />);
    const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
    fireEvent.click(within(menu).getByRole("button", { name: new RegExp(`^${category}\\b`) }));
    const search = await within(menu).findByRole("searchbox") as HTMLInputElement;
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

test("browsing the selected project's channels preserves a nondefault channel until an explicit change", async () => {
  net = stubFetch({ "GET /api/projects/Launch/channels": () => ({ channels: [{ name: "general" }, { name: "deals" }] }) });
  const launch = { ...tree.find(project => project.name === "Globex")!, name: "Launch", title: "Launch" };
  const changes: Selection[] = [];
  let current: Selection = { agents: [], mode: "best_fit", project: "Launch", channel: "deals" };
  function ExistingProjectMenu() {
    const [selection, setSelection] = useState(current);
    current = selection;
    return <PlusMenu agents={[]} skills={[]} onSkill={() => {}} projects={[launch]} selection={selection} onChange={next => { changes.push(next); setSelection(next); }} onAttach={() => {}} onClose={() => {}} />;
  }
  render(<ExistingProjectMenu />);
  const menu = screen.getByRole("dialog", { name: defaultCopy.chat.menu });
  fireEvent.click(within(menu).getByRole("button", { name: /^Projects\b/ }));
  fireEvent.click(within(menu).getByRole("button", { pressed: true }));
  const channels = await within(menu).findByRole("radiogroup");
  assert.equal(within(channels).getByRole("radio", { name: "#deals" }).getAttribute("aria-checked"), "true", "browsing the current project must keep its selected channel");
  assert.equal(current.channel, "deals");
  assert.deepEqual(changes, [], "opening the current project's channels does not change the unsent destination");
  fireEvent.click(within(menu).getByRole("button", { name: defaultCopy.chat.picker.back }));
  await within(menu).findByRole("searchbox", { name: defaultCopy.projects.search });
  assert.equal(current.channel, "deals");
  fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
  assert.ok(within(menu).getByRole("button", { name: /^Projects\b/ }));
  assert.equal(current.channel, "deals");
  assert.deepEqual(changes, []);
});
