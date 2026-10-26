# Integrated local browser validation

This is incremental evidence, not full acceptance of both design sets.

## Persistent preview

The isolated preview uses loopback port 8081, a dedicated Postgres database and disk blob/workspace stores. The original 8080 preview remains available independently. The executable and console bundle come from an immutable integration snapshot.

At snapshot `68e0900` plus Company address fix `46d6b3e`, a real browser verified:

| Flow | Evidence | Result |
| --- | --- | --- |
| Profile save | Display name and briefing preference saved through the console | Passed |
| Server restart | Both preferences, conversation records and seed artifacts survived a native service restart | Passed |
| Personal Home | Personal label rendered before project-list hydration; the announcement appeared above the greeting | Passed |
| Start private work | Browser sent a first message to humagen-analyst in the canonical personal project returned by `/api/me` | Passed after the address regression fix |
| Real provider | Run `r759f806786a384be914455cd22ffed71` completed through Claude Haiku with one recorded model call and response `HUMAGEN_UI_VALIDATED` | Passed |
| Usage | Selected UTC day, chart and semantic table displayed actual bounded server data | Passed |

The provider is configured through an environment credential reference. The existing local direnv provider environment is reused; no credential value is checked in or included in this report. The earlier 1Password credential-resolution failure remains in its conversation as actual history.

## Regressions discovered by browser and CI checks

- `/api/me` prefixed the personal project name with an unsupported tilde. The exact returned name now works unchanged through agent discovery and private thread creation on memory and Postgres.
- Explicit Company agent homes disappeared from inherited agent pickers. Availability now normalizes the root address and preserves narrower project restrictions.
- Failed posts cleared the only copy of a message. Rejected or uncertain posts are retained in an editable held queue for explicit retry, without retrying automatically or replacing a newer draft.
- The screenshot seed attempted to publish completed private work without the current audience-preview token. The seed now uses the same preview/confirmation contract as the UI.

The final integrated wizard, source/integration controls, complete responsive screen suite, refreshed showcase and stack-wide CI remain separate acceptance gates.

## Inactive setup and native file checkpoint

The setup form now uses real registered providers and saved model choices, an explicit personal home before project hydration, bounded source/tool selections, recorded reviewer configuration and native approval rendering. Saving creates an inactive durable setup. Correction invalidates prior test capability before changing the specification; activation requires a completed actual model call. Starter assignments remain inactive until activation. Memory/Postgres race tests cover partial-write recovery, real registry restart, exact owner/revision test capability and explicit self-approval without overriding stricter native rules.

Validation for this checkpoint: 215 console DOM tests, setup/native platform race tests, API response-contract and read-confinement sweeps, console typecheck, and OpenAPI lint pass with no skipped tests. Full integrated acceptance and refreshed showcase captures remain pending.

Native DOCX tables now occupy actual Word XML and preview cells. Native rendering rejects meaningful unsupported fields instead of silently dropping them. Structured revision reading, text extraction and PDF export validate actual authored parts against retained metadata; original downloads remain unchanged. Model instructions require successful writes before dependent reads or publication.
