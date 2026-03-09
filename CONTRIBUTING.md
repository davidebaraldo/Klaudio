# Contributing to Klaudio

Thank you for your interest in contributing to Klaudio! This document provides guidelines for contributing to the project.

## Getting Started

### Prerequisites

- Go 1.22+
- Docker Engine
- Node.js 20+
- A Claude Code Max account (for testing agent execution)

### Setup

```bash
# Clone the repository
git clone https://github.com/klaudio-ai/klaudio.git
cd klaudio

# Install Go dependencies
go mod tidy

# Build the agent Docker image
make docker-build

# Build and run the backend
make run

# In a separate terminal, start the web UI
cd web
npm install
npm run dev
```

## Development Guidelines

### Code Style

**Go backend:**
- Follow standard Go conventions (`gofmt`, `go vet`)
- All I/O functions must accept `context.Context` as the first parameter
- Wrap errors with context: `fmt.Errorf("doing X: %w", err)`
- Use `log/slog` for structured logging
- No global state — pass dependencies via structs
- No CGO — we use modernc.org/sqlite (pure Go)
- Hand-written SQL queries in `internal/db/queries.go`

**Frontend:**
- SvelteKit 2 with Svelte 5 runes syntax
- Tailwind CSS for styling
- TypeScript for type safety

### Project Structure

```
internal/
  api/       # HTTP handlers (add new endpoints here)
  agent/     # Agent pool management
  config/    # Configuration
  db/        # Database queries and models
  docker/    # Docker container management
  files/     # File operations
  repo/      # Git and platform integrations
  state/     # Checkpoint and state management
  stream/    # Real-time streaming
  task/      # Core orchestration logic
```

### Database Changes

1. Create a new migration file in `migrations/` (e.g., `006_your_feature.sql`)
2. Add corresponding model structs in `internal/db/models.go`
3. Add query methods in `internal/db/queries.go`
4. Migrations run automatically on server startup

### Adding API Endpoints

1. Add the handler function in the appropriate file under `internal/api/`
2. Register the route in `internal/api/router.go`

## Making Changes

### Branch Naming

- `feature/description` — New features
- `fix/description` — Bug fixes
- `docs/description` — Documentation changes
- `refactor/description` — Code refactoring

### Commit Messages

Use clear, concise commit messages:

```
feat: add GitHub PR creation support
fix: resolve WebSocket reconnection on network change
docs: update API spec with new team template endpoints
refactor: extract platform interface from bitbucket client
```

### Pull Requests

1. Create a branch from `main`
2. Make your changes
3. Ensure `make build` succeeds
4. Ensure `make test` passes
5. Update documentation if needed
6. Submit a PR with a clear description of the changes

## Areas for Contribution

### Good First Issues

- Add unit tests for existing packages
- Improve error messages and logging
- Add input validation to API endpoints
- Fix UI bugs or improve styling

### Larger Features

- **Phase 7: Multi-platform Git** — GitHub and GitLab integration (see [ROADMAP.md](ROADMAP.md))
- **UI: Agent grid view** — Split-screen terminal view for multiple agents
- **UI: Dependency graph** — Visual DAG of subtask dependencies
- **E2E tests** — Automated integration tests

### Documentation

- Improve API docs with more examples
- Add architecture decision records (ADRs)
- Write user guides and tutorials

## Reporting Issues

When reporting bugs, please include:
- Go version (`go version`)
- Docker version (`docker --version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant log output

## Code of Conduct

Be respectful, inclusive, and constructive. We're all here to build something great together.

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
