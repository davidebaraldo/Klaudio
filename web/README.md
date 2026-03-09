# Klaudio Web UI

SvelteKit 2 frontend for the Klaudio orchestrator.

## Stack

- **SvelteKit 2** with Svelte 5 runes syntax
- **Tailwind CSS** for styling
- **TypeScript** throughout
- **xterm.js** for real-time terminal output
- **adapter-static** for embedding into the Go binary

## Development

```bash
npm install
npm run dev
```

The dev server starts at `http://localhost:5173` and proxies API calls to `http://localhost:8080`.

## Build

```bash
npm run build
```

Output goes to `build/` — static HTML/JS/CSS ready for embedding via `//go:embed`.

## Structure

```
src/
├── routes/                    # Pages
│   ├── +layout.svelte             # Sidebar layout
│   ├── +page.svelte               # Dashboard (task list)
│   └── tasks/
│       ├── new/+page.svelte       # Task creation wizard
│       └── [id]/+page.svelte      # Task detail (4 tabs)
└── lib/
    ├── api.ts                 # TypeScript API client
    ├── components/            # Reusable components
    │   ├── Terminal.svelte        # xterm.js WebSocket terminal
    │   ├── PlanViewer.svelte      # Plan viewer/editor
    │   ├── FileManager.svelte     # File tree with viewer modal
    │   ├── AgentComms.svelte      # Inter-agent communication
    │   ├── QuestionPanel.svelte   # Planner Q&A
    │   ├── StatusBadge.svelte     # Task/agent status badges
    │   └── MessageInput.svelte    # Agent message input
    ├── stores/
    │   ├── websocket.ts           # WebSocket connection store
    │   └── tasks.ts               # Task list store
    └── assets/
        └── klaudio-logo.svg       # Logo
```
