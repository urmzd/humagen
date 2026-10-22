# Upgrading from warp

The product was called warp. It is called Humagen now: the Go module is
`github.com/urmzd/humagen`, the command is `humagen`, and every setting,
file and name a person types starts with `humagen` or `HUMAGEN_`.

Nothing an existing deployment holds needs to change on the day it upgrades.
Every name people typed before still works, and the server prints a note at
start for each one it read, naming the new one. Every name that stored data or
a running deployment depends on is kept as it was. This page lists both, and
what an operator does, when they choose to, to move to the new names.

Old names are read for good unless a later release says otherwise in its
breaking changes, with a release of notice. Kept names do not change.

## What changes

| What | Was | Is |
|------|-----|----|
| Repository | `github.com/urmzd/warp` | `github.com/urmzd/humagen` (GitHub redirects the old address) |
| Go module and root package | `github.com/urmzd/warp`, `warp.Main` | `github.com/urmzd/humagen`, `humagen.Main` |
| Command | `warp`, `cmd/warp` | `humagen`, `cmd/humagen` |
| Settings | `WARP_*` | `HUMAGEN_*` ([settings](#settings)) |
| Config file | `~/.config/warp/warp.yaml`, `$WARP_HOME/warp.yaml` | `~/.config/humagen/humagen.yaml`, `$HUMAGEN_HOME/humagen.yaml` ([config file](#config-file)) |
| Spec | `api_version: warp/v1`, `surfaces.slack.warp_client`, `warp_token` | `humagen/v1`, `humagen_client`, `humagen_token` ([spec](#spec)) |
| Machine scopes | `warp:runs:read` | `humagen:runs:read` ([sign-in](#sign-in)) |
| Default groups | `warp-admins`, `warp-managers`, `warp-operators`, `warp-viewers` | `humagen-admins`, `humagen-managers`, `humagen-operators`, `humagen-viewers` |
| Helm chart | `deploy/helm/warp`, chart `warp` | `deploy/helm/humagen`, chart `humagen` ([Kubernetes](#kubernetes)) |
| Image | `warp:local`, `/usr/local/bin/warp`, user `warp` | `humagen:local`, `/usr/local/bin/humagen` (with a `warp` link), user `humagen`, same uid ([image](#image)) |
| Compose settings | `WARP_IMAGE`, `WARP_BIND`, `WARP_PORT`, `WARP_EXTRA_ARGS`, `WARP_S3_BUCKET` | `HUMAGEN_*`, each falling back to its `WARP_` name ([compose](#compose)) |
| Where `deploy` writes a compose project | `.warp/compose` | `.humagen/compose`, or `.warp/compose` when it is there |
| GitHub review format | `warp.github.review.v1` | `humagen.github.review.v1`; both are rendered ([GitHub](#github)) |
| Slack app name | Warp, Ask Warp | Humagen, Ask Humagen, once the manifest is applied again ([Slack](#slack)) |
| Release notes sender | `warp` | `humagen`; both are shown as Humagen |
| Session cookies | `warp_session`, `warp_login` | `humagen_session`, `humagen_login`. Sessions are kept in memory, so a restart signs everyone out anyway |
| npm workspace scope | `@warp/*` | `@humagen/*` (private packages, nothing published) |
| Console sidebar setting | `warp.sidebar` in the browser | `humagen.sidebar`, moved once on the first visit |

## Settings

The server copies each setting it reads that is set only under its old name to
its new one, at start, before any command runs, and says so:

```
humagen: WARP_POSTGRES_URL is the old name of HUMAGEN_POSTGRES_URL; it still works, rename it
```

The rule is the same everywhere: when both names are set and not blank, the
new one wins, and the note says `HUMAGEN_X and WARP_X differ; using HUMAGEN_X`.
A blank value counts as unset. An old name is never unset, and a note names
variables, never their values.

Only the variables the server reads are copied: the settings
(`internal/legacy` lists them: `HUMAGEN_HOME`, `HUMAGEN_POSTGRES_URL`,
`HUMAGEN_OIDC_*`, `HUMAGEN_SLACK_*`, `HUMAGEN_GITHUB_*` and the rest). Other
programs export `WARP_` variables of their own (the Warp terminal sets
`WARP_IS_LOCAL_SHELL_SESSION`), and those are left alone.

References read either name. A reference stored before the upgrade, in a
client record, a person's credential or a spec (`env:WARP_SLACK_TOKEN_DOCS`),
reads `HUMAGEN_SLACK_TOKEN_DOCS` when that is set and `WARP_SLACK_TOKEN_DOCS`
otherwise. So an operator renames the variable in the environment, and leaves
the references as they are: they are data the workspace stored. The same holds
where names are compared: a `-secret-refs` or `secret_env` grant, and a
`person_refs` start, name one variable whichever prefix each was written with.
`env:WARP_PERSON_{subject}_` and `env:HUMAGEN_PERSON_{subject}_` are one start,
so they cannot be given to two workspaces. Do not rename a `keychain:` or
`op://` start: those name places outside Humagen, and the references stored
under them would stop being their people's.

The sign-in secret is the server's own under both names: no client or person
may reference `HUMAGEN_OIDC*` or `WARP_OIDC*`.

The Slack app reads its own settings the same way (`WARP_TENANT`, `WARP_URL`,
`WARP_TOKEN`, `WARP_OIDC_*` and the rest), and logs a note for each at start.
The console's dev server reads `HUMAGEN_API`, else `WARP_API` with a warning.
The test settings (`HUMAGEN_POSTGRES_TEST_URL`, `HUMAGEN_TEST_S3_*`) fall back
too. `HUMAGEN_TEST_COMMAND`, `HUMAGEN_SERVER_TEST` and `deploy/helm/check.sh`'s
`HUMAGEN` override do not: they are development tools.

## Config file

`start`, `setup`, `config` and `deploy` look for the spec in this order:

1. With `HUMAGEN_HOME` (or `WARP_HOME`) set: `humagen.yaml` there, else
   `warp.yaml` there. Nothing outside that directory.
2. Without it: `~/.config/humagen/humagen.yaml`, else
   `~/.config/warp/warp.yaml` (the user config directory of the platform).

An old file is read where it is, with a note. It is never moved, so the
release before the rename still finds it. `setup` writes back to the file it
read, in the language that file was in, so an older binary can still read it.
Both files present is an error: keep one. To move, `mv` the file and change
its `api_version`.

## Spec

`api_version: warp/v1` is read, with a note, and so are
`surfaces.slack.warp_client` and `warp_token`, which become `humagen_client`
and `humagen_token`. A file that sets both names of one field is refused.
`humagen config print` writes the new names.

The shipped compose specs (`deploy/compose/config/warp.yaml` and
`deploy/compose/dev/warp.yaml`) keep their names and their contents, since
people edit them in place.

## Sign-in

Tokens whose scopes use the old prefix (`warp:runs:read`) keep their
permissions. Define the `humagen:` scopes at the identity provider before
changing a machine client's scopes in the spec; `humagen config check` notes
the old prefix and says the same.

With no policy file, the default groups grant their roles under both names:
`humagen-admins` and `warp-admins` are both admin. A `-*-group` flag set for a
role, or a policy file, drops the old name for that role, as it drops the
default. Under OIDC the server notes the old groups it grants. Name your
groups in `auth.groups` or a policy file to stop relying on either default.

## Kubernetes

A release the warp chart installed names its Deployment, Service, ConfigMap
and volume claim `warp` (or `<release>-warp`), with the selector
`app.kubernetes.io/name: warp`. The humagen chart would name them for itself
and start a second, empty server beside them, so it refuses to upgrade such a
release: set `nameOverride: warp` and it keeps every name and the volume.

```sh
helm upgrade warp deploy/helm/humagen -n warp --reuse-values --set nameOverride=warp
```

`humagen deploy` does this itself, on every deploy: it asks helm whether the
release was installed by the warp chart, or was given `nameOverride: warp`, and
keeps the name. The guard reads the cluster, so `helm template` renders without
it.

Build the image from the same commit as the chart: the chart writes
`api_version: humagen/v1`, which images from before the rename refuse. The
chart's defaults reference `HUMAGEN_OIDC_CLIENT_SECRET` and
`HUMAGEN_POSTGRES_URL`; a Secret that holds `WARP_OIDC_CLIENT_SECRET` and
`WARP_POSTGRES_URL` keeps working with the new image. The paths in the pod
(`/var/lib/warp`, `/etc/warp`) are kept.

### Flux

Flux uninstalls and reinstalls a release whose chart changes name, unless the
`HelmRelease` says otherwise. That takes the server down, deletes the
Deployment and Service, and leaves the guard above nothing to check. The
example's `HelmRelease` therefore sets
`spec.upgrade.chartNameChangeStrategy: InPlaceUpdate`, and its values set
`nameOverride: warp`. A cluster that copied `deploy/flux/app` changes three
things in its copy, in one commit:

- `spec.chart.spec.chart: ./deploy/helm/humagen`
- `spec.upgrade.chartNameChangeStrategy: InPlaceUpdate`
- `nameOverride: warp` in its values

and points its `GitRepository`'s `url` at `https://github.com/urmzd/humagen`.
Until the chart path changes, the chart fetch fails and the running release
stays as it is. The namespace, the `HelmRelease`, the `GitRepository`, the
`Kustomization` and the `warp-env` Secret keep their names: the example
`Kustomization` prunes, so renaming any of them would delete it.

A `HelmRelease` on a helm-controller too old to know `chartNameChangeStrategy`
is refused by its schema, and nothing changes until the controller is
upgraded.

## Compose

`deploy/compose` keeps its project name (`warp`), service, volumes
(`warp_warp-data`, `warp_postgres-data`) and its Postgres user, database and
default password, so an existing stack keeps its data. A `.env` with `WARP_`
names keeps working: each `HUMAGEN_` variable in `compose.yaml` falls back to
its `WARP_` name. The default image tag is `humagen:local`, built on the next
`docker compose up --build`.

A compose project `humagen deploy` wrote keeps its identity too, and is
written again to `.warp/compose` when an earlier deploy wrote there. Its file
passes each variable a spec names from the shell under either name.

## Image

The image runs `/usr/local/bin/humagen`. `/usr/local/bin/warp` links to it,
for custom entrypoints and `docker exec ... warp`, and running it by that name
prints a note. The user is `humagen` with the same uid and gid, 10001, so a
volume's files stay its own. `/var/lib/warp` (the home, the cache and the
volume), `/etc/warp` (the spec) and `/usr/share/warp/web` (the console) are
kept, since volumes and specs bind them.

Nothing publishes an image yet. A workflow that starts to should push each tag
under both `ghcr.io/urmzd/warp` and `ghcr.io/urmzd/humagen` for one release,
since GitHub's container registry does not follow a repository rename.

## Slack

An installed Slack app keeps its name until its manifest is applied again:
`humagen slack manifest`, then App Manifest in the app's settings (or
`slack manifest sync` for the sandbox). Nothing depends on the name: the
callback ids, the commands, the scopes and the events are unchanged. A
deployment that wants to stay "Warp" sets `app_name` or `display_name` in its
spec, or `SLACK_BRANDING`. The stream journal stays in
`$XDG_STATE_HOME/warp-slack/`, and the `warp.connections` view callback stays,
since open modals carry it.

## GitHub

Agents whose instructions ask for `warp.github.review.v1` keep getting
reviews; new examples use `humagen.github.review.v1`. A `/fix` grant issued
before the upgrade still verifies: its domain, `warp.github.mutation.v1`, is
kept. The GitHub App's login is configuration: if the App is renamed, set
`HUMAGEN_GITHUB_LOGIN` to the new login and list the old one in
`HUMAGEN_GITHUB_BOTS`.

## Go

Programs that import the module change their import path to
`github.com/urmzd/humagen` and `warp.Main` to `humagen.Main`. Versions tagged
before the rename keep resolving under the old path through the module proxy
and GitHub's redirect; the old path's `@latest` fails with
`module declares its path as: github.com/urmzd/humagen`, which names the new
one. There is no deprecation release on the old path.

## Kept names

| Name | Why it stays |
|------|--------------|
| Tag meta keys `warp.thread`, `warp.prev`, `warp.ask`, `warp.picked`, `warp.project`, `warp.channel`, `warp.message`, `warp.within`, `warp.sources`, `warp.client`, `warp.deliver`, `warp.call_input`, the call edges `warp.root`, `warp.parent`, `warp.depth`, `warp.mode`, `warp.lease`, the housekeeping keys `warp.hook`, `warp.for`, `warp.keeps_notes`, `warp.place.project`, `warp.place.thread`, and the private `_warp.sources`, `_warp.skills`, `_warp.routed` | Stored in run records, schedules and anchor's ledger. Reading two names would mean deleting both at every point that strips them, or a caller could smuggle an old key past admission |
| Postgres table `warp_state` and lock class `0x77617270` | An older binary would create an empty table, and an older and a newer process on one schema must still exclude each other |
| Built-in agents `warp-writer`, `warp-analyst`, `warp-builder` | Keys in the registry, runs, schedules, allow-lists, project agent lists and mentions |
| `/fix` grant domain `warp.github.mutation.v1` | Grants in flight keep verifying |
| Image paths `/var/lib/warp`, `/etc/warp`, `/usr/share/warp/web`, uid 10001 | Volumes and specs bind them |
| Workspace `os.UserCacheDir()/warp/workspace` | Attachments and files stored records point at |
| Slack journal `warp-slack/` and view callback `warp.connections` | The next start reads the journal; open modals carry the callback |
| Compose project, service, volumes, Postgres user and database `warp` | Volumes are named for the project, and Postgres creates its user only once |
| System `metadata.name` default and `humagen deploy`'s namespace default, `warp` | They name existing compose projects, Helm releases and their `<release>-env` Secrets |
| Flux namespace, `HelmRelease`, `GitRepository`, `Kustomization` and `warp-env` Secret | The example prunes |
| A warp chart release's resource names (`nameOverride: warp`) | Its volume and selector |

## Choices the owner can flip

These are the defaults this release takes. Each can change in a later release.

- **Built-in agent handles** stay `warp-writer`, `warp-analyst` and
  `warp-builder`. Renaming them needs an alias at every read and a registry
  migration.
- **Old default groups** (`warp-*`) keep their roles while the defaults are in
  force, with a note under OIDC. A later release could stop granting them; an
  identity provider that holds groups of those names for another product
  grants roles until then.
- **Deployment defaults** stay `warp`: the System `metadata.name` default and
  `humagen deploy`'s Kubernetes namespace. A later release could default to
  `humagen` and refuse a spec that leaves them unset.
- **Image paths** stay under `warp` (`/var/lib/warp`, `/etc/warp`,
  `/usr/share/warp/web`), with the cache workspace and the Slack journal. A
  later release could move them behind links once that is tested on Docker and
  Kubernetes.
- **No deprecation release** on the old module path.

## After this merges: the owner's steps

1. Rename the repository `urmzd/warp` to `urmzd/humagen` in its settings,
   before merging, so the release's Go publish finds the new path. Never
   create a new `urmzd/warp`: the redirect stops working. Point each clone at
   the new address: `git remote set-url origin https://github.com/urmzd/humagen`.
2. Merge, then check from a clean module cache that
   `go install github.com/urmzd/humagen/cmd/humagen@<the new tag>` works.
3. Apply the Slack manifest again on each installed Slack app
   (`humagen slack manifest`, then App Manifest in the app's settings).
4. If the GitHub App is renamed, set `HUMAGEN_GITHUB_LOGIN` to its new login
   and add the old one to `HUMAGEN_GITHUB_BOTS`.
5. In each Flux cluster, point the `GitRepository`'s `url` at
   `https://github.com/urmzd/humagen` (whether Flux follows GitHub's redirect
   is not verified), and in a copied `deploy/flux/app`, make the three changes
   under [Flux](#flux).
6. Merge the library documentation follow-ups (fiber, anchor, saige,
   playscape), which link here.
7. Capture `showcase/*.png` again.
