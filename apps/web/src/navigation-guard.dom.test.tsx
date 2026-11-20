import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { href, useRoute } from "./route.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

function Navigation() {
  const [route, go] = useRoute();
  return (
    <>
      <output aria-label="Current address">{href(route)}</output>
      <button type="button" onClick={() => go({ page: "projects" })}>
        Leave Conversation
      </button>
      <button
        type="button"
        onClick={() => go({ page: "conversation", id: "test", panel: "deliverables" })}
      >
        Open Deliverables
      </button>
    </>
  );
}

test("a draft guard can cancel navigation without losing the current screen or address", async () => {
  window.history.replaceState(null, "", "/messages/test?panel=files");
  const block = (event: Event) => event.preventDefault();
  window.addEventListener("humagen:before-navigate", block);
  try {
    render(<Navigation />);
    fireEvent.click(screen.getByRole("button", { name: "Leave Conversation" }));
    assert.equal(
      screen.getByLabelText("Current address").textContent,
      "/messages/test?panel=files",
    );
    assert.equal(window.location.pathname + window.location.search, "/messages/test?panel=files");
    // Drawer switches keep the same mounted conversation, so drafts remain available.
    fireEvent.click(screen.getByRole("button", { name: "Open Deliverables" }));
    await waitFor(() =>
      assert.equal(
        window.location.pathname + window.location.search,
        "/messages/test?panel=files&legacy_panel=deliverables",
      ),
    );
    assert.equal(
      screen.getByLabelText("Current address").textContent,
      "/messages/test?panel=files&legacy_panel=deliverables",
    );
  } finally {
    window.removeEventListener("humagen:before-navigate", block);
  }
});

test("a canceled history navigation restores the address of the retained conversation", () => {
  window.history.replaceState(null, "", "/messages/test?panel=files");
  const block = (event: Event) => event.preventDefault();
  window.addEventListener("humagen:before-navigate", block);
  try {
    render(<Navigation />);
    window.history.pushState(null, "", "/projects");
    fireEvent(window, new PopStateEvent("popstate"));
    assert.equal(window.location.pathname + window.location.search, "/messages/test?panel=files");
    assert.equal(
      screen.getByLabelText("Current address").textContent,
      "/messages/test?panel=files",
    );
  } finally {
    window.removeEventListener("humagen:before-navigate", block);
  }
});
