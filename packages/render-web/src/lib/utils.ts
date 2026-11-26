import { type ClassValue, clsx } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

/*
 * The theme's own names (theme.css). Without them a size such as text-aui-sm
 * is read as a text color, so it would remove the color beside it and would
 * not replace the size it was meant to.
 */
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ["aui-2xs", "aui-xs", "aui-sm", "aui-base", "aui-lg", "aui-xl", "aui-2xl", "aui-3xl"],
      radius: ["aui-sm", "aui", "aui-lg"],
      shadow: ["aui-sm", "aui-md", "aui-pop"],
      font: ["aui-sans", "aui-mono", "humagen-display"],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
