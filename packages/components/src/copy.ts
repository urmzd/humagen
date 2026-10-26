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
  outcomeLearning: {
    skill: "Save as Skill",
    remember: "Remember a Decision",
    skillTitle: "Review Skill Draft",
    decisionTitle: "Review Decision Memory",
    scope: "Destination",
    personal: "Personal",
    project: "Current Project",
    name: "Skill Name",
    purpose: "Purpose",
    steps: "Instructions",
    inputs: "Inputs",
    outputs: "Outputs",
    decision: "Recorded Decision",
    text: "Reviewed Memory",
    reviewHint:
      "Review the whole text for its destination audience. Private sources and hidden execution details are not copied automatically.",
    noExternalApproval:
      "This remembers a human-reviewed decision. It grants no tool or external action approval.",
    draftHint:
      "Saved as a draft with no tools. Test and publish the exact revision before using it.",
    createDraft: "Create Skill Draft",
    approveRemember: "Approve Decision and Remember",
    cancel: "Cancel",
    loading: "Loading Current Outcome",
    saving: "Saving",
    retry: "Retry",
    failed: "The outcome could not be loaded or saved. Your review remains available.",
    stale:
      "The outcome or its audience changed. Reload it and review the current snapshot before saving.",
    skillSaved: "The new tool-free skill draft is saved.",
    memorySaved: "Decision approved and remembered.",
    openSkill: "Open Skill Draft",
    openMemory: "Open Memory",
    close: "Close",
    noProject: "Project learning requires ordinary ownership of the current shared project.",
  },

  agentOperations: {
    skillsReview:"Default skills require review against the current agent revision before work can run.",
    reviewSkills:"Review Default Skills",
    reviewDrafts: "Review Drafts",
    addAssignment: "Add Assignment",
    pauseAll: "Pause All",
    resumeAll: "Resume All",
    filter: "Operational State",
    all: "All States",
    title: "Agent Operations",
    loading: "Reading operational state",
    failed: "Operational state could not be read. Retry to refresh it.",
    manager: (name: string) => `Manager: ${name}`,
    lastRun: "Open Last Run",
    states: {
      ready: "Ready",
      working: "Working",
      waiting_on_people: "Waiting on People",
      watching: "Watching",
      paused: "Paused",
      failed: "Needs Attention",
    },
  },
  agentSetup: {
    title: "New Agent",
    lede: "Review its role, context and actions, then test the inactive draft before activation.",
    steps: ["Role & Scope", "Work & Permissions", "Review & Test"],
    name: "Agent Name",
    nameHint: "A unique lowercase name with letters, numbers and hyphens.",
    description: "Description",
    template: "Starting Template",
    roles: { analyst: "Analyst", writer: "Writer", coordinator: "Coordinator" },
    project: "Home Project",
    projectHint:
      "People who can read this project can read the agent’s specification. Personal keeps it in your own project.",
    provider: "Model Provider",
    workspaceModelPolicy: "Workspace Model Policy",
    model: "Model",
    modelHint:
      "Choose a registered provider and a real model. The test verifies access; registration alone does not verify credentials.",
    noModels: "No real provider is registered. Configure a provider before testing this agent.",
    models: "Open Model Settings",
    instructions: "Instructions",
    skills: "Published Default Skills",
    sources: "Context Sources",
    noSources: "No readable sources are available in this project chain.",
    tools: "Allowed Actions",
    toolsHint:
      "Only selected tools are granted. Tests require a separate recorded approval for every action.",
    toolNames: {
      file_list: "List Files",
      file_read: "Read Files",
      file_write: "Write Files",
      publish_artifact: "Publish Deliverables",
      record_outcome: "Record Outcomes",
      source_search: "Search Connected Accounts",
    },
    reviewerSaved: "A reviewer is saved",
    manager: "Reviewer or Manager",
    managerHint:
      "Choose a person by name or handle. Eligibility follows the saved reviewer list and native approval rules.",
    searchPeople: "Find a Reviewer",
    noReviewer: "No Selected Reviewer",
    selfApproval: "Allow Me to Approve My Own Actions",
    selfHint:
      "This explicit agent setting applies to test and future approvals. Every action still needs an actual approval when policy requires one.",
    reviewerNeeded:
      "A different authorized reviewer must approve this test’s actions. You can share its conversation, or stop the test and correct the draft’s approval settings.",
    policy: "Action Policy",
    draftPolicy: "Draft Choice",
    policyUnknown:
      "Current inherited policy could not be read. Retry to review it; the server still enforces every restriction.",
    policyHint: "Inherited restrictions still apply. A local Allow cannot override Never above it.",
    inherit: "Inherit",
    levels: { allow: "Allow", ask: "Ask", never: "Never" },
    categories: {
      read_context: "Read Context",
      write_files: "Write Files",
      publish_artifacts: "Publish Deliverables",
      external_messages: "Send External Messages",
      call_agents: "Call Other Agents",
      other: "Other Actions",
    },
    budget: "Daily Budget (USD)",
    lease: "Per-run Budget (USD)",
    budgetHint:
      "Zero inherits the workspace’s limit. The server applies stricter limits above this agent.",
    budgetReadOnly: "You can use inherited budgets. Changing a cap requires billing permission.",
    addAssignment: "Add Starter Assignment",
    removeAssignment: "Remove Starter Assignment",
    goal: "Goal",
    doneWhen: "Done When",
    trigger: "Trigger",
    manual: "On Request",
    cron: "Schedule",
    sourceChange: "Source Changes",
    cronExpression: "Cron Schedule",
    triggerSource: "Source to Watch",
    assignmentHint:
      "The starter assignment stays inactive until the setup is approved. Additional assignments can be added after activation.",
    next: "Next",
    back: "Back",
    cancel: "Cancel",
    save: "Save Inactive Draft",
    saving: "Saving",
    resume: "Resume Setup",
    edit: "Correct Draft",
    saveCorrection: "Save Correction",
    test: "Run Dry Run",
    testRequest: "Test Request",
    testPlaceholder: "Ask the agent to demonstrate its role with a small, safe task.",
    testHint:
      "This runs the saved revision through the configured provider. Tool grants, budgets, Never rules and workspace pause still apply.",
    openTest: "Open Test Conversation",
    activate: "Activate Agent",
    ready: "The agent is active.",
    saved: "Saved Progress",
    failed: "The setup could not be loaded or changed. Your saved progress is retained.",
    phaseFailed: (phase: string) =>
      `Setup stopped after ${phase}. Resume Setup retries the next unsaved step.`,
    locked:
      "The saved project, budgets and starter assignment stay fixed. Correct the specification and run a fresh test before activation.",
    stopBeforeEdit: "Stop or finish the current test before correcting the draft.",
    phase: {
      reserved: "Name Reserved",
      created: "Inactive Agent Created",
      home: "Project Saved",
      skills: "Published Skills Saved",
      controls: "Controls Saved",
      policy: "Action Policy Saved",
      budget: "Budget Saved",
      assignment: "Starter Assignment Saved",
      configured: "Ready to Test",
      amending: "Saving Correction",
      activating: "Activating",
      ready: "Active",
    },
  },

  navigation: {
    newConversation: "New Conversation",
    people: "People",
    schedules: "Schedules",
    account: (name: string) => `You: ${name}`,
    projects: "Projects",
    library: "Library",
    operations: "Operations",
    you: "You",
    sources: "Sources",
    skills: "Skills",
    memory: "Memory",
    needsYou: "Needs You",
    agents: "Agents",
    assignments: "Assignments",
    network: "Agent Network",
    context: "Context Health",
    integrations: "Integrations",
    policies: "Action Policies",
    controls: "Agent Controls",
    usage: "Usage",
    audit: "Audit",
    connections: "Connected Accounts",
    profile: "Profile",
    notifications: "Notifications",
    privacy: "Privacy",
    personalAgents: "Personal Agents",
    search: "Search Work",
    jump: "Jump to Project",
    appearance: "Appearance",
  },
  attachedFilesPrompt: "Please review the attached files.",
  sourceOperations: {
    health: "Context Health",
    healthHint: "Where readable sources come from, how fresh they are and who depends on them.",
    integrations: "Integrations",
    integrationsHint:
      "Connect systems, choose resources, link projects and review access, events and actions.",
    connect: "Connect",
    resources: "Resources",
    links: "Links",
    access: "Access",
    events: "Events",
    actions: "Actions",
    capabilities: "Connection Capabilities",
    subscriptions: "Event Subscriptions",
    registeredActions: "Registered Actions",
    capabilitiesHint:
      "These limits only tighten this installed connection. Agent grants, project policies and external permissions still apply.",
    noRegisteredActions: "No outbound actions are registered for this connection.",
    managedCapabilities: "The server configuration manages this connection.",
    legacyCapabilities:
      "Existing default behavior is active. Saving records an explicit allow-list; clearing all choices denies all.",
    deliverOutcomes: "Deliver Outcomes",
    saveCapabilities: "Save Connection Limits",
    direction: "Bridge Direction",
    directions: { two_way: "Two Way", inbound: "Inbound Only", outcomes: "Outcomes Only" },
    directionHint:
      "Changing a bridge requires project ownership and connection permissions. Private sources and activity never cross it.",
    saveDirection: "Save Direction",
    bindingChanged: "The channel binding changed. Reload before changing its direction.",
    eventNames: {
      app_mention: "App Mentions",
      message: "Messages",
      slash_command: "Slash Commands",
      issue_comment: "Issue Comments",
      pull_request_review_comment: "Pull Request Review Comments",
      pull_request_review: "Pull Request Reviews",
      issues: "Issues",
      pull_request: "Pull Requests",
    },
    refreshCapabilities: "Reload Connection Limits",
    bridgeSemantics:
      "Inbound Only mirrors human messages passively without waking agents or sending output. Outcomes Only ignores incoming human messages and delivers permitted outcomes and decisions.",
    saveAccess: "Save Access Mode",
    connectionType: "Connection Type",
    connectSystem: "Connect a System",
    messaging: "Slack or GitHub App",
    externalSource: "Drive or Notion Source",
    cancel: "Cancel",
    close: "Close",
    saved: "Saved",
    emptyEvents: "No readable events in the last 24 hours.",
    eventsHint:
      "Recorded events in the past 24 hours. Receipt does not prove an outbound reply was delivered.",
    connectHint:
      "Configure an installed app, or connect your own source reference. No connection is labelled live until provider work succeeds.",
    allSystems: "All Systems",
    systems: "Connected Systems",
    sources: "Context Sources",
    clients: "Messaging & GitHub Apps",
    refresh: "Refresh",
    empty: "No readable sources are configured.",
    loading: "Loading...",
    failed: "The request failed. No change was confirmed.",
    readFailed: "This integration view could not be loaded.",
    configured: "Configured, Not Verified",
    noLiveTest:
      "Configuration does not verify live delivery. A real signed event and reply are needed.",
    newSource: "Connect Source",
    name: "Source Name",
    description: "Description",
    create: "Create Source",
    provider: "System",
    origin: "Folder or Page ID",
    project: "Project",
    workspace: "Company",
    scopeHint:
      "Indexed text is shared with readers of this project. Live searches use the asker's own access.",
    choose: "Choose Resources",
    loadResources: "Load Remote Resources",
    addSelected: "Add Selected",
    noResources: "No remote resources were returned.",
    incomplete:
      "The provider returned an incomplete listing. Nothing can be imported until a complete listing is available.",
    unsupported: "Unsupported",
    selected: "Selected",
    pinned: "Choose the pages to pin. Unselected resources remain live in hybrid mode.",
    liveSelection:
      "Live search queries the configured origin with your own access. Resource selections apply only to indexed or hybrid copies.",
    selectionRequired: "Choose at least one supported resource.",
    importHint:
      "Add Selected saves this exact selection and starts a real provider import. Failed imports retain the last good copy.",
    lastGood: "Last Good Sync",
    never: "No Successful Copy",
    mode: "Access Mode",
    status: "Status",
    dependents: "Depends On It",
    snapshot: "Snapshot",
    keepSnapshot: "Keep as Snapshot",
    replace: "Replace Snapshot",
    snapshotHint:
      "A snapshot keeps the last good copy and will not sync until explicitly replaced.",
    syncSettings: "Sync Settings",
    every: "Refresh Every (Minutes)",
    stop: "Stop After Stale Hours",
    save: "Save",
    sourceDetail: "Open Source",
    noSelection: "Select a system or source.",
    importing: "Importing",
    indexing: "Indexing",
    live: "Live / In Sync",
    snapshots: "Snapshots",
    attention: "Needs Attention",
    filter: "Filter Sources",
    all: "All Sources",
    search: "Find Source",
    lifecycle: "Lifecycle",
    details: "Source Details",
    connected: "Configured",
    attempts: "Recent Attempts",
    noAttempts: "No provider attempts recorded.",
    edit: "Edit Rule",
    previewCandidate: "Unsaved Rule",
    previewBeforeSave: "Preview matching resource metadata before saving an enabled rule.",
    workspaceEvents:
      "This activity covers the whole workspace for this system. It does not identify a particular installed app.",
    ruleTitle: "Auto-link Rules",
    ruleHint:
      "Rules match existing resource attributes. Conflicting destinations always become suggestions. Matching never grants source access.",
    ruleID: "Rule ID",
    attribute: "Attribute",
    pattern: "Match Pattern",
    destination: "Destination Project",
    channel: "Destination Channel",
    ruleMode: "Link Mode",
    suggest: "Suggest",
    auto: "Auto-link by Rule",
    off: "Off",
    newRule: "New Rule",
    preview: "Preview Rules",
    previewHint: "Preview explicit metadata before enabling a rule. This never links a resource.",
    resourceID: "Resource ID",
    metadata: "Attributes (JSON)",
    invalidJSON: "Attributes must be a JSON object of string arrays.",
    noMatches: "No rules matched this resource.",
    noRules: "No readable rules.",
    rulesUnsupported: "This source provider does not support automatic project-link rules.",
    remove: "Remove",
    suggestions: "Suggested Links",
    suggestionsUnavailable: "Suggested links are not available from this server yet.",
    link: "Link",
    ignore: "Ignore",
    turnRule: "Turn Into Rule",
    noBindings: "No readable channel bindings.",
    bindings: "Slack Channel Bindings",
    routing: "Routing",
    defaultAgent: "Default Agent",
    noDefault: "No Default Agent",
    allowed: "Allowed Agents",
    enabled: "Enabled",
    disabled: "Disabled",
    eventHint:
      "Only enabled clients receive signed events. The server enforces the configured agent allow-list and channel bindings.",
    noEventEditor: "This server does not expose editable event subscriptions for this connection.",
    policy: "Workspace Action Policy",
    policyHint:
      "These are workspace restrictions. Project policy, agent policy, tool grants and external credentials can tighten them further.",
    noPolicy: "No workspace override is recorded; inherited restrictions still apply.",
    policyUnavailable: "Action policy could not be read with your permissions.",
    allow: "Allow",
    ask: "Ask",
    neverAction: "Never",
    policies: "Open Policies",
    reconnectRequired:
      "Your access needs reconnecting. Save your own credential reference in Connect, then retry.",
    healthMissing:
      "Missing access or provider failures are shown here. Provider test fixtures are not live validation.",
  },
  sourceLifecycle: {
    heading: "Source connection",
    attention: (name: string, cause: string) =>
      `Source ${name} needs attention: ${cause}. Reconnect your own reference in Context, then retry.`,
    attemptStatus: {
      importing: "Importing",
      indexing: "Indexing",
      syncing: "Syncing",
      succeeded: "Succeeded",
      failed: "Failed",
    },
    dependents: "What relies on this source",
    agents: "Agents",
    projects: "Projects",
    noDependents: "No visible declarations in this directory window",
    dependencyFailed: "Source dependencies could not be loaded.",
    providers: { manual: "Manual", drive: "Google Drive", notion: "Notion" },
    grounding:
      "Indexed sources ground runs in their retained documents. Live searches use the current reader’s own access. A failed refresh keeps the last good copy.",
    configuration: "Configure Source",
    scopeHint:
      "An indexed copy is shared with readers of the selected project. Choose a resource you can share with that audience.",
    provider: "Provider",
    origin: "Folder or page ID",
    mode: "Mode",
    project: "Project (use ~ for workspace)",
    stop: "Stop after stale hours",
    owner: "Owner",
    save: "Save Configuration",
    credential: "Your credential reference",
    connect: "Save Reference",
    credentialHint:
      "Use your own allowed op:// reference. Saving a reference does not verify a connection or grant access.",
    referenceSaved: "Your reference is saved. Refresh or search to verify access.",
    sync: "Refresh Copy",
    working: "Request in progress",
    failed: "The source request failed.",
    liveUnavailable:
      "Live search is unavailable. Any indexed results below come from the retained copy.",
    lastGood: "Last good copy",
    noCopy: "No successful copy yet",
    attempts: "Recent refresh attempts",
    emptyAttempts: "No refresh attempted",
    stopHint: "Dependent work stops after this limit. Manual sources do not expire.",
    modes: { indexed: "Indexed copy", live: "Live search", hybrid: "Hybrid" },
    status: {
      connected: "Configured, not verified",
      importing: "Importing",
      indexing: "Indexing",
      available: "Available",
      syncing: "Syncing",
      in_sync: "In sync",
      needs_attention: "Needs attention",
    },
    cause: {
      auth_expired: "Your access needs reconnecting",
      quota: "Provider quota reached",
      unsupported: "Resource format or size is unsupported",
      unavailable: "Provider unavailable",
      interrupted: "Refresh interrupted. Retry to continue.",
    },
  },
  operations: {
    openAssignment: "Open Assignment",
    openHandoff: "Open Handoff",
    acknowledge: "Acknowledge",
    attention: "Agent work needs a person",
    controls: "Agent controls",
    workspaceControls: "Workspace controls",
    controlsHint:
      "Pause new work while keeping recorded work and approval rules intact. Running work reaches its recorded outcome.",
    pauseReasons: {
      workspace: "Workspace pause is in effect. Resuming this agent does not resume the workspace.",
      setup: "Complete and validate the reviewed setup before this agent can accept new work.",
      agent: "This agent is paused.",
    },
    unansweredReminder: "Remind about unanswered questions after (minutes)",
    blockedReminder: "Remind about blocked work after (minutes)",
    reminderInvalid: "Use a whole number of minutes between 0 and 10080.",
    reminderHint:
      "Use 0 to disable a reminder. Reminders update the existing item; they never approve or restart work.",
    effective: "Effective at this scope",
    scopeKinds: {
      workspace: "Workspace",
      project: "Project",
      agent: "Agent",
      personal: "Only you",
      inherited: "Inherited policy",
    },
    policyContext: "Project, agent and personal rules may narrow this further when a run starts.",
    lockedBy: "Restricted by",
    reviewDeadline: "Review Deadline",
    saveDeadline: "Save Deadline",
    reviewLatest: "Review Latest Record",
    clearDeadline: "Clear Deadline",
    expiredDeadline: "This deadline has passed. Review it before starting or resuming work.",
    lateResult: "The recorded run completed after its deadline.",
    recordedRunState: "Recorded run",
    inputs: "Inputs",
    noInputs: "No input objects were attached.",
    inputKinds: { run: "Run", artifact: "Artifact", thread: "Conversation", text: "Brief" },
    openInputRun: "Open Input Run",
    openInputConversation: "Open Input Conversation",
    openInputArtifact: "Open Input Artifact",
    artifactVersion: (n: string) => `Version ${n}`,
    unassigned: "Unassigned",
    recordedOwner: "Recorded owner",
    findPerson: (label: string) => `Find a person for ${label.toLowerCase()}`,
    ownerSelectionUnavailable:
      "The people directory is unavailable. The recorded owner will be kept.",
    paused: "Paused",
    active: "Accepting new work",
    pause: "Pause Agent",
    resume: "Resume Agent",
    pauseAll: "Pause All Agents",
    resumeAll: "Resume All Agents",
    manager: "Accountable manager",
    stopRules: "Stop rules and reminders",
    stopRulesHint: "Instructions for the agent. These do not replace the enforced action policy.",
    policies: "Action policy",
    policiesHint:
      "Each level can only tighten the rules above it. Allow keeps existing grants; it does not add tools.",
    category: "Action",
    rule: "Rule",
    inherited: "Inherit",
    allow: "Allow",
    ask: "Ask",
    never: "Never",
    categories: {
      read_context: "Read context",
      write_files: "Write files",
      publish_artifacts: "Publish artifacts",
      external_messages: "Send external messages",
      call_agents: "Call agents",
      other: "Other actions",
    },
    assignments: "Assignments",
    assignmentsHint:
      "Durable work with an owner, a goal and a clear definition of done. Standing assignments use the workspace scheduler.",
    newAssignment: "New Assignment",
    cancelCreate: "Cancel New Assignment",
    goal: "Goal",
    doneWhen: "Done when",
    agent: "Agent",
    project: "Project",
    owner: "Owner",
    due: "Due date",
    trigger: "When to run",
    onDemand: "On demand",
    onSchedule: "On a schedule",
    onSourceChange: "When a retained source changes",
    source: "Retained source",
    cron: "Repeat (cron)",
    create: "Create Assignment",
    run: "Run Assignment",
    cancel: "Cancel Assignment",
    watch: "Start Watching",
    accept: "Accept Assignment",
    retry: "Retry Assignment",
    openRun: "Open Recorded Run",
    conversation: "Open Conversation",
    empty: "No assignments are visible here yet.",
    blocker: "Blocker",
    standing: "Standing assignment",
    once: "One-time assignment",
    states: {
      requested: "Requested",
      accepted: "Accepted",
      in_progress: "In progress",
      watching: "Watching",
      result: "Result recorded",
      blocked: "Blocked",
      needs_person: "Needs a person",
      cancelled: "Cancelled",
    },
    working: "Working",
    waitTime: "Waiting time",
    minutes: (n: number) => `${n} min`,
    network: "Agent network",
    networkHint:
      "Recorded handoffs between agents you can read. Every count uses the same read rules as its contracts.",
    networkEmpty: "No handoffs are recorded between visible agents yet.",
    from: "From",
    to: "To",
    volume: "Handoffs",
    waiting: "Waiting",
    blocked: "Blocked",
    contracts: "Handoff contracts",
    truncated: "Showing at most 200 visible contracts.",
    save: "Save Changes",
    saving: "Saving",
    failed: "The change could not be saved. Reload and try again.",
    loadFailed: "Agent operations could not be loaded.",
    saved: "Saved",
    loading: "Loading",
    refresh: "Refresh",
    none: "None",
    workspace: "Company",
  },
  activityControls: {
    filters: "Filter activity",
    all: "All",
    handoffs: "Handoffs",
    tools: "Tool Calls",
    decisions: "Decisions",
    showTools: "Show Tools",
    hideTools: "Hide Tools",
    noMatches: "No matching activity",
    noMatchesBody: "Choose another filter to see the conversation's recorded work.",
  },
  usagePage: {
    title: "Usage",
    lede: "Agent work, human involvement and budgets, from the runs you can read.",
    range: "Time range",
    days: (n: number) => `${n} days`,
    runs: "Runs",
    alone: "Completed without help",
    needed: "Needed a person",
    unknown: "Assistance history unavailable",
    unknownNote: "Unfinished, older or recovered runs may not retain their full question history.",
    chart: "Show Chart",
    table: "Show Table",
    chartTitle: "Work by day",
    chartLabel: "Select a day to inspect its recorded work",
    other: "Other work",
    otherNote:
      "Other work includes in-flight, failed or stopped runs without a recorded question, and runs with unavailable assistance history.",
    timezone: "Days are shown in UTC.",
    day: "Day",
    details: "Selected day",
    clearDay: "Show Entire Range",
    agents: "By agent",
    origin: "By origin",
    avg: "Average duration",
    handoffs: "Handoffs out",
    failed: "Failed",
    lastFailure: "Latest failure",
    noFailure: "None recorded",
    openFailure: "Open Failed Run",
    cost: "Spend",
    tokens: "Tokens",
    noData: "No runs in this interval",
    noDataBody: "Recorded work will appear here when agents run.",
    loadFailed: "Usage could not be loaded.",
    dayFailed: "The selected day's breakdown could not be loaded.",
    retry: "Try Again",
    priced: "Priced in US dollars",
    partialCost: "Spend excludes unpriced calls and other currencies.",
    run: "Open Run",
    ended: "Of ended runs, including waiting time",
    dayLabel: (day: string, runs: number, alone: number, needed: number) =>
      `${day}: ${runs} ${runs === 1 ? "run" : "runs"}, ${alone} completed without help, ${needed} needed a person`,
  },
  /** The product, as a sender: what every system message is shown from, whatever name its record keeps. */
  product: "Humagen",
  forking: {
    fromHere: "Fork From Here",
    archive: "Read-only conversation archive",
    title: "Fork Conversation",
    create: "Create Fork",
    name: "Fork title",
    agents: "Agents for future messages",
    sources: "Sources for future turns",
    inherit: "Inherit available sources",
    none: "No Sources",
    selected: "Choose Sources",
    sourceHint:
      "Only selected sources are available in future turns, including sources configured on agents.",
    hint: "History through this turn becomes read-only context. The new conversation is private in the same project. Source access remains required.",
    failed: "The fork could not be created.",
    loading: "Loading choices…",
    context: "Read-only fork context",
    parent: "Open Source Conversation",
    through: "Forked through",
    empty: "No agents selected. Choose agents or tag one in your next message.",
    bringBack: "Bring Back Result",
    result: "Result to post",
    bringHint:
      "Post this result as your own message in the source conversation. This does not execute tags, tools or pending actions.",
    brought: "Result posted to the source conversation.",
    bringFailed: "The result could not be posted.",
    cancel: "Cancel",
    bestFit: "Best Fit",
    all: "All Selected Agents",
    mode: "Who answers",
  },
  conversationAccess: {
    title: "People & Access",
    add: "Add Participant",
    handle: "Person handle or agent name",
    kind: "Participant type",
    person: "Person",
    agent: "Agent",
    remove: "Remove",
    owner: "Owner",
    failed: "Access could not be changed.",
    updated: "Access updated.",
    share: "Share in Project",
    makePrivate: "Make Private",
    privateHint:
      "Only participants and authorized workspace administrators can read this conversation.",
    sharedHint:
      "Anyone who can read this project can read this conversation. Removing a participant does not remove their project access.",
    forkHint: "Readers also need access to every source conversation in this fork's lineage.",
  },
  projectRollup: {
    title: "Initiative Overview",
    children: "Child Projects",
    notes: "Decisions & Risks",
    needs: "Needs Your Attention",
    inherited:
      "Policies, shared context, skills and approved memory flow down. Child decisions and risks stay in their original projects.",
    emptyProjects: "No readable child projects yet.",
    emptyNotes: "No confirmed decisions or risks.",
    emptyNeeds: "Nothing needs your attention.",
    loading: "Loading project rollup...",
    failed: "Project rollup could not be loaded.",
    more: "More readable items are available in their original project views.",
    active: "Active",
    conversations: "Conversations",
    unread: "Unread",
    attention: "Needs Attention",
    runs: "Runs in 30 Days",
    decision: "Decision",
    risk: "Risk",
    review: "Review",
    open: "Open Project",
    manage: "Manage",
    context: "Shared Context",
    policies: "Policies",
  },
  projectTemplates: {
    parentMismatch: "This template does not fit the selected project type or parent.",
    start: "Start From",
    blank: "Blank Project",
    initiative: "Initiative",
    underInitiative: "Under Initiative",
    hint: "Templates create channel and skill configuration together. Memory, credentials and integrations are never included.",
    failed: "Templates could not be loaded.",
  },
  projectFork: {
    archive: "Copy Conversations as Read-only Archive",
    archiveHint:
      "Copy saved text only, with source access still required. Archives cannot send messages or run agents.",
    origin: "Forked from",
    originHidden: "Forked project",
    title: "Fork Project",
    name: "New project name",
    titleField: "Project title",
    parent: "Parent project",
    company: "Company",
    create: "Create Fork",
    retry: "Retry Copy",
    memory: "Confirmed memory to copy",
    sources: "Sources to link",
    noMemory: "No confirmed memory is available.",
    hint: "Copy project structure, agent configuration and skills. Conversations, schedules and approvals stay in the source. The new project is private; integration rules are copied disabled and live bindings are detached.",
    hidden: "The copy stays hidden and cannot run until every step is ready.",
    failed: "The project could not be copied.",
    cancel: "Cancel",
    loading: "Loading confirmed context…",
    status: "Copy status",
    preparing: "Preparing",
    stopped: "Stopped Before Publication",
    ready: "Ready",
    result: (projects: number, agents: number, memory: number) =>
      `${projects} projects, ${agents} agents and ${memory} confirmed memories`,
  },
  projectSwitcher: {
    title: "Search Workspace",
    search: "Find projects, agents or conversations",
    kinds: "Search in",
    all: "All",
    projects: "Projects",
    agents: "Agents",
    conversations: "Conversations",
    empty: "No readable destinations match.",
    failed: "Search could not be loaded.",
    loading: "Searching…",
    open: "Switch Project",
  },
  conversation: {
    settings: "Conversation Settings",
    messageActions: "Message actions",
    copyMessage: "Copy message",
    copyAnswer: "Copy answer",
    outcomeKind: { recommendation: "Recommendation", decision: "Decision", update: "Result" },
    outcomeDecisions: "Decisions",
    outcomeNext: "Next Action",
    redirect: "Redirect",
    redirectPrompt: (agent: string, link: string) =>
      `@${agent} redirecting [this outcome](${link}). Please change direction: `,
    stopTurn: "Stop Turn",
    stoppingTurn: "Stopping…",
    stopPending: "Stop requested. Waiting for the recorded turn to end.",
    stopDelayed: "The turn has not ended yet. Check its activity or try Stop Turn again.",
    activity: "Activity",
    context: "Context",
    people: "People & Access",
    deliverables: "Deliverables",
    project: "Project",
    projectAction: "Project and sharing",
    private: "Private",
    shared: "Project conversation",
    agents: (count: number) => `${count} ${count === 1 ? "agent" : "agents"} took part`,
    sourcesFailed: "Project context could not be loaded.",
    sourcesEmpty: "No sources are attached to this project yet.",
    sourceMode: "Indexed",
    sourceUpdates: "Imported documents",
    sourceOrigin: "Origin",
    sourceImported: "Imported",
    sourceHealth: "Available for search",
    sourceEmpty: "No text has been indexed",
    sourcePassages: (count: number) => `${count} ${count === 1 ? "passage" : "passages"} indexed`,
    sourceExcluded: "Excluded from search",
    more: "Show More",
    artifactUse: "Use This Result",
    artifactAccept:
      "I reviewed the whole file, including notes and sources, for this project's audience",
    artifactAddKnowledge: "Add to Knowledge",
    artifactSuggestMemory: "Suggest Memory",
    artifactSource: "Knowledge Source",
    artifactChooseSource: "Choose a Source",
    artifactNoSource:
      "Add a manual indexed knowledge source in this project to retain accepted results.",
    artifactOpenContext: "Open Project Context",
    artifactPromotionFailed:
      "The accepted result could not be retained. Your selection is kept so you can try again.",
    artifactPromoted: "This exact version was added to the selected knowledge source.",
    artifactMemoryPending: "Memory proposed. A project owner must review it before it is used.",
    artifactSharedRequired:
      "Publish a reviewed shared result before adding it to project knowledge or memory.",
    artifactPromotionSaving: "Saving…",
    artifactSourcesLoading: "Loading knowledge sources…",
    artifactSourceRetry: "Try Again",
    artifactSourceFailed: "Knowledge sources could not be loaded.",
    artifactPresentation: (name: string) => `Presentation: ${name}`,
    artifactPDF: "Download PDF",
    artifactPDFBusy: "Preparing PDF…",
    artifactPDFFailed:
      "The PDF could not be prepared. Download the original to retain the full content.",
    artifactReadSource: (id: string, version: number) =>
      ` Read the immutable source with file_read path artifact:${id}@${version}.`,
    artifactNoMatch: "No deliverables match this type.",
    sourceDocs: (count: number) => `${count} ${count === 1 ? "document" : "documents"}`,
    peopleEmpty: "No participants are recorded yet.",
    contextHint:
      "Sources available to this project. Expand a source to see its documents, origins and import times.",
    tools: (count: number) => `${count} ${count === 1 ? "work step" : "work steps"}`,
    complete: (count: number) => `${count} completed`,
    blocked: (count: number) => `${count} need attention`,
    askAgent: (agent: string) => `Ask ${agent}`,
    reply: "Reply",
    question: "Clarification",
    answerChoice: "Use This Answer",
    questionHint: "This answers the question. Approvals for actions remain separate.",
    artifactFailed: "The artifact could not be loaded.",
    artifactEmpty: "No deliverables have been saved in this conversation yet.",
    artifactFeedback: "Request Changes",
    artifactPreview: "Preview",
    artifactVersions: "Versions",
    artifactSources: "Provenance",
    artifactDraft: "Saved artifact",
    artifactHint:
      "Review and download this version, or ask its agent for a revision in the conversation.",
  },
  clientConnect: {
    heading: "Connect a system",
    open: "Connect a System",
    intro:
      "Install your Slack app or GitHub App first, then provide its credentials here. Saving configures the connection; a real reply verifies it works.",
    invalidDefaultAgent:
      "The default must name one agent. Use letters, digits, underscores or dashes, starting with a letter or digit.",
    invalidAgents:
      "List individual agent names, separated by commas. This form does not grant access to every agent.",
    invalidGithubLogin:
      "Give the GitHub App's own login, such as your-app[bot], so mentioning it can reach the default agent.",
    credentialsRequired: "Give both a token and a signing secret.",
    failed: "The connection could not be created. Try again.",
    system: "System",
    slack: "Slack",
    github: "GitHub",
    id: "Connection ID",
    idHint: "A unique name of up to 40 lowercase letters, digits and dashes.",
    name: "Name",
    slackTeam: "Slack team ID",
    slackTeamPlaceholder: "T0123ABC",
    githubAccounts: "GitHub accounts",
    githubAccountsHint: "Account or organization names, separated by commas.",
    installation: "Installation ID (optional)",
    botLogin: "GitHub bot login",
    botLoginHint:
      "The app's own GitHub login, such as your-app[bot]. Required with a default agent so native @mentions can reach it.",
    botLoginPlaceholder: "your-app[bot]",
    app: "App ID (optional)",
    defaultAgent: "Default agent (optional)",
    defaultAgentHint:
      "Answers messages that name no agent. With no default and no allowed agents, nothing can run.",
    allowedAgents: "Other allowed agents (optional)",
    allowedAgentsHint:
      "Individual agent names, separated by commas. No access is granted by default.",
    credentialHint:
      "Credential values are stored by the server and never shown again. An environment reference must be permitted by your workspace operator. GitHub needs a current installation token; it is not refreshed automatically.",
    tokenSource: "Token source",
    signingSource: "Signing secret source",
    value: "Value",
    reference: "Environment reference",
    token: "Token",
    tokenReference: "Token reference",
    tokenPlaceholder: "env:HUMAGEN_SLACK_TOKEN",
    signing: "Signing secret",
    signingReference: "Signing secret reference",
    signingPlaceholder: "env:HUMAGEN_SLACK_SIGNING_SECRET",
    connecting: "Connecting…",
    connect: "Connect",
    emptyAdmin:
      "Connect an installed Slack app or GitHub installation. Its secrets are written once and never shown again.",
    emptyReader: "An administrator can connect a Slack app or GitHub installation.",
    webhook: (surface: "slack" | "github") =>
      `Your app must send signed events to the server's integration listener at /connectors/${surface}. Your workspace operator provides its public address.`,
  },
  notifications: {
    title: "Notifications",
    empty: "No notifications ready.",
    digest: "Digest",
    immediate: "Update",
    dismiss: "Dismiss Notification",
    settings: "Notification Preferences",
    failed: "Notifications could not be loaded.",
    items: (count: number) => `${count} updates`,
    description:
      "Console updates arrive immediately or in hourly digests. Quiet hours delay delivery.",
  },
  agentSkillDefaults: {
    inspect:(name:string,version:number)=>`Open ${name}, Version ${version}, in a new tab`,
    tools: "Tool Restrictions",
    noTools: "No Tools",
    title: "Default skills",
    help: "Choose up to four published revisions. Every turn uses these exact instructions and their tool restrictions.",
    scope: "Skill library scope",
    workspace: "Workspace",
    personal: "Personal",
    empty: "No published skills in this scope.",
    selected: "Selected revisions",
    savedRevision: "Saved skill revision",
    remove: "Remove",
    save: "Save Default Skills",
    reload: "Reload Saved Bindings",
    saved: "Default skills saved.",
    loading: "Loading default skills",
    failed: "Default skills could not be loaded or saved.",
    stale: "The agent changed. Review these defaults against its current revision before saving.",
    readOnly: "Default skills are read only.",
    unavailable: "Reading available skill revisions requires access to the project library.",
    version: (version: number) => `Version ${version}`,
  },
  skillsLibrary: {
    title: "Skills",
    description:
      "Draft, test and publish reusable instructions. Editing creates a new version; a draft leaves the published version unchanged.",
    scope: "Scope",
    personal: "Personal",
    project: "Project",
    workspace: "Workspace",
    projectName: "Project",
    new: "New Skill",
    name: "Name",
    purpose: "Purpose",
    inputs: "Inputs",
    outputs: "Outputs",
    steps: "Instructions",
    tools: "Allowed Tools",
    toolsHint:
      "Exact tool names, one per line. Empty permits no tools. Agent and workspace restrictions still apply.",
    save: "Save Draft",
    draft: "Draft",
    published: "Published",
    version: "Version",
    empty: "No skills in this scope.",
    failed: "The skill library could not be loaded or changed.",
    test: "Test Skill",
    agent: "Test Agent",
    request: "Test Request",
    testHint: "Tests use the exact selected version. Every permitted action requires approval.",
    publish: "Publish Version",
    promotion: "Promote to Another Scope",
    promote: "Create Promotion Draft",
    promotionHint:
      "The destination can read the copied instructions. Promotion creates a draft that must be tested before publication.",
    testRun: "Open Test Conversation",
    testUses: "Agents That Tested This Version",
    refresh: "Refresh",
    provenance: "Promoted From",
    readOnly: "You may read this scope. Its owners manage revisions.",
  },
  personalAgents: {
    title: "My Agents",
    description: "Agents whose home is your personal project.",
    empty: "No personal agents yet.",
    failed: "Your agents could not be loaded or changed.",
    pause: "Pause",
    resume: "Resume",
    settings: "Agent Settings",
    add: "Add to a Project",
    project: "Destination Project",
    review: "Review Audience",
    confirm: "Move Agent to Project",
    cancel: "Cancel",
    audience: "Members of the destination can use this agent and read its configuration.",
    create: "Create Agent",
  },
  personalPreferences: {
    profile: "Profile",
    privacy: "Privacy and Defaults",
    agents: "My Agents",
    displayName: "Display Name",
    role: "Role",
    connections: "Connected Accounts",
    memory: "Review My Memory",
    email: "Email",
    deliveryUnavailable:
      "Email delivery is not configured. Console notifications use the choices below; your inbox history stays available.",
    lastAgentUse: "Last Agent Access",
    grantedScopes: "Granted Scopes",
    scopesUnknown: "Not Reported by This Connection",
    neverUsed: "Not Used Yet",
    title: "You",
    description: "How agents brief you, your notification choices, and your private defaults.",
    briefing: "How Should Agents Brief You?",
    notifications: "Notifications",
    quiet: "Quiet Hours",
    start: "Start",
    end: "End",
    timezone: "Time Zone",
    save: "Save Preferences",
    saved: "Preferences saved.",
    failed: "Your preferences could not be loaded or saved.",
    immediate: "Immediately",
    digest: "Digest",
    off: "Off",
    remember: "Remember How I Work",
    personalMemory: "Allow My Personal Memory in Project Turns",
    privateDefault: "New conversations start private. Sharing requires an audience review.",
    events: {
      approval: "Approvals",
      clarification: "Questions",
      failure: "Failures",
      result: "Results",
      mention: "Mentions",
      announcement: "Announcements",
      memory: "Memory Review",
      budget: "Budget",
      artifact: "Files",
      alert: "Alerts",
    },
  },
  memoryLibrary: {
    promote: "Propose to Project",
    destination: "Destination Project",
    promotionReview:
      "The selected text, kind and expiry will be shared with this project. Its owners must confirm the new suggestion. Original private provenance stays private.",
    promotionSaved: "Suggested to the project. Its owners will review it.",

    search: "Search Memory",
    scope: "Scope",
    workspace: "Workspace",
    project: "Project",
    agent: "Agent",
    owner: "Owner",
    edit: "Edit",
    cancel: "Cancel",
    saveEdit: "Save Correction",
    discard: "Discard",
    openSource: "Open Source",
    noMatches: "No memory matches this search.",
    readOnly: "You may read these notes. Their owners review and correct them.",
    emptyScope: "Choose a project or agent.",

    title: "Memory",
    approve: "Keep",
    description:
      "Private notes stay yours. Project and workspace notes are reviewed where the work happens.",
    personal: "Personal",
    text: "What Should Be Remembered?",
    kind: "Kind",
    expires: "Expires",
    save: "Remember",
    forget: "Forget",
    empty: "No memory in this scope yet.",
    failed: "Memory could not be loaded or saved.",
    fact: "Fact",
    risk: "Risk",
    decision: "Decision",
    preference: "Preference",
    person: "Person",
    confirmed: "Confirmed",
    suggested: "Suggested",
    expired: "Expired",
    uses: "Uses",
    source: "Provenance",
    never: "No Expiry",
  },
  audit: {
    actor: "Actor",
    domain: "Domain",
    from: "From",
    to: "To",
    apply: "Apply Filters",
    export: "Export CSV",
    open: "Open Resource",
    allDomains: "All Domains",
    csvColumns: [
      "ID",
      "When",
      "Actor",
      "Action",
      "Resource",
      "Resource ID",
      "Outcome",
      "Rule",
      "Route",
      "Before",
      "After",
    ],

    title: "Audit",
    description:
      "Workspace changes in the last 24 hours. Pending means completion could not yet be confirmed.",
    empty: "No recorded changes in this window.",
    failed: "The audit could not be loaded.",
    before: "Before",
    after: "After",
    refresh: "Refresh",
    route: "Route",
    rule: "Rule",
  },
  readAudit: {
    label: "Read Audit",
    denied: "Only workspace administrators may read this audit.",
    title: "Break-glass reads",
    description:
      "Reads allowed only by project governance in the last 24 hours. Showing at most 100 records, newest first.",
    empty: "No break-glass reads in the last 24 hours.",
    failed: "The read audit could not be loaded.",
  },
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
  /** A question answered in words, not by picking one of its options. */
  repliedBy: (by: string, text: string, time: string) => `${by} answered "${text}" · ${time}`,
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
  /** A page's menu of its other actions, beside its one primary action. */
  more: "More",
  /** The second step of an action that cannot be undone. */
  confirm: {
    ask: (what: string) => `Delete ${what}? This cannot be undone.`,
    keep: "Keep It",
  },
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
  publish: "Publish",
  discardDraft: "Discard Draft",
  /** Above an agent's draft: who wrote it and where publishing posts it. */
  draftTitle: (agent: string, project: string) =>
    `${agent} wrote this for ${project}. Publishing posts it there, from ${agent}.`,
  memoryTitle: (n: number) => `${n} thing${n === 1 ? "" : "s"} to remember`,
  allowOnce: "Allow This Run",
  raiseTo: (amount: string) => `Raise to ${amount}`,
  budgetTitle: "Limit reached",
  budgetBody: (used: string, limit: string, period?: string, needs?: string) =>
    `${used} of ${limit} used${period ? ` this ${period}` : ""}.${needs ? ` This run needs ${needs} more.` : ""}`,
  noPermission: "You can read this, but you don't have permission to resolve it.",
  mailbox: {
    /** How many are open, beside the Inbox's title. */
    openCount: (n: number) => `${n} open`,
    empty: "Nothing here",
    emptyHint: "Approvals, questions, alerts and new artifacts arrive here, and are resolved here.",
    emptyOpen: "You're all caught up",
    emptyOpenHint: "Nothing is waiting on you. New approvals, alerts and artifacts land here.",
    pick: "Pick a message to read it",
    pickHint: "Decisions come first, then updates.",
    back: "Back",
    needsYou: "Needs you",
    /** The inbox's announcements, by the name Home gives them. */
    updates: "What's new",
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
    draft: "Draft",
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
    announcement: "Announcement",
  },
  /** Announcements: news posted in a project's inbox, read under What's new. */
  announcements: {
    post: "Post Announcement",
    withdraw: "Withdraw",
    hideForMe: "Hide for Me",
    showForMe: "Show for Me",
    /** A row the reader hid for themselves, where others' rows say Dismissed. */
    hidden: "Hidden",
    hiddenForYou: "Hidden for you. Others still see it.",
    snoozedForYou: "Snoozed for you. Others still see it.",
    withdrawn: "Withdrawn for everyone.",
    /** Under an open announcement, whose button is Hide for Me. */
    readOnly: "Nothing to decide. Hide it when you're done. Others still see it.",
    /** A project's inbox with nothing open for the reader. */
    emptyOpenHint: "New announcements land here. What you hid is under Resolved.",
    includeSubProjects: "Include Sub-projects",
    postedIn: (project: string) => `Posted in ${project}`,
    withdrawnBy: (who: string) => `Withdrawn by ${who}`,
    mute: "Mute Project",
    unmute: "Unmute Project",
    muted: "Muted. Its announcements stay out of your inbox and stay in the project's.",
    /** Beside Mute Project, when the change could not be saved, or the preferences it is saved with could not be read. */
    muteFailed: "Could not mute. Try again.",
    unmuteFailed: "Could not unmute. Try again.",
    rootNotMuted: "The workspace's announcements are for everyone, so they can't be muted.",
    tooMany: (n: number) => `At most ${n} announcements a day are posted here. Try again tomorrow.`,
    /**
     * What an announcement says once it was withdrawn, where it was posted:
     * the first turn of a thread a reply to it started, and its post in a
     * Slack channel.
     */
    seedWithdrawn: "This announcement was withdrawn.",
    /** In Slack, to someone replying under an announcement that was withdrawn. */
    replyGone: "This announcement was withdrawn, so a reply to it starts nothing.",
  },
  /** A conversation pinned to the top of the sidebar. Said beside its pin when the change could not be saved, or the preferences it is saved with could not be read. */
  pins: {
    pinFailed: "Could not pin. Try again.",
    unpinFailed: "Could not unpin. Try again.",
  },
  /** Who holds a role in a project, and what it lets them do. */
  roles: {
    viewer: "Viewer",
    member: "Member",
    owner: "Owner",
    everyone: "Everyone in the workspace",
    addPerson: "Add Person",
    nameOwners: "Name Owners",
    moveProject: "Move Project",
    archive: "Archive Project",
    unarchive: "Restore Project",
    ownerFrom: (project: string) => `Owner from ${project}`,
    everyoneOwns:
      "Everyone in the workspace owns this project. Name owners to narrow who can change it.",
    needOwner: (project: string) => `You need to own ${project}`,
    archived: "Archived. Nothing new can be added under it.",
  },
  /** A project's page: its tabs, its kinds in words, and what each tab says. */
  project: {
    tabs: {
      inbox: "Inbox",
      overview: "Overview",
      context: "Context",
      people: "People",
      limits: "Limits",
      history: "History",
    },
    /** A kind in words. A workspace's own kind shows its title. */
    kind: (kind: string, title?: string) =>
      kind === "company"
        ? "Company-wide"
        : kind === "team"
          ? "Team"
          : kind === "account"
            ? "Account"
            : (title ?? kind),
    in: (parent: string) => `in ${parent}`,
    /** The caller's role, in words: "You own it". */
    yourRole: (role: string) =>
      role === "owner"
        ? "You own it"
        : role === "member"
          ? "You are a member"
          : role === "viewer"
            ? "You can view it"
            : "",
    allProjects: "All projects",
    newSubProject: "New Sub-project",
    newTask: "New Conversation",
    deleteProject: "Delete Project",
    move: "Move",
    moveUnder: "Move under",
    topLevel: "The top level",
    subProjects: "Sub-projects",
    noSubProjects: "No sub-projects yet.",
    instructions: "Instructions",
    instructionsHint:
      "Sent to every agent answering here and below, after the instructions of the projects above.",
    noInstructions: "No instructions of its own.",
    saveInstructions: "Save Instructions",
    proposeHint: "Your note is a proposal. The project's owners keep or discard it in their inbox.",
    members: "Members",
    ownersAbove: "Owners from above",
    /** The project above that an owner entry is on, when it is the workspace. */
    theWorkspace: "the workspace",
    remove: "Remove",
    who: "Who",
    whoHint: "A person's subject, or role:<name> for everyone who holds a role.",
    role: "Role",
    saveMembers: "Save Members",
    everyoneNeeded: "Everyone is at least a member of a company-wide project.",
    /** A project with no entry for everyone. */
    membersOnly:
      "Members only. Only the people listed and the owners above see it and what it holds.",
    /** The nudge on an account everyone can see. */
    visibleToEveryone: (project: string) =>
      `${project} is visible to everyone. Make it members only?`,
    makeMembersOnly: "Make Members Only",
    /** A project under one everyone owns, which therefore cannot be members only. */
    everyoneOwnsAbove: (project: string) =>
      `Everyone owns ${project}, so everyone sees this project too. To make it members only, name owners for ${project} first.`,
    limitFrom: (project: string) => `${project}'s limit also applies here.`,
    workspaceLimit: "The workspace's own limit applies here too.",
    /** A Limits tab the reader can only read: who may change it. */
    limitsReadOnly: (above?: string) =>
      above
        ? `Only an admin can change these. An owner of ${above} can set a tighter one.`
        : "Only an admin can change these.",
    ownerLimit: "A tighter limit, as an owner above",
    ownerLimitHint:
      "It can only be tighter than every limit over this project. An admin's limit is changed under Settings.",
    period: "Each",
    maxRuns: "Most runs",
    setLimit: "Set Limit",
    removeLimit: "Remove Limit",
    what: {
      created: "Created",
      updated: "Changed",
      deleted: "Deleted",
      managed: "Taken over or let go by the platform file",
      moved: "Moved",
      archived: "Archived or restored",
      members: "Members changed",
      agents: "Agents changed",
      home: "An agent's home changed",
      experts: "Experts changed",
      knowledge: "Knowledge sources changed",
      skills: "Skills changed",
      channel: "A channel changed",
    } as Record<string, string>,
    noHistory: "Nothing has changed here yet.",
    older: "Show Older",
    subject: "Subject",
    text: "What to say",
    warning: "Mark as a warning",
    whoAnnounces: "Who may announce",
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
  credentialUnavailable:
    "The model credential is unavailable. Ask your workspace operator to check the provider configuration, then retry.",
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
  /** The chat bar's + menu and what it selects for a conversation. */
  chat: {
    /** The + button, which opens the menu. */
    plus: "Add files, agents or a project",
    /** The shortcut that opens it, said in its tooltip: Cmd+/ on a Mac, Ctrl+/ elsewhere. */
    plusShortcut: (mac: boolean) => (mac ? "Cmd+/" : "Ctrl+/"),
    /** The sidebar's filter of conversations, and how many filters are on. */
    filter: (on: number) => (on > 0 ? `Filter conversations, ${on} on` : "Filter conversations"),
    /** Why a shared conversation's project can't change from the + menu. */
    projectShared: "A shared conversation stays in the project it was shared in.",
    /** Why someone added to a conversation can't move it. */
    projectStarter:
      "Only the person who started this conversation, or an owner of its project, can move it.",
    /** Why a Slack conversation's project can't change. */
    projectFromSlack: "A conversation started in Slack stays in the project of its Slack channel.",
    menu: "Add to this conversation",
    attach: "Attach Files",
    agents: "Agents",
    findAgent: "Find an agent",
    agentsLoading: "Loading agents…",
    noAgents: "No agents can answer in this project.",
    noAgentMatch: "No agent matches.",
    project: "Project",
    chooseProject: "Choose a project",
    projectless: "Company",
    channel: "Channel",
    /** The chips row, as a list. */
    selected: "Selected for this conversation",
    removeAgent: (agent: string) => `Remove ${agent}`,
    removeProject: (where: string) => `Remove ${where}`,
    /** How an untagged message goes to two agents or more. */
    routing: "How messages are routed",
    bestFit: "Best Fit",
    bestFitHint: "One of them answers each message: the one that fits it best.",
    all: "All of Them",
    allHint: "Each of them answers every message, side by side.",
    allMax: (n: number) =>
      `At most ${n} agents answer side by side. Select fewer to use All of Them.`,
    /** Under the agents once as many are checked as a conversation takes. */
    maxAgents: (n: number) => `A conversation can have at most ${n} agents.`,
    /** Agents taken off the conversation because they can't answer in the project picked. */
    notHere: (agents: string[]) =>
      `${agents.join(", ")} can't answer in this project, so ${agents.length === 1 ? "it was" : "they were"} taken off. Pick another project, or another agent.`,
    /** The chat bar's prompt with nothing selected, with one agent, and with several. */
    placeholder: "Message, or @ an agent",
    placeholderOne: (agent: string) => `Message ${agent}`,
    placeholderBest: (_n: number) => "Message, and the agent that fits best answers",
    placeholderAll: (n: number) =>
      n === 2 ? "Message, and both agents answer" : `Message, and all ${n} agents answer`,
    /** Under an answer that went to an agent because it was selected, not tagged. */
    routedBest: (agent: string) => `Routed to ${agent}, the best fit`,
    routedAll: (agent: string) => `Routed to ${agent}, with every agent selected`,
    routed: (agent: string) => `Routed to ${agent}`,
    /** Home's suggestions, as a list. */
    suggestions: "Suggestions",
    /** The selection could not be saved on the conversation. */
    saveFailed: "The conversation's agents could not be saved. Try again.",
  },
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
    platform: "Company agent: available wherever a project enables it",
    everywhere: "Available everywhere",
    nowhere: "Not enabled in any project yet.",
    loses: (n: number) => `${n} ${n === 1 ? "place" : "places"} will lose this agent.`,
    /** On an agent's Access tab. */
    title: "Where it answers",
    hint: "Company agents answer wherever a project enables them. An agent with another project as its home answers there and below.",
    home: "Lives in",
    noHome: "Company",
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
    platformAgent: "Company agent",
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
  /** Where a schedule's answers go, and the weekly recap a project can add. */
  schedules: {
    deliver: "Answer",
    deliverHint: "Where each run's answer goes. A recap that finds nothing to say sends nothing.",
    deliverNone: "Keep in the conversation",
    /** A new schedule is the reader's own; an existing one goes to whoever created it. */
    deliverInbox: (mine: boolean) => (mine ? "Send to my inbox" : "Send to its creator's inbox"),
    deliverProject: "Post to the project",
    addWeeklyRecap: "Add Weekly Recap",
    recapText: (title: string) => `What changed in ${title} since the last recap?`,
    recapDescription: "A weekly recap of this project, posted to its inbox",
    recapHint: (agent: string) =>
      `Each Monday morning ${agent} looks at what changed here and posts it to the project.`,
    hasRecap: "This project has a weekly recap.",
    /** A schedule the platform file declares: run and pause it here, change it in the file. */
    managed: "From the platform file. Change it there.",
    tab: "Schedules",
    none: "Nothing runs on a timer here.",
    /**
     * When a schedule runs, in words. Its expression is read in UTC, and `at`
     * is a time of day in it, "08:09". `day` is 0 for Sunday to 6 for Saturday.
     */
    when: {
      everyMinute: "Every minute",
      hourly: "Hourly",
      hourlyAt: (minute: number) => `Hourly at minute ${minute}`,
      daily: (at: string) => `Daily at ${at} UTC`,
      weekdays: (at: string) => `Weekdays at ${at} UTC`,
      weekends: (at: string) => `Weekends at ${at} UTC`,
      weekly: (day: number, at: string) =>
        `${["Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"][day]} at ${at} UTC`,
    },
  },
  /** The one-field form a Slack mention or an expert's question opens to reply. */
  replyModal: {
    title: "Reply",
    label: "Your reply",
    answerLabel: "Your answer",
    placeholder: "Write a reply",
    send: "Send",
    cancel: "Cancel",
  },
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
    needsYou: "Needs you",
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
    /** An inbox item above normal, said in words before when it came. A normal one says nothing. */
    urgency: { critical: "Urgent", warning: "Warning" } as Partial<Record<string, string>>,
    whatsNew: "What's new",
    nothingNew: "Nothing new from your projects.",
    moreUpdates: (n: number) => `${n} more under What's new`,
    /** The console's Needs you box: its way to the rest. */
    openInbox: "Open Inbox",
    /** What's new's way to every announcement in the inbox. */
    seeAll: "See All",
    /** A box on Home before what it lists has been read. */
    loading: "Loading",
    needsFailed: "What needs you could not be loaded. It is all in the inbox.",
    newsFailed: "What's new could not be loaded. It is all in the inbox.",
    /** Home for a person with nothing waiting and nothing new: what Home is for. */
    banner: {
      failed: "The announcement could not be updated. Try again.",
      label: "Company announcement",
      markRead: "Mark as Read",
      hide: "Hide for Me",
      open: "Open Announcement",
    },
    findHelp: {
      title: "Find Help",
      description: "Find a project and an agent for your work",
      label: "Describe the work",
      placeholder: "What are you working on?",
      search: "Find Help",
      searching: "Finding help",
      empty: "No matching projects and agents you can read were found.",
      failed: "Help could not be loaded. Try again.",
      choose: "Choose",
      open: "Open Project",
    },
    welcome: {
      title: "Welcome to Humagen",
      text: "Your conversations start in your personal project. Publish a reviewed result to a shared project when it is ready.",
      /** For a person who cannot start a conversation here. */
      quiet: "When an agent needs you, or someone posts in one of your projects, it shows here.",
    },
    yourProjects: "Your projects",
    noProjects: "You are not in any project yet.",
    moreProjects: (n: number) => `${n} more in Projects`,
    /** A row's way in on Slack, where the title is not a link. */
    open: "Open",
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
    companyName: "Company",
    personalTitle: "Personal",
    companyStart: "Start in the company-wide project",
    unreadUpdates: "Unread updates",
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
    emptyCreate: "No projects yet. Make one to keep related conversations together.",
    noMatch: "No project matches.",
    allProjects: "All projects",
    /** Before the agent that answers a project's untagged Slack and GitHub messages, in its meta line: "Lead globex-helper". */
    lead: "Lead",
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
    newTask: "New Conversation",
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
      /** Under the kind, for an account: who sees it. */
      accountMembersOnly:
        "Accounts are members only: you, the people you add and the owners above see it. Under a project everyone owns, everyone sees it.",
    },
    count: {
      running: (n: number) => `${n} running`,
      review: (n: number) => `${n} to review`,
      tasks: (n: number) => `${n} ${n === 1 ? "conversation" : "conversations"}`,
      below: (n: number) => `${n} ${n === 1 ? "sub-project" : "sub-projects"}`,
      active: (when: string) => `active ${when}`,
    },
    tab: {
      overview: "Overview",
      context: "Context",
      agents: "Agents",
      schedules: "Schedules",
      channels: "Channels",
    } as Record<string, string>,
    /** A project's Agents tab: what it turns on, and who answers untagged messages. */
    agents: {
      available: "Available here",
      availableHint:
        "Agents that may answer here and below: what this project and those above it enable, the agents that live here, narrowed by every allow list on the way.",
      everyPlatform: "Every Company agent",
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
        "Company agents this project enables for itself and everything below. It adds to what the projects above enable.",
      enableEvery: "Every Company agent",
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
    tasks: "Conversations",
    noTasks:
      "Start a conversation here to keep its execution history and shared context in this project.",
    includeBelow: "Include sub-projects",
    includeBelowHint: "Also list the conversations filed in the projects below this one.",
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
      preview: {
        title: "Turn preview",
        hint: "Saved context for the selected agent and optional channel. Instruction and inherited-note budgets are shared across the chain. Conversation history, retrieval and model limits depend on the turn.",
        agent: "Preview agent",
        channel: "Preview channel",
        submit: "Preview Turn",
        loading: "Loading",
        failed: "The turn preview could not be loaded.",
        available: "Available when named in the message",
        notSent: "Not sent",
        omitted: (n: number) => `${n} omitted`,
        previewOmitted: (n: number) => `${n} shortened in this preview only`,
        usage: (used: number, budget: number, unit: string) =>
          budget > 0
            ? `${used.toLocaleString()} / ${budget.toLocaleString()} ${unit}`
            : `${used.toLocaleString()} ${unit} · no additional cap`,
        kinds: {
          instructions: "Instructions",
          agent_instructions: "Agent instructions",
          channel: "Channel purpose",
          experts: "Experts",
          project_notes: "Project notes",
          ancestor_notes: "Notes from above",
          agent_notes: "Agent notes",
          knowledge: "Project knowledge source names",
          agent_knowledge: "Agent knowledge source names",
          agent_skills: "Default skill names",
          skills: "Skill names",
        },
      },
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
    /** A project's knowledge sources, on its Context tab. */
    knowledge: {
      heading: "Knowledge",
      hint: "Workspace sources every run filed here or below is grounded in, besides its agent's own. At most 8.",
      none: "This project names no sources.",
      /** An owner with nothing to choose: the workspace has no sources yet. */
      noSources: "The workspace has no sources yet. Add one under Context to name it here.",
      add: "Add Source",
      /** Shown once 8 are named. */
      full: "That's 8, the most a project names. Remove one to add another.",
      save: "Save Knowledge",
      saved: "Saved",
      given: "What runs here are grounded in",
      givenHint:
        "This project's sources, then each project's above it, nearest first, each once. Read only. Change a source on the project it comes from.",
      noneGiven: "No project here or above names a source.",
      fromHere: "From this project",
      fromWorkspace: "From the workspace",
      /** A project above, by its title: "From Field engineering". */
      from: (title: string) => `From ${title}`,
      managed: "The platform file sets these sources. They can't be changed here.",
      stale:
        "Someone changed this project since you opened it. Its latest sources are shown now. Make your change again.",
      failed: "The sources could not be saved.",
      readFailed: "The sources could not be read.",
    },
  },
  /** Channels and shared threads: where people and agents work together in a project. */
  threads: {
    posted: (at: string) => `Posted ${at}`,
    /** A channel's title on its page: "#renewals". */
    channel: (name: string) => `#${name}`,
    channels: "Channels",
    channelsLede:
      "Each holds threads. Everyone who can see the project reads a shared one, and only the people in it a private one.",
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
    /** A channel's page, when the project holds no channel by that name. The name wraps in the line under the title. */
    missingChannel: "No such channel",
    missingChannelHint: (name: string) =>
      `This project has no channel named #${name}. Its Channels tab lists the ones it has.`,
    /** A conversation the reader may not read reads as one that does not exist, as a project does. */
    missingTask: "No such conversation",
    /** A conversation that could not be read for a reason other than its not being there. */
    readFailed: "This conversation could not be read. Reload to try again.",
    /** A channel bound to Slack, in its row: which app, and which Slack channel. */
    boundTo: (client: string, slackChannel: string) =>
      `In Slack through ${client}, channel ${slackChannel}`,
    noThreads: "No threads here yet. Start one below.",
    /** Under a room or a channel, for a viewer of its project. */
    viewerOnly:
      "You can view this project. Its members post here; you answer what you're asked from your inbox.",
    moreThreads: "See More",
    /** Where a thread is, in its meta line: "Shared in Globex › #renewals". */
    sharedIn: (project: string, channel: string) => `Shared in ${project} › #${channel}`,
    /** A thread only the people in it read, in its meta line and its row. */
    private: "Private",
    /** The control that starts a private thread instead of a shared one. */
    startPrivate: "Private",
    privateHint: "Only the people in it read a private thread. Tagging a person invites them.",
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
    /** A run's status, to someone its question is not for. */
    waitingOnOthers: "Waiting for an answer",
    /** Under a question, to someone it is not for: whom it is for. */
    questionFor: {
      experts: (topic: string) =>
        topic
          ? `This question is for the ${topic} experts.`
          : "This question is for this project's experts.",
      person: (handle: string) =>
        handle
          ? `This question is for @${handle}.`
          : "This question is for the person this run acts for.",
      answerers: (agent: string) => `This question is for the people who answer for ${agent}.`,
      /** A question for the experts where there are none goes to the owners of the nearest project that names any. project is absent when the reader may not see it, or it is the workspace. */
      owners: (project: string) =>
        project
          ? `This question is for the owners of ${project}.`
          : "This question is for the owners who answer for this project.",
      anyone: "You can read this question, but you can't answer it.",
      nobody: "Nobody can answer this question, because the rules of its run can't be read.",
    },
    /** A thread started in Slack, in its meta line. */
    inSlack: "Also in Slack",
    /** A message posted in Slack, beside when it was posted. */
    fromSlack: "From Slack",
    /** A failed answer to the reader's own message: it is asked again for them from their inbox. */
    retryFromInbox: "Retry it from your inbox, where it answers your message again.",
    /** A failed turn that acted for nobody, such as a machine's from before threads: it is retried for nobody, by anyone who may start runs. */
    retryNobodyFromInbox:
      "This turn acted for nobody, so anyone who may start runs can retry it from the inbox.",
    answer: "Answer",
    answered: "Answered",
    typeahead: "People, agents and skills to tag",
    /** A mention kind, as completion offers `@kind:`: its title, or what it is. */
    mentionKind: (kind: string, title?: string) => title || `Mention by ${kind}`,
    /** The chat bar's prompt in a conversation. Nothing runs until a message tags an agent. */
    message: "Message. Tag an agent with @ to ask it",
    /** The control at the top of a private conversation that shares it in a project's channel. */
    share: "Share",
    shareTitle: "Share this conversation",
    placementLoading: "Reviewing the destination audience…",
    placementFailed: "The destination could not be reviewed. Try again.",
    placementRetry: "Review Destination",
    placementReview: "I reviewed the destination audience and everything being published.",
    placementContents: (audience: string, runs: number) =>
      `${audience}. ${runs} recorded ${runs === 1 ? "turn" : "turns"}.`,
    publicationSummary: "Reviewed Summary",
    publicationSummaryHint:
      "Write only the information this audience may read. The original conversation stays private; files, private source history and pending actions are not copied.",
    publishSummary: "Publish Summary",
    placementChanged:
      "The conversation or destination changed. Review the updated audience before publishing.",
    shareLede:
      "Everyone who can see the project reads it in the channel. Each person you add is told in their inbox; someone who can't see the project isn't added. Selected agents that can't answer there are taken off.",
    shareProject: "Project",
    /** Under the project, once an agent has run in the conversation. */
    shareProjectFixed: "The project can't change once an agent has answered here.",
    shareChannel: "Channel",
    shareAdd: "People to add",
    shareAddHint: "Type a name or a handle",
    /** Once shared: who was told, and who the share named but did not add, each with the platform's reason. */
    sharedTold: (handles: string[]) =>
      handles.length === 0
        ? "Shared."
        : `Shared. ${handles.map((h) => `@${h}`).join(", ")} ${handles.length === 1 ? "was" : "were"} told in their inbox.`,
    /** People a share named but did not add, by why: they can't read the project, or for another reason. */
    notAdded: (cannotRead: string[], other: string[]) =>
      [
        cannotRead.length > 0
          ? `${cannotRead.map((h) => `@${h}`).join(", ")} can't see this project, so ${cannotRead.length === 1 ? "wasn't" : "weren't"} added.`
          : "",
        other.length > 0 ? `${other.map((h) => `@${h}`).join(", ")} couldn't be added.` : "",
      ]
        .filter(Boolean)
        .join(" "),
    /** The people picked to add, as a list. */
    shareAdding: "Adding",
    shareRemove: (handle: string) => `Remove @${handle}`,
    /** The server would not move it to another project: an agent has already run in it. */
    shareConflict:
      "An agent has already answered in this conversation, so it stays in its project. Share it there, in any of its channels.",
    shareFailed: "The conversation could not be shared. Try again.",
    /** The server would not move it to another channel: an agent has already run in it. */
    shareChannelFixed:
      "An agent has already answered in this conversation, so it stays in its channel.",
    /** Someone else changed the conversation since the page read it. */
    changedMeanwhile:
      "Someone changed this conversation just now. It shows as it is now; try again.",
    experts: (topic: string) => (topic ? `Experts on ${topic}` : "Every expert here"),
    olderTurns: "Show Earlier",
    failed: {
      post: "The message could not be posted. Try again.",
      channel: "The channel could not be created.",
    },
    /** In a Slack channel bound to a project, to someone the app maps to nobody in humagen. */
    unmappedInBound:
      "This channel is a project's channel in Humagen, and only people Humagen knows post in it. Ask an admin to connect your Slack account.",
    /** In a Slack channel bound to a project, to someone asking about another person's message. */
    notTheirsInBound:
      "This channel is a project's channel in Humagen, where a message is asked only as its writer's own. Ask in the thread in your own words.",
  },
};

export type Copy = typeof defaultCopy;
