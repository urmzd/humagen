import { describe, expect, it } from "vitest";
import { buildManifest, CAPABILITIES, capabilitiesExcept, ENTRY } from "./manifest";

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

describe("app identity", () => {
  const manifest = buildManifest({ capabilities: capabilitiesExcept("workflowStep") });

  it("is Collegium, within Slack's limits", () => {
    const info = manifest.display_information;
    expect(info.name).toBe("Collegium");
    expect(info.name.length).toBeLessThanOrEqual(35);
    expect(info.description.length).toBeLessThanOrEqual(140);
    // App Settings rejects an About text under 175 characters.
    expect(info.long_description.length).toBeGreaterThanOrEqual(175);
    expect(info.long_description.length).toBeLessThanOrEqual(4000);
    expect(info.background_color).toMatch(/^#[0-9a-f]{6}$/i);
    expect(manifest.features.agent_view.agent_description.length).toBeLessThanOrEqual(300);
  });

  it("uses a bot name Slack accepts", () => {
    expect(manifest.features.bot_user.display_name).toMatch(/^[a-z0-9._-]{1,80}$/);
  });

  it("renames the shortcuts without changing what they call", () => {
    const shortcuts = manifest.features.shortcuts ?? [];
    expect(shortcuts.map((s) => [s.name, s.callback_id])).toEqual([
      ["Ask Collegium", ENTRY.ask],
      ["Ask Collegium about this", ENTRY.askAbout],
    ]);
    expect(manifest.features.slash_commands?.[0]).toMatchObject({
      command: "/agent",
      description: "Ask Collegium in this channel",
    });
  });

  it("enables the Home tab and listens for it opening", () => {
    expect(manifest.features.app_home.home_tab_enabled).toBe(true);
    expect(manifest.settings.event_subscriptions.bot_events).toContain("app_home_opened");
  });
});
