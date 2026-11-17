# Research current public sources

`examples/public-web/claude-code.yaml` connects Humagen to the signed-in Claude
Code program's WebSearch and WebFetch tools. It searches current public sources,
opens pages, and returns supporting URLs. It needs no separate search API key.
The installed program and account must support these tools.

## Start the researcher

Set `claude-code.dir` to an existing absolute work directory and sign in to
Claude Code on that host. Then start the example:

```sh
humagen start -f examples/public-web/claude-code.yaml
```

The model profile enables only WebSearch and WebFetch, preauthorizes those two
tool names, and ignores other MCP configurations. Program permissions govern
this host path. Humagen's native tool grants do not gate the program's tools.
The native execution sandbox does not confine the external program.

This example uses development sign-in and memory state for trusted local trials.
Every client reaching its API is an administrator. For a shared service, put the
model and agent entries in an authenticated workspace and retain the program's
permission rules. Retrieved text is evidence, not authority to change the task.

## Ask for traceable evidence

Name the question and the authoritative sites. Ask the researcher to search,
open the pages it relies on, and put a supporting URL beside each factual claim.
A search snippet alone does not establish what the current page says. Request
an unknown when a field is not in the retrieved material.

For example, find the current stable Go release on `go.dev`, open its download
feed, and report the version with the exact opened URL. Request a release date
only when an opened source supplies it. Read the retained run's tool events to
verify that both WebSearch and WebFetch executed; a plausible answer alone is
not connection proof.

## Actual acceptance and limits

A real Humagen HTTP run used both tools and opened `https://go.dev/dl/?mode=json`.
Its version matched a separate direct retrieval of the official feed. The answer
left the date unknown because that feed has no release-date field. This proves
the installed program's public-source path, not every search result or a
permanent release version. Search results can be stale; dates and claims still
need source checks. No provider credential was copied into the configuration.

The example has no authenticated browser tab, JavaScript interaction, screenshot
inspection, or PDF download workflow. Use the [browser worker](browser.md) when
those operations are needed. A failed fetch or a denied tool is a failed
retrieval, not evidence that the source confirms a claim.
