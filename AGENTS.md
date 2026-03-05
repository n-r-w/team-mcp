# Project Specific Rules and Information

NO BACKWARDS COMPATIBILITY! NO FALLBACK! NO DEPRECATIONS! JUST REMOVE OLD CODE/DOCUMENTATION AS NEEDED.
THIS IS A NEW PROJECT, NOT IN PRODUCTION YET. FEEL FREE TO MAKE BREAKING CHANGES AS NEEDED.

## Project Overview
Team MCP Server.

The agent orchestrator defines a channel-based interaction model, and sub-agents exchange messages only through it predictably and in isolation.

## MCP Tools
1. **desk_create:** Creates a collaboration desk. 
2. **desk_remove:** Removes a desk and all linked topics/messages from memory and disk. 
3. **topic_create:** Creates a topic in a desk.
4. **topic_list:** Lists ordered topic headers for a desk.
5. **message_create:** Creates a message in a topic.
6. **message_list:** Lists ordered message headers for a topic.
7. **message_get:** Retrieves the full message payload.

Full schema:
- `internal/server/consts.go`
- `internal/server/dto.go`

## Component map
1. `cmd/team-mcp`: App entrypoint and process lifecycle.
2. `internal/appinit`: Composition root.
3. `internal/server`: MCP tool layer.
4. `internal/usecase`: Core orchestration.
5. `internal/adapters/runstate`: In-memory run state management.
6. `internal/adapters/queue`: In-memory queue management.
7. `internal/adapters/filesystem`: Message persistence.
8. `internal/domain/coordination`: Shared domain objects.
9. `internal/config`: Configuration management.

## Tech stack
1. go 1.26
2. `github.com/modelcontextprotocol/go-sdk` for MCP server implementation
3. `github.com/stretchr/testify` for tests
4. `go.uber.org/mock` (no custom mocks, `//go:generate` directives in interface files)
5. `github.com/caarlos0/env/v11` for loading configuration from environment variables
6. `log/slog` for logging (must use structured logging with context). Use global logger with context, instead of passing logger instances around. E.g. `slog.DebugContext`.

## Instructions
1. DON'T edit AGENTS.md without DIRECT user request.
2. Generate new documentation in English unless user specifically requests another language.
3. When updating documents, the original language of the document must be used.
4. ALWAYS use English version of official sources.
5. Maintain consistency of environment variables between `.env.example`, `.env`, Taskfile.yml, scripts, code, and documentation.
6. DON'T use tables in user-facing markdown docs, use lists or sections instead.

## Coding rules
1. All interfaces MUST be prefixed with uppercase `I` letter
2. For single package:
    1) All interfaces should be in file `interfaces.go`
    2) Main package struct and its constructor should be in `service.go` or `client.go`
    3) All internal structs (except main service struct and DTOs) should be in models.go
    4) All DTO should be in dto.go (structs with tag `json`, `yaml`, etc.)
    5) All configuration related code should be in config.go (using `github.com/caarlos0/env/v11`)
    6) All internal errors should be in errors.go file
    7) All internal constants should be in consts.go file
    8) All mock generation commands should be in `interfaces.go`
3. ALL DTOs MUST be not exported.
4. Use `task lint` and `task test` to check code before completing changes.
5. Run `task fix` after making batch changes to improve code quality.

## Testing rules
1. Use `t.Context()` instead of `context.Background()`
2. Use `go.uber.org/mock` for mocks. Custom mocks are FORBIDDEN. Use `//go:generate` directives in interface files to generate mocks as needed.
3. Use `github.com/stretchr/testify`
4. Use `testify/suite`