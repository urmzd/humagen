import { GlobalRegistrator } from "@happy-dom/global-registrator";
import { loadTests } from "./testload.ts";

// Scope composer menu regressions to their own DOM process.
GlobalRegistrator.register({ url: "http://localhost:5173/" });
await loadTests(["/src/composer-menu.dom.test.tsx"]);
