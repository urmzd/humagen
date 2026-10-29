# Develop with Tilt

One command starts humagen's API, the console with hot reload and a database
that keeps conversations across restarts, and restarts only what a change
needs. It is a [Tilt](https://docs.tilt.dev/) extension,
[`deploy/tilt/dev/Tiltfile`](../../deploy/tilt/dev/Tiltfile), so a project
built on humagen uses the same workflow with its own command and spec, rather
than keeping a launcher of its own.

```sh
make tilt                                   # tilt up -f dev.Tiltfile; open http://127.0.0.1:5173
tilt up -f dev.Tiltfile -- --memory         # no Docker; nothing is kept
tilt up -f dev.Tiltfile -- --mock           # sign in as a persona instead of as admin
tilt up -f dev.Tiltfile -- --spec examples/dev/humagen.yaml   # real models
tilt down -f dev.Tiltfile                   # stop everything; the database is kept
```

It needs Tilt, Go, Node.js and pnpm, and Docker for the database (not with
`--memory`). The root [`Tiltfile`](../../Tiltfile) is a different setup: humagen
on a local Kubernetes cluster through its Helm chart
([deploy](deploy.md#local-kubernetes-with-tilt)).

## What runs

| Resource | What it is | Runs again when |
|----------|------------|-----------------|
| `binary` | `go build` of the command into `.humagen-dev/bin` | a watched Go file changes |
| `doctor` | Versions, addresses, where state is kept, `humagen config check` on the spec, credentials not set | a spec file or the env file changes; the API waits for it at start |
| `postgres` | Postgres 17 in Docker on `127.0.0.1:5434`, volume `NAME-dev_postgres-data` | never; `tilt down` stops it and keeps the volume |
| `api` | `humagen serve` on `127.0.0.1:8080`; ready when `/healthz` answers | the binary or a spec file (branding, agents) changes |
| `console-deps` | `pnpm install --frozen-lockfile` | the lockfile changes |
| `console` | The Vite dev server on `127.0.0.1:5173`, proxying `/api` and `/auth` to the API | never: Vite reloads the page itself, and the API keeps running |

Tilt is the only supervisor: it starts, restarts and stops each process.
[`run.sh`](../../deploy/tilt/dev/run.sh) checks the process's port first, so a
port another process holds stops the start with that process named instead of
a bind error, waits for the database, then becomes the process (`exec`), so the
signal Tilt sends reaches it and nothing is left holding the port. Agents'
files and published files go to `.humagen-dev/workspace`, so they are kept too.

A changed spec restarts the API and reruns `doctor` together. A spec that does
not check fails both, with the same error, and the API starts again once the
file is fixed. Branding is read at start, so a branding change shows after the
restart and a reload of the page.

## In a project built on humagen

A project that calls `humagen.Main` with its own options, and has its own spec,
loads the extension pinned to the release its `go.mod` requires.
[`examples/tilt/Tiltfile`](../../examples/tilt/Tiltfile) is a copy to start from:

```python
v1alpha1.extension_repo(name = 'humagen', url = 'https://github.com/urmzd/humagen', ref = 'v0.38.0')
v1alpha1.extension(name = 'humagen-dev', repo_name = 'humagen', repo_path = 'deploy/tilt/dev')
load('ext://humagen-dev', 'humagen_dev')

humagen_dev(
    name = 'ignite',               # its own database volume
    go_package = './cmd/ignite',   # its own command
    specs = ['config'],            # its branding and agents, watched
    env_file = '.env.dev',         # model keys, from a file git ignores
    credentials = ['ANTHROPIC_API_KEY'],
)
```

`ref` is a humagen release that has `deploy/tilt/dev`. The binary is built
from the project's `go.mod`; the console runs from the extension's checkout, so
`doctor` warns when the two versions differ. While changing humagen beside the
project, load from a local checkout instead and the console follows it:
`load('../humagen/deploy/tilt/dev/Tiltfile', 'humagen_dev')`. Add
`.humagen-dev/` and the env file to the project's `.gitignore`.

## Options

Relative paths are from the Tiltfile that calls `humagen_dev`.

| Option | Default | Meaning |
|--------|---------|---------|
| `name` | `humagen` | Names the database's Docker project and volume |
| `specs` | none | Spec files or directories passed as `-f`, watched |
| `agent_specs` | none | Agent JSON files passed as `-agent-specs`, watched |
| `go_dir` | the calling Tiltfile's directory | The Go module the binary is built in |
| `go_package` | `./cmd/humagen` | The main package |
| `go_watch` | humagen's Go sources, else `go_dir` | What rebuilds the binary |
| `humagen_dir` | the checkout the extension is in | Where the console runs from |
| `port`, `console_port` | `8080`, `5173` | Loopback ports |
| `console` | `True` | `False` runs the API alone |
| `auth` | `dev` | `-auth`: `dev` signs everyone in as admin; `mock` offers personas from a provider on `127.0.0.1:8081` |
| `seed` | on with no spec | The example agents on the offline model |
| `state` | `postgres` | `postgres` keeps conversations; `memory` does not; `None` leaves it to the spec |
| `postgres_url` | none | A database of your own instead of the one in Docker |
| `postgres_port` | `5434` | The Docker database's loopback port |
| `data_dir` | `.humagen-dev` | The binary, and agents' and published files |
| `env_file`, `env` | none | Variables for every process; `env` wins |
| `credentials` | none | Variables the project needs; `doctor` lists those not set |
| `strict_credentials` | `False` | `True` holds the API back while one is missing |
| `extra_args` | none | More `humagen serve` flags; they win over the spec |

## What has been checked

The Tiltfiles were run with Tilt's functions stubbed out, to check the commands
and dependencies they produce for the default, `--memory --mock` and
`--spec ... --no-console`. The commands themselves were run: the build, `doctor`
on no spec, on a spec with unset keys (fatal only with `strict_credentials`)
and on a spec that does not check (fatal); the API and console through
`run.sh`, the console's proxy answering `/api/me`; a second start on a held
port refused with the holder named; and both stopped by `SIGTERM` with their
ports free and no child left. Not run here: `tilt up` itself, and the Docker
database (no Docker daemon), so conversations surviving a restart rests on the
Postgres stores' own tests and on seeding being idempotent.
