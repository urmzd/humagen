import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { ItemCard, Pill, Reference } from "./cards.tsx";

afterEach(() => cleanup());

test("a card is one link with its mark, one status, a description, facts and a ⋯ menu", async () => {
  const chosen: string[] = [];
  let opened = 0;
  render(
    <ItemCard
      title="Nightly digest"
      href="/schedules/nightly"
      onOpen={() => opened++}
      mark={<span data-testid="mark" />}
      status={<Pill tone="ok">Active</Pill>}
      description="Posts the day's sessions to #eng"
      facts={<Reference kind="slack" label="#eng" href="https://app.slack.com/client/T1/C1" />}
      menu={[
        { label: "Pause", onSelect: () => chosen.push("pause") },
        { label: "Delete", onSelect: () => chosen.push("delete"), destructive: true },
      ]}
    />,
  );
  const card = screen.getByRole("article");
  // One link for the card, and no Open button.
  const link = within(card).getByRole("link", { name: "Nightly digest" });
  assert.equal(link.getAttribute("href"), "/schedules/nightly");
  assert.equal(within(card).queryByRole("button", { name: /^Open/ }), null);
  fireEvent.click(link);
  assert.equal(opened, 1);
  assert.ok(within(card).getByTestId("mark"));
  assert.ok(within(card).getByText("Active"));
  assert.ok(within(card).getByText("Posts the day's sessions to #eng"));
  // Controls inside the card stay their own: the reference and the menu.
  assert.equal(within(card).getByRole("link", { name: "#eng" }).getAttribute("target"), "_blank");
  fireEvent.click(within(card).getByRole("button", { name: "More" }));
  const menu = await within(card).findByRole("menu");
  const rows = [...menu.children].map((el) => (el.tagName === "HR" ? "—" : el.textContent));
  assert.deepEqual(rows, ["Pause", "—", "Delete"]);
  for (const row of within(menu).getAllByRole("menuitem"))
    assert.doesNotMatch(row.className, /\bborder\b/);
  fireEvent.click(within(menu).getByRole("menuitem", { name: "Delete" }));
  assert.deepEqual(chosen, ["delete"]);
});

test("a card leaves out a status it was not given", () => {
  render(<ItemCard title="Plain" />);
  assert.equal(screen.getByRole("article").querySelectorAll(".shrink-0").length, 0);
});

test("a reference has a mark, a label, an optional count, and opens where it points", () => {
  render(
    <ul>
      <li>
        <Reference
          kind="github"
          label="urmzd/humagen PR #5"
          href="https://github.com/urmzd/humagen/pull/5"
        />
      </li>
      <li>
        <Reference kind="corpus" label="Uploads" count={1} />
      </li>
      <li>
        <Reference kind="session" label="@ops" href="/messages/s_child" />
      </li>
    </ul>,
  );
  const pr = screen.getByRole("link", { name: "urmzd/humagen PR #5" });
  assert.equal(pr.getAttribute("target"), "_blank");
  assert.ok(pr.querySelector("svg"), "GitHub's mark leads it");
  assert.match(screen.getByText("Uploads").parentElement?.textContent ?? "", /Uploads· 1/);
  const child = screen.getByRole("link", { name: "@ops" });
  assert.equal(child.getAttribute("target"), null);
  fireEvent.click(child);
  assert.equal(window.location.pathname, "/messages/s_child");
});
