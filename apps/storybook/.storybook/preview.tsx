import type { Preview } from "@storybook/react-vite";
import "@humagen/render-web/fonts.css";
import "../src/styles.css";

const preview: Preview = {
  globalTypes: {
    theme: {
      description: "Light or dark",
      toolbar: {
        title: "Theme",
        icon: "mirror",
        items: [
          { value: "light", title: "Light" },
          { value: "dark", title: "Dark" },
        ],
        dynamicTitle: true,
      },
    },
    accent: {
      description: "Accent preset",
      toolbar: {
        title: "Accent",
        icon: "paintbrush",
        items: ["purple", "blue", "yellow", "teal"],
        dynamicTitle: true,
      },
    },
    shift: {
      description: "Accent hue shift in degrees (--aui-accent-shift)",
      toolbar: {
        title: "Shift",
        icon: "transfer",
        items: [
          { value: "-30", title: "−30°" },
          { value: "-15", title: "−15°" },
          { value: "0", title: "0°" },
          { value: "15", title: "+15°" },
          { value: "30", title: "+30°" },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: { theme: "dark", accent: "purple", shift: "0" },
  decorators: [
    (Story, { globals, parameters }) => {
      const root = document.documentElement;
      root.dataset.theme = globals.theme;
      root.dataset.accent = globals.accent;
      root.style.setProperty("--aui-accent-shift", globals.shift ?? "0");
      return (
        <div
          className={
            parameters.workspace
              ? "h-dvh bg-aui-bg text-aui-fg"
              : "min-h-screen bg-aui-bg px-4 py-8 text-aui-fg sm:px-8"
          }
        >
          <Story />
        </div>
      );
    },
  ],
  parameters: {
    layout: "fullscreen",
    controls: { expanded: true },
    options: {
      storySort: {
        order: ["Workspace", "Foundations", "Primitives", "Agent", "Web-only"],
      },
    },
  },
};

export default preview;
