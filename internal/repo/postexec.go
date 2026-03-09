package repo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// PostExecResult holds the outcome of the post-execution repository flow.
type PostExecResult struct {
	HasChanges bool   `json:"has_changes"`
	CommitHash string `json:"commit_hash,omitempty"`
	Pushed     bool   `json:"pushed"`
	PRUrl      string `json:"pr_url,omitempty"`
}

// PostExecution handles the complete post-execution flow for tasks with
// repository integration: detect changes, auto-commit, auto-push, and auto-PR.
func PostExecution(ctx context.Context, taskName, workspaceDir string, repoConfig db.RepoConfig) (*PostExecResult, error) {
	mgr := NewManager("")
	result := &PostExecResult{}

	// Check for changes.
	hasChanges, err := mgr.HasChanges(workspaceDir)
	if err != nil {
		return nil, fmt.Errorf("checking for changes: %w", err)
	}
	result.HasChanges = hasChanges

	if !hasChanges {
		slog.Info("no changes detected in workspace", "dir", workspaceDir)
		return result, nil
	}

	// Auto-commit if configured.
	if repoConfig.AutoCommit {
		msg := fmt.Sprintf("klaudio: %s", taskName)
		hash, err := mgr.CommitAll(workspaceDir, msg)
		if err != nil {
			return result, fmt.Errorf("auto-commit failed: %w", err)
		}
		result.CommitHash = hash
		slog.Info("auto-committed changes", "hash", hash, "dir", workspaceDir)
	}

	// Auto-push if configured.
	if repoConfig.AutoPush {
		if err := mgr.Push(workspaceDir, repoConfig); err != nil {
			return result, fmt.Errorf("auto-push failed: %w", err)
		}
		result.Pushed = true
		slog.Info("auto-pushed changes", "dir", workspaceDir)
	}

	// Auto-PR if configured.
	if repoConfig.AutoPR {
		workspace, repoSlug, err := ParseBitbucketURL(repoConfig.URL)
		if err != nil {
			return result, fmt.Errorf("parsing repo URL for PR: %w", err)
		}

		// Determine the source branch name from the repo.
		repo, err := openRepo(workspaceDir)
		if err != nil {
			return result, fmt.Errorf("opening repo for PR branch: %w", err)
		}
		sourceBranch, err := getCurrentBranch(repo)
		if err != nil {
			return result, fmt.Errorf("getting current branch for PR: %w", err)
		}

		destination := repoConfig.PRTarget
		if destination == "" {
			destination = "main"
		}

		bbClient := NewBitbucketClient("", nil)
		pr, err := bbClient.CreatePullRequest(ctx, CreatePROptions{
			Workspace:   workspace,
			RepoSlug:    repoSlug,
			Title:       fmt.Sprintf("[Klaudio] %s", taskName),
			Description: fmt.Sprintf("Automated pull request created by Klaudio for task: %s", taskName),
			Source:      sourceBranch,
			Destination: destination,
			Reviewers:   repoConfig.PRReviewers,
			AccessToken: repoConfig.AccessToken,
		})
		if err != nil {
			return result, fmt.Errorf("creating PR: %w", err)
		}
		result.PRUrl = pr.URL
		slog.Info("auto-created pull request", "pr_url", pr.URL, "pr_id", pr.ID)
	}

	return result, nil
}
