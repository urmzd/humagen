# Providers

warp runs agents on model providers and gives them tools from MCP tool
providers. Both are declared in one JSON file, `providers.json`, so the common
cases need no Go. `warp start` and `warp setup` write it for you;
`warp serve -providers FILE` reads one you wrote.

```json
{
  "models": [
    {"name": "anthropic", "kind": "anthropic", "api_key_ref": "env:ANTHROPIC_API_KEY"},
    {"name": "openai", "api_key_ref": "op://Private/OpenAI/credential"},
    {"name": "ollama", "base_url": "http://127.0.0.1:11434/v1"},
    {"name": "vllm", "base_url": "http://10.0.0.5:8000/v1", "api_key_ref": "keychain:warp/vllm"}
  ],
  "tool_providers": [
    {
      "name": "docs", "transport": "http", "endpoint": "https://mcp.example.com",
      "credential_ref": "env:DOCS_MCP_TOKEN",
      "tools": [{"name": "search", "schema_hash": "9f2c...", "class": "read"}]
    }
  ]
}
```

An agent selects a model provider by `name` as its spec's `provider`, and a
tool provider with an `mcp:<name>` (read tools) or `mcp:<name>:write` grant.

## Keys are referenced, never stored

A key is never written in the file, in a flag or in a store. An entry names
where it is kept, and warp reads it each time an agent is built or a tool
session opens, so rotating a key is changing it where it lives.

| Reference | Read with |
|-----------|-----------|
| `env:NAME` | this process's environment |
| `keychain:SERVICE` or `keychain:SERVICE/ACCOUNT` | macOS `security`, Linux `secret-tool` |
| `op://VAULT/ITEM/FIELD` | 1Password `op read` |

Anything else is refused at startup, including a pasted key.

## Models

`kind` is `openai-compatible` (the default) or `anthropic`. `base_url` empty is
the vendor's own API, which needs a key; a local server usually needs none.
`api_key_env: "NAME"` is accepted as fiber spells `api_key_ref: "env:NAME"`, so a
file written for fiber reads the same here.

## Tool providers

`transport` is `http` (with an `https://` `endpoint`) or `stdio` (with a command
and `args`). Tools are accepted only as pinned: each listed tool must carry the
`schema_hash` of the schema you reviewed. A tool the server adds or changes
stays pending, and warp logs every pending tool with its hash at startup, which
is how you find the hash to pin. A class left out is `write`.

## In Go

A program that needs more than the file imports `warpapp` and passes options
(`WithModelProvider`, `WithToolProvider`, `WithToolSource`, `WithConnector`,
`WithStore`, `WithBlobStore`, `WithPlatform`). Options and the file combine; a
name declared by both is an error rather than an override.
