import assert from "node:assert/strict";
import { test } from "node:test";
import { readAttached, uploadsOf, withAttached } from "./attachments.ts";

test("binary attachments retain exact bytes in the upload payload instead of a text fence", async () => {
  const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0, 255]);
  const { files, skipped } = await readAttached(
    [new File([bytes], "capture.png", { type: "image/png" })],
    [],
  );
  assert.deepEqual(skipped, []);
  const upload = uploadsOf(files)[0];
  assert.ok(upload);
  assert.deepEqual(Buffer.from(upload.data, "base64"), Buffer.from(bytes));
  assert.equal(withAttached("inspect these pixels", files), "inspect these pixels");
  assert.equal(files[0]?.name, "capture.png");
});

test("text files use the upload path and rejected files do not discard the draft files", async () => {
  const first = await readAttached(
    [new File(["notes"], "notes.md", { type: "text/markdown" })],
    [],
  );
  assert.equal(withAttached("read", first.files), "read");
  const textUpload = uploadsOf(first.files)[0];
  assert.ok(textUpload);
  assert.equal(Buffer.from(textUpload.data, "base64").toString(), "notes");
  const next = await readAttached([new File(["opaque"], "unknown.exe")], first.files);
  assert.deepEqual(next.files, first.files);
  assert.match(next.skipped[0] ?? "", /unsupported/);
});

test("attachments enforce count and cumulative byte bounds", async () => {
  const { files, skipped } = await readAttached(
    [new File(["x"], "clip.wav", { type: "audio/wav" })],
    [{ id: "existing", name: "existing.pdf", text: "", data: "AA==", bytes: 25 * 1024 * 1024 }],
  );
  assert.equal(files.length, 1);
  assert.match(skipped[0] ?? "", /25 MB/);
  const count = await readAttached(
    Array.from({ length: 6 }, (_, i) => new File(["x"], `${i}.png`, { type: "image/png" })),
    [],
  );
  assert.equal(count.files.length, 5);
  assert.match(count.skipped[0] ?? "", /at most 5/);
});
