import assert from "node:assert/strict";
import { DESKTOP, LANDSCAPE, PHONE, type Rules } from "./checks.ts";

export interface Viewport {
  name: string;
  width: number;
  height: number;
  rules: Rules;
}

export const RESPONSIVE_VIEWPORTS = "390x844,480x900,768x1024,1024x768,1280x800";

export function handheld(width: number, height: number): boolean {
  return Math.min(width, height) < 600;
}

export function viewports(value = RESPONSIVE_VIEWPORTS): Viewport[] {
  const seen = new Set<string>();
  return value.split(",").map((size) => {
    const match = /^(\d+)x(\d+)$/.exec(size);
    const width = Number(match?.[1]);
    const height = Number(match?.[2]);
    assert.ok(
      width >= 240 && width <= 2560 && height >= 320 && height <= 2160 && !seen.has(size),
      `Invalid or duplicate SCREENS_VIEWPORTS size: ${size}`,
    );
    seen.add(size);
    return {
      name: size,
      width,
      height,
      rules: width < 768 ? PHONE : handheld(width, height) ? LANDSCAPE : DESKTOP,
    };
  });
}
