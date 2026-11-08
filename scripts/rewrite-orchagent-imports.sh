#!/bin/sh
# Rewrites Go imports of github.com/urmzd/orchagent to their new homes, so a
# branch started before humagen moved off orchagent builds on saige alone.
# Run it from the repository root on your branch, after rebasing onto a main
# that already has the move (go.mod no longer requires orchagent):
#
#   scripts/rewrite-orchagent-imports.sh          # every .go file in the repo
#   scripts/rewrite-orchagent-imports.sh pkg/x    # only under these paths
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
#   github.PrepareReview(r, files, allow)         -> github.PrepareReview(r, files)
#
# It does not touch code that used orchagent's flow.App (flow.New, Ask,
# Reply, the session handle) or its knowledge store: those did not move, and
# the build names each use left. It prints every file it changed and every
# orchagent reference still left. It edits files in place: commit first.
set -eu

if ! command -v perl >/dev/null 2>&1; then
	echo "rewrite-orchagent-imports: perl is required" >&2
	exit 2
fi

[ "$#" -gt 0 ] || set -- .

files=$(find "$@" -name '*.go' -not -path '*/.worktrees/*' -not -path '*/node_modules/*' -not -path '*/.git/*' -print | xargs grep -l 'github.com/urmzd/orchagent' 2>/dev/null || true)
if [ -z "$files" ]; then
	echo "rewrite-orchagent-imports: no file imports orchagent"
	exit 0
fi

O='github\.com/urmzd/orchagent'
for f in $files; do
	perl -0pi -e '
		my $O = q{github\.com/urmzd/orchagent};
		# Alias packages: keep the local name the code already uses.
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/turn/native/saigetypes"}{$1 . ($2 // "saigetypes") . " \"github.com/urmzd/saige/agent/types\""}gme;
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/turn/native/ragtypes"}{$1 . ($2 // "ragtypes") . " \"github.com/urmzd/saige/rag/types\""}gme;
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/turn/native/agenttest"}{$1 . (defined $2 ? "$2 " : "") . "\"github.com/urmzd/saige/agent/agenttest\""}gme;
		# Packages that moved into humagen.
		s{"$O/flow/runschema"}{"github.com/urmzd/humagen/internal/runschema"}g;
		s{"$O/flow/toolprovider(/toolprovidertest)?"}{"github.com/urmzd/humagen/internal/toolprovider$1"}g;
		s{"$O/flow/mcptools"}{"github.com/urmzd/humagen/internal/mcptools"}g;
		s{"$O/flow/internal/legacy"}{"github.com/urmzd/humagen/internal/legacy"}g;
		s{^(\s*)(?:(\w+)\s+)?"$O/flow/knowledge"}{$1 . ($2 // "knowledge") . " \"github.com/urmzd/humagen/pkg/dense\""}gme;
		# Everything else keeps its path under saige.
		s{"$O/(durable(?:/[^"]*)?|sandbox/[^"]*|flow/[^"]*)"}{"github.com/urmzd/saige/$1"}g;
		# Agents never approve: PrepareReview lost its approval argument.
		s{\bPrepareReview\(([^(),]+,[^(),]+),\s*(?:true|false)\s*\)}{PrepareReview($1)}g;
	' "$f"
	echo "rewrote $f"
done

gofmt -w $files

left=$(printf '%s\n' $files | xargs grep -Hn "$O" 2>/dev/null || true)
if [ -n "$left" ]; then
	echo
	echo "rewrite-orchagent-imports: still names orchagent (rewrite by hand):"
	printf '%s\n' "$left"
	exit 1
fi
