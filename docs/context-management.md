# Klaudio — Context Management & Architecture

## The Idea

Klaudio is an **AI agent orchestrator**. Its purpose is to take a complex software development task, break it down into subtasks, and have them executed by multiple Claude Code instances running in isolated Docker containers.

The core problem is: **how to give each agent the right context at the right time**, without agents stepping on each other's toes.

The solution is a multi-layered system that combines:
- **Enriched prompts** (dynamically built before each spawn)
- **Shared filesystem** (a workspace volume mounted into each container)
- **Database-backed messaging** (inter-agent communication via REST API)
- **Directive files** (manager→worker coordination via Markdown)

---

## Why a Layered Context System?

A naive approach would be to dump all context into a single giant prompt and let the agent figure it out. This breaks down quickly with multiple agents working on a real codebase. Klaudio uses layered context because each layer solves a different problem that the others can't:

### The Problem with a Single Layer

```mermaid
graph LR
    subgraph naive ["Single-layer approach"]
        style naive fill:#f8d7da,stroke:#dc3545,color:#333
        P["Giant prompt<br/>everything at once"] --> A1["Agent"]
    end
```

- **Prompt-only**: LLM context windows are finite. Stuffing the full codebase, all messages, and all coordination rules into one prompt would blow the token limit — and even if it fit, the agent would lose focus on what matters.
- **Filesystem-only**: Agents in different containers can't easily discover what other agents have done or are doing. There's no notification mechanism — just files sitting on disk.
- **Database-only**: Agents run Claude Code, which naturally reads and writes files. Forcing all communication through API calls adds friction and goes against how Claude Code works best.

### How the Layers Complement Each Other

```mermaid
graph TD
    subgraph L1 ["Layer 1 — Enriched Prompt"]
        style L1 fill:#d4edda,stroke:#28a745,color:#333
        P1["Task description"]
        P2["Role & instructions"]
        P3["Dependency summaries"]
        P4["Team composition"]
    end

    subgraph L2 ["Layer 2 — Filesystem"]
        style L2 fill:#cce5ff,stroke:#007bff,color:#333
        F1[".klaudio/directives/*.md"]
        F2[".klaudio/context/*.md"]
        F3["Shared workspace files"]
    end

    subgraph L3 ["Layer 3 — Database + API"]
        style L3 fill:#fff3cd,stroke:#ffc107,color:#333
        D1["Broadcast messages"]
        D2["System events"]
        D3["Status tracking"]
    end

    L1 -->|"sets the mission<br/>(what to do)"| Agent["Agent in Docker"]
    L2 -->|"provides the material<br/>(what to work with)"| Agent
    L3 -->|"enables coordination<br/>(what others are doing)"| Agent

    style Agent fill:#e2d5f1,stroke:#6f42c1,color:#333
```

| Layer | What it carries | Why it's the right medium | What would break without it |
|-------|----------------|--------------------------|---------------------------|
| **Prompt** | Mission, role, instructions, summaries of prior work | Arrives before the agent starts — sets intent and focus from the first token | Agents wouldn't know what to do or what already happened |
| **Filesystem** | Directives, completion context, actual code | Claude Code naturally reads/writes files — zero friction | Manager couldn't coordinate workers; sequential agents couldn't see prior results |
| **Database + API** | Real-time messages, system events, status | Works across container boundaries, supports polling | Agents in isolated containers would have no way to communicate during execution |

### The Key Insight

Each layer operates at a **different point in time** and serves a **different audience**:

```mermaid
graph LR
    subgraph before ["Before spawn"]
        style before fill:#d4edda,stroke:#28a745,color:#333
        PR["Prompt<br/>assembled by orchestrator"]
    end

    subgraph during ["During execution"]
        style during fill:#cce5ff,stroke:#007bff,color:#333
        FS["Filesystem<br/>read/written by agents"]
        API["API Messages<br/>polled by agents"]
    end

    subgraph after ["After completion"]
        style after fill:#fff3cd,stroke:#ffc107,color:#333
        CTX["Context files<br/>consumed by next agents"]
        DB["DB records<br/>displayed in UI"]
    end

    PR --> FS
    PR --> API
    FS --> CTX
    API --> DB

    style PR fill:#d4edda,stroke:#28a745,color:#333
    style FS fill:#cce5ff,stroke:#007bff,color:#333
    style API fill:#cce5ff,stroke:#007bff,color:#333
    style CTX fill:#fff3cd,stroke:#ffc107,color:#333
    style DB fill:#fff3cd,stroke:#ffc107,color:#333
```

- **Prompt** = what you know **before** work begins (static, curated, focused)
- **Filesystem** = what you produce and consume **during** work (natural for code agents)
- **Database** = what needs to cross container boundaries or persist for the **UI** (structured, queryable)

This separation means each agent gets a **focused, relevant** context window instead of an overwhelming dump, while still having access to everything it needs through the appropriate channel at the appropriate time.

---

## How It Works, Step by Step

### 1. The User Creates a Task

The user submits a prompt (e.g., "Add JWT authentication to the project") and optionally:
- Input files (uploaded via API)
- A Git repository to clone
- A team template (predefined roles)

### 2. The Planner Analyzes

A **planner** agent is launched in a Docker container with the workspace mounted **read-only**. It cannot modify anything — only analyze.

The planner receives in its prompt:
- The user's task description
- The list of input files
- Team template constraints (roles, max agents)
- Answers to previously asked questions (if any)

The planner can do one of two things:
1. **Ask clarifying questions** → questions are saved to the DB, the user answers via the UI, and the planner is re-run with the answers injected into its prompt
2. **Produce a plan** → a structured JSON with subtasks, dependencies, and files involved

### 3. The User Approves the Plan

The plan is displayed in the UI. The user can approve, modify, or reject it.

### 4. Execution: Two Modes

#### Sequential Mode (DAG)

Subtasks are executed respecting a dependency graph:

```mermaid
graph LR
    A[Auth middleware] --> C[Protected routes]
    B[DB schema] --> C
    C --> D[E2E tests]

    style A fill:#d4edda,stroke:#28a745,color:#333
    style B fill:#d4edda,stroke:#28a745,color:#333
    style C fill:#cce5ff,stroke:#007bff,color:#333
    style D fill:#fff3cd,stroke:#ffc107,color:#333
```

**Context flow:**
1. The orchestrator finds "ready" tasks (no pending dependencies)
2. For each one, it collects:
   - Context from completed dependencies (files at `.klaudio/context/{subtaskID}.md`)
   - Broadcast messages from other agents (from DB)
3. Builds an enriched prompt with all collected context
4. Spawns the Docker container
5. When the agent finishes, it saves a **completion summary** to:
   - `.klaudio/context/{subtaskID}.md` (filesystem, for subsequent agents)
   - `agent_messages` table (DB, for the UI and API queries)
6. Releases file locks and looks for the next ready task

**File locking:** each subtask declares which files it touches. The lock manager prevents two agents from modifying the same file concurrently. If a task is ready but its files are locked by another agent, it is deferred.

#### Collaborative Mode (Manager + Workers)

All agents work **in parallel**, coordinated by a manager:

```mermaid
graph TD
    M[Manager<br/>coordinate] -->|writes directives| W1[Worker 1<br/>backend]
    M -->|writes directives| W2[Worker 2<br/>frontend]
    M -->|writes directives| W3[Worker 3<br/>tests]
    W1 -.->|status via API| M
    W2 -.->|status via API| M
    W3 -.->|status via API| M

    style M fill:#e2d5f1,stroke:#6f42c1,color:#333
    style W1 fill:#d4edda,stroke:#28a745,color:#333
    style W2 fill:#cce5ff,stroke:#007bff,color:#333
    style W3 fill:#fff3cd,stroke:#ffc107,color:#333
```

**Phase 1 — Manager spawns first:**
- Receives the task prompt, team composition, and the API URL
- Writes directive files to `.klaudio/directives/`:
  - `coordination.md` → shared contracts, naming conventions, file ownership
  - `{subtaskID}.md` → specific instructions for each worker

**Phase 2 — Workers spawn immediately (in parallel):**
- Each worker starts but **waits** for its directive:
  ```bash
  while [ ! -f .klaudio/directives/{subtaskID}.md ]; do
    echo 'Waiting for manager directives...'
    sleep 3
  done
  ```
- Once the directive appears, the worker reads it and begins working

**Phase 3 — Work, review, and fix loops:**

Workers do **not** exit when they finish their work. Instead, each worker sends a `[WORK_DONE]` message and enters an **approval loop**, polling for the manager's response:

```mermaid
graph TD
    W[Worker completes work] -->|sends WORK_DONE| M[Manager reviews]
    M -->|WORKER_APPROVED| E[Worker exits]
    M -->|CONTINUE_WORK + instructions| F[Worker applies fix]
    F -->|sends WORK_DONE| M

    style W fill:#cce5ff,stroke:#007bff,color:#333
    style M fill:#e2d5f1,stroke:#6f42c1,color:#333
    style E fill:#d4edda,stroke:#28a745,color:#333
    style F fill:#fff3cd,stroke:#ffc107,color:#333
```

The manager stays alive and polls the API every 10–15 seconds. For each worker that reports `[WORK_DONE]`, the manager can:

- **`[WORKER_APPROVED]`** → the worker exits cleanly
- **`[CONTINUE_WORK]` + fix instructions** → the worker reads the instructions, applies the fix, sends `[WORK_DONE]` again, and waits for another review. This can repeat as many times as needed.

The manager also sees system messages (`[WORKER_COMPLETED]`, `[WORKER_FAILED]`) and can send additional guidance via broadcast messages at any time.

**Phase 4 — Completion and optional respawn:**

When all workers have exited (approved or otherwise), the orchestrator sends `[ALL_WORKERS_DONE]` to the manager. The manager then has two choices:

1. **Exit** → orchestration ends, optional reviewer runs
2. **Send `[RESPAWN_WORKERS]`** → the orchestrator respawns specific workers with new fix instructions

```mermaid
graph TD
    AWD["[ALL_WORKERS_DONE]"] --> R{Manager decides}
    R -->|satisfied| EXIT[Manager exits]
    R -->|needs fixes| RS["[RESPAWN_WORKERS]<br/>subtask-1: fix X<br/>subtask-2: fix Y"]
    RS --> SPAWN[Orchestrator respawns workers]
    SPAWN --> WORK[Workers execute fixes]
    WORK --> AWD

    EXIT --> REV[Reviewer<br/>code review]

    style AWD fill:#fff3cd,stroke:#ffc107,color:#333
    style R fill:#e2d5f1,stroke:#6f42c1,color:#333
    style EXIT fill:#d4edda,stroke:#28a745,color:#333
    style RS fill:#f8d7da,stroke:#dc3545,color:#333
    style SPAWN fill:#cce5ff,stroke:#007bff,color:#333
    style WORK fill:#cce5ff,stroke:#007bff,color:#333
    style REV fill:#d4edda,stroke:#28a745,color:#333
```

The respawn mechanism is a **fallback** for when a worker has already exited (crash, timeout) or needs a complete redo. The inline `CONTINUE_WORK` approach is preferred for small fixes because it preserves the worker's context and avoids the overhead of spawning a new container.

There is **no limit** on the number of fix rounds — the manager continues until satisfied.

Optionally, a **reviewer** agent performs a final code review after the manager exits.

---

## Communication Channels

| Channel | Medium | Direction | When |
|---------|--------|-----------|------|
| **Enriched prompt** | Env var `CLAUDE_PROMPT` | Orchestrator → Agent | Before spawn |
| **Directive files** | `.klaudio/directives/*.md` | Manager → Workers | Start of collaboration |
| **Context files** | `.klaudio/context/*.md` | Agent N → Agent N+1 | After completion (seq.) |
| **Broadcast messages** | DB `agent_messages` via REST API | Agent ↔ Agent | During execution |
| **Approval signals** | DB `agent_messages` via REST API | Manager → Worker | `[WORKER_APPROVED]`, `[CONTINUE_WORK]` |
| **Work done signals** | DB `agent_messages` via REST API | Worker → Manager | `[WORK_DONE]` + summary |
| **System messages** | DB `agent_messages` | Orchestrator → Manager | `[WORKER_COMPLETED]`, `[WORKER_FAILED]`, `[ALL_WORKERS_DONE]`, `[WORKERS_RESPAWNED]` |
| **Respawn requests** | DB `agent_messages` via REST API | Manager → Orchestrator | `[RESPAWN_WORKERS]` + subtask list |
| **Shared workspace** | Docker volume mount | All agents | Always |

---

## How Prompts Are Built

Prompts are not static. They are **dynamically assembled** by the orchestrator before each spawn, combining different blocks depending on the agent's role:

```mermaid
graph TD
    subgraph planner_prompt ["Planner Prompt"]
        style planner_prompt fill:#d4edda,stroke:#28a745,color:#333
        PP1["Planner template"] --> PP["Final prompt"]
        PP2["User's task prompt"] --> PP
        PP3["Input file listing"] --> PP
        PP4["Team template constraints"] --> PP
        PP5["Previous Q&A answers"] --> PP
    end

    subgraph seq_prompt ["Sequential Worker Prompt"]
        style seq_prompt fill:#cce5ff,stroke:#007bff,color:#333
        SP1["Task prompt"] --> SP["Final prompt"]
        SP2["Subtask instructions"] --> SP
        SP3["Role hints"] --> SP
        SP4["Dependency context"] --> SP
        SP5["Broadcast messages"] --> SP
        SP6["Files involved"] --> SP
        SP7["API instructions"] --> SP
    end

    subgraph mgr_prompt ["Manager Prompt"]
        style mgr_prompt fill:#e2d5f1,stroke:#6f42c1,color:#333
        MP1["Task prompt"] --> MP["Final prompt"]
        MP2["Team composition"] --> MP
        MP3["Lifecycle phases"] --> MP
        MP4["Directive writing rules"] --> MP
        MP5["API monitoring guide"] --> MP
        MP6["System message types"] --> MP
        MP7["Approval protocol<br/>(WORKER_APPROVED / CONTINUE_WORK)"] --> MP
        MP8["Respawn protocol<br/>(RESPAWN_WORKERS)"] --> MP
    end

    subgraph collab_prompt ["Collaborative Worker Prompt"]
        style collab_prompt fill:#fff3cd,stroke:#ffc107,color:#333
        CP1["Task prompt"] --> CP["Final prompt"]
        CP2["Team composition"] --> CP
        CP3["Role hints"] --> CP
        CP4["Wait-for-directive"] --> CP
        CP5["Broadcast messages"] --> CP
        CP6["Subtask instructions"] --> CP
        CP7["File ownership rules"] --> CP
        CP8["Approval loop<br/>(WORK_DONE → wait)"] --> CP
        CP9["API instructions"] --> CP
    end

    subgraph fix_prompt ["Fix Worker Prompt (respawned)"]
        style fix_prompt fill:#f8d7da,stroke:#dc3545,color:#333
        FP1["Fix instructions from manager"] --> FP["Final prompt"]
        FP2["Task prompt"] --> FP
        FP3["Original directives"] --> FP
        FP4["Team status after prev. round"] --> FP
        FP5["Original subtask instructions"] --> FP
        FP6["Approval loop"] --> FP
        FP7["API instructions"] --> FP
    end
```

---

## The Docker Container

Each agent runs in an isolated container based on `klaudio-agent`:

- **Base**: Node.js 22 + Claude Code CLI
- **User**: `agent` (non-root)
- **Working dir**: `/home/agent/workspace` (volume-mounted from host)
- **Auth**: `.credentials.json` and `settings.json` copied by the entrypoint into `~/.claude/`
- **Startup**: the entrypoint runs:
  ```bash
  claude --dangerously-skip-permissions \
    --output-format stream-json \
    --verbose \
    -p "$CLAUDE_PROMPT"
  ```

The prompt arrives as the **environment variable** `CLAUDE_PROMPT`. Output is streamed as JSON for real-time parsing by the orchestrator.

---

## Full Lifecycle

```mermaid
flowchart TD
    A[User creates task] --> B[Planner<br/>analyze]
    B -->|questions?| C[User answers]
    C -->|answers injected| B
    B -->|plan| D[User approves]
    D --> E{Orchestrator}
    E -->|Sequential| F[Spawn in order<br/>context between steps]
    E -->|Collaborative| G[Manager + Workers<br/>directives + API]
    G --> G1[Workers complete<br/>send WORK_DONE]
    G1 --> G2{Manager reviews}
    G2 -->|CONTINUE_WORK| G3[Worker applies fix]
    G3 --> G1
    G2 -->|WORKER_APPROVED| G4[Worker exits]
    G4 --> G5{All workers done?}
    G5 -->|no| G2
    G5 -->|yes| G6{Manager satisfied?}
    G6 -->|RESPAWN_WORKERS| G
    G6 -->|exit| H
    F --> H[Reviewer<br/>code review]
    H -->|optional| I[Post-execution]
    I --> J[git commit]
    I --> K[git push]
    I --> L[auto-PR on GitHub]

    style A fill:#e2d5f1,stroke:#6f42c1,color:#333
    style B fill:#d4edda,stroke:#28a745,color:#333
    style C fill:#fff3cd,stroke:#ffc107,color:#333
    style D fill:#e2d5f1,stroke:#6f42c1,color:#333
    style E fill:#f8d7da,stroke:#dc3545,color:#333
    style F fill:#cce5ff,stroke:#007bff,color:#333
    style G fill:#cce5ff,stroke:#007bff,color:#333
    style G1 fill:#cce5ff,stroke:#007bff,color:#333
    style G2 fill:#e2d5f1,stroke:#6f42c1,color:#333
    style G3 fill:#fff3cd,stroke:#ffc107,color:#333
    style G4 fill:#d4edda,stroke:#28a745,color:#333
    style G5 fill:#e2d5f1,stroke:#6f42c1,color:#333
    style G6 fill:#e2d5f1,stroke:#6f42c1,color:#333
    style H fill:#d4edda,stroke:#28a745,color:#333
    style I fill:#fff3cd,stroke:#ffc107,color:#333
    style J fill:#e2e3e5,stroke:#6c757d,color:#333
    style K fill:#e2e3e5,stroke:#6c757d,color:#333
    style L fill:#e2e3e5,stroke:#6c757d,color:#333
```

---

## Key Architectural Decisions

| Decision | Rationale |
|----------|-----------|
| Read-only planner | Separation of analysis and execution, security |
| Prompt via env var | Simplicity, no extra files to mount |
| Dual persistence (filesystem + DB) | Files for agents, DB for the UI |
| Directives via filesystem | Manager writes, workers read — natural for Claude Code |
| Messages via API/DB | Async communication between isolated containers |
| File lock service | Prevents write conflicts between parallel agents |
| Ephemeral containers | State lives in the workspace and DB, containers are disposable |
| Shared workspace | A single mounted volume per task, all agents see the same files |
| Workers wait for approval | Manager reviews each worker's output before it exits — enables inline fixes without respawn |
| Two-tier fix mechanism | `CONTINUE_WORK` for inline fixes (fast, preserves context), `RESPAWN_WORKERS` for full rework (fallback) |
| No fix round limit | Manager decides when quality is sufficient — unlimited iterations |
