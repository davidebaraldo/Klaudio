package repo

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/klaudio-ai/klaudio/internal/db"
)

// openRepo opens an existing git repository at the given directory.
func openRepo(dir string) (*git.Repository, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("opening repository at %s: %w", dir, err)
	}
	return repo, nil
}

// getCurrentBranch returns the short name of the current HEAD branch.
func getCurrentBranch(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("getting HEAD: %w", err)
	}
	return head.Name().Short(), nil
}

// GetLastCommitHash returns the hash of the latest commit on HEAD for the repo at dir.
func GetLastCommitHash(dir string) (string, error) {
	r, err := openRepo(dir)
	if err != nil {
		return "", err
	}
	return getLastCommitHash(r)
}

// getLastCommitHash returns the hash of the latest commit on HEAD.
func getLastCommitHash(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("getting HEAD: %w", err)
	}
	return head.Hash().String(), nil
}

// buildAuth creates the appropriate transport.AuthMethod from a RepoConfig.
// The username varies by platform: GitHub uses "x-access-token",
// Bitbucket uses "x-token-auth".
func buildAuth(config db.RepoConfig) transport.AuthMethod {
	if config.AccessToken == "" {
		return nil
	}
	platform := DetectPlatform(config.URL)
	return &http.BasicAuth{
		Username: authUsername(platform),
		Password: config.AccessToken,
	}
}

// branchRefName returns the full reference name for a branch.
func branchRefName(branch string) plumbing.ReferenceName {
	return plumbing.NewBranchReferenceName(branch)
}
