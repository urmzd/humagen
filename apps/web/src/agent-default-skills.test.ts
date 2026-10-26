import { GlobalRegistrator } from "@happy-dom/global-registrator";
import { loadTests } from "./testload.ts";

GlobalRegistrator.register({ url: "http://localhost:5173/" });
await loadTests(["/src/agent-default-skills.dom.test.tsx"]);
