/**
 * All user-facing strings in one table so hosts can rebrand or translate
 * without forking. Conventions follow Slack's UI: buttons in Title Case,
 * everything else in sentence case, no end punctuation in headings, one
 * neutral voice, no emoji in buttons or headings.
 */
const OUTCOMES: Record<string, string> = {
  allowed_once: "Allowed once",
  raised: "Limit raised",
  kept: "Kept",
  discarded: "Discarded",
  replied: "Answered",
  done: "Done",
  retried: "Retried",
  handled: "Handled",
};

export const defaultCopy = {
  approve: "Approve",
  cancel: "Cancel",
  edit: "Edit",
  confirmDestructive: {
    title: "Are you sure?",
    text: "This can't be undone.",
    confirm: "Yes, Continue",
    deny: "Go Back",
  },
  actingAs: (who: string) => `Will run as ${who}`,
  actsForPerson:
    "Uses the credentials of the person this run acts for. Only they may change its arguments.",
  actsForPersonIn: (calls: string[]) =>
    `${calls.join("; ")}: ${calls.length === 1 ? "uses" : "use"} the credentials of the person this run acts for. Only they may change ${calls.length === 1 ? "its" : "their"} arguments.`,
  expires: (time: string) => `Expires ${time}`,
  approvedBy: (by: string, time: string) => `Approved by ${by} · ${time}`,
  rejected: "Rejected, I won't run it.",
  rejectedBy: (by: string, time: string) => `Rejected by ${by} · ${time}`,
  timedOut: "Timed out, I didn't run it.",
  cancelled: "Cancelled, the request was stopped.",
  somethingElse: "Something Else",
  answeredBy: (by: string, choice: string, time: string) => `${by} chose ${choice} · ${time}`,
  declinedBy: (by: string, time: string) => `Declined by ${by} · ${time}`,
  approvalNeeded: (summary: string) => `Approval needed: ${summary}`,
  inputNeeded: (question: string) => `Input needed: ${question}`,
  planWorking: "Working",
  planDone: (seconds: number) => `Worked for ${seconds}s`,
  planError: "Error",
  planStopped: "Stopped",
  failedTitle: "Couldn't finish",
  stopped: "Okay, I stopped.",
  retry: "Retry",
  sources: "Sources",
  /** The one link a message closes with: the address itself, and nothing around it. */
  runLink: (url: string) => `[${url}](${url})`,
  disclaimer: (name: string) =>
    `${name} is AI and can make mistakes. Double-check its answers before you rely on them.`,
  helpful: "Helpful",
  notHelpful: "Not helpful",
  /** Asked after a rating. Answering is optional; the rating is already recorded. */
  feedbackPrompt: {
    askUp: "What was helpful?",
    askDown: "What went wrong?",
    placeholder: "Add a comment (optional)",
    send: "Send",
    skip: "Skip",
    thanks: "Thanks for the feedback",
    reasons: [
      { id: "wrong", label: "Wrong" },
      { id: "incomplete", label: "Incomplete" },
      { id: "slow", label: "Too slow" },
      { id: "other", label: "Other" },
    ],
  },
  status: {
    queued: "Queued",
    working: "Working",
    waiting: "Needs your input",
    done: "Done",
    failed: "Failed",
    stopped: "Stopped",
  },
  tone: { ok: "Done", warn: "Warning", bad: "Error", info: "Note", neutral: "Note" },
  ask: "Ask",
  alwaysAllow: (tool: string) => `Always Allow ${tool}`,
  askedBy: (by: string, time: string) => `${by} asked · ${time}`,
  notYours: "Only the person who started this can approve it. You can still ask or stop it.",
  batchTitle: (n: number) => `${n} actions need your approval`,
  approveSelected: "Approve Selected",
  denyAll: "Cancel All",
  unsure: "I'm Not Sure",
  unsureBy: (by: string, time: string) => `${by} wasn't sure · ${time}`,
  stopPhase: {
    requested: "Stopping. Other viewers may still see it finish.",
    acknowledged: "Stop requested. Waiting for the agent to confirm.",
    failed: "Stopped here, but the agent didn't confirm. It may still finish elsewhere.",
    confirmed: "Stopped. You can pick up where it left off.",
  },
  resume: "Resume",
  handoff: (to: string) => `Switched to ${to}`,
  /** Where a row can't look different, the words say another agent did it. */
  delegated: (name: string, title: string) => `${name}: ${title}`,
  subagent: (name: string, steps?: number, tokens?: number) =>
    [name, steps ? `${steps} steps` : null, tokens ? `${Math.round(tokens / 1000)}k tokens` : null]
      .filter(Boolean)
      .join(" · "),
  cantRetry: "This turn can't be retried. Send a new message instead.",
  /** A turn that acted for a person is theirs to retry, and the reader is not them. */
  retryOnlyTheirs: "Only the person this turn acted for can retry it.",
  compacting: "Compacting the conversation to stay within context",
  compacted: (saved: string) => `Compacted the conversation, freed ${saved} tokens`,
  compactFailed: (reason: string) => `Couldn't compact: ${reason}`,
  contextNearlyFull: (pct: number) => `Context is ${pct}% full`,
  compactNow: "Compact Now",
  startFresh: "Start Fresh",
  decline: "Decline",
  approveAll: (n: number) => `Approve All (${n})`,
  moreOptions: "More options",
  inbox: "Inbox",
  inboxCount: (n: number) => (n === 0 ? "Nothing needs you" : `${n} need${n === 1 ? "s" : ""} you`),
  needsYou: "Needs you",
  alerts: "System alerts",
  failures: "Failed runs",
  resolved: "Resolved",
  openConversation: "Open Conversation",
  dismiss: "Dismiss",
  snooze: "Snooze 1 Hour",
  from: (label: string) => `From ${label}`,
  acknowledge: "Acknowledge",
  openRule: "Open Rule",
  openSource: (label: string) => `Open ${label}`,
  reply: "Reply",
  send: "Send",
  keepSelected: "Keep Selected",
  discardAll: "Discard All",
  memoryTitle: (n: number) => `${n} thing${n === 1 ? "" : "s"} to remember`,
  allowOnce: "Allow This Run",
  raiseTo: (amount: string) => `Raise to ${amount}`,
  budgetTitle: "Limit reached",
  budgetBody: (used: string, limit: string, period?: string, needs?: string) =>
    `${used} of ${limit} used${period ? ` this ${period}` : ""}.${needs ? ` This run needs ${needs} more.` : ""}`,
  noPermission: "You can read this, but you don't have permission to resolve it.",
  mailbox: {
    empty: "Nothing here",
    emptyHint: "Approvals, questions, alerts and new artifacts arrive here, and are resolved here.",
    emptyOpen: "You're all caught up",
    emptyOpenHint: "Nothing is waiting on you. New approvals, alerts and artifacts land here.",
    pick: "Pick a message to read it",
    pickHint: "Decisions come first, then updates.",
    back: "Back",
    needsYou: "Needs you",
    updates: "Updates",
    snooze: "Snooze",
    snoozeTitle: "Hide until tomorrow morning",
    about: "About",
    messages: "Messages",
    reading: "Message",
    unread: "Unread",
    readOnly: "Nothing to decide. Dismiss it when you're done.",
    noRenderer: (name: string) => `This message has a ${name} part that this surface can't show.`,
  },
  /** How a settled part reads: "Acknowledged by Dana · 3:04 PM". */
  outcome: (outcome: string, by: string, time: string, detail?: string) =>
    [
      `${OUTCOMES[outcome] ?? outcome.charAt(0).toUpperCase() + outcome.slice(1)}${by ? ` by ${by}` : ""}`,
      detail,
      time,
    ]
      .filter(Boolean)
      .join(" · "),
  /** Said next to the kind tag, so urgency never rests on color. */
  severity: { bad: "Urgent", warn: "Warning" } as Partial<Record<string, string>>,
  show: { label: "Show", open: "Open", resolved: "Resolved", all: "All" },
  /** The tag every message carries, in the list and above its parts. */
  partKind: {
    text: "Note",
    approval: "Approval",
    choice: "Question",
    reply: "Reply",
    memory: "Memory",
    budget: "Limit",
    alert: "Alert",
    failure: "Failed run",
    reference: "Reference",
    artifact: "Artifact",
    custom: "Message",
  },
  /** What each message template is called, in the list and above its parts. */
  template: {
    alert: "Alert",
    approval: "Approval",
    question: "Question",
    failure: "Failed run",
    memory: "Memory",
    limit: "Limit",
    artifact: "Artifact",
    result: "Result",
    note: "Note",
    mention: "Mention",
  },
  serviceStatus: {
    operational: "Operational",
    degraded: "Degraded",
    down: "Down",
    maintenance: "Maintenance",
    resolved: "Resolved",
  },
  started: (t: string) => `Started ${t}`,
  updated: (t: string) => `Updated ${t}`,
  affects: (list: string) => `Affects: ${list}`,
  statusPage: "Status page",
  reconnect: (service: string) => `Reconnect ${service}`,
  report: "Report Issue",
  retryIn: (seconds: number) =>
    `Try again in ${seconds < 90 ? `${seconds}s` : `${Math.round(seconds / 60)} min`}.`,
  reference: (ref: string) => `Reference: ${ref}`,
  failure: {
    auth_required: (service = "A connected app") => `${service} needs a reconnect`,
    rate_limited: () => "Rate limited",
    unavailable: (service = "A service") => `${service} is unavailable`,
    timeout: () => "Timed out",
    tool_error: (service = "A tool") => `${service} failed`,
    forbidden: () => "Not allowed",
    internal: () => "Something broke on my end",
  },
  /**
   * What to say under each failure's title, for a host that has no words of its
   * own to put in `Failure.message`. It never repeats the title.
   */
  failureBody: {
    auth_required: (service = "the app") => `Reconnect ${service}, then try again.`,
    rate_limited: () => "Too many requests at once. Wait a moment, then try again.",
    unavailable: (service = "It") => `${service} isn't answering right now. Try again shortly.`,
    timeout: () => "It took too long, so it was stopped.",
    tool_error: (service = "A tool") => `${service} returned an error, so the run stopped.`,
    forbidden: () => "You don't have access to do this here.",
    internal: () => "The run stopped on an error. Retry, or report it if it happens again.",
  },
  /** Nobody can answer: say how to name an agent. `example` is a name to show. */
  noAgent: (example?: string) =>
    `No agent answers here yet. Name one at the start of your message, such as @${example ?? "name"}.`,
  /** Fallback text for a message that continues a longer one: "(2/3) …". */
  continued: (page: number, of: number, text: string) => `(${page}/${of}) ${text}`,
  /** The one-field form for asking from somewhere with nowhere to type (a Slack shortcut). */
  askForm: {
    title: "Ask the agent",
    label: "What do you need?",
    send: "Send",
    cancel: "Cancel",
  },
  notInChannel: "I'm not in this channel yet. Invite me with `/invite`, then try again.",
  /** A Retry refused because someone else asked for the run. */
  retryNotYours: "Only the person who asked for this can retry it. You can ask again yourself.",
  /** A Retry refused because it can't run here anymore, such as when the agent was removed. */
  retryNotHere: "This can't be retried here anymore. Send a new message instead.",
  /** The agents a person can ask, with how to ask one. */
  agentList: {
    title: "Agents you can ask",
    hint: "Start a message with an agent's name, such as @name, to ask it.",
    noDescription: "No description",
    empty: "No agent can be asked here yet.",
    handle: (name: string) => `@${name}`,
  },
  /** @auto, the built-in router, and who answers a message that tags no agent. */
  auto: {
    handle: "@auto",
    description: "Picks the agent that fits, from the agents available here.",
    /** Under the composer: who answers a message that tags nobody. */
    answers: (handle: string) => `@${handle} answers`,
    nobody: "Nobody answers; tag an agent",
    /** @auto matched nothing, and no default on the way up names an agent. */
    noFit: "No agent fits; tag one.",
    /** Nothing answers here by default, so the person names an agent. */
    tagOne: (example?: string) => `Tag an agent, for example @${example ?? "name"}.`,
    /** Where a default comes from: "from Field". */
    from: (project: string) => `from ${project}`,
    /** The composer's menu of who answers. */
    choose: "Choose who answers",
    /** Marks the agent that answers untagged messages where the person is. */
    defaultChip: "Default",
    /** @auto is chosen, or is the default, and has picked. */
    picks: (agent: string) => `@auto picks @${agent}`,
    picksWho: "@auto picks who answers",
    /** The composer's prompt with nobody to answer yet. */
    tagToAsk: "Tag an agent to ask",
  },
  /** The control for the root's default, which every project inherits. */
  workspaceDefault: "Workspace Default",
  /** The control that makes an agent live in a project. */
  livesHere: "Lives Here",
  /** Where an agent can answer, on its Access tab and in the Directory. */
  agentPlace: {
    livesIn: (project: string) => `Lives in ${project}`,
    availableIn: "Available in",
    platform: "A platform agent: available wherever a project enables it",
    everywhere: "Available everywhere",
    nowhere: "Not enabled in any project yet.",
    loses: (n: number) => `${n} ${n === 1 ? "place" : "places"} will lose this agent.`,
    /** On an agent's Access tab. */
    title: "Where it answers",
    hint: "A platform agent answers wherever a project enables it. An agent that lives in a project answers there and below, and nowhere else.",
    home: "Lives in",
    noHome: "No project: a platform agent",
    livesHere: "Lives here",
    /** Where a project agent answers: its home and everything below it. */
    homeAndBelow: (title: string) => `${title} and the projects below it`,
    fromProject: (title: string) => `from ${title}`,
    moved: "Saved. It answers where it lives now.",
    failed: "Where the agent lives could not be changed.",
  },
  /** Who and what is in the workspace, readable by everyone in it. */
  directory: {
    title: "Directory",
    lede: "Who and what is in this workspace.",
    tabs: { people: "People", agents: "Agents", projects: "Projects" },
    search: "Search the directory",
    empty: {
      people: "Nobody is listed yet. People appear here once they sign in.",
      agents: "No agents yet.",
      projects: "No projects yet. Everything is filed in the workspace itself.",
    },
    noMatch: "Nothing matches that search.",
    /** A person's handle, which is how they are tagged. */
    handle: (handle: string) => `@${handle}`,
    more: "Show More",
    failed: "The directory could not be read.",
    /** An agent's row: where it lives or that it is a platform agent, and where it is enabled. */
    platformAgent: "Platform agent",
    enabledIn: (places: string) => `Enabled in ${places}`,
    enabledEverywhere: "Enabled everywhere",
    notEnabled: "Not enabled anywhere",
    /** A project's row: what it holds besides its tasks. */
    holds: (schedules: number, notes: number) =>
      [
        schedules ? `${schedules} ${schedules === 1 ? "schedule" : "schedules"}` : "",
        notes ? `${notes} ${notes === 1 ? "note" : "notes"}` : "",
      ]
        .filter(Boolean)
        .join(", "),
    livingHere: (agents: string) => `Agents living here: ${agents}`,
    /** People are tagged by handle; agents by name. */
    roles: (roles: string[]) => (roles.length ? roles.join(", ") : "No role"),
  },
  /** Raising a limit to an amount the reader types. */
  raiseLimit: "Raise Limit",
  raiseLimitTitle: "Raise the limit",
  newLimit: "New limit",
  /** A number field's label with its unit: "New limit ($)", "New limit (runs)". */
  amountLabel: (label: string, unit: string) =>
    unit === "usd" ? `${label} ($)` : `${label} (${unit})`,
  /** Labels a message that continues a longer one. */
  part: (n: number) => `Part ${n}`,
  /** Under the field: the new limit must be more than the current one. */
  moreThan: (amount: string) => `More than ${amount}`,
  /** A turn still running after its live output was closed. Said once, in words, never animated. */
  stillWorking: (name: string) =>
    `${name} is still working on this. The answer will appear here when it's ready.`,
  /** What Slack says under the rating buttons, which can't show which one was pressed. */
  rated: { up: "Rated helpful", down: "Rated not helpful" },
  home: {
    greeting: (first: string, assistant: string) => `Hi ${first}, I'm ${assistant}.`,
    /** Follows the host's intro: how to reach the assistant from here. */
    reachWeb: "Ask me something and I'll hand it to the right one, or pick where to start below.",
    reachSlack: (handle: string) =>
      `Mention **@${handle}** in a channel, send me a message, or start from here.`,
    askLabel: "What should we work on?",
    askPlaceholder: "Ask anything",
    ask: "Ask",
    attach: "Attach a File",
    answeredBy: "Answered by",
    anyAgent: "Whoever fits best",
    startHere: "Start here",
    /** Each way to start. `slack` is its button on the Slack Home tab, where it has one. */
    starts: {
      talk: {
        label: "Talk to an agent",
        description: "Pick one by name or let the router choose.",
        slack: "Ask a Question",
      },
      build: {
        label: "Build or change an agent",
        description: "Describe it. The author drafts it and you approve the change.",
        slack: "Build an Agent",
      },
      search: {
        label: "Search what agents know",
        description: "Docs and wiki pages, with the passage each answer came from.",
        slack: "Search Context",
      },
      review: {
        label: "Review what needs you",
        description: "Approvals and questions agents are waiting on.",
      },
      runs: {
        label: "See what is running",
        description: "Runs in flight, what they cost, and how to stop one.",
      },
      schedule: {
        label: "Schedule recurring work",
        description: "A nightly digest, a weekly review, a check every hour.",
      },
    } as Record<string, { label: string; description: string; slack?: string }>,
    needsYou: "Notifications",
    nothingWaiting: "Nothing needs you right now, and no alerts are open.",
    /** The page's one-line point, after the greeting. */
    notificationCount: (n: number) =>
      n === 0 ? "Nothing needs you" : `${n} notification${n === 1 ? "" : "s"}`,
    /** The tag before when it arrived, so severity is said in words. */
    notificationTag: (kind: string, severity?: string) =>
      kind === "alert"
        ? severity === "critical"
          ? "Critical alert"
          : severity === "warning"
            ? "Warning alert"
            : "Alert"
        : "Inbox",
    moreWaiting: (n: number) => `${n} more in the inbox`,
    moreAlerts: (n: number) => `${n} more open alert${n === 1 ? "" : "s"}`,
    review: "Review",
    whoAnswers: "Who answers you",
    dmRouting: "In direct messages, send my questions to",
    connections: "Connections",
    manageAll: "Manage all",
    connectedTo: (account: string) => `Connected to ${account}`,
    connectedHere: "Connected for this workspace.",
    connected: "Connected",
    notConnected: "Not connected.",
    adminCanAdd: "Not connected. An admin can add it.",
    connect: "Connect",
    personal: "Your own accounts",
    personalWeb: "Agents act as you only where you have connected an account. Nothing is shared.",
    personalSlack: "Agents act as you only where you have connected one.",
    personalCount: (n: number) => `${n} connected`,
    connectAccount: "Connect an Account",
    access: "Your access",
    /** "You are signed in through X as an **operator**, because you are in the group `eng-oncall`." */
    signedIn: (via: string | undefined, role: string, group?: string) => {
      const article = /^[aeiou]/i.test(role) ? "an" : "a";
      return `You are signed in${via ? ` through ${via}` : ""} as ${article} **${role}**${group ? `, because you are in the group \`${group}\`` : ""}.`;
    },
    noRole: (via: string | undefined) =>
      `You are signed in${via ? ` through ${via}` : ""}, but no rule grants you a role yet. An admin can add one to the access policy.`,
    allows: (what: string) => `You can ${what}.`,
    denies: (what: string) => `You cannot ${what}.`,
    why: "Why can I do this?",
    whyNot: "Why can't I do more?",
    policy: "Read the access policy",
    /** The Slack tab's one line about access. */
    signedInAs: (name: string, role?: string) =>
      role ? `Signed in as ${name}, ${role}.` : `Signed in as ${name}, with no role yet.`,
    openConsole: (url: string) => `[Open the console](${url}) for runs, schedules and access.`,
  },
  /** The project tree: its kinds, where a project sits, and what a person does with one. */
  projects: {
    title: "Projects",
    lede: "Where work belongs. A project's instructions, notes and limits reach everything below it.",
    /** A kind in words, on a row and in a project's meta line. */
    kind: { company: "Company-wide", team: "Team", account: "Account" } as Record<string, string>,
    /** The Projects page's groups, in the order they are shown. */
    group: { company: "Company-wide", team: "Teams", account: "Accounts" } as Record<
      string,
      string
    >,
    anyKind: "All",
    kindFilter: "Kind",
    search: "Search projects",
    /** The workspace is the root of the tree. */
    workspace: "The workspace",
    workspaceHint: "Holds every project. Its instructions and notes reach every agent.",
    /** Where a row sits when the row above it is not its parent: "in Field › Globex". */
    within: (path: string[]) => `in ${path.join(" › ")}`,
    empty: "No projects yet.",
    emptyCreate: "No projects yet. Make one to keep related tasks together.",
    noMatch: "No project matches.",
    allProjects: "All projects",
    missing: (name: string) => `No project named ${name}`,
    missingHint: "It may have been deleted, or you may not be allowed to see it.",
    breadcrumb: "Where this project sits",
    newProject: "New Project",
    newSubProject: "New Sub-project",
    createProject: "Create Project",
    creating: "Creating…",
    editProject: "Edit Project",
    deleteProject: "Delete Project",
    saveChanges: "Save Changes",
    saving: "Saving…",
    cancel: "Cancel",
    newTask: "New Task",
    form: {
      heading: "New project",
      title: "Title",
      titleHint: "What people see. It can change.",
      name: "Name",
      nameHint: "Can't change later. Letters, digits, spaces, dashes and underscores.",
      kind: "Kind",
      parent: "Inside",
      topLevel: "Top level",
      description: "Description (optional)",
      customer: "Customer (optional)",
      externalId: "Customer id elsewhere (optional)",
      url: "Link to the customer's record (optional)",
      governed: "Only those who govern projects make company-wide ones.",
    },
    count: {
      running: (n: number) => `${n} running`,
      review: (n: number) => `${n} to review`,
      tasks: (n: number) => `${n} ${n === 1 ? "task" : "tasks"}`,
      below: (n: number) => `${n} ${n === 1 ? "sub-project" : "sub-projects"}`,
      active: (when: string) => `active ${when}`,
    },
    tab: {
      overview: "Overview",
      context: "Context",
      agents: "Agents",
      channels: "Channels",
    } as Record<string, string>,
    /** A project's Agents tab: what it turns on, and who answers untagged messages. */
    agents: {
      available: "Available here",
      availableHint:
        "Agents that may answer here and below: what this project and those above it enable, the agents that live here, narrowed by every allow list on the way.",
      everyPlatform: "Every platform agent",
      noneAvailable: "No agent may answer here. Enable one, or let one live here.",
      default: "Default",
      defaultHint:
        "Answers a message that tags no agent, here and below. A project below can set its own.",
      inherit: "Inherit",
      inherits: (agent: string, from: string) => `Inherits @${agent} from ${from}`,
      nobody: "Nobody answers untagged messages here. A message has to tag an agent.",
      answers: (agent: string) => `@${agent} answers untagged messages here.`,
      enable: "Enable",
      enableHint:
        "Platform agents this project turns on for itself and everything below. It adds to what the projects above enable.",
      enableEvery: "Every platform agent",
      enableEveryHint:
        "Every agent that lives in no project. An agent that lives in a project is never reached this way.",
      enableNone: "Enables nothing of its own.",
      addAgent: "Add Agent",
      allow: "Only these may run",
      allowHint:
        "When on, only the agents listed may answer here and below. A project below can only narrow it further.",
      allowEmpty: "Name at least one agent, or turn this off.",
      save: "Save Agents",
      saved: "Saved",
      governed: "Only those who govern projects change the workspace's agents and its default.",
      managed: "The platform file sets these agents. They can't be changed here.",
      lives: "Lives here",
      livesHint: "Agents that live in this project answer here and below, and nowhere else.",
      noneLive: "No agent lives here.",
      failed: "The agents could not be saved.",
    },
    tasks: "Tasks",
    noTasks:
      "Start a task here to keep its conversation, execution history and shared context in this project.",
    includeBelow: "Include sub-projects",
    includeBelowHint: "Also list the tasks filed in the projects below this one.",
    moreTasks: "See More",
    subProjects: "Sub-projects",
    noSubProjects: "Nothing sits below this project yet.",
    deepest: "Projects go at most four levels deep, so nothing can sit below this one.",
    limits: "Limits",
    /** A limit set higher up the tree that also holds this project: "Field's limit also applies here". */
    limitAbove: (title: string) => `${title}'s limit also applies here`,
    workspaceLimit: "The workspace's limit also applies here",
    managed: "The platform file declares this project. Fields it sets can't be changed here.",
    governed: "Only those who govern projects change the workspace and company-wide projects.",
    stale: "Someone changed this project since you opened it. Its latest version is shown now.",
    failed: {
      create: "The project could not be created. Try again.",
      save: "The project could not be saved. Try again.",
      delete: "The project could not be deleted.",
    },
    context: {
      instructions: "Instructions",
      instructionsHint:
        "Sent to every agent answering here and below, after the instructions of the projects above. An agent's own instructions win over these.",
      none: "No instructions.",
      save: "Save Instructions",
      saved: "Saved",
      inherited: "From above",
      inheritedHint:
        "What a turn here is also sent, read only. Change it on the project it comes from.",
      /** One project above, by its title: "From Field". */
      from: (title: string) => `From ${title}`,
      nothingAbove: "Nothing above this project sends anything yet.",
      notes: "Notes",
      noNotes: "No notes.",
      order:
        "A turn here is sent the workspace's instructions first, then each project's down to this one, then this project's notes, then the notes above it, nearest first, and then the agent's own.",
    },
  },
  /** Channels and shared threads: where people and agents work together in a project. */
  threads: {
    /** A channel's title on its page: "#renewals". */
    channel: (name: string) => `#${name}`,
    channels: "Channels",
    channelsLede: "Each holds shared threads. Everyone who can see the project reads them.",
    newChannel: "New Channel",
    createChannel: "Create Channel",
    archiveChannel: "Archive Channel",
    archived: "Archived",
    general: "Every project has general. It can't be archived.",
    channelForm: {
      heading: "New channel",
      name: "Name",
      nameHint: "Can't change later. Lower case letters, digits and dashes.",
      title: "Title (optional)",
      purpose: "Purpose (optional)",
      purposeHint: "Sent to every agent answering here.",
    },
    noChannels: "Only general so far.",
    noThreads: "No threads here yet. Start one below.",
    moreThreads: "See More",
    /** Where a thread is, in its meta line: "Shared in Globex › #renewals". */
    sharedIn: (project: string, channel: string) => `Shared in ${project} › #${channel}`,
    /** Who takes part, in the meta line. */
    participants: (n: number) => `${n} ${n === 1 ? "person" : "people"}`,
    startThread: "Start a thread",
    reply: "Reply",
    send: "Send",
    /** The control that posts a message no agent answers, unless it tags one. */
    postOnly: "Post Only",
    postOnlyHint: "Post for people to read. Only an agent you tag answers.",
    /** Under the composer: who answers a message that tags nobody. */
    answers: (agent: string) => `@${agent} answers untagged messages. Tag anyone with @.`,
    nobody: "Nobody answers untagged messages here. Tag an agent with @.",
    stale: (agent: string, why: string) =>
      `@${agent} answers untagged messages here, but ${why}. Tag an agent with @.`,
    /** What a message did, said once it is posted: people by handle, experts a role names as the role. */
    notified: (names: string[]) =>
      `Sent to ${names.map((n) => (n.startsWith("role:") ? n : `@${n}`)).join(", ")}.`,
    refused: (tag: string, why: string) => `${tag}: ${why}`,
    /** A question an agent asked of a person, shown still in the thread. */
    asks: (agent: string) => `${agent} asks`,
    answer: "Answer",
    answered: "Answered",
    typeahead: "People and agents to tag",
    experts: (topic: string) => (topic ? `Experts on ${topic}` : "Every expert here"),
    olderTurns: "Show Earlier",
    failed: {
      post: "The message could not be posted. Try again.",
      channel: "The channel could not be created.",
    },
  },
};

export type Copy = typeof defaultCopy;
