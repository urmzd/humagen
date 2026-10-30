# Humagen design rules for every board

The boards these rules governed, and their GitHub Pages site, were removed on
October 30, 2026. The rules stay as the design record that RFC 0001 and the gap
analysis refer to.

## Format
- Each board = one `.html` file in `boards/`: `assets/boards.css` and `assets/boards.js` in the head, the markup in `<template data-board>` (with a `<helmet>` for fonts and page basics), and one `<script type="text/x-dc" data-props='{...}'>` holding `class Component extends DCLogic` with `renderVals()`. `boards.js` renders the template and re-renders on `setState`.
- Holes `{{x}}` are dotted lookups only. Computed styles/labels come from renderVals. Lists via `<sc-for list="{{ items }}" as="it" hint-placeholder-count="N">`, branches via `<sc-if value="{{ flag }}" hint-placeholder-val="{{ true }}">`. Events: `onClick="{{ fn }}"`.
- Close every element, quote every attribute. No emoji, no gradients-as-decoration, no left-border cards, no iframes, no innerHTML.
- Real `<button>`, `<a href>`, `<input>`+`<label>`. `aria-label` on icon-only buttons. Decorative svg `aria-hidden="true"`.
- PAGE artboards (app screens): fluid root `width: 100%; min-height: <H>px; display:flex; flex-wrap:wrap`, nav `flex: 1 1 220px`, main `flex: 999 1 640px; min-width:0`. Wide tables in `overflow-x:auto` box. Must work at phone width.
- Links between artboards: `<a href="Other.html">`.
- Write copy as literal markup unless it is genuinely data. No lorem ipsum. Use the Spring Launch story: Supplier ETA changed (packaging Nov 4 → Nov 8), Launch Planner recommends moving launch Nov 12 → Nov 19, Ops Agent holds 4 vendor notices pending approval, Media Analyst failed because Media buy tracker access expired, Research Analyst works on a private Pricing memo draft.
- People: Avery Park (AP, the viewer, "You"), Mira K. (MK, launch lead), Jon T. (JT, ops), Sam R. (SR, marketing). Agents: Launch Planner (LP), Ops Agent (OA), Monitor Agent (MA), Research Analyst (RA), Media Analyst (MD), Onboarding Guide (OB).

## Tokens
- Font: Inter 400/500/600/700. Body 14px/1.45. Labels: 11px 600 uppercase letter-spacing .06em #5F6168. H1 22px 600. Section h2 13px 600 uppercase .04em #5F6168.
- Ground #F6F6F4 · Nav #EFEFEC · Surface #FFFFFF · Subtle surface #FAFAF8 · Line #E2E2DE · Inner line #F0F0ED · Control border #D5D5D0
- Ink #1B1C1E · Ink-2 #3A3C41 · Muted #5F6168 (min for any text)
- Accent #2E5B88 (hover #1F4166), accent tint #E3E8EE, selected row #F3F6FA
- Status pills (12px 600, radius 999, padding 2px 8px): ok #E6F2EC/#1F6B45 dot #1F7A4D · working #E3E8EE/#2E5B88 · waiting/attention #F4E6CC/#6B4100 dot #B86E00 (panel #FBF3E4 border #EED9B3) · error #F7E3DF/#8C2F22 dot #A33A2B · neutral #EEEEEB/#3A3C41 dot #8A8C92
- Radii: cards 12, inputs/buttons 8, chips 999, modal 16. Buttons 36–40px tall. Primary button #1B1C1E bg white text; secondary white bg 1px #D5D5D0. Touch targets ≥ 32px in dense tables, 36+ elsewhere.
- Cards: white, 1px #E2E2DE, radius 12. Stat tiles: label 12px muted + value 22px 600.

## Object identity (always the same everywhere)
Each object type has ONE shape + ONE icon + ONE tint. Tints are for the badge/avatar only, never big fills.
- Person: CIRCLE avatar, initials 600, warm tints (#D9C7B4, #BFD3C6, #C9C3DD, #E2CFC0), ink text.
- Agent: ROUNDED SQUARE (radius = 25% of size), initials 700, bg #E3E8EE text #2E5B88. Type icon: bot.
- Skill: rounded square badge bg #ECE6F4 fg #563A80, icon: wrench.
- Memory: rounded square badge bg #E0EFEC fg #1E5A52, icon: bookmark.
- Source (context): rounded square badge bg #EEEEEB fg #3A3C41, icon: database.
- Decision: icon check-circle, uses accent.
- Handoff: icon arrow-right in a 26px circle bg #F1ECE4 fg #6B4E2E.
- Project: folder icon. Channel: hash icon. Conversation: message icon.

## Scope (who it belongs to) - a small neutral chip with icon + word, always
`display:inline-flex;gap:4px;font-size:11px;font-weight:600;padding:2px 7px;border-radius:6px;border:1px solid #E2E2DE;color:#3A3C41;background:#FFFFFF`
- Workspace (icon: layout-grid) · Project (icon: folder) + project name · Personal (icon: user) "Only you" · Agent (icon: bot) + agent name.
Inheritance order: Workspace → Project → Agent → Personal (narrower wins only within what the wider allows; a workspace "Never" can't be loosened lower down).

## Icons - stroke set, 24 viewBox, fill none, stroke currentColor, stroke-width 1.75, round caps & joins. Sizes 16 (inline/dense), 18 (buttons), 20 (nav). Copy these paths EXACTLY:
- search: <circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/>
- plus: <path d="M12 5v14M5 12h14"/>
- close: <path d="M6 6l12 12M18 6L6 18"/>
- chevron-down: <path d="M6 9l6 6 6-6"/>  chevron-right: <path d="M9 6l6 6-6 6"/>
- check: <path d="M5 12l5 5L20 7"/>
- check-circle (decision): <circle cx="12" cy="12" r="9"/><path d="M8 12.5l2.5 2.5L16 9.5"/>
- alert (attention): <path d="M12 3l9.5 17h-19z"/><path d="M12 10v4M12 17.5v.5"/>
- folder (project): <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>
- hash (channel): <path d="M5 9h14M5 15h14M10 3L8 21M16 3l-2 18"/>
- message (conversation): <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z"/>
- bot (agent): <rect x="4" y="7" width="16" height="12" rx="3"/><path d="M12 7V4M9 12v1M15 12v1"/>
- user (person/personal): <circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>
- users (people/members): <circle cx="9" cy="8" r="3.5"/><path d="M2.5 20a6.5 6.5 0 0 1 13 0"/><path d="M16 4.5a3.5 3.5 0 0 1 0 7M18 14a6 6 0 0 1 3.5 6"/>
- wrench (skill): <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18l3 3 6.3-6.3a4 4 0 0 0 5.4-5.4l-2.5 2.5-2.4-.6-.6-2.4z"/>
- bookmark (memory): <path d="M6 3h12v18l-6-4-6 4z"/>
- database (source/context): <ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>
- arrow-right (handoff): <path d="M5 12h14M13 6l6 6-6 6"/>
- pulse (activity): <path d="M3 12h4l3-8 4 16 3-8h4"/>
- sliders (settings): <path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1"/><circle cx="15" cy="6" r="2"/><circle cx="9" cy="12" r="2"/><circle cx="17" cy="18" r="2"/>
- shield (policy): <path d="M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6z"/>
- bar-chart (usage): <path d="M3 21h18M6 17v-5M11 17V7M16 17v-8M21 17V4"/>
- list (audit log): <path d="M9 6h11M9 12h11M9 18h11"/><path d="M4.5 6h.01M4.5 12h.01M4.5 18h.01"/>
- inbox (needs you): <path d="M3 13l3-8h12l3 8v6H3z"/><path d="M3 13h5l1 2h6l1-2h5"/>
- layout-grid (workspace): <rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>
- refresh (sync): <path d="M20 12a8 8 0 1 1-2.3-5.7L20 8"/><path d="M20 3v5h-5"/>
- camera (snapshot): <rect x="3" y="7" width="18" height="13" rx="2"/><circle cx="12" cy="13.5" r="3.5"/><path d="M8.5 7l1.5-3h4l1.5 3"/>
- live (radio): <circle cx="12" cy="12" r="2"/><path d="M7.8 7.8a6 6 0 0 0 0 8.4M16.2 7.8a6 6 0 0 1 0 8.4M4.9 4.9a10 10 0 0 0 0 14.2M19.1 4.9a10 10 0 0 1 0 14.2"/>
- pause: <path d="M9 5v14M15 5v14"/>  play: <path d="M7 4l13 8-13 8z"/>
- lock (private): <rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>
- eye (visible to): <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>
- clock (schedule): <circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>
- bell (notifications): <path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4z"/><path d="M10 21h4"/>
- link (connected account): <path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>
- doc (deliverable): <path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8z"/><path d="M14 3v5h5"/>
- attach: <path d="M21 11.5l-8.6 8.6a5 5 0 0 1-7-7l8.6-8.6a3.3 3.3 0 0 1 4.7 4.7L10 17.8a1.7 1.7 0 0 1-2.4-2.4l8-8"/>
- at (mention): <circle cx="12" cy="12" r="4"/><path d="M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8"/>
- send: <path d="M12 19V5M5 12l7-7 7 7"/>
- more: <circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>
(Note: in board markup write every svg child with an explicit close tag, e.g. <path d="..."></path>, never self-closing.)

## Canonical workspace nav (used by every non-conversation page)
Left nav `flex: 1 1 220px; background:#EFEFEC; border-right:1px solid #E2E2DE; padding:16px 12px; gap:18px; flex column`:
1. Workspace row: 28px black rounded "H" mark + "Humagen" 15px 600.
2. Group "Projects" (folder icons): Spring Launch (→ Main.html), Q4 Research, Onboarding Revamp.
3. Group "Library": Skills (wrench, → Skills.html), Memory (bookmark, → Memory.html).
4. Group "Operations": Needs you (inbox, blue count pill 3), Agents (bot, → OpsAgents.html), Agent network (arrow-right, → AgentComms.html), Context health (database, amber dot, → OpsContext.html), Policies (shield, → Policies.html), Usage (bar-chart, → Usage.html), Audit log (list, → AuditLog.html).
5. Bottom (margin-top:auto): user row link → UserSettings.html: 28px circle avatar "AP" (#C9C3DD) + "Avery Park" + muted "Your settings".
Nav items: `display:flex;align-items:center;gap:10px;padding:8px;border-radius:6px;color:#1B1C1E;text-decoration:none`, icon 18px color #5F6168; current item: background #FFFFFF, font-weight 600, aria-current="page".

## Added: hierarchy, integrations, forking
Model:
- Hierarchy: Workspace → Initiative (optional parent, e.g. "Spring 2027 Go-to-market", icon flag) → Project → Channel → Subchannel → Conversation. Projects may also have sub-projects (child projects).
- Parents pass DOWN: policies (tighten-only), shared context sources, skills, memory (children read parent memory; children promote up), initiative-level agents (e.g. "Portfolio Monitor" operating across child projects). Members optional-inherit.
- Children roll UP: status, decisions, Needs-you items, usage, risks.
- Integrations (GitHub, Slack, Google Drive, Notion): Connection (workspace-level auth) → Resources (repos, channels, folders, databases) → Links to projects (manual, suggested, or rule-based auto-link) → Access mode (Indexed / Live search / Hybrid) → Events (triggers for agents: PR opened, issue labeled, message in bridged Slack channel, page edited) → Actions (comment on PR, post to Slack; governed by policy "Message people outside the project").
- Auto project adding: link modes per integration = Suggest (default) · Auto-link by rule · Off. Rules e.g. "repo topic `spring-launch` → Spring Launch", "Slack channels starting #launch- → Spring Launch", "Notion pages under Launch HQ". Suggestions queue shows WHY (shared contributors, matching keywords) with Link / Ignore.
- External objects never use brand logos: show a neutral 32px rounded-square badge with a 2-letter monogram (GH, SL, GD, NO) in #EEEEEB/#3A3C41 plus the system name in text. Linked objects carry a "via GitHub" provenance chip.
- Context access modes (chip on every source): Indexed ("Indexed · synced 9:42": copied and indexed, has sync lifecycle; best for Drive folders) · Live search ("Live search · queried 9:51": no copy, queried at question time with the asker's permissions; always current, slower, unavailable if source down; best for a Notion workspace) · Hybrid ("Hybrid · 24 pages indexed, rest live": pinned key pages indexed + live search for the rest).
- Forking: Conversation fork = "Fork from here" on any message → new conversation with history up to that message as read-only context, chosen agents and sources, lineage chip "Forked from <title> at 9:51"; "Bring back" posts the fork's outcome card into the parent. Project fork = new project from an existing one: copies structure (channels), agent configs, skills (linked or copied), policies, memory (optional snapshot), sources (live re-linked, snapshots copied); conversations NOT copied by default; lineage "Forked from Spring Launch · Oct 23".
Extra icons (same stroke rules):
- flag (initiative): <path d="M5 21V4"/><path d="M5 4h11l-2 4 2 4H5"/>
- fork: <circle cx="6" cy="5" r="2"/><circle cx="18" cy="5" r="2"/><circle cx="12" cy="19" r="2"/><path d="M6 7v2a3 3 0 0 0 3 3h6a3 3 0 0 0 3-3V7M12 12v5"/>
- plug (integration): <path d="M9 3v5M15 3v5"/><path d="M6 8h12v3a6 6 0 0 1-12 0z"/><path d="M12 17v4"/>
- tree (hierarchy): <rect x="3" y="3" width="6" height="5" rx="1"/><rect x="15" y="10" width="6" height="5" rx="1"/><rect x="15" y="17" width="6" height="5" rx="1"/><path d="M6 8v11h9M6 12.5h9"/>
- sparkle-free "rule" (auto-link rule): <path d="M4 7h10M4 12h16M4 17h7"/><circle cx="18" cy="7" r="2"/><circle cx="15" cy="17" r="2"/>
Workspace nav addition: under Operations add "Integrations" (plug, → Integrations.html) after Context health; under Projects add a first item "All projects" (tree, → Projects.html) and show "Spring 2027 GTM" initiative (flag, → Initiative.html) with Spring Launch and Q4 Research nested under it.
