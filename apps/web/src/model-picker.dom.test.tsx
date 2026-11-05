import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import type { MessagePick } from "./api.ts";
import { ModelPicker } from "./ModelPicker.tsx";

// DES-067: the model and effort for one message, a text control under the composer.

afterEach(cleanup);

function Harness({ seen }: { seen: MessagePick[] }) {
  const [pick, setPick] = useState<MessagePick>({});
  return (
    <ModelPicker
      offers={{ models: ["anthropic/opus"], efforts: ["low", "high"] }}
      pick={pick}
      onPick={(p) => {
        seen.push(p);
        setPick(p);
      }}
    />
  );
}

test("a model and an effort are picked from a text control", async () => {
  const seen: MessagePick[] = [];
  render(<Harness seen={seen} />);
  fireEvent.click(
    screen.getByRole("button", {
      name: "Model and effort for this message: Agent's model · Default effort",
    }),
  );
  fireEvent.click(await screen.findByRole("menuitemradio", { name: "anthropic/opus" }));
  fireEvent.click(screen.getByRole("button", { name: /: opus · Default effort$/ }));
  fireEvent.click(await screen.findByRole("menuitemradio", { name: "High" }));
  await screen.findByRole("button", { name: /: opus · High$/ });
  assert.deepEqual(seen.at(-1), { model: "anthropic/opus", effort: "high" });
});

test("nothing renders when no agent offers a pick", () => {
  const { container } = render(
    <ModelPicker offers={{ models: [], efforts: [] }} pick={{}} onPick={() => {}} />,
  );
  assert.equal(container.textContent, "");
});
