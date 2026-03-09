package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/klaudio-ai/klaudio/internal/config"
	"github.com/klaudio-ai/klaudio/internal/db"
	"github.com/klaudio-ai/klaudio/internal/docker"
	"github.com/klaudio-ai/klaudio/internal/stream"
)

// ErrPoolFull is returned when the pool has reached its maximum agent count.
var ErrPoolFull = errors.New("agent pool is full")

// ErrTaskLimitReached is returned when a task has reached its per-task agent limit.
var ErrTaskLimitReached = errors.New("per-task agent limit reached")

// AgentRole represents the role an agent plays in a team.
type AgentRole string

const (
	RolePlanner   AgentRole = "planner"
	RoleDeveloper AgentRole = "developer"
	RoleReviewer  AgentRole = "reviewer"
	RoleTester    AgentRole = "tester"
)

// AgentStatus represents the current status of an agent.
type AgentStatus string

const (
	StatusCreated  AgentStatus = "created"
	StatusRunning  AgentStatus = "running"
	StatusStopped  AgentStatus = "stopped"
	StatusComplete AgentStatus = "completed"
	StatusFailed   AgentStatus = "failed"
)

// AgentInstance represents a running agent container.
type AgentInstance struct {
	ID            string
	TaskID        string
	SubtaskID     string
	ContainerID   string
	Role          AgentRole
	Status        AgentStatus
	Stream        *stream.AgentStream
	StartedAt     time.Time
	ModifiedFiles []string
	LastOutput    string
}

// AgentResult holds the outcome of an agent execution.
type AgentResult struct {
	ExitCode int
	Error    error
}

// SpawnOpts configures a new agent container.
type SpawnOpts struct {
	TaskID          string
	SubtaskID       string
	Role            AgentRole
	Prompt          string
	WorkspaceDir    string
	EnvVars         map[string]string
	ClaudeAuthConfig *config.ClaudeConfig
}

// Pool manages concurrent agent containers.
type Pool struct {
	mu          sync.Mutex
	maxAgents   int
	maxPerTask  int
	active      map[string]*AgentInstance // agentID -> agent
	waiters     map[string]chan AgentResult // agentID -> result channel
	docker      *docker.Manager
	streamHub   *stream.Hub
	database    *db.DB
}

// NewPool creates a new agent pool.
func NewPool(dockerMgr *docker.Manager, hub *stream.Hub, database *db.DB, cfg *config.Config) *Pool {
	maxPerTask := cfg.Docker.MaxAgentsPerTask
	if maxPerTask <= 0 {
		maxPerTask = 3
	}
	return &Pool{
		maxAgents:  cfg.Docker.MaxAgents,
		maxPerTask: maxPerTask,
		active:     make(map[string]*AgentInstance),
		waiters:    make(map[string]chan AgentResult),
		docker:     dockerMgr,
		streamHub:  hub,
		database:   database,
	}
}

// Spawn creates and starts a new agent container.
func (p *Pool) Spawn(ctx context.Context, opts SpawnOpts) (*AgentInstance, error) {
	p.mu.Lock()
	if len(p.active) >= p.maxAgents {
		p.mu.Unlock()
		return nil, ErrPoolFull
	}
	// Check per-task limit
	taskCount := 0
	for _, a := range p.active {
		if a.TaskID == opts.TaskID {
			taskCount++
		}
	}
	if taskCount >= p.maxPerTask {
		p.mu.Unlock()
		return nil, ErrTaskLimitReached
	}
	p.mu.Unlock()

	agentID := uuid.New().String()
	logger := slog.With("agent_id", agentID, "task_id", opts.TaskID, "subtask_id", opts.SubtaskID)

	// Create agent record in DB
	role := string(opts.Role)
	if role == "" {
		role = "developer"
	}
	dbAgent := &db.Agent{
		ID:        agentID,
		TaskID:    opts.TaskID,
		SubtaskID: &opts.SubtaskID,
		Role:      role,
		Status:    "created",
		CreatedAt: time.Now().UTC(),
	}
	if err := p.database.CreateAgent(ctx, dbAgent); err != nil {
		return nil, fmt.Errorf("creating agent record: %w", err)
	}

	// Build container name
	taskShort := opts.TaskID
	if len(taskShort) > 8 {
		taskShort = taskShort[:8]
	}
	containerName := fmt.Sprintf("klaudio-%s-%s", taskShort, opts.SubtaskID)

	// Build volumes
	volumes := []docker.VolumeMount{
		{
			HostPath:      opts.WorkspaceDir,
			ContainerPath: "/home/agent/workspace",
			ReadOnly:      false,
		},
	}

	// Create container
	containerID, err := p.docker.CreateContainer(ctx, docker.ContainerOpts{
		Name:    containerName,
		Prompt:  opts.Prompt,
		EnvVars: opts.EnvVars,
		Volumes: volumes,
	})
	if err != nil {
		return nil, fmt.Errorf("creating container: %w", err)
	}

	// Update agent with container ID
	if err := p.database.UpdateAgentContainer(ctx, agentID, containerID); err != nil {
		logger.Error("failed to update agent container", "error", err)
	}

	// Register stream
	agentStream := p.streamHub.RegisterAgent(agentID, opts.TaskID)

	agent := &AgentInstance{
		ID:          agentID,
		TaskID:      opts.TaskID,
		SubtaskID:   opts.SubtaskID,
		ContainerID: containerID,
		Role:        AgentRole(role),
		Status:      StatusRunning,
		Stream:      agentStream,
		StartedAt:   time.Now().UTC(),
	}

	// Start the container
	if err := p.docker.StartContainer(ctx, containerID); err != nil {
		p.docker.RemoveContainer(ctx, containerID) //nolint:errcheck
		p.streamHub.UnregisterAgent(agentID)
		return nil, fmt.Errorf("starting container: %w", err)
	}

	// Register in active map and create result channel
	resultCh := make(chan AgentResult, 1)
	p.mu.Lock()
	p.active[agentID] = agent
	p.waiters[agentID] = resultCh
	p.mu.Unlock()

	// Monitor container in background
	go p.monitorContainer(ctx, agent, resultCh)

	logger.Info("agent spawned", "container_id", containerID)
	return agent, nil
}

// monitorContainer watches a container until it exits and cleans up.
func (p *Pool) monitorContainer(ctx context.Context, agent *AgentInstance, resultCh chan AgentResult) {
	logger := slog.With("agent_id", agent.ID, "task_id", agent.TaskID)

	exitCh, errCh := p.docker.WaitContainer(ctx, agent.ContainerID)

	var result AgentResult

	select {
	case exitCode := <-exitCh:
		waitErr := <-errCh
		result.ExitCode = int(exitCode)
		if waitErr != nil {
			result.Error = waitErr
		}
		logger.Info("agent container exited", "exit_code", exitCode)

	case <-ctx.Done():
		result.ExitCode = -1
		result.Error = ctx.Err()
		logger.Info("agent context cancelled")
	}

	// Clean up container
	cleanupCtx := context.Background()
	if err := p.docker.RemoveContainer(cleanupCtx, agent.ContainerID); err != nil {
		logger.Warn("failed to remove container", "error", err)
	}

	// Update status
	p.mu.Lock()
	if result.ExitCode == 0 && result.Error == nil {
		agent.Status = StatusComplete
	} else {
		agent.Status = StatusFailed
	}
	delete(p.active, agent.ID)
	delete(p.waiters, agent.ID)
	p.mu.Unlock()

	// Unregister stream
	p.streamHub.UnregisterAgent(agent.ID)

	// Update DB
	var agentErr *string
	if result.Error != nil {
		s := result.Error.Error()
		agentErr = &s
	}
	p.database.UpdateAgentCompleted(cleanupCtx, agent.ID, result.ExitCode, agentErr) //nolint:errcheck

	// Send result
	resultCh <- result
	close(resultCh)
}

// Terminate stops and removes a single agent.
func (p *Pool) Terminate(agentID string) error {
	p.mu.Lock()
	agent, ok := p.active[agentID]
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	ctx := context.Background()
	if err := p.docker.StopContainer(ctx, agent.ContainerID, 10); err != nil {
		slog.Warn("failed to stop container", "agent_id", agentID, "error", err)
	}
	// Container removal and cleanup will happen in monitorContainer
	return nil
}

// TerminateTaskAgents stops all agents for a given task.
func (p *Pool) TerminateTaskAgents(taskID string) error {
	p.mu.Lock()
	var agents []*AgentInstance
	for _, a := range p.active {
		if a.TaskID == taskID {
			agents = append(agents, a)
		}
	}
	p.mu.Unlock()

	var errs []error
	for _, a := range agents {
		if err := p.Terminate(a.ID); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors terminating agents: %v", errs)
	}
	return nil
}

// SendMessage injects a message to a running agent via the stream hub.
func (p *Pool) SendMessage(agentID string, msg []byte) error {
	p.mu.Lock()
	_, ok := p.active[agentID]
	p.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent %s not found or not active", agentID)
	}

	p.streamHub.InjectMessage(agentID, msg)
	return nil
}

// Get returns an agent instance by ID.
func (p *Pool) Get(agentID string) *AgentInstance {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.active[agentID]
}

// GetBySubtask returns the agent instance assigned to a subtask.
func (p *Pool) GetBySubtask(subtaskID string) *AgentInstance {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, a := range p.active {
		if a.SubtaskID == subtaskID {
			return a
		}
	}
	return nil
}

// ActiveCount returns the number of currently active agents.
func (p *Pool) ActiveCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.active)
}

// ActiveForTask returns all active agents for a given task.
func (p *Pool) ActiveForTask(taskID string) []*AgentInstance {
	p.mu.Lock()
	defer p.mu.Unlock()
	var agents []*AgentInstance
	for _, a := range p.active {
		if a.TaskID == taskID {
			agents = append(agents, a)
		}
	}
	return agents
}

// Wait returns a channel that receives the result when an agent completes.
func (p *Pool) Wait(agentID string) <-chan AgentResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.waiters[agentID]
	if !ok {
		// Agent already completed or doesn't exist; return closed channel with error
		ch = make(chan AgentResult, 1)
		ch <- AgentResult{ExitCode: -1, Error: fmt.Errorf("agent %s not found", agentID)}
		close(ch)
	}
	return ch
}
