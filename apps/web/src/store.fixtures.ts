import type { StoreAgent, StorePackage } from "./api.ts";

/**
 * Store packages for the gallery. Core and Review copy what the server ships
 * (internal/platform/builtin.go); `generatedPackages` are made up, to show a
 * fuller catalog, and are named as such wherever they are drawn.
 */
const chain = {
  provider: "anthropic",
  model: "claude-sonnet-5-5",
  fallbacks: [
    { provider: "openai", model: "gpt-5.1" },
    { provider: "google", model: "gemini-3-flash-preview" },
  ],
};

function agent(name: string, description: string, over: Partial<StoreAgent> = {}): StoreAgent {
  return {
    name,
    description,
    installed: false,
    customized: false,
    update_available: false,
    taken: false,
    ...chain,
    tools: [],
    access: {},
    connectors: [],
    tool_servers: [],
    ...over,
  };
}

const writing = [
  "file_write",
  "file_read",
  "file_list",
  "publish_artifact",
  "record_outcome",
  "web_search",
  "web_fetch",
];
const reading = ["file_read", "file_list", "github_read_context", "github_read_file"];
const github = { tools: reading, connectors: ["github"] };

export function corePackage(): StorePackage {
  return {
    name: "humagen/core",
    title: "Core",
    description:
      "A writer that publishes documents and an analyst that reports on how your agents are doing.",
    version: "sha256:a90a780c41d2",
    installed: false,
    missing: ["humagen-writer", "humagen-analyst"],
    agents: [
      agent("humagen-writer", "Writes documents, reports and tables, and publishes them as files", {
        tools: writing,
      }),
      agent(
        "humagen-analyst",
        "Answers questions about runs, failures, speed, spend and alerts, writes reports and project recaps",
        { tools: ["runs_summary", "list_alerts", "project_changes", ...writing] },
      ),
    ],
  };
}

export function reviewPackage(): StorePackage {
  const specialists = ["security-reviewer", "architect-reviewer", "qa-reviewer"];
  return {
    name: "humagen/review",
    title: "Review",
    description:
      "Pull request reviews, with security, architecture and test specialists and one verdict.",
    version: "sha256:b4d5e04f77a1",
    installed: false,
    missing: ["code-reviewer", ...specialists],
    agents: [
      agent("code-reviewer", "Reviews code and combines specialist findings into one verdict", {
        ...github,
        access: { spawn: specialists },
      }),
      agent(
        "security-reviewer",
        "Reviews security: authentication, permissions, passwords and hashing, cryptography, secrets, untrusted input and dependencies, with concrete exploit scenarios",
        github,
      ),
      agent(
        "architect-reviewer",
        "Reviews architecture, boundaries and contract compatibility",
        github,
      ),
      agent(
        "qa-reviewer",
        "Reviews behavior, regressions, reproductions and test coverage",
        github,
      ),
    ],
  };
}

/** A package as the workspace has it once installed. */
export function installed(pkg: StorePackage, over: Record<string, Partial<StoreAgent>> = {}) {
  return {
    ...pkg,
    installed: true,
    missing: [],
    agents: pkg.agents.map((a) => ({ ...a, installed: true, ...over[a.name] })),
  };
}

/** Made up, not shipped: only here to show the catalog with more than two packages. */
export function generatedPackages(): StorePackage[] {
  const made = (
    name: string,
    title: string,
    description: string,
    agents: StoreAgent[],
  ): StorePackage => ({
    name: `example/${name}`,
    title,
    description,
    version: "sha256:0000000generated",
    installed: false,
    missing: agents.map((a) => a.name),
    agents,
  });
  return [
    made(
      "support",
      "Support (generated)",
      "Answers customer questions in Slack from your help center and files what it cannot answer.",
      [
        agent("support-desk", "Answers questions from the help center", {
          tools: ["source_search", "slack_reply"],
          connectors: ["slack"],
          access: { spawn: ["support-triage"] },
        }),
        agent("support-triage", "Files what could not be answered", {
          tools: ["source_search", "record_outcome"],
          connectors: ["slack"],
        }),
      ],
    ),
    made("releases", "Releases (generated)", "Drafts release notes from merged pull requests.", [
      agent("release-notes", "Drafts release notes from merged pull requests", {
        tools: [...reading, "file_write", "publish_artifact"],
        connectors: ["github"],
      }),
    ]),
    made(
      "research",
      "Research (generated)",
      "Reads the web and your sources, and writes a cited brief.",
      [
        agent("researcher", "Reads the web and your sources", {
          tools: ["web_search", "web_fetch", "source_search"],
          provider: "openai",
          model: "gpt-5.1",
          fallbacks: [],
        }),
        agent("brief-writer", "Writes a cited brief", { tools: writing }),
      ],
    ),
  ];
}

export const storeAgentNames = [
  ...corePackage().agents,
  ...reviewPackage().agents,
  ...generatedPackages().flatMap((p) => p.agents),
].map((a) => a.name);
