import { describe, expect, it } from "vitest";
import { buildManifest, CAPABILITIES, capabilitiesExcept } from "./manifest";

const all = Object.keys(CAPABILITIES);

describe("capabilitiesExcept", () => {
  it("keeps every capability when nothing is skipped", () => {
    expect(capabilitiesExcept()).toEqual(all);
    expect(capabilitiesExcept("")).toEqual(all);
    expect(capabilitiesExcept(" , ")).toEqual(all);
  });

  it("leaves out the named capabilities", () => {
    expect(capabilitiesExcept("workflowStep")).toEqual(all.filter((k) => k !== "workflowStep"));
    expect(capabilitiesExcept(" workflowStep , files ")).toEqual(
      all.filter((k) => k !== "workflowStep" && k !== "files"),
    );
  });

  it.each(["workflowstep", "nope", "toString", "constructor", "__proto__", "hasOwnProperty"])(
    "rejects %s, which is not a capability",
    (name) => {
      expect(() => capabilitiesExcept(name)).toThrow(
        `SLACK_SANDBOX_SKIP names unknown capabilities: ${name}. Known: ${all.join(", ")}`,
      );
      expect(() => capabilitiesExcept(`workflowStep,${name}`)).toThrow(
        `SLACK_SANDBOX_SKIP names unknown capabilities: ${name}.`,
      );
    },
  );
});

describe("buildManifest", () => {
  it("includes the workflow step by default", () => {
    const manifest = buildManifest();
    expect(manifest).toHaveProperty("functions");
    expect(manifest.settings.org_deploy_enabled).toBe(true);
    expect(manifest.settings.event_subscriptions.bot_events).toContain("function_executed");
  });

  it("drops the workflow step for a free workspace", () => {
    const manifest = buildManifest({ capabilities: capabilitiesExcept("workflowStep") });
    expect(manifest).not.toHaveProperty("functions");
    expect(manifest.settings.org_deploy_enabled).toBe(false);
    expect(manifest.settings.event_subscriptions.bot_events).not.toContain("function_executed");
  });

  it.each(["toString", "constructor", "__proto__", "nope"])("rejects the capability %s", (name) => {
    expect(() => buildManifest({ capabilities: [name] })).toThrow(`Unknown capabilities: ${name}`);
  });
});
