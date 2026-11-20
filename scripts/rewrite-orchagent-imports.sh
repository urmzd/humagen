#!/bin/sh
# Rewrites Go imports of github.com/urmzd/orchagent to their new homes, so a
# branch started before humagen moved off orchagent builds on saige alone.
# Run it from the repository root on your branch, after merging a main that
# already has the move (go.mod no longer requires orchagent):
#
#   scripts/rewrite-orchagent-imports.sh          # every .go file in the repo
#   scripts/rewrite-orchagent-imports.sh pkg/x    # only under these paths
#
# It is safe to run twice: a file with nothing left to rewrite is not touched.
# It prints every file it changed, then every orchagent reference it left
# (in Go and in any other file under the paths), and exits 1 if any is left.
# It edits files in place, so commit first.
#
# What moves where:
#   orchagent/durable/..., sandbox/..., flow/...  -> github.com/urmzd/saige/...
#   flow/turn/native/saigetypes                   -> saige/agent/types (imported as saigetypes)
#   flow/turn/native/ragtypes                     -> saige/rag/types   (imported as ragtypes)
#   flow/turn/native/agenttest                    -> saige/agent/agenttest
#   flow/runschema                                -> humagen/internal/runschema
#   flow/toolprovider[/toolprovidertest]          -> humagen/internal/toolprovider[/toolprovidertest]
#   flow/mcptools                                 -> humagen/internal/mcptools
#   flow/internal/legacy                          -> humagen/internal/legacy
#   flow/knowledge (its embedders)                -> humagen/pkg/dense
#   github.PrepareReview(r, files, false)         -> github.PrepareReview(r, files)
#
# What it leaves for you, because there is no one-to-one replacement:
#   - orchagent's flow package (flow.New, flow.App, Ask, Reply, the With...
#     options): compose runtime.New(durable.New(ledger, queue), resolver, bus,
#     ...) as internal/platform/platform.go does.
#   - flow/knowledge's store, chunker and retriever (saige rag/), and
#     ragtypes.AutoExtractor() / ragtypes.ErrNoText, which are
#     extractor.NewAuto().PartExtractor() / extractor.ErrNoText in
#     github.com/urmzd/saige/rag/extractor.
#   - flow/connectors/{author,jira,webhook}, flow/dispatch,
#     durable/adapters/redis/redisqueue, sandbox/runner/k8s, sandbox/tool/cli:
#     not carried over.
#   - PrepareReview called with anything but a literal false, and
#     github.Config.AllowReviewApproval[For]: agents never approve a review
#     now (an APPROVE review returns github.ErrNoApproval), so decide what the
#     caller should do instead.
#   - A park is one event for a whole batch of waiting tool calls: read
#     event.Parked.Questions and event.Resumed.Answers where code read one
#     question per park.
set -eu

if ! command -v perl >/dev/null 2>&1; then
	echo "rewrite-orchagent-imports: perl is required" >&2
	exit 2
fi

[ "$#" -gt 0 ] || set -- .

self=scripts/rewrite-orchagent-imports.sh
list=$(mktemp)
trap 'rm -f "$list" "$list.changed" "$list.fmt"' EXIT

# Go files that still import orchagent, one per line (paths may hold spaces).
find "$@" \( -name .git -o -name .worktrees -o -name node_modules \) -prune -o \
	-type f -name '*.go' -print0 |
	xargs -0 grep -l 'github\.com/urmzd/orchagent' 2>/dev/null >"$list" || true

: >"$list.changed"
: >"$list.fmt"
while IFS= read -r f; do
	[ -n "$f" ] || continue
	before=$(cksum <"$f")
	perl -0pi -e '
		my $O = q{github\.com/urmzd/orchagent};
		# Packages saige holds under another path: keep the local name the
		# code already uses.
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/turn/native/saigetypes"}{$1 . ($2 // "saigetypes") . " \"github.com/urmzd/saige/agent/types\""}gme;
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/turn/native/ragtypes"}{$1 . ($2 // "ragtypes") . " \"github.com/urmzd/saige/rag/types\""}gme;
		s{"$O/flow/turn/native/agenttest"}{"github.com/urmzd/saige/agent/agenttest"}g;
		# Packages that live in humagen now.
		s{"$O/flow/runschema"}{"github.com/urmzd/humagen/internal/runschema"}g;
		s{"$O/flow/toolprovider(/toolprovidertest)?"}{"\"github.com/urmzd/humagen/internal/toolprovider" . ($1 // "") . "\""}ge;
		s{"$O/flow/mcptools"}{"github.com/urmzd/humagen/internal/mcptools"}g;
		s{"$O/flow/internal/legacy"}{"github.com/urmzd/humagen/internal/legacy"}g;
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/knowledge"}{$1 . ($2 // "knowledge") . " \"github.com/urmzd/humagen/pkg/dense\""}gme;
		# Everything else under durable, sandbox and flow keeps its path,
		# except what saige did not carry: those stay named so the report
		# below lists them.
		s{"$O/((?:durable|sandbox|flow)/[^"]+|durable)"}{
			my $p = $1;
			$p =~ m{^(?:flow/connectors/(?:author|jira|webhook)|flow/dispatch|flow/knowledge|flow/internal|durable/adapters/redis|sandbox/runner/k8s|sandbox/tool/cli)(?:/|$)}
				? "\"github.com/urmzd/orchagent/$p\""
				: "\"github.com/urmzd/saige/$p\""
		}ge;
		# Agents never approve a review: PrepareReview lost its argument.
		s{\bPrepareReview\(([^(),]+,[^(),]+),\s*false\s*\)}{PrepareReview($1)}g;
	' "$f"
	if [ "$before" != "$(cksum <"$f")" ]; then
		printf '%s\n' "$f" >>"$list.changed"
		printf '%s\0' "$f" >>"$list.fmt"
	fi
done <"$list"

if [ -s "$list.fmt" ]; then
	xargs -0 gofmt -w <"$list.fmt"
fi

changed=$(wc -l <"$list.changed" | tr -d ' ')
if [ "$changed" -gt 0 ]; then
	sed 's/^/rewrote /' "$list.changed"
fi
echo "rewrite-orchagent-imports: rewrote $changed file(s)"

# Everything that still names orchagent under the paths: Go the rules above do
# not cover, and module files, scripts and workflows. Prose that describes
# the move (docs, changelogs) and this script are expected to name it.
left=$(find "$@" \( -name .git -o -name .worktrees -o -name node_modules -o -name docs \) -prune -o \
	-type f ! -name 'CHANGELOG*' ! -name '*.md' ! -path "*$self" -print0 |
	xargs -0 grep -InE 'urmzd/orchagent|PrepareReview\([^()]*,[^()]*,[^()]*\)|AllowReviewApproval' 2>/dev/null || true)
if [ -n "$left" ]; then
	echo
	echo "rewrite-orchagent-imports: left for you (see the header of $self):"
	printf '%s\n' "$left"
	exit 1
fi
echo "rewrite-orchagent-imports: nothing names orchagent"
