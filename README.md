# Team MCP Server

An MCP (Model Context Protocol) server that provides a structured collaboration space for multi-agent workflows.

The server exposes a desk-topic-message model so agents can coordinate work predictably and keep context in one place.

## Tools

### Coordination tools

- `desk_create` — Creates a collaboration desk and returns `desk_id`
- `desk_remove` — Removes a desk and all linked topics/messages from memory and disk
- `topic_create` — Creates a topic in a desk and returns `topic_id`
- `topic_list` — Lists topic headers for a desk in creation order
- `message_create` — Creates a message in a topic and returns `message_id`
- `message_list` — Lists message headers for a topic in creation order
- `message_get` — Returns full message payload by `message_id`

### 🚨 CRITICAL: Role and tool access policy

- The orchestrator can use all tools.
- Subagents should not have access to `desk_create`, `desk_remove`, and `topic_create`.
- Subagents should focus on topic/message-level work to avoid coordination conflicts.

## Installation

### Binary Releases

Pre-compiled binaries are published for multiple platforms:

- **Linux (AMD64)**
- **Linux (ARM64)**
- **macOS (Intel)**
- **macOS (Apple Silicon)**
- **Windows (AMD64)**

Download the latest release from [GitHub Releases](https://github.com/n-r-w/team-mcp/releases).

### Homebrew

```bash
brew install n-r-w/homebrew-tap/team-mcp
```

### Build from Source

```bash
go build -o team-mcp ./cmd/team-mcp
```

or use Task:

```bash
task build
```

## Environment variables

- `TEAM_MCP_MESSAGE_DIR` (optional, default: `<os-temp-dir>/team-mcp/messages`)
  * Absolute directory path where desk/message payloads are stored.
  * If empty, the server uses the OS temp directory.

- `TEAM_MCP_SESSION_TTL` (optional, default: `24h`)
  * TTL for desk sessions.
  * Must be at least `1m`.

- `TEAM_MCP_MAX_BUFFERED_MESSAGES` (optional, default: `10000`)
  * Maximum buffered headers/messages in memory.
  * Must be `>= 1`.

- `TEAM_MCP_MAX_ACTIVE_RUNS` (optional, default: `1000`)
  * Maximum number of active desks tracked in memory.
  * Must be `>= 1`.

- `TEAM_MCP_MAX_TITLE_LENGTH` (optional, default: `200`)
  * Maximum allowed title length for topics/messages (in runes).
  * Must be `>= 1`.

- `TEAM_MCP_LOG_LEVEL` (optional, default: `info`)
  * Log level for structured JSON logs.
  * Allowed values: `debug`, `info`, `warn`, `error`.

- `TEAM_MCP_LIFECYCLE_COLLECT_INTERVAL` (optional, default: `60s`)
  * Interval for runtime lifecycle cleanup of expired desks.
  * Must be greater than `0`.

## Client configuration examples

### Claude Code

```bash
claude mcp add -s user --transport stdio team /path/to/team-mcp
```

### VS Code, RooCode, etc.

```json
"team": {
  "command": "/path/to/team-mcp",
  "env": {
    "TEAM_MCP_LOG_LEVEL": "info"
  }
}
```

Notes:

- The `command` must point to the built executable (for this repo, `task build` produces `bin/team-mcp`).
- The server communicates over stdio; clients should use stdio transport.

## Operational notes

- Payloads are persisted on disk per desk/message; headers and indices are managed in memory.
- `desk_remove` performs synchronous cascade cleanup of in-memory state and persisted payloads.
- On shutdown, the server attempts to clean up all active desks.
