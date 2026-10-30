import assert from "node:assert/strict";
import { test } from "node:test";
import { appHeight } from "./viewport.ts";

// An iPhone 15 Pro Max: 430x932 points, a 59pt status bar.
const iphone = {
  innerWidth: 430,
  innerHeight: 873,
  screenWidth: 430,
  screenHeight: 932,
  standalone: true,
  ios: true,
};

test("appHeight: installed on an iPhone, the app fills the screen, not the short viewport", () => {
  assert.equal(appHeight(iphone), 932);
});

test("appHeight: held sideways, the screen's short side is the height", () => {
  assert.equal(appHeight({ ...iphone, innerWidth: 932, innerHeight: 400 }), 430);
});

test("appHeight: in a browser tab, or off iOS, the stylesheet's height stands", () => {
  assert.equal(appHeight({ ...iphone, standalone: false }), null);
  assert.equal(appHeight({ ...iphone, ios: false }), null);
});

test("appHeight: where Safari already gets it right, nothing changes", () => {
  assert.equal(appHeight({ ...iphone, innerHeight: 932 }), null);
});
