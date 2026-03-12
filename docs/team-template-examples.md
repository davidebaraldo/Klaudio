# Team Template Examples

Ready-to-use team templates for common development scenarios. Each example includes the `curl` command to create it via the API.

> **Collaborative vs Sequential**: Use `collaborative` when agents work on independent parts of the same codebase simultaneously (the manager coordinates). Use `sequential` when subtasks have strict dependencies and must run in order.

---

## 1. Full-Stack Feature Team (Collaborative)

A classic 3-agent team for building a full-stack feature. The manager ensures the backend API contract matches what the frontend expects, and that tests cover both.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Full-Stack Feature",
    "description": "Backend + Frontend + Tests working in parallel with manager coordination",
    "max_agents": 4,
    "review": true,
    "mode": "collaborative",
    "roles": [
      {
        "name": "backend",
        "description": "Implements API endpoints, database queries, and business logic",
        "prompt_hint": "You are the backend developer. Focus on Go code: handlers, services, database queries, and models. Define clear API contracts (request/response JSON schemas) early so the frontend developer can work in parallel. Write clean, testable code with proper error handling.",
        "max_instances": 1
      },
      {
        "name": "frontend",
        "description": "Implements UI components, pages, and API integration",
        "prompt_hint": "You are the frontend developer. Focus on SvelteKit: components, pages, stores, and API client calls. Follow the API contracts defined by the backend developer. Use Tailwind for styling. Implement proper loading states and error handling.",
        "max_instances": 1
      },
      {
        "name": "tester",
        "description": "Writes integration tests and E2E tests",
        "prompt_hint": "You are the test engineer. Write Go integration tests for the API endpoints and, if applicable, frontend component tests. Wait for the backend and frontend to define their interfaces before writing tests against them. Focus on happy paths, edge cases, and error scenarios.",
        "max_instances": 1
      }
    ]
  }'
```

**How the manager uses fix rounds:**
1. Workers implement backend API, frontend UI, and tests in parallel
2. Workers send `[WORK_DONE]` — manager inspects each output
3. Manager runs `go build ./...` and `go test ./...` to verify
4. If the frontend is calling the wrong endpoint shape, manager sends `[CONTINUE_WORK]` to the frontend worker with the correct API contract
5. If tests fail, manager sends `[CONTINUE_WORK]` to the tester with the error output
6. Once all pass, manager sends `[WORKER_APPROVED]` to each and exits

---

## 2. Microservice Scaffold (Collaborative)

For bootstrapping multiple services at once — e.g., spinning up an API gateway, a user service, and a notification service. Each agent gets its own service directory.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Microservice Scaffold",
    "description": "Multiple services built in parallel with shared contracts",
    "max_agents": 5,
    "review": true,
    "mode": "collaborative",
    "roles": [
      {
        "name": "developer",
        "description": "Implements a single microservice",
        "prompt_hint": "You are responsible for a single microservice. Create the full structure: main.go, handlers, models, Dockerfile, and a README. Follow the shared API contracts from the manager directives. Do NOT modify files outside your assigned service directory.",
        "max_instances": 4
      }
    ]
  }'
```

**How the manager uses fix rounds:**
1. Manager writes directives defining the shared protobuf/JSON contracts between services
2. Each developer builds their service independently
3. Manager verifies that service A's client matches service B's API
4. If there's a contract mismatch, `[CONTINUE_WORK]` to the offending service with the correct schema

---

## 3. Bug Fix Squad (Collaborative)

Multiple developers fixing different bugs from a backlog in parallel. The manager triages and assigns, then reviews each fix.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Bug Fix Squad",
    "description": "Parallel bug fixing with manager triage and review",
    "max_agents": 4,
    "review": false,
    "mode": "collaborative",
    "roles": [
      {
        "name": "developer",
        "description": "Fixes assigned bugs",
        "prompt_hint": "You are a bug fixer. Follow the manager directives to understand which bug you are assigned. Investigate the root cause, implement the fix, and write a regression test. Keep your changes minimal and focused — do not refactor unrelated code.",
        "max_instances": 3
      }
    ]
  }'
```

**How the manager uses fix rounds:**
- Manager assigns each developer a different bug via directives
- When a developer sends `[WORK_DONE]`, manager reads the diff and runs tests
- If the fix introduces a new issue or doesn't fully resolve the bug, `[CONTINUE_WORK]` with specifics
- `review: false` because the manager is already doing the review inline

---

## 4. Refactoring Pipeline (Sequential)

For large refactors that must happen in a specific order — e.g., extract interface first, then update all callers, then clean up.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Refactoring Pipeline",
    "description": "Step-by-step refactoring with dependency chain",
    "max_agents": 3,
    "review": true,
    "mode": "sequential",
    "roles": [
      {
        "name": "architect",
        "description": "Defines interfaces and abstractions",
        "prompt_hint": "You are the architect. Your job is to define the new interfaces, extract types, and create the abstraction layer. Do NOT update callers — that is the next agent job. Leave clear documentation in the code about the new interface contract.",
        "max_instances": 1
      },
      {
        "name": "developer",
        "description": "Updates callers and implementations",
        "prompt_hint": "You are the implementation developer. Read the context from the previous agent (the architect) to understand the new interfaces. Update all callers and concrete implementations to use the new abstractions. Run tests after each change.",
        "max_instances": 1
      },
      {
        "name": "developer",
        "description": "Cleanup and final tests",
        "prompt_hint": "You are the cleanup developer. Remove dead code left behind by the refactor, update imports, and ensure all tests pass. Run the full test suite and fix any failures. Do not add new features — only clean up.",
        "max_instances": 1,
        "run_last": true
      }
    ]
  }'
```

---

## 5. API + SDK Team (Collaborative)

Building an API and its client SDK simultaneously. The manager ensures the SDK always matches the API.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "API + SDK",
    "description": "API implementation and client SDK built in parallel",
    "max_agents": 3,
    "review": true,
    "mode": "collaborative",
    "roles": [
      {
        "name": "developer",
        "description": "Implements the API server",
        "prompt_hint": "You are the API developer. Implement the REST API endpoints with proper request validation, error responses, and documentation comments. Broadcast the final OpenAPI-style contract (endpoint, method, request body, response body) for each endpoint you create.",
        "max_instances": 1
      },
      {
        "name": "developer",
        "description": "Implements the client SDK",
        "prompt_hint": "You are the SDK developer. Build a typed client library that wraps the API. Read the API contract from the manager directives and from broadcast messages. Each API endpoint should have a corresponding SDK method with proper types, error handling, and retry logic.",
        "max_instances": 1
      }
    ]
  }'
```

**How the manager uses fix rounds:**
1. Manager defines the API contract in directives
2. API developer builds endpoints, SDK developer builds client methods
3. Manager compares the actual API responses with what the SDK expects
4. `[CONTINUE_WORK]` to fix any contract drift on either side

---

## 6. Documentation Team (Collaborative)

For generating comprehensive docs — API reference, user guide, and examples — in parallel.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Documentation Team",
    "description": "Parallel documentation writing with consistency review",
    "max_agents": 4,
    "review": false,
    "mode": "collaborative",
    "roles": [
      {
        "name": "developer",
        "description": "Writes a section of documentation",
        "prompt_hint": "You are a technical writer. Write clear, concise documentation with code examples. Follow the style guide from the manager directives (tone, heading levels, example format). Cross-reference other sections where appropriate but do NOT write content assigned to other writers.",
        "max_instances": 3
      }
    ]
  }'
```

**How the manager uses fix rounds:**
- Manager assigns each writer a section (API reference, getting started, advanced usage)
- Reviews for consistency in tone, formatting, and cross-references
- `[CONTINUE_WORK]` if a section contradicts another or uses inconsistent terminology

---

## 7. Migration Team (Sequential)

Database migration + code update + data backfill, each step depending on the previous.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Migration Pipeline",
    "description": "Database migration, code update, and data backfill in sequence",
    "max_agents": 3,
    "review": true,
    "mode": "sequential",
    "roles": [
      {
        "name": "developer",
        "description": "Creates database migration files",
        "prompt_hint": "You are the migration developer. Write the SQL migration files (up and down) for the schema changes. Test that migrations apply and rollback cleanly. Document any breaking changes in the context file for the next agent.",
        "max_instances": 1
      },
      {
        "name": "developer",
        "description": "Updates application code for new schema",
        "prompt_hint": "You are the application developer. Read the migration context from the previous agent to understand the schema changes. Update all models, queries, and handlers to work with the new schema. Ensure backward compatibility where noted.",
        "max_instances": 1
      },
      {
        "name": "developer",
        "description": "Writes data backfill scripts and tests",
        "prompt_hint": "You are the data engineer. Write a backfill script that migrates existing data to the new schema format. Include validation, dry-run mode, and progress logging. Write integration tests that verify the backfill produces correct results.",
        "max_instances": 1,
        "run_last": true
      }
    ]
  }'
```

---

## 8. Solo + Reviewer (Sequential)

The simplest useful template: one developer does the work, then a reviewer checks it.

```bash
curl -s -X POST http://localhost:8080/api/team-templates \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Dev + Review",
    "description": "Single developer with code review",
    "max_agents": 2,
    "review": true,
    "mode": "sequential",
    "roles": [
      {
        "name": "developer",
        "description": "Implements the feature or fix",
        "prompt_hint": "You are the developer. Implement the requested changes with clean code, proper error handling, and tests. Write a summary of your changes in the context file.",
        "max_instances": 1
      },
      {
        "name": "reviewer",
        "description": "Reviews the implementation",
        "prompt_hint": "You are the code reviewer. Read the developer context and review all changed files. Check for bugs, security issues, performance problems, and code style. If you find issues, fix them directly rather than just reporting them.",
        "max_instances": 1,
        "run_last": true
      }
    ]
  }'
```

---

## Quick Reference

| Template | Mode | Agents | Best for |
|----------|------|--------|----------|
| Full-Stack Feature | collaborative | 3+mgr | New features spanning backend + frontend |
| Microservice Scaffold | collaborative | 4+mgr | Bootstrapping multiple services |
| Bug Fix Squad | collaborative | 3+mgr | Parallel bug fixing from a backlog |
| Refactoring Pipeline | sequential | 3 | Step-by-step large refactors |
| API + SDK | collaborative | 2+mgr | Building an API and its client in sync |
| Documentation Team | collaborative | 3+mgr | Parallel doc writing with consistency |
| Migration Pipeline | sequential | 3 | DB migration + code + backfill |
| Dev + Review | sequential | 2 | Simple feature with code review |
