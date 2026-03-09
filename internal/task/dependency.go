package task

import (
	"fmt"
	"sync"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// DependencyGraph tracks subtask dependencies and completion status.
// It is safe for concurrent use.
type DependencyGraph struct {
	mu     sync.RWMutex
	nodes  map[string]*db.Subtask
	edges  map[string][]string // subtaskID -> list of dependency IDs
	status map[string]string   // subtaskID -> "pending" | "running" | "completed" | "failed" | "skipped"
}

// BuildGraph creates a DependencyGraph from an ExecutionPlan.
func BuildGraph(plan *ExecutionPlan) *DependencyGraph {
	g := &DependencyGraph{
		nodes:  make(map[string]*db.Subtask, len(plan.Subtasks)),
		edges:  make(map[string][]string, len(plan.Subtasks)),
		status: make(map[string]string, len(plan.Subtasks)),
	}
	for i := range plan.Subtasks {
		st := &plan.Subtasks[i]
		g.nodes[st.ID] = st
		g.edges[st.ID] = st.DependsOn
		if st.Status == "" || st.Status == "pending" {
			g.status[st.ID] = "pending"
		} else {
			g.status[st.ID] = st.Status
		}
	}
	return g
}

// GetNode returns the subtask with the given ID.
func (g *DependencyGraph) GetNode(id string) *db.Subtask {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[id]
}

// GetReady returns subtasks whose dependencies are all completed and whose
// status is "pending".
func (g *DependencyGraph) GetReady() []*db.Subtask {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var ready []*db.Subtask
	for id, st := range g.nodes {
		if g.status[id] != "pending" {
			continue
		}
		if g.allDepsCompleted(id) {
			ready = append(ready, st)
		}
	}
	return ready
}

// MarkRunning marks a subtask as running.
func (g *DependencyGraph) MarkRunning(subtaskID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status[subtaskID] = "running"
}

// Complete marks a subtask as completed and returns newly unblocked subtasks.
func (g *DependencyGraph) Complete(subtaskID string) []*db.Subtask {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.status[subtaskID] = "completed"

	// Find newly unblocked subtasks
	var unblocked []*db.Subtask
	for id, st := range g.nodes {
		if g.status[id] != "pending" {
			continue
		}
		if g.allDepsCompleted(id) {
			unblocked = append(unblocked, st)
		}
	}
	return unblocked
}

// Fail marks a subtask as failed.
func (g *DependencyGraph) Fail(subtaskID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status[subtaskID] = "failed"

	// Mark dependents as skipped
	g.skipDependents(subtaskID)
}

// skipDependents recursively marks all subtasks that depend on the given one as skipped.
// Must be called with mu held.
func (g *DependencyGraph) skipDependents(failedID string) {
	for id := range g.nodes {
		if g.status[id] == "pending" || g.status[id] == "running" {
			for _, dep := range g.edges[id] {
				if dep == failedID {
					g.status[id] = "skipped"
					g.skipDependents(id) // recursively skip downstream
					break
				}
			}
		}
	}
}

// AllCompleted returns true if all subtasks are in a terminal state
// (completed, failed, or skipped).
func (g *DependencyGraph) AllCompleted() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, s := range g.status {
		if s == "pending" || s == "running" {
			return false
		}
	}
	return true
}

// HasFailures returns true if any subtask has failed.
func (g *DependencyGraph) HasFailures() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, s := range g.status {
		if s == "failed" {
			return false
		}
	}
	return false
}

// HasCycles returns true if the dependency graph contains a cycle.
func (g *DependencyGraph) HasCycles() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	const (
		white = 0
		gray  = 1
		black = 2
	)

	color := make(map[string]int)
	for id := range g.nodes {
		color[id] = white
	}

	var dfs func(id string) bool
	dfs = func(id string) bool {
		color[id] = gray
		for _, dep := range g.edges[id] {
			switch color[dep] {
			case gray:
				return true // back edge = cycle
			case white:
				if dfs(dep) {
					return true
				}
			}
		}
		color[id] = black
		return false
	}

	for id := range g.nodes {
		if color[id] == white {
			if dfs(id) {
				return true
			}
		}
	}
	return false
}

// TopologicalSort returns subtasks in dependency order.
func (g *DependencyGraph) TopologicalSort() ([]*db.Subtask, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.HasCycles() {
		return nil, fmt.Errorf("graph contains cycles")
	}

	visited := make(map[string]bool)
	var order []*db.Subtask

	var visit func(id string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, dep := range g.edges[id] {
			visit(dep)
		}
		order = append(order, g.nodes[id])
	}

	for id := range g.nodes {
		visit(id)
	}

	return order, nil
}

// allDepsCompleted checks whether all dependencies of a subtask are completed.
// Must be called with at least a read lock held.
func (g *DependencyGraph) allDepsCompleted(subtaskID string) bool {
	for _, dep := range g.edges[subtaskID] {
		if g.status[dep] != "completed" {
			return false
		}
	}
	return true
}

// Status returns the current status of a subtask in the graph.
func (g *DependencyGraph) Status(subtaskID string) string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.status[subtaskID]
}
