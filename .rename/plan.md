# Rename warp to Humagen: the plan

One PR, one engineer or one cloud session. Repo `urmzd/warp` becomes `urmzd/humagen`, and the Go module becomes `github.com/urmzd/humagen`. The display name is "Humagen". Everything else is lowercase `humagen`.

## 0. Where to start, and the three rules behind each decision

- Branch `rename/humagen` from `main` **after the chat v2 PR merges** (task #37). Every line number below is pinned to `60e527c` (v0.18.0-134). Main is now at v0.19.0. Before the bulk pass, re-pin the counts with the commands in step 3.0.
- Every other branch that touches Go imports or `@warp/*` should merge first. After this PR, a branch catches up by running the same rename script (step 3.4) and then rebasing. Do not merge the rename into it by hand.
- Each name falls into one of three tiers:
  1. **Names people type or read** get the new name. The old one is still read, and using it prints a note at start: env vars, the config file, spec fields, `api_version`, scopes, default groups, the CLI name and the review format id.
  2. **Names that stored data or a running deployment depends on** stay as they are, and a test pins each one: the Postgres table, the lock, tag meta keys, the built-in agent handles, the mutation MAC domain, container data paths, compose and Flux identities, and Helm resource names for existing releases. Renaming these buys nothing a person sees, and each rename would need a migration that has no rollback.
  3. **Names nothing stores** change outright with no compat code: cookies (sessions live in memory, `internal/httpapi/session.go:47`), mock IdP names, `WARP_REF_n`, CSS names, TS identifiers and copy.
- How this follows AGENTS.md:
  - There is no `cmd/warp` shim and no second chart directory, so nothing is forked.
  - Old names are read in one place: `internal/legacy` for Go, and `apps/slack/src/legacy.ts` and `apps/web/src/legacy.ts` for TS.
  - Ambiguous states fail closed: two spec files, two Slack credential fields, or a chart upgrade over a warp release without `nameOverride`.
  - `CHANGELOG.md` is never edited.
  - The API changes through `api/openapi.yaml` and then `make client`.

## 1. Scope

Counts were taken at `60e527c` with `git grep -I -i -c warp`, excluding CHANGELOG.md and pnpm-lock.yaml: **4,426 lines in 579 files**.

### 1a. Renamed (old name keeps working where marked "compat")

| Category | Old | New | Files and counts | Compat |
|---|---|---|---|---|
| GitHub repo | `urmzd/warp` | `urmzd/humagen` | GitHub setting. 12 link lines (13 occurrences) in README.md:7,14,15,55, SECURITY.md:6, apps/web/src/AccessExplain.tsx:63, deploy/flux/clusters/example/warp.yaml:14, deploy/helm/warp/Chart.yaml:9,11, examples/github-review/app-manifest.json:3, docs/spec.md:557 | GitHub redirect. Never recreate `urmzd/warp` |
| Go module | `github.com/urmzd/warp` | `github.com/urmzd/humagen` | go.mod:1. 893 lines in 270 files: 868 import lines in 268 files, plus internal/release/release.go:15, internal/httpapi/openapi_test.go:257,266, examples/github-review/main.go:9 | None possible (Go has no alias). Old tags resolve through the proxy and the redirect |
| Root package | `package warp`, `warp.go`, `warp.Main`, `warp.With*` | `package humagen`, `humagen.go`, `humagen.Main` | warp.go (16 lines), changelog.go, changelog_test.go, cmd/warp/main.go:10,21, AGENTS.md:11,44,45 | Breaking for importers (CHANGELOG breaking footer) |
| Command | `cmd/warp`, binary `warp` | `cmd/humagen`, `humagen` | cmd/warp/main.go, Makefile:3,27,44,48,54,81,82, Dockerfile:58,69,85, deploy/helm/check.sh:22, apps/slack/src/warp.server.test.ts:41-43, README.md:71, docs (spec, ecosystem, integrations) | The image ships a `/usr/local/bin/warp` symlink. Running it as `warp` prints a note |
| CLI copy | `warp:` prefix, usage text | `humagen:` | About 40 non-test lines in 12 files: internal/app/main.go:74-92,225, start.go:78, authn.go:269, config.go:712,780, slack.go:116,261, setup.go:61-205, deploy.go (14 lines), release.go:120, httpapi/tailscale.go:72, pkg/spec/projects.go:249, pkg/projects/kinds.go:42. About 20 test assertions | None |
| Env vars | `WARP_*` | `HUMAGEN_*` | 486 lines in 76 files. 34 variables the server reads, 9 it writes for the Slack app, 3 prefix families (`env:WARP_` refs, the `WARP_OIDC` exclusion, `WARP_PERSON_{subject}_`), 6 test-only variables, 5 compose interpolations, `WARP_API` (vite), `WARP_SERVER_TEST` | compat (2.2) |
| Config file | `$WARP_HOME` or `os.UserConfigDir()/warp/warp.yaml` | `$HUMAGEN_HOME` or `.../humagen/humagen.yaml` | internal/app/start.go:15-26,60, setup.go:22,27, config.go:787-793. About 40 test lines in 13 files | compat (2.3) |
| Spec | `api_version: warp/v1`, `surfaces.slack.warp_client` and `warp_token` | `humagen/v1`, `humagen_client`, `humagen_token` | pkg/spec/spec.go:39,324-325,365, platform.go:467,471,496,602-610, chart _helpers.tpl:57,63, deploy/compose/config/warp.yaml:5,48, dev/warp.yaml:4,28. About 45 test lines | compat (2.4) |
| Go identifiers | `WarpClient`, `WarpToken`, the `warp` local var in deploy.go, 4 test names | `HumagenClient`, `HumagenToken`, `svc` | 39 identifier tokens (28 in 8 non-test files, 11 in 7 test files) | None |
| Auth names | scope prefix `warp:`, groups `warp-admins` / `-managers` / `-operators` / `-viewers`, client ids `warp` and `warp-slack`, mock `warp-dev` and `api://warp-dev` | `humagen:`, `humagen-*`, `humagen`, `humagen-slack`, `humagen-dev`, `api://humagen-dev` | pkg/auth/oidc.go:17,187, policy.go:141, internal/app/authn.go:28,39-42,52, setup.go:71,77,173, mockidp.go:42,69,112-115,124, config.go:779-781. 84 group test lines in 24 files, 80 scope test lines in 27 files | compat for the scope and groups (2.7). Setup and mock values are prefills only |
| Cookies | `warp_session`, `warp_login` (and `__Host-` forms) | `humagen_session`, `humagen_login` | internal/httpapi/session.go:148,192,211,212,244,262,267, api/openapi.yaml:123,143,4368,4371 | None. Sessions live in memory, so every restart already signs everyone out |
| Review format id | `warp.github.review.v1` | `humagen.github.review.v1` | internal/presentation/github/review.go:14,53,140-141, pkg/integrations/github/github.go:187,196, presentation.go:14, examples/github-review/{main.go:14,agent.json:6,fixer.json:6}, checks_test.go:54-55 | Both accepted. The new one is advertised |
| System sender | `{system, warp}`, "What's new in warp X" | `{system, humagen}`, "What's new in Humagen X" | internal/platform/announce.go:74, internal/release/release.go:120, pkg/threads/transcript.go:241. 8 sender and 13 subject test lines | Old records keep `warp`. The console and transcript show every `system` sender as "Humagen" |
| npm scope | `@warp/*` (14 packages), root `"name": "warp"` | `@humagen/*`, `humagen` | 427 lines in 180 files: 15 package.json names, 41 `workspace:*` deps, 320 import specifiers in 148 files, 2 CSS `@import`, packages/render-slack/src/reconciler.ts:48, 5 root scripts. Build glue: Makefile:43,59,96,97, Dockerfile:35, ci.yml:72,73, internal/app/main.go:225. pnpm-lock.yaml (41, regenerated) | None. All 15 packages are private and unpublished |
| TS identifiers | apps/slack/src/warp.ts (`Warp`, `WarpError` ×51, `httpWarp` ×16, `FakeWarp` ×5, `this.warp` ×50, `w.warp` ×458, `config.warp`); packages/client/src/warp.ts (`Warp`, `WarpOptions`) | `humagen.ts`, `Humagen`, `HumagenError`, `httpHumagen`, `FakeHumagen`, `this.humagen`, `w.humagen`, `config.humagen`, `HumagenOptions` | About 650 sites in apps/slack, 9 in packages/client | None (internal) |
| CSS | `--warp-*` (25 names), `.warp-*` classes and keyframes (27 names) | `--humagen-*`, `humagen-*` | About 340 occurrences in about 24 files (styles.css 204). Asserted in ui.test.tsx (13), layout.dom.test.tsx (7), agents.test.tsx:103, home.test.tsx:38 | None |
| Browser storage | localStorage `warp.sidebar` | `humagen.sidebar` | apps/web/src/App.tsx:93,102,181 | Moved once (2.8) |
| UI copy | "Warp" | "Humagen" | apps/web/index.html:13,16, App.tsx:74, Sidebar.tsx:351, Connections.tsx:225, connections.ts:22-23, public/manifest.webmanifest:2-3 (name and short_name only, keep `start_url` and `scope`), packages/components/src/copy.ts:908,911, packages/core/src/fixtures.ts:1477,1719, render-slack snapshot (16 lines), apps/slack/src/app.tsx:318-319,924-1154,2256 | None |
| Slack app env | `WARP_URL`, `_TENANT`, `_TOKEN`, `_PUBLIC_URL`, `_CLIENT`, `_DEFAULT_AGENT`, `_OIDC_TOKEN_URL`, `_OIDC_CLIENT_ID`, `_OIDC_CLIENT_SECRET`, `_OIDC_SCOPES`, `_OIDC_AUDIENCE` | `HUMAGEN_*` | apps/slack/src/config.ts:67-112, main.ts:17, internal/app/slack.go:230-249, apps/slack/.envrc.local.example:22-38, README (34 lines) | compat in TS (2.12) |
| Slack manifests | "Warp", "Ask Warp", bot `warp` | "Humagen", "Ask Humagen", bot `humagen` | internal/app/slack.go:91-134, apps/slack/manifest.json (golden), apps/slack-sandbox/src/manifest.ts, slack-manifest.json, serve.tsx:219,544, cli.tsx:13,64,94,118,172, examples/slack/manifest.json:4,9,12 | Installed apps keep their name until the manifest is re-applied |
| API contract | `title: Warp API` plus 16 prose lines | `Humagen API` | api/openapi.yaml (18 lines). Generated: client.ts:260,465,492,3739, types.ts (9 lines) | Regenerate. Never edit `generated/` |
| Chart | `deploy/helm/warp`, `name: warp`, helpers `warp.*` (6 defines, 22 includes) | `deploy/helm/humagen`, `humagen`, `humagen.*` | 13 files and 115 lines. References: deploy/chart.go:7,9, internal/app/deploy.go:433,439, Dockerfile:49-51, .dockerignore:14, check.sh:13, flux helmrelease.yaml:9, docs/deploy.md (6 lines), docs/architecture.md (1) | Existing releases set `nameOverride: warp` (2.9) |
| Image | `ghcr.io/urmzd/warp`, `warp:local`, `/usr/local/bin/warp`, user `warp` | `ghcr.io/urmzd/humagen`, `humagen:local`, `/usr/local/bin/humagen` (plus a warp symlink), user `humagen` with uid/gid 10001 | values.yaml:21, Makefile:62, Dockerfile (23 lines), docs/spec.md:114, deploy_test.go:33,327 | Nothing is published today (values.yaml:19-20, no release assets) |
| Compose variables | `WARP_IMAGE`, `_BIND`, `_PORT`, `_EXTRA_ARGS`, `_S3_BUCKET`, plus pass-throughs | `HUMAGEN_*` with nested `WARP_*` defaults | compose.yaml:18,23,25,33,80-81,104,106-107, compose.dev.yaml:17,19,23, .env.example (14 lines) | `${HUMAGEN_X:-${WARP_X:-default}}` |
| Docs | | | 17 files, 491 lines, 651 occurrences. README 22, AGENTS 4, SECURITY 1 | The old names are documented in docs/renaming.md only |
| Libraries | prose and links | humagen | 53 lines in 20 files across fiber, anchor, saige and playscape (section 5) | Docs only |

### 1b. Kept on purpose (pinned by a test, listed in docs/renaming.md)

| Name | Where | Why it stays |
|---|---|---|
| CHANGELOG.md (64 lines, all `github.com/urmzd/warp` links) | root, embedded by changelog.go:10 | Repo rule. The links redirect. **changelog_test.go:25 asserts a real CHANGELOG compare link, so it must keep the old URL** (through `legacy.RepoURL`) |
| Postgres table `warp_state` (and `warp_state_pkey`) | pkg/state/postgres.go:21,97,109,136,139 | One table for all 22 namespaces. A rename has no rollback, because an older binary would run `CREATE TABLE IF NOT EXISTS warp_state` and start on empty state. Nobody ever sees it |
| Advisory lock class `0x77617270` | pkg/state/postgres.go:34,55,69,88 | An old and a new process on one schema must still exclude each other during a rolling upgrade. Only the comment changes, and it stops spelling the word |
| Tag meta keys `warp.thread` / `prev` / `ask` / `picked` / `project` / `channel` / `message` / `within` / `sources` / `skills` / `client`, call edges `warp.root` / `parent` / `depth` / `mode` / `lease` / `call_input`, `warp.deliver`, `warp.hook` / `for` / `keeps_notes` / `place.project` / `place.thread` | pkg/runs/runs.go:56-122, pkg/call/call.go:51 (`metaPrefix = "warp."`), pkg/call/input.go:26, internal/platform/recaps.go:32, hooks.go:32-36, sees.go:63-64 | These are stored in run records, schedules and anchor's ledger, and people never see them. A read-with-fallback would have to delete both keys at every strip point (pin, ingress, schedules, knowledge, skills), or a caller could smuggle an old `warp.skills`, `warp.within` or `warp.sources` past admission. Keeping them is the only option that is both free and safe |
| GitHub `/fix` MAC domain `warp.github.mutation.v1` | pkg/integrations/github/mutations.go:29 | An invisible protocol constant. Grants for runs that are in flight during the upgrade keep verifying |
| Built-in agents `warp-writer`, `warp-analyst`, `warp-builder` | internal/platform/builtin.go:25,39,53, pkg/spec/agents.go:30, JS constants (App.tsx:96-97, Chat.tsx:193-195, recap.ts:80-82, apps/slack/src/agents.ts:42), about 62 test lines | Record keys in fiber's registry, runs, schedules (`agent: warp-analyst` in spec files), client defaults and allow-lists, project agent lists and @mentions. Renaming means an alias at every read plus a registry migration, and a missed read fails open or closed silently. Open question 1 |
| Slack view `callback_id` `warp.connections` | packages/render-slack/src/connections.ts:2,12, apps/slack/src/app.tsx:2503, app.test.tsx:1542 | Slack holds it for open modals, and nobody sees it |
| Slack stream journal dir `$XDG_STATE_HOME/warp-slack/` | apps/slack/src/journal.ts:40 | A data location the next start reads. It moves to `apps/slack/src/legacy.ts` as `JOURNAL_DIR` |
| Workspace dir `os.UserCacheDir()/warp/workspace` (`/var/lib/warp/cache/warp/workspace` in the image) | internal/platform/platform.go:331,439 (duplicated) | Attachments and disk blobs that stored records point at. Becomes one function using `legacy.CacheDir` |
| In-image paths `/var/lib/warp` (HOME, XDG_CACHE_HOME, VOLUME, WORKDIR), `/etc/warp`, `/usr/share/warp/web`, uid/gid 10001 | Dockerfile:65-88, chart deployment.yaml:51,53,63,65,102,107, compose.yaml:23,36,38, compose.dev.yaml:17,21, deploy.go:307-308 | These bind `docker run -v warp-data:/var/lib/warp` volumes, old generated compose files and spec paths people wrote (`policy_file: /etc/warp/policy.authz`). Moving the volume path loses data silently. Open question 4 |
| Compose project `name: warp`, service `warp`, volumes `warp-data` and `postgres-data`, Postgres user, db and default password `warp`, MinIO bucket default `warp` | deploy/compose/compose.yaml:11,14,36,60-66,104,123, compose.dev.yaml:12,19, internal/app/deploy.go:305-355 | Volume names come from the project name. Postgres creates its role only on first init. A renamed service leaves an orphan holding port 8080 |
| File names `deploy/compose/config/warp.yaml`, `deploy/compose/dev/warp.yaml` | deploy/compose | People edit them in place. `serve -f /etc/warp` reads every `*.yaml`, so the name does not matter |
| System `metadata.name` default `warp`, and deploy's Kubernetes namespace default `warp` | pkg/spec/system.go:214, internal/app/deploy.go:410-413 | They name the compose project, the Helm release and the `<release>-env` Secret of existing deployments. Setup prefills `humagen` for new specs instead. Open question 3 |
| Flux `Namespace warp`, `HelmRelease warp`, `GitRepository warp` and `Kustomization warp` in flux-system, Secret `warp-env` | deploy/flux/app/{namespace,kustomization,helmrelease,values}.yaml, deploy/flux/clusters/example/warp.yaml:10,27,33 | **The example Kustomization applies `./deploy/flux/app` straight from this repo with `prune: true` (clusters/example/warp.yaml:30-34).** Renaming the namespace would delete it, together with the PVC and the user's Secret |
| `.warp/` in .gitignore:45 and .dockerignore:19 | | Users' repos ignore it. Add `.humagen/` next to it |

## 2. The compatibility layer

### 2.1 Go: `internal/legacy` (new package, the only Go code that names warp)

```go
// Package legacy holds every name Humagen had as warp that it still reads,
// the names data and deployments are kept under, and the note printed
// when an old name is used.
package legacy

// Old names, read as fallbacks.
const (
	EnvPrefix    = "WARP_"            // HUMAGEN_X falls back to WARP_X
	APIVersion   = "warp/v1"
	SlackClient  = "warp_client"      // surfaces.slack fields
	SlackToken   = "warp_token"
	ScopePrefix  = "warp:"
	ReviewFormat = "warp.github.review.v1"
	ConfigDir    = "warp"             // under os.UserConfigDir()
	ConfigFile   = "warp.yaml"
	Command      = "warp"             // argv[0]
	ChartName    = "warp"             // chart of releases installed before the rename
	ComposeDir   = ".warp"            // where deploy wrote compose files
	RepoURL      = "https://github.com/urmzd/warp" // CHANGELOG history
)
// Groups are the default identity provider groups, by role name.
var Groups = map[string]string{"warp-admins": "admin", "warp-managers": "manager", "warp-operators": "operator", "warp-viewers": "viewer"}

// Kept names: data and deployments are stored under these.
const (
	DeployName = "warp"                // System metadata.name and namespace defaults, compose service, Postgres user and db
	DataVolume = "warp-data"
	DataDir    = "/var/lib/warp"
	SpecDir    = "/etc/warp"
	WebDir     = "/usr/share/warp/web"
	CacheDir   = "warp"                // os.UserCacheDir()/warp/workspace
)

type Notes struct { /* io.Writer, mutex, seen set */ }
func NewNotes(w io.Writer) *Notes
func (n *Notes) Add(format string, a ...any)   // prints "humagen: ..." once per distinct text
func AdoptEnv(environ []string, setenv func(k, v string) error, n *Notes)
func LookupEnv(name string) (string, bool)       // exact name, then the other prefix; blank counts as unset
func OldEnv(newName string) string               // HUMAGEN_X -> WARP_X
func SpecFile(getenv func(string) string, userConfigDir func() (string, error), n *Notes) (string, error)
func FromCommand(arg0 string, n *Notes)
```

Files: `internal/legacy/{legacy.go,env.go,specfile.go,notes.go}`, plus `env_test.go`, `specfile_test.go`, `kept_test.go` and `names_test.go` (section 4).

### 2.2 Settings: one reader with a fallback, warned once at start

**Where it runs.** At the top of `app.Execute` (internal/app/main.go:85). `Execute` is the entry for every subcommand and for `humagen.Main`, so downstream programs get it too. Split it so it can be tested: `Execute(o, args)` calls `execute(o, args, sys{Environ: os.Environ, Setenv: os.Setenv, Stderr: os.Stderr, Arg0: os.Args[0]})`. That function creates one `*legacy.Notes` writing to stderr, calls `legacy.AdoptEnv` and `legacy.FromCommand`, stores the notes on `Options` (unexported field), and then calls `run`.

**AdoptEnv rules**, for each `WARP_X=v` in the environment with a valid name and a non-empty `X`:
- If `HUMAGEN_X` is unset or blank and `v` is not blank, call `setenv("HUMAGEN_X", v)` and add the note `WARP_X is the old name of HUMAGEN_X; it still works, rename it`.
- If both are set and equal: note `WARP_X is set beside HUMAGEN_X and is ignored; remove it`.
- If both are set and differ: note `HUMAGEN_X and WARP_X differ; using HUMAGEN_X`.
- **Never unset `WARP_X`.** Stored client and person references name `WARP_*` variables by exact name, and children such as `docker compose` (deploy.go:229) get `os.Environ()`.
- A note names variables only and never prints a value. Notes print sorted, once each.
- Blank counts as unset, as credref already treats it (credref.go:131). Generated compose passes `${X:-}`, which is an empty string.

**Why copying is the single choke point.** About 14 reads bypass the `overlay` at main.go:227: main.go:111,170,302,349, start.go:59-60, setup.go:27, config.go:722,744,788, deploy.go:58,229,572, credref.go:130, statetest.go:60. After `AdoptEnv`, each of them reads `HUMAGEN_*` and needs nothing more than its constant changed.

**Exact-name references** (`env:NAME` in client records, people's refs, spec refs) resolve through `legacy.LookupEnv`, which works in both directions. A stored `env:WARP_SLACK_TOKEN_DOCS` therefore still works after the operator renames the variable, as the note tells them to. Two call sites change:
- `pkg/credref/credref.go:128-131`: the default `look` becomes `legacy.LookupEnv`.
- `internal/app/main.go:302`: `cfg.Env = legacy.LookupEnv`, used by `internal/platform/clients.go:282-289` envSecret.

Grants (`SecretRefs`, `-secret-refs`, `secret_env`) stay exact-name matches on the name as written. That fails closed when a ref is rewritten without its grant.

**Constants switch to the new names**, and duplicates merge:
- `envPostgres` (config.go:269) and `postgresURLEnv` (state.go:21) become one constant.
- Change the overlay keys config.go writes from the spec (`l.env[...]`, config.go:174-175,199,219,222,231,238) and connectors.go:23-50 (`EnvSlack*`, `EnvGitHub*`).
- `connectors.go:104-108` `secretRefs` grants each new name and `legacy.OldEnv(name)`. Both hold the same secret.
- statetest (`URLEnv`) and pkg/blob/s3_test.go read through `legacy.LookupEnv`, because they never pass through Execute.

**Security coupling. All of this ships together, never the env copy alone:**
- `pkg/clients/clients.go:155-168`: `EnvRef` accepts `HUMAGEN_` and `legacy.EnvPrefix`, permanently. Its error names `HUMAGEN_`.
- New `clients.SignInSecret(name string) bool`: true for any name starting with `HUMAGEN_OIDC` or `WARP_OIDC`. There is no trailing underscore, which is the stricter of today's two spellings. Use it at **all 5 sites**: internal/app/connectors.go:98, internal/platform/credentials.go:201, pkg/spec/system.go:270, pkg/credentials/starts.go:75 and :144. Otherwise `AdoptEnv` copies `WARP_OIDC_CLIENT_SECRET` into `HUMAGEN_OIDC_CLIENT_SECRET`, and a client or person could reference the new name and read the server's own sign-in secret.
- `pkg/credentials/starts.go:72-78`: an `env:` start accepts `env:HUMAGEN_` and `env:WARP_`, with `root` set to whichever prefix matched, so the `before == root` guard (:96) still refuses a bare `env:HUMAGEN_{subject}`. The overlap rule in `CheckStarts` is unchanged.

**The TS mirror** is `adoptEnv` in `apps/slack/src/legacy.ts` (2.12) and `apiTarget` in `apps/web/src/legacy.ts` (`HUMAGEN_API`, then `WARP_API` with a `console.warn`, then the default).

**Test-only variables** (`WARP_TEST_COMMAND`, `WARP_SERVER_TEST`) are renamed with no fallback. `deploy/helm/check.sh`'s `WARP` override becomes `HUMAGEN`, also with no fallback, because it is a dev tool and not a deployment setting.

### 2.3 Config file lookup order

`legacy.SpecFile` is the one resolver. It replaces start.go:60, setup.go:22,27 (the default for `setup -o`) and config.go:787-793 `defaultSpec` (callers config.go:722, deploy.go:51,58).

1. If `HUMAGEN_HOME` is set (it already includes `WARP_HOME` through AdoptEnv), the directory is that value and the candidates are `$dir/humagen.yaml`, then `$dir/warp.yaml`. Nothing outside that directory is looked at.
2. Otherwise the candidates are `UserConfigDir()/humagen/humagen.yaml`, then `UserConfigDir()/warp/warp.yaml`.
3. If both candidates exist: **error** `X and Y both exist; keep one`. Fail closed rather than guess which spec is live.
4. Only the new one exists: use it.
5. Only the old one exists: use it in place, with the note `reading ~/.config/warp/warp.yaml, the old place; it still works, move it to ~/.config/humagen/humagen.yaml`. Do not move it, so a downgrade still finds it.
6. Neither exists: the new path. First-run setup writes there.
7. `setup` writes back to the path the resolver returned, so it never forks a second spec. `serve -f DIR` is unchanged: it reads every `*.yaml`, and a directory with a System document in both files already fails (pkg/spec/spec.go:276-279).

### 2.4 Spec documents

- `api_version`: `Parse` accepts `humagen/v1` and `legacy.APIVersion`, permanently. On the old value it appends `document N: api_version warp/v1 still works; write humagen/v1` to a new `Config.Notes []string` (`json:"-"`). `Marshal` writes `humagen/v1` (spec.go:365). `loadFiles` adds `cfg.Notes` to the start notes.
- `surfaces.slack`: the struct keeps both pairs of JSON fields, `HumagenClient` with `json:"humagen_client,omitempty"` and an unexported old field with `json:"warp_client,omitempty"`, and the same for the token. The strict decoder (spec.go:230) keeps accepting old files. After decode:
  - Only the old field is set: move it into the new field and add a note.
  - Both are set: **error** `surfaces.slack: humagen_client and warp_client, not both`.
  - `Marshal` writes only the new field. The error texts at platform.go:496,602-610 name the new fields.
- Scopes written in a spec (`humagen_client.scopes`): `machineWarnings` (config.go:771-783) checks both prefixes and notes each `warp:` scope.
- The chart and the shipped compose specs render `humagen/v1`. That holds because the chart pairs with an image from the same release (2.9).

### 2.5 Persisted keys: the decision per key

| Key | Stored in | Seen by people | Decision |
|---|---|---|---|
| `env:WARP_*` client secret refs | client records (namespace `clients`), `-secret-refs`, spec `secret_env` | operators | Accept both prefixes forever. Resolve with LookupEnv in both directions. Write whatever the person wrote |
| `env:WARP_PERSON_{subject}_` starts and refs | namespace `credentials`, `-person-refs`, spec `person_refs` | operators | Accept both forever |
| `env:WARP_REF_n` | compose.yaml, carried spec, Secret `<release>-env` | no | Write `HUMAGEN_REF_n`. Every deploy regenerates all three. A stale key left in the Secret is harmless |
| tag meta `warp.*` (23 keys) | runs, schedules, anchor ledger | no | **Keep**. Pinned (1b) |
| `warp.github.review.v1` | agents' system prompts (fiber registry), model output | agent authors | Accept both in `FormatReview` (review.go:53) and in the structured and pending checks (:140-141, `Contains` either id). Advertise and emit `humagen.github.review.v1` |
| `warp.github.mutation.v1` | MAC inside `write_grant` | no | **Keep** |
| `warp_state`, lock class | Postgres | no | **Keep** (2.6) |
| built-in agent handles | everywhere | yes | **Keep** for now (open question 1) |
| inbox sender `{system, warp}` | inbox records, thread announcements | yes | New records write `humagen`. Display any `system` sender as "Humagen" in apps/web/src/Room.tsx:296, packages/adapter/src/mail.ts:386 and pkg/threads/transcript.go:241. No reader compares the name. The dedup key `release:<version>` has no name in it, so nothing reposts |
| cookies | browser, memory sessions | no | Rename. No compat |
| `warp.sidebar` | localStorage | no | Move once (2.8) |
| `warp.connections` callback | Slack open modals | no | **Keep** |
| `warp-slack` journal, `cache/warp/workspace` | disk | no | **Keep** |
| GitHub `login` (for example `warp[bot]`) | client records, `HUMAGEN_GITHUB_LOGIN` | operators | Operator data, nothing in code. If the GitHub App is renamed, set the new login and map the old one in `HUMAGEN_GITHUB_BOTS`. fiber `Config.Login` stays a single string |

### 2.6 Database objects

Keep `warp_state` and lock class `0x77617270`, permanently.
- Renaming the table needs `ALTER TABLE ... RENAME` inside the migrate transaction. That can never be rolled back: an older binary started afterwards creates an empty `warp_state` and comes up with no state.
- The lock value is what lets old and new processes on one schema exclude each other during a Recreate rollout or a botched double start. That protection matters most during exactly this upgrade.
- Neither name is ever shown. The only edit is the comment at postgres.go:34.
- The test schema prefix `warptest_` (statetest.go:65) becomes `humagentest_`.

### 2.7 Groups, roles, scopes and existing policies

- **Policy files** (`.authz`, `HUMAGEN_AUTHZ_POLICY`) and the spec's `auth.groups.roles` name their groups explicitly, so the rename does not touch them. `pkg/auth/default.authz` names no groups. Role names (admin, manager, operator, viewer) contain no warp.
- **Default groups** (authn.go:39-42): the flag defaults become `humagen-*`. In `loadPolicy` (authn.go:52), when no policy file is set, the defaults stay in force for every role whose flag was not set, so also add `legacy.Groups` to `all`, each at its role. When the server runs `oidc` with those defaults in force, add the note `the default groups also grant warp-admins, warp-managers, warp-operators and warp-viewers, the old names; name your groups in auth.groups or a policy file` (open question 2). A policy file or an explicit `-*-group` flag drops the old names. Mock personas (mockidp.go:112-115) move to `humagen-*`. A user's `-mock-personas` file with `warp-*` groups is covered by the dual default.
- **Scopes** (pkg/auth/oidc.go:17,186-195): `ScopePrefix = "humagen:"`. The loop tries `ScopePrefix` and then `legacy.ScopePrefix`, permanently. Tokens are minted by the identity provider, so there is no per-request note. `policy.go:141` explain prints the new prefix.

### 2.8 Browser storage

`apps/web/src/legacy.ts`:

```ts
export const OLD_SIDEBAR_KEY = "warp.sidebar";
/** Moves a stored value to its new key once. Storage can throw; nothing here may. */
export function moveStored(from: string, to: string, storage: () => Storage = () => window.localStorage): void
```

`moveStored` does, all inside one try/catch:
- If `to` is null and `from` is not, `setItem(to, value)` and then `removeItem(from)`.
- If both are present, `removeItem(from)`.

`App.tsx` calls `moveStored(OLD_SIDEBAR_KEY, SIDEBAR_KEY)` once, before `stored()` at :102 runs (in the `useState` initializer). After that only the new key is read and written. No other browser storage contains warp: sessionStorage, IndexedDB, service workers and `document.cookie` were checked.

### 2.9 Helm chart upgrade path

Renaming the chart changes `.Chart.Name`. For release `warp`, `fullname` would then become `warp-humagen`, because "humagen" is not contained in "warp" (_helpers.tpl:9-13). Helm would create a second Deployment, a new empty PVC and a new Service, and leave the old PVC orphaned. Setting only `fullnameOverride` changes the immutable `spec.selector` instead. The chart therefore does the following:

1. `git mv deploy/helm/warp deploy/helm/humagen`. In Chart.yaml: `name: humagen`, `version: 0.2.0`, `appVersion: "0.20.0"` (the first humagen release, open question 5), and the new `home` and `sources`.
2. Helpers become `humagen.*`. `humagen.name` = `default .Chart.Name .Values.nameOverride`, as today. values.yaml gains `nameOverride: ""` with the comment: "Set to warp for a release installed by the warp chart, so the upgrade keeps its Deployment, Service, ConfigMap and volume."
3. **Upgrade guard** (fail closed): a new `humagen.legacyFullname` mirrors the old naming (fullnameOverride, else the release name when it contains `warp`, else `<release>-warp`). When `include "humagen.name" .` is not `warp`, `lookup` the Deployment of that name in `.Release.Namespace`. If it exists and its `spec.selector.matchLabels["app.kubernetes.io/name"]` is `warp`, `fail` with: `release R was installed by the warp chart: set nameOverride: warp to keep its Deployment, Service and volume (docs/renaming.md#kubernetes)`. `lookup` returns nothing under `helm template`. Real installs and upgrades (helm, Flux helm-controller, `humagen deploy`) see it.
4. **Image guard**: if the tag (`image.tag`, else appVersion) parses as semver and is below `0.20.0`, `fail` with `image tag T is from before the rename; this chart renders humagen/v1 specs it cannot read. Build and push the image from this release`. A non-semver tag (a sha, `dev`) passes. This keeps a Flux cluster that follows `main` with a pinned old tag from crash-looping under Recreate: the render fails and the running release stays.
5. The container name becomes `humagen`, which rolls the pod. The strategy is Recreate anyway. The `helm.sh/chart` label sits on metadata only. Mounts and env keep the 1b paths. Args become `-f=/etc/warp/humagen.yaml` and `-web=/usr/share/warp/web`. The ConfigMap key and check.sh:38-46 become `humagen.yaml`.
6. Chart defaults use `env:HUMAGEN_OIDC_CLIENT_SECRET` and `env:HUMAGEN_POSTGRES_URL` (values.yaml:50,59,65-66,96, ci/full-values.yaml, NOTES.txt:10). Users' `warp-env` Secret keys arrive by `envFrom` as `WARP_*`, and AdoptEnv copies them in. This holds only for images at or after the rename, which is why the image guard exists.
7. **`humagen deploy`** (deploy.go:404-480): before `helm upgrade`, when the kubernetes block's values set no `nameOverride`, run `helm list -n NS --filter '^RELEASE$' -o json` (with `--kube-context`). If the chart is `warp-*`, set `nameOverride: warp` and add a note. The namespace default stays `legacy.DeployName`.
8. **Flux** (deploy/flux):
   - `helmrelease.yaml:9` becomes `./deploy/helm/humagen`.
   - `values.yaml` gains `nameOverride: warp`, and its placeholder image becomes `ghcr.io/your-org/humagen:0.20.0`.
   - `warp-values` becomes `humagen-values`, in both places. It is generated with a hash suffix, so this is safe.
   - Everything else in 1b stays.
   - `clusters/example/warp.yaml` is renamed `humagen.yaml` and its `url` becomes `https://github.com/urmzd/humagen`, but its object names stay `warp`, because `helmrelease.yaml:15` names the GitRepository `warp`.
   - Clusters with a copied `deploy/flux/app` still say `./deploy/helm/warp`. Their chart fetch fails when the PR merges, which is not destructive (the running release stays and the HelmRelease is not Ready). docs/renaming.md tells them to change the path and add `nameOverride: warp`.

### 2.10 Compose and `humagen deploy` compose

- deploy/compose (edited by hand, outside the bulk pass): the 1b identities stay. Interpolations become nested, for example `${HUMAGEN_PORT:-${WARP_PORT:-8080}}`, `${HUMAGEN_IMAGE:-${WARP_IMAGE:-humagen:local}}`, `HUMAGEN_POSTGRES_URL: ${HUMAGEN_POSTGRES_URL:-${WARP_POSTGRES_URL:-postgres://warp:...@postgres:5432/warp?sslmode=disable}}`, and the same for MinIO and the bucket. `.env` still reaches the container through `env_file`, and AdoptEnv handles any `WARP_*` it holds. `.env.example` documents the new names. The specs `config/warp.yaml` and `dev/warp.yaml` keep their file names and switch to `humagen/v1`, `HUMAGEN_*` refs and `display_name: Humagen`.
- Generated compose (deploy.go:257-375): the service, volume, Postgres and path literals become `legacy.*` constants, so the values do not change. The output file becomes `humagen.yaml`, mounted at `legacy.SpecDir + "/humagen.yaml"`. The default directory is `.humagen/compose`. If that is absent and `legacy.ComposeDir/compose` exists, deploy reuses it and adds a note. The compose identity is the `name:` key, so volumes are unaffected either way. `deploy` refuses a `compose.image` whose tag parses as semver below 0.20.0, for the same reason as the chart's image guard.

### 2.11 Container image

The Dockerfile is edited by hand:
- `--filter=@humagen/console`, `COPY deploy/helm/humagen`, and `-o /out/humagen ./cmd/humagen`.
- `addgroup -g 10001 humagen` and `adduser -u 10001 -G humagen -h /var/lib/warp humagen`. `COPY --from=go /out/humagen /usr/local/bin/humagen`, then `RUN ln -s humagen /usr/local/bin/warp` as root.
- `ENTRYPOINT ["/usr/local/bin/humagen"]`. `CMD`, `ENV`, `VOLUME` and `WORKDIR` keep the 1b paths.
- Optionally add `LABEL org.opencontainers.image.source=https://github.com/urmzd/humagen`.
- `legacy.FromCommand` notes `the command is humagen now; warp still runs it` when `filepath.Base(arg0) == "warp"`. This covers `docker exec ... warp config check` and custom entrypoints.
- The Makefile uses `IMAGE ?= humagen:local` and `BIN := humagen`.
- No workflow pushes images today. If one is added before or with this, it pushes each tag to both `ghcr.io/urmzd/warp` and `ghcr.io/urmzd/humagen` for one release, because GHCR does not redirect package names.

### 2.12 Slack app

- `apps/slack/src/legacy.ts`: `adoptEnv(env)` returns `{ env, notes }` and follows the Go rules (new wins, old fills a blank new, old never deleted, names only). `readConfig` runs it first and then reads only `HUMAGEN_*`. `need()` errors name the new variable. `main.ts` logs each note once at start. The file also exports `JOURNAL_DIR = "warp-slack"`, which journal.ts:40 uses.
- Go `internal/app/slack.go:230-249` writes only `HUMAGEN_*`, because the server and the app ship together. A standalone Slack app gets the TS fallback.
- Manifests: the default name is "Humagen". Regenerate `apps/slack/manifest.json` and `apps/slack-sandbox/slack-manifest.json`.
  - **Installed apps keep their name** until an admin re-applies the manifest (`humagen slack manifest`, then App Manifest in the Slack app settings, or `slack manifest sync` for the sandbox).
  - Nothing functional depends on the name. The callback ids `aui_ask` and `aui_ask_about`, the commands `/agent` and `/skill`, the scopes and the events are unchanged, and `app_mention` routes by bot user id.
  - The Home tab greets with `branding.name ?? "Humagen"`. A deployment that wants to stay "Warp" until it re-syncs sets `SLACK_BRANDING`, or the spec's `display_name` or `app_name`.
- The sandbox's `warp_fixture` metadata becomes `humagen_fixture`. Nothing reads it.

### 2.13 Go module and CLI

- There is no `cmd/warp`, following "one way". Pinned versions of the old path keep resolving (`go install github.com/urmzd/warp/cmd/warp@v0.19.0`). `@latest` on the old path fails with `module declares its path as: github.com/urmzd/humagen`, and that message itself names the new path.
- `internal/release/release.go:15`: `Module = "github.com/urmzd/humagen"`. Only the new path can be the main module of a new binary, so `running()` needs no second path.
- A deprecation release on the old path is optional, and the plan skips it. Go reads `// Deprecated` from the latest version, and once v0.20.0 exists the latest version of the old path is invalid.

## 3. Mechanical steps, in order

All commands run from the repo root. `$S` is a scratch directory outside the repo.

**3.0 Re-pin.**
```sh
git switch main && git pull && git switch -c rename/humagen
git grep -I -i -c warp -- ':!CHANGELOG.md' ':!pnpm-lock.yaml' | awk -F: '{s+=$NF} END {print NR" files, "s" lines"}'
# Must list only product words (warp, warperror, httpwarp, warpclient, warptoken, fakewarp, warpoptions, test names, warptest, nwarp from "\nwarp"):
git grep -o -i -h -E '[a-z0-9]*warp[a-z0-9]*' -- ':!CHANGELOG.md' | tr A-Z a-z | sort | uniq -c | sort -rn
```
If a third-party word or name containing "warp" shows up, add it to the protect list in 3.4.

**3.1 Pin what stays (first commit, green on the old tree).** Add `internal/legacy/kept_test.go` (section 4, test 1), plus the internal pins in pkg/state, pkg/call and pkg/integrations/github. Run `go test ./internal/legacy/ ./pkg/state/ ./pkg/call/ ./pkg/integrations/github/`.

**3.2 Write the compat layer before the bulk pass.** The bulk pass excludes these files, so the old names in them survive:
- `internal/legacy/*.go` (2.1 to 2.3) and its tests.
- `apps/slack/src/legacy.ts` and `legacy.test.ts`, `apps/web/src/legacy.ts` and `legacy.test.ts`.
- Switch the bare kept literals to constants:
  - `internal/app/deploy.go:305-355`: the local var becomes `svc`, plus `legacy.DeployName`, `DataVolume`, `DataDir`, `SpecDir` and `WebDir`.
  - `deploy.go:410-413`: namespace default.
  - `deploy.go:263-267`: compose dir.
  - `pkg/spec/system.go:214`: metadata.name default.
  - `internal/platform/platform.go:331,439`: one `workspaceDir` function using `legacy.CacheDir`.
  - `apps/slack/src/journal.ts:40`: `JOURNAL_DIR`.
  - `changelog_test.go:25`: `legacy.RepoURL + "/compare/v0.11.0...v0.12.0"`.

**3.3 Moves and module path.**
```sh
git mv cmd/warp cmd/humagen
git mv warp.go humagen.go
git mv deploy/helm/warp deploy/helm/humagen
git mv deploy/flux/clusters/example/warp.yaml deploy/flux/clusters/example/humagen.yaml
git mv apps/slack/src/warp.ts apps/slack/src/humagen.ts
git mv apps/slack/src/warp.server.test.ts apps/slack/src/humagen.server.test.ts
git mv packages/client/src/warp.ts packages/client/src/humagen.ts
go mod edit -module github.com/urmzd/humagen
```

**3.4 Bulk rewrite: protect the kept names, rename everything else, then restore.**
```sh
cat > "$S/rename.pl" <<'PERL'
my @keep = (
  qr{warp_state\b},
  qr{\bwarp\.(?:thread|prev|ask|picked|project|channel|message|within|sources|skills|client|root|parent|depth|mode|lease|call_input|deliver|hook|for|keeps_notes|place\.project|place\.thread)\b},
  qr{"warp\."},                              # pkg/call/call.go:51 metaPrefix
  qr{warp\.github\.mutation\.v1},
  qr{\bwarp-(?:writer|analyst|builder)\b},
  qr{\bwarp\.connections\b},
  qr{/var/lib/warp\b}, qr{/etc/warp\b}, qr{/usr/share/warp/web\b},
);
while (<>) {
  my @saved;
  for my $re (@keep) { s{($re)}{push @saved, $1; "\0K" . $#saved . "\0"}ge }
  s/WARP/HUMAGEN/g; s/Warp/Humagen/g; s/warp/humagen/g;
  s{\0K(\d+)\0}{$saved[$1]}g;
  print;
}
PERL
git ls-files -z -- . \
  ':!CHANGELOG.md' ':!pnpm-lock.yaml' ':!packages/client/src/generated/**' \
  ':!docs/renaming.md' ':!deploy/compose/**' ':!deploy/flux/**' \
  ':!Dockerfile' ':!.gitignore' ':!.dockerignore' ':!showcase/**' \
  ':!internal/legacy/**' ':!apps/slack/src/legacy.ts' ':!apps/slack/src/legacy.test.ts' \
  ':!apps/web/src/legacy.ts' ':!apps/web/src/legacy.test.ts' \
| xargs -0 grep -lIiZ warp | xargs -0 perl -i "$S/rename.pl"
# The compat layer only needs its import paths moved:
perl -pi -e 's#github\.com/urmzd/warp\b#github.com/urmzd/humagen#g' internal/legacy/*.go
```

Do **not** blind-replace any of the following:
- CHANGELOG.md, which is never edited.
- Anything regenerated: `packages/client/src/generated/**` and `pnpm-lock.yaml`.
- The history rows in `docs/renaming.md` (Collegium, Vultus, the mandatum to warp table). Add a new section instead.
- Hand-edited deployment files: `deploy/compose/**`, `deploy/flux/**`, `Dockerfile`, `.gitignore` (keep `.warp/` and add `.humagen/`), `.dockerignore` (keep `**/.warp` and add `**/.humagen`).
- URLs pinned to old releases. `docs/spec.md:557` `go install github.com/urmzd/warp/cmd/warp@v0.16.0` must change path **and** version together (`github.com/urmzd/humagen/cmd/humagen@v0.20.0`), because v0.16.0's go.mod says warp. Re-grep `urmzd/warp[^ ]*@v` and fix each hit the same way.
- The CHANGELOG compare link asserted at changelog_test.go:25 (done in 3.2). Release test fixtures (internal/release/release_test.go, internal/app/release_test.go) only parse text and may change.
- Binary files (showcase/*.png). Re-capture them with `teasr run` after merge.
- The library repos' history (their docs/renaming.md rows).

**3.5 Hand edits after the bulk pass.** Re-locate each by grep, because line numbers shift.
- Wire the compat layer: 2.2 (Execute, LookupEnv at credref and main.go:302, secretRefs, EnvRef, SignInSecret at 5 sites, person starts), 2.3 (three call sites), 2.4 (spec), 2.5 (review id, sender display), 2.7 (groups, scopes) and the `machineWarnings` prefix check.
- Advisory lock comment (postgres.go:34): drop the spelled-out word.
- Chart guards and `nameOverride` (2.9), deploy detection, the compose tag check (2.10), Dockerfile (2.11), compose and Flux (2.9, 2.10), Slack (2.12), and `moveStored` in App.tsx.
- Display text that should read "Humagen" where the bulk pass produced lowercase:
  - copy.ts:908,911
  - apps/web/src/Connections.tsx:225 and connections.ts:22-23
  - apps/slack/src/app.tsx:318-319 (the "isn't answering" service name)
  - internal/release/release.go:120 subject ("What's new in Humagen X")
  - announce sender display
- Markdown prose pass: product name to "Humagen" outside code, then review the diff:
  ```sh
  git ls-files -z '*.md' ':!CHANGELOG.md' ':!docs/renaming.md' | xargs -0 perl -i -pe '
    $f = !$f if /^\s*```/; next if $f;
    s/(?<![`\/\w.\-\[])humagen(?![`\/\w:\-\[]|\.\w)/Humagen/g'
  ```
- `docs/deploy.md`: fix the Flux section back to the kept names (namespace `warp`, `warp-env`, HelmRelease `warp`) and link the upgrade guide.
- `docs/renaming.md`:
  - Add a "Humagen" row and list Warp among the earlier names.
  - Add a "warp to humagen" table (Was / Is / Upgrade) for every row in 1a, and a "Kept" table copying 1b with its reasons.
  - Add sections "Upgrading from warp" with anchors `#settings`, `#kubernetes`, `#compose`, `#slack`, `#github` and `#go`.
  - Change the closing line to "Environment variables use `HUMAGEN_`; `WARP_` is still read."
- README: one line, "Formerly warp. Old settings still work; see docs/renaming.md."
- AGENTS.md: the `humagen.WithKind`, `humagen.Main` and `cmd/humagen` mentions, plus a rule: "**Old names live in one place.** A name from before Humagen is read only through `internal/legacy` (Go) or `apps/*/src/legacy.ts`; `internal/legacy/names_test.go` fails on any other mention." No em dashes anywhere.
- `api/openapi.yaml`:
  - title `Humagen API`, summary.
  - The cookie names (:123,143,4368,4371).
  - Scopes (:4361): `humagen:<permission>`, with "see docs/renaming.md for the old prefix, still accepted".
  - The system sender (:5407-5424): "`humagen`, or `warp` in records written before the rename".
  - env refs (:7937) and `humagen[bot]` (:7981).

**3.6 Regenerate and format.**
```sh
gofmt -w . && go mod tidy && go build ./... && go vet ./...
make oag-install   # only if `oag version` is not v0.20.6
make client && git diff --stat packages/client/src/generated
pnpm install                         # rewrites pnpm-lock.yaml for @humagen/*
pnpm exec biome check --write .      # import order moves: @humagen sorts before @tanstack
pnpm --filter @humagen/render-slack exec vitest run -u   # review the home.test.tsx.snap diff
go run ./cmd/humagen slack manifest > apps/slack/manifest.json
SLACK_SANDBOX_SKIP=workflowStep pnpm sandbox manifest apps/slack-sandbox/slack-manifest.json
```

**3.7 Verify locally** (CI repeats most of this).
```sh
make check && golangci-lint run
HUMAGEN_POSTGRES_TEST_URL=postgres://... go test -race ./pkg/state/... ./internal/platform/... ./internal/legacy/...
pnpm install --frozen-lockfile && pnpm exec biome check && pnpm run typecheck && pnpm test
pnpm --filter @humagen/console build && pnpm --filter @humagen/storybook build && pnpm test:server
make client-check && make spec-lint && make chart-check
make image && docker run --rm humagen:local version && docker run --rm --entrypoint /usr/local/bin/warp humagen:local version
git diff --stat main -- CHANGELOG.md          # must be empty
git diff main -U0 | grep '^+' | grep -c '—'    # must be 0
```

**3.8 Commit and PR.** The PR is squash-merged as `feat!: rename warp to humagen`, with a footer that sr files under Breaking:

```
BREAKING CHANGE: the Go module is github.com/urmzd/humagen (package humagen, command cmd/humagen) and the npm workspace scope is @humagen. WARP_* variables, warp.yaml, api_version warp/v1, warp_client and warp_token, warp: scopes and the warp-* default groups still work and print a note at start. A Kubernetes release installed by the warp chart sets nameOverride: warp (docs/renaming.md#kubernetes).
```

Paste the 3.4 script into the PR description so that in-flight branches can apply it.

## 4. Tests to add

1. **Pinned kept names**: `internal/legacy/kept_test.go` (package `legacy_test`, written first in 3.1). It asserts exact values and fails if the bulk pass renamed a kept key:
   - `runs.MetaThread == "warp.thread"` and every other meta key.
   - `recaps.MetaDeliver`, the hooks and sees keys, `call.MetaInput`.
   - `platform.BuiltinWriter`, `BuiltinAnalyst` and `BuiltinBuilder`, and `spec.BuiltinAgents`.
   - Internal pins: `pkg/state/postgres_internal_test.go` (`lockClass == 0x77617270`, and `schema` creates `warp_state`), `pkg/call` (`metaPrefix == "warp."`) and `pkg/integrations/github` (the mutation domain).
2. **Fallback reader** (`internal/legacy/env_test.go`):
   - Only old set copies to new.
   - Both equal, and both differing (new wins).
   - Blank new with old set copies.
   - Old is never unset.
   - `WARP_` with an empty suffix is ignored.
   - `LookupEnv` both directions, and blank counts as unset.
   - `OldEnv`.
   - Cross-package: `pkg/credref` (`env:WARP_X` resolves when only `HUMAGEN_X` is set, and the reverse), `internal/platform` envSecret, and statetest's URL fallback.
3. **The warning**:
   - `internal/app` `execute` with a fake `sys`: `WARP_POSTGRES_URL=secret` gives exactly one stderr line naming `WARP_POSTGRES_URL` and `HUMAGEN_POSTGRES_URL`, never `secret`.
   - It is printed before the command output, also for `version`, and once even when the same note is added twice. No `WARP_*` set means no output.
   - `Arg0` `/usr/local/bin/warp` gives the command note.
   - Slack: in `apps/slack/src/units.test.ts`, `adoptEnv` with only `WARP_TENANT` gives tenant read plus one note. Both set and differing gives the new value plus a note. Neither set gives `Missing HUMAGEN_TENANT`. main.ts logs the notes once.
4. **Security** (one table test per site, `internal/legacy/signin_test.go` or beside each site): `SignInSecret` is true for `HUMAGEN_OIDC_CLIENT_SECRET`, `WARP_OIDC_CLIENT_SECRET` and `HUMAGEN_OIDCX`. Each of the 5 sites refuses both prefixes: connectors `-secret-refs`, platform people's refs, spec `secret_env`, `CheckStart` and `Expand`. `EnvRef` accepts both prefixes and refuses others. `CheckStart` refuses bare `env:HUMAGEN_{subject}_` and `env:WARP_{subject}_`.
5. **Config file** (`internal/legacy/specfile_test.go` and `internal/app/setup_test.go`), with an injected `userConfigDir` and `t.Setenv`:
   - The `HUMAGEN_HOME` dir with each candidate, and `WARP_HOME` through AdoptEnv.
   - The new file only; the old place only (note); both (error); neither (new path).
   - `setup` writes back to the old file it read.
   - `start` with only `~/.config/warp/warp.yaml` does not ask the first-run questions.
6. **Spec** (`pkg/spec/spec_test.go`):
   - `warp/v1` parses with a note. `humagen/v1` parses with no note. `Marshal` writes `humagen/v1`.
   - `warp_client` alone moves into `humagen_client` with a note. Both set is an error. `Marshal` writes only `humagen_client`.
   - The docs/spec.md examples still parse (they are tested today).
7. **Auth**:
   - `pkg/auth/oidc_test.go`: a token with `warp:runs:read` and one with `humagen:runs:read` both get `RunsRead`. `other:runs:read` gets nothing.
   - `internal/app/authn_test.go`: with no policy and no flags, `humagen-admins` and `warp-admins` are both admin, and under oidc there is one note. With `-admin-group=x` set, the old group is still granted for the roles whose flags were not set. With a policy file, only the flags that were set apply.
   - `machineWarnings` with `warp:` scopes.
8. **Review format** (`internal/presentation/github/review_test.go`): both ids render as a review, a pending prefix of either id is held back, and the guide advertises the new id.
9. **Stored keys across a restart, on memory and Postgres** (`internal/platform/rename_restart_test.go`, using `statetest.Each`). Open the store once. For memory, reuse the same `state.Store`. For Postgres, `Close` and then `OpenPostgres` again on the same pool and schema. Then:
   - (a) Create a client through the store with secret ref `env:WARP_SLACK_TOKEN_DOCS`, rebuild the platform on the reopened store with only `HUMAGEN_SLACK_TOKEN_DOCS` set, and check it resolves.
   - (b) The same for a person credential `env:WARP_PERSON_ANA_ISSUES` with only `HUMAGEN_PERSON_ANA_ISSUES` set.
   - (c) A run tag with `warp.project` and a schedule with `warp.deliver` written before the reopen are read back as project and recap delivery.
   - (d) Postgres only: a row inserted with raw SQL into `warp_state`, as v0.19.0 would write it, is returned by `Get`. A second connection holding `pg_try_advisory_lock(0x77617270, hashtext(current_schema()))` makes `OpenPostgres` return `ErrLocked`.
   - CI does not set the Postgres URL, so run (d) locally before merging (3.7).
10. **Browser storage**:
    - `apps/web/src/legacy.test.ts`: only `warp.sidebar="0"` moves to `humagen.sidebar="0"` and the old key is gone. Both present keeps the new value and removes the old. Storage that throws does not throw.
    - An App DOM test: a browser holding only `warp.sidebar="0"` starts with the sidebar closed.
    - A mail and Room test: a `system` sender stored as `warp` shows as "Humagen".
11. **Chart and deploy**:
    - check.sh adds `ci/upgrade-values.yaml` (`nameOverride: warp`) and asserts that the rendered Deployment, Service and PVC are named `warp` with selector `app.kubernetes.io/name: warp`.
    - The default render gives `humagen`.
    - A render with `image.tag: 0.9.0` fails with the image-guard message.
    - `internal/app/deploy_test.go`: a fake `run` answers `helm list` with chart `warp-0.1.0`, and the written values gain `nameOverride: warp`. Generated compose keeps service `warp`, volume `warp-data`, the `/var/lib/warp` and `/etc/warp` paths and the Postgres user. An existing `.warp/compose` is reused with a note. `compose.image: x:0.19.0` is refused.
12. **Guard against new warp** (`internal/legacy/names_test.go`): it runs `git ls-files` from the module root (fails if git is missing; CI has it), skips binary files, and checks every path and every line matching `(?i)warp`. Allowed are:
    - **files**: `CHANGELOG.md`, `docs/renaming.md`, `internal/legacy/**`, `apps/slack/src/legacy*.ts`, `apps/web/src/legacy*.ts`, `deploy/compose/**` and `deploy/flux/**` (deployment identities, edited by hand).
    - **tokens, anywhere**: the 1b meta keys, `"warp."` in pkg/call/call.go, `warp_state`, `warp.github.mutation.v1`, `warp-(writer|analyst|builder)` and `warp.connections`.
    - **tokens in Dockerfile, deploy/helm/humagen/**, docs/** and .gitignore/.dockerignore**: `/var/lib/warp`, `/etc/warp`, `/usr/share/warp/web`, `/usr/local/bin/warp`, `\.warp/?`, `nameOverride: warp`, and in the chart helpers the legacy fullname `warp`.
    - **README.md**: `Formerly warp`.
    - Each entry carries its reason string. A line passes when every warp match is covered by an allowed token. On failure the test prints `path:line`.

## 5. Library changes

No library has warp in code, in go.mod or in a filename. Fiber reads no warp meta key, takes its GitHub login from `Config.Login`, and reads no env. **No code change and no release is needed.** humagen's go.mod (fiber v0.23.0, saige v0.41.0, playscape v0.7.0, anchor v0.5.1) is unchanged. Each library gets one `docs:` PR **after** the humagen PR merges and v0.20.0 is tagged, so the links and the identifiers they describe match what humagen ships. There is no order between them. Follow the merge chain if you like: anchor, playscape, saige, fiber. The changes ride along with each library's next ordinary release.

- **anchor** (8 lines): README.md:297,303 (link becomes `urmzd/humagen/blob/main/docs/ecosystem.md`), docs/architecture/overview.md:40, and docs/renaming.md:8,14,26,27,29. Add Humagen and list Warp as earlier. The review row says `humagen.github.review.v1` with the old id still accepted. The mutation row stays `warp.github.mutation.v1`, kept. The env line says `HUMAGEN_` with `WARP_` still read.
- **playscape** (13 lines): README.md:236 (keep the anchor `#limitations-in-one-place`), :244,248,249, docs/architecture/overview.md:105,123,128, docs/design/agent-execution.md:6, and docs/renaming.md with the same edits as anchor's (the files are byte-identical).
- **saige** (5 lines): README.md:418-423.
- **fiber** (27 lines): AGENTS.md:7,156, README.md:57,304,309,311,333, SECURITY.md:66, llms.txt:5, fiber.go:27 and cmd/fiber/main.go:22 (doc comments), pkg/connectors/github/github.go:19,110 and event.go:334 (examples `humagen[bot]` and `@humagen`), docs/architecture/github.md:14-40 (also fix the stale "warp's go/github package" to `pkg/integrations/github`), slack.md:15-19 (`HUMAGEN_*`, `humagen serve`), overview.md:808, and docs/renaming.md:8. Making `Config.Login` a list is not needed: `HUMAGEN_GITHUB_BOTS` maps an old login.
- humagen must keep `docs/ecosystem.md` (with its heading "Limitations in one place"), `docs/github.md` and `docs/slack.md` at those paths, because the libraries' released docs link to them.

## 6. Order of operations with the GitHub rename

1. Open the PR from `rename/humagen` and get CI green. CI does not depend on the repo name. **Do not merge yet.** Before the rename, merge or close the other open branches.
2. The owner checks the sr GitHub App (client id `Iv23liUsXGDICnIdo3Ev`): its repository selection, the `release` environment and the `SR_RELEASER_PRIVATE_KEY` secret. All are tied to the repo id and survive the rename. `release.yml:33` uses `github.event.repository.name`, so it follows on its own.
3. **The owner renames the repo** `urmzd/warp` to `urmzd/humagen` (Settings, General). Then everyone runs `git remote set-url origin https://github.com/urmzd/humagen` in every clone (worktrees share the config). The name `urmzd/warp` must never be reused.
4. **Merge the PR right after.** The Release workflow runs CI and then sr. sr tags `v0.20.0` in `urmzd/humagen`, writes the CHANGELOG entry with a `urmzd/humagen` compare link, and its `type: go` publish resolves `github.com/urmzd/humagen` from go.mod, which now exists.
5. Verify from a clean `GOPATH`:
   - `go install github.com/urmzd/humagen/cmd/humagen@v0.20.0` works.
   - `go install github.com/urmzd/warp/cmd/warp@v0.19.0` still works through the proxy and the redirect.
   - `go install github.com/urmzd/warp/cmd/warp@latest` fails with the module-path message. That is expected and documented.
6. Merge the library docs PRs (section 5).
7. Owner follow-ups:
   - Re-apply the Slack manifest on the owner's own Slack apps.
   - Optionally rename the GitHub App, then set `HUMAGEN_GITHUB_LOGIN` and map the old login in `HUMAGEN_GITHUB_BOTS`.
   - Point the owner's Flux clusters at the new URL and chart path.
   - Re-capture showcase/*.png.

What goes wrong in the other order:
- **Merge first, rename later:**
  - sr tags v0.20.0 in `urmzd/warp` with go.mod saying humagen. Its Go publish (and anyone running `go install github.com/urmzd/humagen/...`) asks proxy.golang.org for a repo that does not exist yet. The Release job fails, and the proxy may cache the not-found for a while.
  - The v0.20.0 CHANGELOG entry links to `urmzd/warp`. That is cosmetic, and by rule it cannot be hand-fixed.
  - Merging forces downstream breakage on the old path before the new path exists.
- **Rename long before merging:** nothing breaks. The old module path resolves through the redirect, and any release in between is still a warp-path release. Renaming first is strictly safer.
- **Image pushes:** none exist today. If an image workflow is added that pushes to `ghcr.io/${{ github.repository }}`, then merging first pushes the humagen binary as `ghcr.io/urmzd/warp`. Renaming first starts a new GHCR package with no link from the old one, so push both names for one release.

## 7. Risks and how each is checked

| Risk | Check |
|---|---|
| A kept stored key gets renamed by the bulk pass (meta keys, table, built-ins) | Test 1 is written before the pass and fails on any change. Review `git diff main -- pkg/runs pkg/call pkg/state internal/platform/{recaps,hooks,sees,builtin}.go` |
| Sign-in secret exfiltration through `env:HUMAGEN_OIDC_*` | Test 4. `git grep -n 'OIDC' -- '*.go' ':!*_test.go' \| grep HasPrefix` returns only `clients.SignInSecret` |
| An old env var is ignored somewhere (a raw read before AdoptEnv, or a process that does not start through Execute) | Test 3. `git grep -n 'os.Getenv\|os.LookupEnv' -- '*.go' ':!*_test.go'` reviewed: every read happens after `execute`, or goes through `legacy.LookupEnv`. `pnpm test:server` runs the Slack app against a real server built from `./cmd/humagen` |
| Kubernetes upgrade creates a second writer and an empty PVC, or fails on the immutable selector | Test 11. Before merge, manually on kind: `helm install warp` with the v0.19.0 chart and write a file to the PVC, then `helm upgrade warp deploy/helm/humagen`. Without `nameOverride` the guard fails with no change. With it, `kubectl get deploy,svc,pvc warp -o jsonpath='{.metadata.uid}'` is unchanged and the file is there |
| Flux prunes namespace `warp` | `git diff main -- deploy/flux/app` touches only the chart path, `nameOverride`, the placeholder image and the `humagen-values` name. namespace.yaml, the kustomization's `namespace` and the HelmRelease name are untouched |
| Flux source-controller (go-git) may not follow GitHub's redirect (unverified) | On a test cluster, reconcile a GitRepository still pointing at `urmzd/warp` after the rename. docs/renaming.md tells users to change `url` either way |
| Compose data loss (project, volume or service renamed) | Manually: `make up` on v0.19.0 and write data, check out the branch, `make up`. `docker volume ls` shows the same `warp_warp-data` and `warp_postgres-data`, and the data is there. `docker compose config` with only `WARP_PORT=9090` in `.env` publishes 9090 (this checks nested defaults on the Compose v2 in use) |
| New chart or new CLI paired with an old image (crash-loop under Recreate) | The chart image guard and deploy's tag check (test 11). Release note: build the image from v0.20.0 before upgrading the chart |
| Generated client stale or lockfile stale | CI "API contract and client" (`make client`, then the diff) and `pnpm install --frozen-lockfile` |
| Build filters miss the new scope (Makefile, Dockerfile:35, ci.yml:72-73, main.go:225) | `git grep -n '@warp/'` is empty. `make image` builds a console (`docker run ... ls /usr/share/warp/web/index.html`) |
| The display name drifts (lowercase in UI, or "Warp" left in Slack) | Review the snapshot diff, `grep -rn "Warp" apps packages --include=*.tsx` is empty, the manifest golden test passes. Installed Slack apps rename only on re-sync (owner action) |
| Default `warp-*` groups grant roles on fresh installs, for example where another product named Warp has groups | The start note under oidc (test 7). Open question 2 |
| A new "warp" slips back in later | Test 12 in CI, plus the AGENTS.md rule |
| CHANGELOG edited, or em dashes added | The `git diff --stat main -- CHANGELOG.md` and em-dash greps in 3.7 |
| Docs promise a name the code does not read | Old names appear only in docs/renaming.md. The docs/spec.md examples are parsed by tests |
| In-flight branches conflict on 893 import lines | Merge them first, or apply the 3.4 script on the branch and then rebase |
| The release number differs from the pinned 0.20.0 (chart appVersion and both guards) | Before merging, confirm main's latest tag is v0.19.0 and that this is the only pending feat. If sr produces another number, fix appVersion and the guards right away in a `fix:` |

## Corrections to the inventories (checked at 60e527c)

- The cookie scheme in api/openapi.yaml is at :4368 and :4371 (ops), not 4157/4160. The generated client title is at client.ts:260, not 255.
- The JS inventory asked the server to keep reading old cookie names. Sessions and logins live in an in-memory map (session.go:47-48), so a restart already signs everyone out, and no cookie compat is needed.
- The module path is on 893 lines in 270 files. That counts go.mod, release.go:15, openapi_test.go and examples, and agrees with both the "868 import lines" and the "892 occurrences" figures.
- changelog_test.go:25 is not a free fixture: it asserts a link in the embedded CHANGELOG.md, so it must keep the old URL.
- docs/spec.md:557 pins `@v0.16.0` on the old path, so the path and the version must change together.
- The Flux example applies `./deploy/flux/app` from this repo itself (clusters/example/warp.yaml:30-34, `prune: true`), not only from users' copies. That is why every Kubernetes identity in `deploy/flux/app` stays.
- No word in the repo contains "warp" other than the product's own names (`git grep -o -i -E '[a-z0-9]*warp[a-z0-9]*'`), so a case-preserving replace is safe once the kept names are protected.
