import type { StorybookConfig } from "@storybook/react-vite";
import tailwindcss from "@tailwindcss/vite";

const config: StorybookConfig = {
  framework: "@storybook/react-vite",
  stories: ["../src/**/*.stories.@(ts|tsx)", "../src/**/*.mdx"],
  addons: ["@storybook/addon-docs", "@storybook/addon-a11y"],
  viteFinal: async (vite) => {
    vite.plugins = [...(vite.plugins ?? []), tailwindcss()];
    return vite;
  },
};

export default config;
