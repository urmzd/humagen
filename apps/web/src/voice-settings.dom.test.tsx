import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { VoiceProvider } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { VoiceSettings } from "./VoiceSettings.tsx";

// Workspace settings › Voice: the workspace's voice provider, off until set up.

const openaiPrice = { hear_per_hour: 360_000, min_seconds: 0, speak_per_million_chars: 15_000_000 };
const kinds = [
  {
    kind: "groq",
    model: "whisper-large-v3-turbo",
    speech_model: "canopylabs/orpheus-v1-english",
    voice: "autumn",
    voices: ["autumn", "diana"],
    price: { hear_per_hour: 40_000, min_seconds: 10, speak_per_million_chars: 22_000_000 },
  },
  {
    kind: "openai",
    model: "whisper-1",
    speech_model: "tts-1",
    voice: "alloy",
    voices: ["alloy", "nova"],
    price: openaiPrice,
  },
];

const openai: VoiceProvider = {
  kind: "openai",
  model: "whisper-1",
  speech_model: "tts-1",
  voice: "nova",
  price: openaiPrice,
  key_stored: true,
  set_by: "ada",
  set_at: "2026-11-07T00:00:00Z",
};

let net: ReturnType<typeof stubFetch> | undefined;
const realAudio = globalThis.Audio;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  globalThis.Audio = realAudio;
});

test("an admin sets OpenAI up with a pasted key and a voice; the key is not shown again", async () => {
  let provider: VoiceProvider | undefined;
  net = stubFetch({
    "GET /api/voice/provider": () => ({ provider, kinds }),
    "PUT /api/voice/provider": () => {
      provider = openai;
      return openai;
    },
  });
  render(<VoiceSettings canEdit />);
  const list = await screen.findByRole("list", { name: "Voice" });
  assert.equal(within(list).getAllByText("Not set up").length, 2);
  fireEvent.click(within(list).getAllByRole("button", { name: "Set Up" })[1] as HTMLElement);
  const form = await screen.findByRole("form", { name: "Set up OpenAI" });
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: " sk-test " } });
  fireEvent.change(within(form).getByLabelText("Voice"), { target: { value: "nova" } });
  fireEvent.click(within(form).getByRole("button", { name: "Save" }));
  await screen.findByText("whisper-1 · voice nova · key kept");
  const put = net.seen.find((r) => r.method === "PUT");
  // The price shown was left alone, so it is not pinned: it follows the provider's.
  assert.deepEqual(put?.body, {
    kind: "openai",
    voice: "nova",
    model: "whisper-1",
    speech_model: "tts-1",
    key: "sk-test",
  });
  assert.equal(screen.queryByRole("form"), null);
  assert.ok(screen.getByText("On"));
  // Groq can replace it, and says so.
  assert.ok(screen.getByText("Replaces OpenAI"));
});

test("a changed price is sent in micro-USD, and a price that is not one is refused", async () => {
  net = stubFetch({
    "GET /api/voice/provider": () => ({ kinds }),
    "PUT /api/voice/provider": () => ({ ...openai, kind: "groq" }),
  });
  render(<VoiceSettings canEdit />);
  fireEvent.click((await screen.findAllByRole("button", { name: "Set Up" }))[0] as HTMLElement);
  const form = await screen.findByRole("form", { name: "Set up Groq" });
  fireEvent.change(within(form).getByLabelText(/^Value/), { target: { value: "gsk" } });
  const hear = within(form).getByLabelText(/^Dollars an hour of audio heard/);
  assert.equal((hear as HTMLInputElement).value, "0.04");
  fireEvent.change(hear, { target: { value: "0.05x" } });
  fireEvent.click(within(form).getByRole("button", { name: "Save" }));
  await screen.findByText("A price is dollars, such as 0.04, up to 1000.");
  assert.equal(net.seen.filter((r) => r.method === "PUT").length, 0);
  fireEvent.change(hear, { target: { value: "0.05" } });
  fireEvent.click(within(form).getByRole("button", { name: "Save" }));
  await waitFor(() => assert.ok(net?.seen.some((r) => r.method === "PUT")));
  const put = net.seen.find((r) => r.method === "PUT")?.body as { price?: unknown };
  assert.deepEqual(put.price, {
    hear_per_hour: 50_000,
    min_seconds: 10,
    speak_per_million_chars: 22_000_000,
  });
});

test("Try Voice plays the sample and says what was heard; Turn Off asks first", async () => {
  const played: string[] = [];
  globalThis.Audio = class {
    src: string;
    constructor(src: string) {
      this.src = src;
    }
    play() {
      played.push(this.src);
      return Promise.resolve();
    }
    pause() {}
  } as unknown as typeof Audio;
  let provider: VoiceProvider | undefined = openai;
  net = stubFetch({
    "GET /api/voice/provider": () => ({ provider, kinds }),
    "POST /api/voice/provider/check": () => ({
      audio: "SUQz",
      media_type: "audio/mpeg",
      heard: "Voice is set up.",
      cost_micros: 1102,
    }),
    "DELETE /api/voice/provider": () => {
      provider = undefined;
      return undefined;
    },
  });
  render(<VoiceSettings canEdit />);
  fireEvent.click(await screen.findByRole("button", { name: "Try Voice" }));
  await screen.findByText('Heard back: "Voice is set up."');
  assert.equal(screen.getByRole("status").textContent, 'Heard back: "Voice is set up."');
  assert.deepEqual(played, ["data:audio/mpeg;base64,SUQz"]);
  fireEvent.click(screen.getByRole("button", { name: "Turn Off" }));
  assert.equal(net.seen.filter((r) => r.method === "DELETE").length, 0);
  fireEvent.click(screen.getByRole("button", { name: "Turn Off Voice" }));
  await waitFor(() => assert.equal(screen.getAllByText("Not set up").length, 2));
});

test("someone who cannot manage connections is told so, with nothing to press", async () => {
  net = stubFetch({
    "GET /api/voice/provider": () =>
      new Response(JSON.stringify({ error: "you need clients:write" }), { status: 403 }),
  });
  render(<VoiceSettings canEdit={false} />);
  await screen.findByText("Only someone who manages the workspace's connections can change voice.");
  assert.equal(screen.queryByRole("button"), null);
});
