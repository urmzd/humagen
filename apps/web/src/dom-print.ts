import { inspect } from "node:util";
import { EventTarget } from "happy-dom";
import { describeDom } from "./test-assert.ts";

// console.log(element) prints a happy-dom node with util.inspect, which
// walks the window it belongs to: one log of a button took a test process
// past 2 GB. Importing this module makes every happy-dom node, window and
// event target print as a few words instead: <button aria-label="Send">
// "Send". happy-dom gives each window its own subclasses of EventTarget, so
// one patch on the package's prototype covers every window a test registers.
// Node's assert does not use custom inspectors; ./test-assert.ts covers it.
Object.defineProperty(EventTarget.prototype, inspect.custom, {
  value(this: unknown) {
    return describeDom(this);
  },
  configurable: true,
});
