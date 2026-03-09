# Contributing to Klaudio

Thank you for your interest in contributing! Whether it's a bug report, feature request, documentation improvement, or code contribution — every bit helps.

## Getting Started

### Prerequisites

| Tool | Version | Notes |
|------|---------|-------|
| Go | 1.22+ | Backend |
| Docker Engine | Latest | Agent containers |
| Node.js | 20+ | Frontend build |
| Claude Code Max | — | Required for agent execution |

### Local Setup

```bash
# 1. Clone and enter the repository
git clone https://github.com/davidebaraldo/Klaudio.git
cd Klaudio

# 2. Install Go dependencies
go mod tidy

# 3. Build the agent Docker image
make docker-build

# 4. Start the backend
make dev

# 5. (Optional) Start the frontend dev server
cd web && npm install && npm run dev
```

The API runs at `http://localhost:8080`, the dev UI at `http://localhost:5173`.

---

## How to Contribute

### Reporting Bugs

Open an [issue](https://github.com/davidebaraldo/Klaudio/issues/new) with:

- Go/Docker/Node.js versions
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant log output (use code blocks)

### Suggesting Features

Start a [discussion](https://github.com/davidebaraldo/Klaudio/discussions) or open an issue tagged `enhancement`. Describe the use case, not just the solution.

### Submitting Code

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Ensure `make build` and `make test` pass
4. Submit a Pull Request with a clear description

---

## Code Style

### Go Backend

| Rule | Details |
|------|---------|
| Formatting | `gofmt` / `go vet` — enforced in CI |
| Context | All I/O functions accept `context.Context` as the first parameter |
| Errors | Always wrap: `fmt.Errorf("doing X: %w", err)` |
| Logging | `log/slog` — structured, never `fmt.Println` |
| State | No global state — dependencies passed via structs |
| CGO | **Never** — we use `modernc.org/sqlite` (pure Go) |
| SQL | Hand-written queries in `internal/db/queries.go` |

### Frontend

| Rule | Details |
|------|---------|
| Framework | SvelteKit 2 + Svelte 5 runes syntax |
| Styling | Tailwind CSS utility classes |
| Types | TypeScript everywhere |

---

## Branch & Commit Conventions

### Branch Naming

```
feature/short-description
fix/short-description
docs/short-description
refactor/short-description
```

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add GitHub PR creation support
fix: resolve WebSocket reconnection on network change
docs: update API reference with team template endpoints
refactor: extract platform interface from bitbucket client
```

---

## Where to Contribute

### Good First Issues

- Add unit tests for existing packages
- Improve error messages and logging
- Add input validation to API endpoints
- Fix UI bugs or improve styling
- Improve documentation

### Larger Features

Check the [Roadmap](ROADMAP.md) for planned work:

- **Phase 7** — GitHub and GitLab integration
- **Phase 8** — Service installation (Windows/Linux)
- **Agent grid UI** — Split-screen terminal for multiple agents
- **Dependency graph** — Visual DAG of subtask dependencies

### Adding Database Changes

1. Create a migration in `migrations/` (e.g., `006_your_feature.sql`)
2. Add model structs in `internal/db/models.go`
3. Add queries in `internal/db/queries.go`
4. Migrations run automatically on startup

### Adding API Endpoints

1. Add handler in the appropriate file under `internal/api/`
2. Register the route in `internal/api/router.go`

---

## Code of Conduct

Be respectful, inclusive, and constructive. We're building something great together.

## License

By contributing, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
