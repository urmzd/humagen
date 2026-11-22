# CI and release

Two workflows check and ship Humagen: CI (`.github/workflows/ci.yml`) on
every pull request and push to main, and Release
(`.github/workflows/release.yml`) on every push to main.

## The Go tests

The Go tests run with `-race` against a pgvector Postgres, in three shards
that run at the same time. Each shard has its own runner and its own
Postgres.

| Shard | Packages |
|-------|----------|
| `Test (platform)` | `internal/platform` |
| `Test (httpapi)` | `internal/httpapi` |
| `Test (rest)` | Every other package that `go list ./...` finds |

A new package goes into `rest` with no change to the workflow. A package
gets 20 minutes (`-timeout 20m`).

The job named **Test** needs the three shards and is the one check to read.
It fails when:

1. A shard did not pass.
2. A package from `go list ./...` ran in no shard, or in more than one.
3. A Postgres suite skipped in any shard. A suite skips when its variable
   (`HUMAGEN_POSTGRES_TEST_URL`, `FLOW_POSTGRES_TEST_URL`) is unset, so a
   renamed variable would otherwise pass in silence.

Test also records the tree the run checked out as the `tested-tree`
artifact. On a pull request that tree is the head merged into the base.

## Release

```
push to main
  -> tested: a successful CI run on this exact tree?
       yes -> release
       no  -> ci (the whole CI, again) -> release
  -> release: urmzd/sr tags the version
  -> image: build once, push to Artifact Registry
```

**The rule: a tree is never tagged before CI has passed on that tree.**
The `tested` job compares the tree of the pushed commit
(`git rev-parse HEAD^{tree}`) with the `tested-tree` of successful CI runs
on:

1. The pushed commit itself, from a push or a manual run.
2. The head of the pull request that the pushed commit merged.

A match skips the second CI run. A pull request run matches only when main
did not move between that run and the merge. If main moved, for example by
another merge or a release commit, the trees differ and CI runs again
before the tag. To skip the second run, update the branch with main and let
its CI pass before you merge. If the lookup fails, CI runs.

## The release image

When sr cuts a release, the Image job builds the image from the release tag
and pushes it to the Artifact Registry repository that the GKE deploy pulls
from:

```
us-central1-docker.pkg.dev/gen-lang-client-0907526271/humagen/humagen:<tag>
us-central1-docker.pkg.dev/gen-lang-client-0907526271/humagen/humagen:<release commit SHA>
```

It builds with buildx and the GitHub Actions cache, and passes
`VERSION=<tag>` and `COMMIT=<SHA>`. A tag is written once. The deploy's
build from source may have pushed one of the two tags first. If so, the job
points the missing tag at that image and builds nothing, so a running pod's
tag keeps its digest.

The job signs in through Workload Identity Federation, with no stored key.
The `github-humagen` provider in urmzd/infra (`gcp/humagen/ci.tf`) accepts
only this workflow on main of this repository. Its service account,
`humagen-release`, can write to the `humagen` repository and nothing else.

The deploy in urmzd/infra (`gcp-humagen.yml`, the Images job) builds
`humagen:<humagen_ref>` only when that tag is missing. Set `humagen_ref` to
a release tag, or to a release commit's full SHA, and the deploy uses the
pushed image. Any other ref is built from source there, as before.
