import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { useQueue } from "@humagen/web";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Composer } from "./Composer.tsx";
import type { VoiceNotes } from "./voice.ts";

const words = defaultCopy.voice;

// A recorder that hands back one chunk of the size asked for when stopped.
class FakeRecorder {
  static chunk = 64;
  static isTypeSupported = (t: string) => t === "audio/webm;codecs=opus";
  state: "inactive" | "recording" = "inactive";
  mimeType: string;
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  constructor(
    readonly stream: { getTracks: () => { stop: () => void }[] },
    opts?: { mimeType?: string },
  ) {
    this.mimeType = opts?.mimeType ?? "";
  }
  start() {
    this.state = "recording";
  }
  stop() {
    this.state = "inactive";
    this.ondataavailable?.({ data: new Blob([new Uint8Array(FakeRecorder.chunk)]) });
    this.onstop?.();
  }
}

const stopped: string[] = [];
function install(allow = true) {
  const g = globalThis as Record<string, unknown>;
  g.MediaRecorder = FakeRecorder;
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: {
      getUserMedia: async () => {
        if (!allow) throw new Error("NotAllowedError");
        return { getTracks: () => [{ stop: () => stopped.push("track") }] };
      },
    },
  });
}

afterEach(() => {
  cleanup();
  delete (globalThis as Record<string, unknown>).MediaRecorder;
  FakeRecorder.chunk = 64;
  stopped.length = 0;
});

function setup(voice?: VoiceNotes) {
  const sent: string[] = [];
  function Compose() {
    const queue = useQueue({ onStart: (m) => sent.push(m.text), onInterrupt: () => {} });
    return <Composer placeholder="Message" queue={queue} disclaimer={null} voice={voice} />;
  }
  render(<Compose />);
  return { sent, box: screen.getByRole("textbox") as HTMLTextAreaElement };
}

test("a voice note is recorded, transcribed, and put in the draft marked, to read and send", async () => {
  install();
  const heard: Blob[] = [];
  const { box, sent } = setup({
    maxBytes: 1024,
    transcribe: async (audio) => {
      heard.push(audio);
      return { text: "Move standup to ten.", language: "english" };
    },
  });
  fireEvent.click(screen.getByRole("button", { name: words.record }));
  const stop = await screen.findByRole("button", { name: words.stop });
  assert.equal(stop.getAttribute("aria-pressed"), "true");
  assert.ok(screen.getByText(words.recording), "the person is told the microphone is on");
  await act(async () => fireEvent.click(stop));
  await waitFor(() => assert.equal(box.value, "[Voice note, english] Move standup to ten."));
  assert.equal(heard.length, 1);
  assert.equal(heard[0]?.type, "audio/webm;codecs=opus");
  assert.deepEqual(stopped, ["track"], "the microphone is let go");
  // Nothing is sent until the person sends it.
  assert.equal(sent.length, 0);
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
  assert.deepEqual(sent, ["[Voice note, english] Move standup to ten."]);
});

test("a recording over what the server takes is not sent, and the person is told", async () => {
  install();
  FakeRecorder.chunk = 2048;
  let calls = 0;
  setup({
    maxBytes: 1024,
    transcribe: async () => {
      calls++;
      return { text: "x" };
    },
  });
  fireEvent.click(screen.getByRole("button", { name: words.record }));
  const stop = await screen.findByRole("button", { name: words.stop });
  await act(async () => fireEvent.click(stop));
  assert.ok(await screen.findByText(words.tooLong));
  assert.equal(calls, 0);
});

test("a refused microphone or a failed transcription is said, and the draft is kept", async () => {
  install(false);
  const { box } = setup({ maxBytes: 1024, transcribe: async () => ({ text: "x" }) });
  fireEvent.click(screen.getByRole("button", { name: words.record }));
  assert.ok(await screen.findByText(words.denied));
  cleanup();

  install();
  const again = setup({
    maxBytes: 1024,
    transcribe: async () => {
      throw new Error("transcription is unavailable right now");
    },
  });
  fireEvent.change(again.box, { target: { value: "" } });
  fireEvent.click(screen.getByRole("button", { name: words.record }));
  const stop = await screen.findByRole("button", { name: words.stop });
  await act(async () => fireEvent.click(stop));
  assert.ok(await screen.findByText(words.failed("transcription is unavailable right now")));
  assert.equal(again.box.value, "");
  assert.ok(box);
});

test("without voice notes from the server, the microphone dictates as before", () => {
  install();
  setup(undefined);
  assert.equal(screen.queryByRole("button", { name: words.record }), null);
});
