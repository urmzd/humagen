import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Component, type ComponentType, type ReactNode } from "react";
import { LazyFrame, lazyHost, lazyPage, lazyView } from "./lazy.tsx";

// Loading the console again is counted, not done.
let reloads = 0;
const reload = lazyHost.reload;
// React reports each error a frame catches; the tests assert on what is drawn instead.
const report = console.error;
beforeEach(() => {
  reloads = 0;
  lazyHost.reload = () => {
    reloads++;
  };
  console.error = () => {};
});
afterEach(() => {
  lazyHost.reload = reload;
  console.error = report;
  cleanup();
});

const words = defaultCopy.pageLoad;
const Reports = ({ who }: { who: string }) => <h1>Reports for {who}</h1>;

/** A page's code, read when the test says: `land` answers the read, `fail` refuses it. */
function code<P>(view: ComponentType<P>) {
  let calls = 0;
  let settle: { land: () => void; fail: () => void } | undefined;
  return {
    load: () => {
      calls++;
      return new Promise<ComponentType<P>>((land, fail) => {
        settle = { land: () => land(view), fail: () => fail(new TypeError("Failed to fetch")) };
      });
    },
    land: () => act(async () => settle?.land()),
    fail: () => act(async () => settle?.fail()),
    calls: () => calls,
  };
}

test("a page read on demand holds its place with rows, then draws", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, { title: () => "Reports" });
  render(<Page who="Ada" />);
  // Its own header over rows that stand in for its list: no bare word, no spinner.
  assert.ok(screen.getByRole("heading", { name: "Reports" }));
  assert.ok(screen.getByTestId("skeleton"));
  assert.equal(screen.queryByText("Reports for Ada"), null);
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.equal(screen.queryByTestId("skeleton"), null);
  assert.equal(reports.calls(), 1);
});

test("a page read ahead draws at once, with no loading state", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, { title: () => "Reports" });
  const ahead = Page.preload();
  await reports.land();
  await ahead;
  render(<Page who="Ada" />);
  // In the same step as the render: nothing stood in for it.
  assert.ok(screen.getByRole("heading", { name: "Reports for Ada" }));
  assert.equal(screen.queryByTestId("skeleton"), null);
  assert.equal(reports.calls(), 1);
});

test("a page whose code cannot be read says so and offers Retry, which reads it again", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load, { title: () => "Reports" });
  render(<Page who="Ada" />);
  await reports.fail();
  // Never an empty page: a title, what happened, and a way to try again.
  assert.ok(await screen.findByRole("heading", { name: words.title }));
  assert.equal(screen.getByRole("alert").textContent, words.failed);
  assert.equal(screen.queryByText("Reports for Ada"), null);

  fireEvent.click(screen.getByRole("button", { name: defaultCopy.retry }));
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
  assert.equal(screen.queryByRole("alert"), null);
  assert.equal(reloads, 0, "read again where it stood, without loading the console again");
});

test("when trying again fails too, the console is loaded again at its address", async () => {
  // A browser may hold the failed read against the address, and after an update the code is gone.
  const reports = code(Reports);
  const Page = lazyPage(reports.load, { title: () => "Reports" });
  render(<Page who="Ada" />);
  await reports.fail();
  assert.equal(reloads, 0, "the first failure only says so");
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.retry }));
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.fail();
  await waitFor(() => assert.equal(reloads, 1));
  // The failure stays on view until the console loads again.
  assert.equal(screen.getByRole("alert").textContent, words.failed);
  assert.ok(screen.getByRole("button", { name: defaultCopy.retry }));
});

test("offline, trying again never loads the console again, which would lose it", async () => {
  const online = Object.getOwnPropertyDescriptor(navigator, "onLine");
  Object.defineProperty(navigator, "onLine", { configurable: true, get: () => false });
  try {
    const reports = code(Reports);
    const Page = lazyPage(reports.load);
    render(<Page who="Ada" />);
    await reports.fail();
    fireEvent.click(await screen.findByRole("button", { name: defaultCopy.retry }));
    await waitFor(() => assert.equal(reports.calls(), 2));
    await reports.fail();
    assert.ok(await screen.findByRole("alert"));
    assert.equal(reloads, 0);
  } finally {
    if (online) Object.defineProperty(navigator, "onLine", online);
    else delete (navigator as { onLine?: boolean }).onLine;
  }
});

test("a failed read ahead is not held against the page: opening it reads again", async () => {
  const reports = code(Reports);
  const Page = lazyPage(reports.load);
  const ahead = Page.preload();
  await reports.fail();
  await ahead;
  render(<Page who="Ada" />);
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
});

test("a part of a headed page fails in place: its message and Retry, under the page's own title", async () => {
  const reports = code(Reports);
  const Part = lazyPage(reports.load, { frame: "rows" });
  render(
    <main>
      <h1>Monitor</h1>
      <Part who="Ada" />
    </main>,
  );
  assert.ok(screen.getByTestId("skeleton"));
  await reports.fail();
  assert.ok(await screen.findByRole("alert"));
  assert.ok(screen.getByRole("button", { name: defaultCopy.retry }));
  // The page keeps its one title.
  assert.equal(screen.getAllByRole("heading").length, 1);
});

test("what opens over the page draws nothing when it cannot be read, and reads again when opened again", async () => {
  const reports = code(Reports);
  const Dialog = lazyPage(reports.load, { frame: "none" });
  const first = render(<Dialog who="Ada" />);
  assert.equal(first.container.textContent, "");
  await reports.fail();
  await waitFor(() => assert.equal(first.container.textContent, ""));
  assert.equal(screen.queryByRole("alert"), null);
  first.unmount();

  render(<Dialog who="Ada" />);
  await waitFor(() => assert.equal(reports.calls(), 2));
  await reports.land();
  assert.ok(await screen.findByRole("heading", { name: "Reports for Ada" }));
});

test("a failure is left behind when the person goes somewhere else", async () => {
  const reports = code(Reports);
  const View = lazyView(reports.load);
  const Other = () => <h1>Agents</h1>;
  const at = (path: string) => (
    <LazyFrame resetKey={path} fallback={null}>
      {path === "/reports" ? <View who="Ada" /> : <Other />}
    </LazyFrame>
  );
  const page = render(at("/reports"));
  await reports.fail();
  assert.ok(await screen.findByRole("alert"));
  page.rerender(at("/agents"));
  assert.ok(await screen.findByRole("heading", { name: "Agents" }));
  assert.equal(screen.queryByRole("alert"), null);
});

/** Whatever is above a lazy frame: it is handed the errors the frame does not answer. */
class Above extends Component<{ children: ReactNode }, { error: unknown }> {
  override state: { error: unknown } = { error: null };
  static getDerivedStateFromError(error: unknown) {
    return { error };
  }
  override render() {
    const { error } = this.state;
    return error === null ? this.props.children : <p>above: {(error as Error).name}</p>;
  }
}

test("an error that is not a failed read is not this frame's to answer", async () => {
  const Broken = (): never => {
    throw new RangeError("a page's own mistake");
  };
  const broken = code(Broken);
  const Page = lazyPage(broken.load);
  render(
    <Above>
      <Page />
    </Above>,
  );
  await broken.land();
  assert.ok(await screen.findByText("above: RangeError"));
  assert.equal(screen.queryByText(words.failed), null);
});
