/**
 * Slack app manifest, generated from what agent-ui actually uses. Each
 * capability lists the scopes and events it needs, so the manifest never
 * drifts from the code: add a capability, regenerate, reinstall.
 *
 * Reference: docs.slack.dev/reference/app-manifest
 */

export interface Capability {
  why: string;
  methods: string[];
  scopes: string[];
  events?: string[];
}

/**
 * Ways into the agent besides a DM or a mention. The ids live here so the
 * manifest and the handlers that answer them can't drift apart.
 */
export const ENTRY = {
  /** Global shortcut: ask from anywhere, answered in the app's DM. */
  ask: "aui_ask",
  /** Message shortcut: ask about one message, answered in its thread. */
  askAbout: "aui_ask_about",
  command: "/agent",
  /** Workflow Builder step: a workflow asks, and waits for the turn (and any approval) to end. */
  step: "aui_ask_step",
  /** The one text form behind the shortcut, Edit, Ask and Something Else. */
  form: "aui_form",
} as const;

export const CAPABILITIES: Record<string, Capability> = {
  messages: {
    why: "Post and update turns, approvals, notices (chat.postMessage, chat.update).",
    methods: ["chat.postMessage", "chat.update", "chat.postEphemeral"],
    scopes: ["chat:write"],
  },
  streaming: {
    why: "Native token streaming with task and plan chunks.",
    methods: ["chat.startStream", "chat.appendStream", "chat.stopStream"],
    scopes: ["chat:write"],
  },
  sessions: {
    why: "Agent session status (processing, suspended, active) and the native Stop button.",
    methods: ["agents.sessions.setStatus", "agents.sessions.rename"],
    scopes: ["chat:write"],
    events: ["agent_session_stopped"],
  },
  suggestedPrompts: {
    why: "Prompt bar suggestions, shown at the top of the agent's Messages tab.",
    methods: ["assistant.threads.setSuggestedPrompts"],
    scopes: ["assistant:write"],
    events: ["app_home_opened"],
  },
  reactions: {
    why: "Lifecycle reactions on the user's message (👀 ✋ ✅ ❌ ⏹️).",
    methods: ["reactions.add", "reactions.remove"],
    scopes: ["reactions:write"],
  },
  triggers: {
    why: "Start a turn from a DM or an @mention in a channel.",
    methods: ["conversations.replies"],
    scopes: ["app_mentions:read", "im:history", "channels:history", "groups:history"],
    events: ["app_mention", "message.im"],
  },
  files: {
    why: "Upload artifacts so people download them natively in Slack.",
    methods: ["files.getUploadURLExternal", "files.completeUploadExternal"],
    scopes: ["files:write"],
  },
  shortcuts: {
    why: "Start a turn from the shortcuts menu, a message's menu, or /agent.",
    methods: ["views.open"],
    scopes: ["commands"],
  },
  forms: {
    why: "Collect text for Edit, Ask and Something Else in a modal (views.open on the click's trigger_id).",
    methods: ["views.open"],
    scopes: [],
  },
  home: {
    why: "Publish what is waiting on the viewer to the App Home tab.",
    methods: ["views.publish"],
    scopes: [],
    events: ["app_home_opened"],
  },
  workflowStep: {
    why: "Offer the agent as a Workflow Builder step that finishes when the turn does.",
    methods: ["functions.completeSuccess", "functions.completeError"],
    scopes: [],
    events: ["function_executed"],
  },
  identity: {
    why: "Resolve the bot and team for stream recipients (auth.test).",
    methods: ["auth.test"],
    scopes: [],
  },
};

export interface ManifestOptions {
  name?: string;
  description?: string;
  botName?: string;
  capabilities?: (keyof typeof CAPABILITIES)[];
}

export function buildManifest(options: ManifestOptions = {}) {
  const caps = (options.capabilities ?? Object.keys(CAPABILITIES)).map(
    (k) => CAPABILITIES[k] as Capability,
  );
  const unique = (xs: string[]) => [...new Set(xs)].sort();
  const has = (name: string) => caps.includes(CAPABILITIES[name] as Capability);
  return {
    _metadata: { major_version: 2, minor_version: 1 },
    display_information: {
      name: options.name ?? "agent-ui sandbox",
      description:
        options.description ??
        "Renders agent-ui fixtures in real Slack: streaming, approvals, failures.",
      background_color: "#111111",
    },
    features: {
      app_home: {
        home_tab_enabled: true,
        messages_tab_enabled: true,
        messages_tab_read_only_enabled: false,
      },
      bot_user: { display_name: options.botName ?? "agent-ui-sandbox", always_online: true },
      ...(has("shortcuts")
        ? {
            shortcuts: [
              {
                name: "Ask the agent",
                type: "global",
                callback_id: ENTRY.ask,
                description: "Ask from anywhere. The answer arrives in the app's messages.",
              },
              {
                name: "Ask agent about this",
                type: "message",
                callback_id: ENTRY.askAbout,
                description: "Answer in this message's thread, using the message as the question.",
              },
            ],
            slash_commands: [
              {
                command: ENTRY.command,
                description: "Ask the agent in this channel",
                usage_hint: "[question] or help",
                should_escape: false,
              },
            ],
          }
        : {}),
      // Prompts are set at runtime (setSuggestedPrompts); static ones here would block that.
      agent_view: {
        agent_description:
          "Test bench for agent-ui. Ask anything to see streaming, plans, approvals, and failures rendered by the real Slack client.",
      },
    },
    oauth_config: { scopes: { bot: unique(caps.flatMap((c) => c.scopes)) } },
    ...(has("workflowStep")
      ? {
          functions: {
            [ENTRY.step]: {
              title: "Ask the agent",
              description: "Run one agent turn in a channel and continue when it ends.",
              input_parameters: {
                properties: {
                  prompt: { type: "string", title: "Prompt", description: "What to ask" },
                  channel_id: {
                    type: "slack#/types/channel_id",
                    title: "Channel",
                    description: "Where the turn runs. The app must be a member.",
                  },
                  user_id: {
                    type: "slack#/types/user_id",
                    title: "On behalf of",
                    description: "Who the answer is for, and who may approve.",
                  },
                },
                required: ["prompt", "channel_id", "user_id"],
              },
              output_parameters: {
                properties: {
                  status: {
                    type: "string",
                    title: "Status",
                    description: "done, denied or stopped",
                  },
                  answer: { type: "string", title: "Answer", description: "The agent's reply" },
                },
                required: ["status", "answer"],
              },
            },
          },
        }
      : {}),
    settings: {
      event_subscriptions: { bot_events: unique(caps.flatMap((c) => c.events ?? [])) },
      interactivity: { is_enabled: true },
      // Custom steps only exist on org-ready apps that run their own code.
      org_deploy_enabled: has("workflowStep"),
      ...(has("workflowStep") ? { function_runtime: "remote" } : {}),
      // Socket Mode: no public URL needed for a local sandbox.
      socket_mode_enabled: true,
      token_rotation_enabled: false,
    },
  };
}

/** Slack prefills the "Create app from manifest" flow from this URL. */
export function createAppUrl(manifest: object): string {
  return `https://api.slack.com/apps?new_app=1&manifest_json=${encodeURIComponent(JSON.stringify(manifest))}`;
}
