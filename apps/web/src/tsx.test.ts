import { loadTests } from "./testload.ts";

// Static renders: .test.tsx files that render to markup with no DOM.
await loadTests([
  "/src/ui.test.tsx",
  "/src/home.test.tsx",
  "/src/usage.test.tsx",
  "/src/storage.test.tsx",
  "/src/agents.test.tsx",
  "/src/projects.test.tsx",
  "/src/connections.test.tsx",
  "/src/availability.test.tsx",
  "/src/cron.test.tsx",
]);
