# Humagen agentic workspace and control plane design

A design for taking the conversation workspace (#64) to persistent, delegated
agent work: agents with roles and standing assignments, a control plane to
oversee them, visible context provenance, skills and memory, initiatives,
forking, and integrations with GitHub, Slack, Drive and Notion.

This is a prepared design. Nothing here is implemented beyond what
[gap-analysis.md](../../reference/control-plane-gap-analysis.md) marks as built.

- [Product and design spec](../../rfcs/0001-agentic-workspace-control-plane.md)
- [Gap analysis against the code](../../reference/control-plane-gap-analysis.md)
- [Design rules: tokens, identity, icons, navigation](../../reference/board-design-rules.md)
- [Boards](boards/index.html): 25 clickable screens as plain HTML, CSS and JavaScript,
  published at https://urmzd.github.io/humagen/

## Opening the boards

Each board is a standalone page in `boards/`. It loads two shared files and
nothing else from this repository:

- `boards/assets/boards.css`: the design tokens and base styles.
- `boards/assets/boards.js`: a small renderer (no dependencies) that fills the
  board's template from its `Component` class and re-renders on `setState`, so
  toggles, filters, steppers and links between boards work.

Open `boards/index.html` in a browser, or serve the folder:

```sh
python3 -m http.server 8000 --directory docs/assets/control-plane/boards
```

GitHub shows HTML files as source. The `Design boards` workflow
(`.github/workflows/pages.yml`) publishes this folder to GitHub Pages at
https://urmzd.github.io/humagen/ whenever it changes on `main`; it needs
Settings, Pages, Source set to GitHub Actions. The screenshots below are
captured from these pages.

## Where the boards differ from the repo's rules

The boards draw agents as initials in rounded squares and composer controls as
pills. AGENTS.md requires `AgentMark` colours for agents and text controls under
the composer. Implementations follow AGENTS.md; see the "Diverges on purpose"
table in the gap analysis.

## Screens

<table>
<tr><td width="50%"><a href="boards/Main.html"><img src="boards/screens/01-Main.png" alt="Project conversation: outcome first"></a><br><sub>Project conversation: outcome first</sub></td><td width="50%"><a href="boards/MoveConversation.html"><img src="boards/screens/02-MoveConversation.png" alt="Add a private conversation to a project"></a><br><sub>Add a private conversation to a project</sub></td></tr>
<tr><td width="50%"><a href="boards/Activity.html"><img src="boards/screens/03-Activity.png" alt="Settings › Activity"></a><br><sub>Settings › Activity</sub></td><td width="50%"><a href="boards/Context.html"><img src="boards/screens/04-Context.png" alt="Context with provenance"></a><br><sub>Context with provenance</sub></td></tr>
<tr><td width="50%"><a href="boards/OpsAgents.html"><img src="boards/screens/05-OpsAgents.png" alt="Operations › Agents (control plane)"></a><br><sub>Operations › Agents (control plane)</sub></td><td width="50%"><a href="boards/AgentDetail.html"><img src="boards/screens/06-AgentDetail.png" alt="Agent › Launch Planner"></a><br><sub>Agent › Launch Planner</sub></td></tr>
<tr><td width="50%"><a href="boards/OpsContext.html"><img src="boards/screens/07-OpsContext.png" alt="Operations › Context health"></a><br><sub>Operations › Context health</sub></td><td width="50%"><a href="boards/Icons.html"><img src="boards/screens/08-Icons.png" alt="Identity & icons"></a><br><sub>Identity & icons</sub></td></tr>
<tr><td width="50%"><a href="boards/FlowMap.html"><img src="boards/screens/09-FlowMap.png" alt="How it fits together"></a><br><sub>How it fits together</sub></td><td width="50%"><a href="boards/AgentComms.html"><img src="boards/screens/10-AgentComms.png" alt="Operations › Agent network"></a><br><sub>Operations › Agent network</sub></td></tr>
<tr><td width="50%"><a href="boards/CreateAgent.html"><img src="boards/screens/11-CreateAgent.png" alt="Create an agent"></a><br><sub>Create an agent</sub></td><td width="50%"><a href="boards/Skills.html"><img src="boards/screens/12-Skills.png" alt="Library › Skills"></a><br><sub>Library › Skills</sub></td></tr>
<tr><td width="50%"><a href="boards/Memory.html"><img src="boards/screens/13-Memory.png" alt="Library › Memory"></a><br><sub>Library › Memory</sub></td><td width="50%"><a href="boards/Policies.html"><img src="boards/screens/14-Policies.png" alt="Operations › Policies"></a><br><sub>Operations › Policies</sub></td></tr>
<tr><td width="50%"><a href="boards/Usage.html"><img src="boards/screens/15-Usage.png" alt="Operations › Usage"></a><br><sub>Operations › Usage</sub></td><td width="50%"><a href="boards/AuditLog.html"><img src="boards/screens/16-AuditLog.png" alt="Operations › Audit log"></a><br><sub>Operations › Audit log</sub></td></tr>
<tr><td width="50%"><a href="boards/UserSettings.html"><img src="boards/screens/17-UserSettings.png" alt="You · settings"></a><br><sub>You · settings</sub></td><td width="50%"><a href="boards/Projects.html"><img src="boards/screens/18-Projects.png" alt="All projects · hierarchy"></a><br><sub>All projects · hierarchy</sub></td></tr>
<tr><td width="50%"><a href="boards/Initiative.html"><img src="boards/screens/19-Initiative.png" alt="Initiative · Spring 2027 GTM"></a><br><sub>Initiative · Spring 2027 GTM</sub></td><td width="50%"><a href="boards/ForkProject.html"><img src="boards/screens/20-ForkProject.png" alt="Fork a project"></a><br><sub>Fork a project</sub></td></tr>
<tr><td width="50%"><a href="boards/ForkConversation.html"><img src="boards/screens/21-ForkConversation.png" alt="Fork a conversation"></a><br><sub>Fork a conversation</sub></td><td width="50%"><a href="boards/Integrations.html"><img src="boards/screens/22-Integrations.png" alt="Operations › Integrations"></a><br><sub>Operations › Integrations</sub></td></tr>
<tr><td width="50%"><a href="boards/LinkRules.html"><img src="boards/screens/23-LinkRules.png" alt="Auto-link rules · GitHub"></a><br><sub>Auto-link rules · GitHub</sub></td><td width="50%"><a href="boards/SlackBridge.html"><img src="boards/screens/24-SlackBridge.png" alt="Slack bridge"></a><br><sub>Slack bridge</sub></td></tr>
<tr><td width="50%"><a href="boards/AddSource.html"><img src="boards/screens/25-AddSource.png" alt="Add context · indexed, live, hybrid"></a><br><sub>Add context · indexed, live, hybrid</sub></td></tr>
</table>
