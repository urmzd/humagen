# Research current public sources

`examples/public-web/humagen.yaml` gives a native agent, `researcher`, the
`web_search` and `web_fetch` tools. It searches current public sources, opens
the pages it relies on, and reports the supporting URL beside each claim. The
URLs are answer text the model wrote, not platform-verified citations.

Both tools run on the Humagen server, not in a sandbox, and need no account
or search key. `web_search` returns up to 10 results, each a title, an address
and a snippet, from DuckDuckGo's no-script page, which may refuse an automated
client; the tool then says so. `web_fetch` reads one public `http` or `https` page as text, at
most 40,000 characters; it refuses addresses inside a private network and
content that is not text, such as a PDF or an image. Every result is marked as
web content, not instructions.

Before #383 this example ran on the Claude Code program's own WebSearch and
WebFetch tools.

## Start the researcher

Set a provider key in the shell (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY` or
`GOOGLE_API_KEY`; the agent tries them in that order), then:

```sh
humagen config check -f examples/public-web/humagen.yaml
humagen start -f examples/public-web/humagen.yaml
```

The profile uses development sign-in and memory state for trusted local
trials: every client reaching its API is an administrator. For a shared
service, put the agent in an authenticated workspace. Retrieved text is
evidence, not authority to change the task.

## Ask for traceable evidence

Name the question and the authoritative sites. Ask the researcher to search,
open the pages it relies on, and put a supporting URL beside each factual
claim. A search snippet alone does not establish what the current page says.
Ask for an unknown when a field is not in the retrieved material.

For example, find the current stable Go release on `go.dev`, open its download
feed, and report the version with the exact opened URL. Ask for a release date
only when an opened source supplies it. Read the run's tool events to confirm
that `web_search` and `web_fetch` ran; a plausible answer alone is not proof.

## Limits

A native turn makes at most 10 rounds of tool calls, so a question that needs
many pages takes more than one turn. The researcher has no signed-in browser
tab, JavaScript, screenshot or PDF; use the [browser worker](browser.md) for
those. A failed fetch is a failed retrieval, not evidence that a source
confirms a claim.

The tools were checked against the live web on their own
(`HUMAGEN_WEB_LIVE=1 go test ./pkg/webtools -run TestLive`). This profile has
not been run with a live model; the earlier end-to-end check ran on the removed
Claude Code path.
