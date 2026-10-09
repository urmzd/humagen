import type { Preview } from "@storybook/react-vite";
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
      description: "Accent hue shift in degrees",
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
  initialGlobals: { theme: "light", accent: "purple", shift: "0" },
  decorators: [
    (Story, { globals }) => {
      const root = document.documentElement;
      root.dataset.theme = globals.theme;
      root.dataset.accent = globals.accent;
      root.style.setProperty("--aui-accent-shift", globals.shift ?? "0");
      return (
        <div className="min-h-screen bg-aui-bg p-6 text-aui-fg">
          <Story />
        </div>
      );
    },
  ],
  parameters: {
    layout: "fullscreen",
    controls: { expanded: true },
  },
};

export default preview;
