#!/bin/sh
# Put text files into a conversation's working directory, and fetch what an
# agent wrote there, through the files API (PUT and GET
# /api/threads/{id}/files/content). The records, finance-exports and
# travel-refund examples use it. The API takes UTF-8 text only, at most 2 MiB
# a file; a conversation holds at most 200 files and 20 MiB.
#
# usage:
#   conversation-files.sh new [TITLE]             start a conversation that no agent answers yet; prints its id
#   conversation-files.sh put THREAD FILE...      save each file under its own base name
#   conversation-files.sh get THREAD DIR PATH...  save each conversation file PATH under DIR/PATH
#
# HUMAGEN_URL is the server (default http://127.0.0.1:8080). A server with
# dev sign-in needs no token; for any other, set HUMAGEN_TOKEN.
set -eu

url=${HUMAGEN_URL:-http://127.0.0.1:8080}

call() {
  method=$1
  path=$2
  shift 2
  if [ -n "${HUMAGEN_TOKEN:-}" ]; then
    curl -fsS -X "$method" -H "Authorization: Bearer $HUMAGEN_TOKEN" -H 'Content-Type: application/json' "$@" "$url$path"
  else
    curl -fsS -X "$method" -H 'Content-Type: application/json' "$@" "$url$path"
  fi
}

usage() {
  printf '%s\n' 'usage: conversation-files.sh new [TITLE] | put THREAD FILE... | get THREAD DIR PATH...' >&2
  exit 2
}

[ "$#" -ge 1 ] || usage
command=$1
shift
case "$command" in
  new)
    title=${1:-Files}
    python3 -I -c 'import json, sys; print(json.dumps({"title": sys.argv[1], "text": "Files for this conversation.", "post_only": True}))' "$title" |
      call POST /api/threads --data-binary @- |
      python3 -I -c 'import json, sys; print(json.load(sys.stdin)["thread"]["id"])'
    ;;
  put)
    [ "$#" -ge 2 ] || usage
    thread=$1
    shift
    for file in "$@"; do
      python3 -I -c 'import json, os, sys
with open(sys.argv[1], encoding="utf-8") as f:
    print(json.dumps({"path": os.path.basename(sys.argv[1]), "content": f.read()}))' "$file" |
        call PUT "/api/threads/$thread/files/content" --data-binary @- >/dev/null
      printf 'put %s\n' "$(basename "$file")"
    done
    ;;
  get)
    [ "$#" -ge 3 ] || usage
    thread=$1
    dir=$2
    shift 2
    for path in "$@"; do
      mkdir -p "$dir/$(dirname "$path")"
      query=$(python3 -I -c 'import sys, urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$path")
      call GET "/api/threads/$thread/files/content?path=$query" |
        python3 -I -c 'import json, sys
sys.stdout.write(json.load(sys.stdin)["content"])' >"$dir/$path"
      printf 'got %s\n' "$path"
    done
    ;;
  *) usage ;;
esac
