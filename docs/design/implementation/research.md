# Resolved implementation constraints

Both checked-in design sets are the user's approved target. Their prototype
state and downloads are design evidence, not execution records.

- Project constitution is AGENTS.md. Preserve the existing Go composition,
  React framework, Page layout, AgentMark identity, and project typography.
- Default-deny remains at the capability boundary. An absent tightening action
  rule is unspecified, not a new grant. A child Allow cannot override Ask/Never.
- Personal projects are exact-owner boundaries. Exceptional governance reads
  remain separately authorized and recorded. No inherited owner bypass.
- Current conversation audience is separate from immutable run accounting.
  Publication must be explicit and race-safe; no cross-store best-effort rewrite.
- Unconfigured managers, budgets and external identities do not invent grants.
  Existing policy caps and authorized owner/manager changes remain in force.
- Unmatched external guests cannot approve. Cross-project handoffs require all
  existing call grants, readable context and destination access.
- Source expiry and retention are explicit settings. Do not silently adopt a
  destructive snapshot expiry because a design open question proposes 30 days.
- New OAuth registrations and live third-party installations require actual
  user setup and approved scopes. Missing credentials are a visible state;
  mock/provider tests are never labelled live successful delivery.
- Domain audit begins with a durable pending attempt after access checks and
  records success/failure afterward. An unknown completion stays pending.
- Stacked PRs use normal no-ff dependency merges, without rebasing, resetting or
  force-pushing active worker branches. Root owns integration and publication.
