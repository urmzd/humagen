import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ImageSettings, type ImagesApi, type WorkspaceImage } from "./ImageSettings.tsx";

const words = defaultCopy.images;

afterEach(() => cleanup());

/** A server in memory: an image added checks for two reads, then is ready. */
function server(registries = ["ghcr.io"]) {
  const images: WorkspaceImage[] = [];
  const reads = new Map<string, number>();
  const calls: string[] = [];
  const api: ImagesApi = {
    list: async () => {
      for (const img of images) {
        if (img.status !== "checking") continue;
        const n = (reads.get(img.id) ?? 0) + 1;
        reads.set(img.id, n);
        if (n >= 2) {
          img.status = img.ref.includes("broken") ? "failed" : "ready";
          if (img.status === "failed") img.problem = "docker pull: manifest unknown";
          else img.digest = `${img.ref.split(":")[0]}@sha256:${"ab".repeat(32)}`;
        }
      }
      return { images: images.map((i) => ({ ...i })), registries };
    },
    add: async (input) => {
      calls.push(`add ${input.name} ${input.ref} ${input.credential ?? ""} ${!!input.warm}`);
      const img: WorkspaceImage = {
        id: `img_${images.length}`,
        name: input.name,
        ref: input.ref,
        status: "checking",
        warm: input.warm,
        credential: !!input.credential,
        createdBy: "ana",
        createdAt: "2026-11-21T00:00:00Z",
      };
      images.push(img);
      return img;
    },
    change: async (id, change) => {
      calls.push(`change ${id} ${JSON.stringify(change)}`);
      const img = images.find((i) => i.id === id);
      if (!img) throw new Error("not found");
      if (change.warm !== undefined) img.warm = change.warm;
      if (change.check) img.status = "checking";
      return img;
    },
    remove: async (id) => {
      calls.push(`remove ${id}`);
      images.splice(
        images.findIndex((i) => i.id === id),
        1,
      );
    },
    defaultRef: async () => "python:3.13-alpine",
  };
  return { api, calls };
}

test("an image added is checking, then ready and pinned; it can be kept warm and removed", async () => {
  const { api, calls } = server();
  render(<ImageSettings canEdit api={api} pollMs={10} />);
  await screen.findByText(words.empty);
  assert.match(screen.getByTestId("image-settings").textContent ?? "", /python:3\.13-alpine/);

  fireEvent.click(screen.getByRole("button", { name: words.add }));
  const form = screen.getByRole("form", { name: words.add });
  fireEvent.change(within(form).getByLabelText(words.name), { target: { value: "Agent tools" } });
  fireEvent.change(within(form).getByLabelText(words.ref), {
    target: { value: "ghcr.io/acme/agent:1.4" },
  });
  fireEvent.change(within(form).getByLabelText(words.credential), {
    target: { value: "bot:s3cret" },
  });
  fireEvent.submit(form);
  await waitFor(() => assert.equal(calls[0], "add Agent tools ghcr.io/acme/agent:1.4 bot:s3cret false"));

  const row = () => document.querySelector('[data-image="img_0"]') as HTMLElement;
  await waitFor(() => assert.equal(row()?.getAttribute("data-status"), "ready"));
  assert.match(row().textContent ?? "", /Pinned sha256:abababababab/);
  assert.match(row().textContent ?? "", new RegExp(words.status.ready));

  fireEvent.click(within(row()).getByRole("checkbox", { name: words.warm }));
  await waitFor(() => assert.ok(calls.includes('change img_0 {"warm":true}')));
  fireEvent.click(within(row()).getByRole("button", { name: words.remove }));
  fireEvent.click(within(row()).getByRole("button", { name: `${words.remove} Agent tools` }));
  await waitFor(() => assert.equal(document.querySelector('[data-image="img_0"]'), null));
  assert.ok(calls.includes("remove img_0"));
});

test("an image that fails its check says why, and can be checked again", async () => {
  const { api, calls } = server();
  await api.add({ name: "Broken", ref: "ghcr.io/acme/broken:1" });
  render(<ImageSettings canEdit api={api} pollMs={10} />);
  await waitFor(() =>
    assert.match(screen.getByTestId("image-problem").textContent ?? "", /manifest unknown/),
  );
  fireEvent.click(screen.getByRole("button", { name: words.checkAgain }));
  await waitFor(() => assert.ok(calls.includes('change img_0 {"check":true}')));
});

test("a deployment that takes no images says so; a reader sees images and changes nothing", async () => {
  const off = server([]);
  render(<ImageSettings canEdit api={off.api} />);
  await screen.findByTestId("images-off");
  assert.equal(screen.queryByRole("button", { name: words.add }), null);
  cleanup();
  const on = server();
  await on.api.add({ name: "Agent", ref: "ghcr.io/acme/agent:1" });
  render(<ImageSettings canEdit={false} api={on.api} pollMs={10} />);
  await waitFor(() => assert.ok(document.querySelector('[data-image="img_0"]')));
  assert.equal(screen.queryByRole("button", { name: words.add }), null);
  assert.equal(screen.queryByRole("button", { name: words.remove }), null);
});
