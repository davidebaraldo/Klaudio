package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const defaultGitHubBaseURL = "https://api.github.com"

// GitHubClient provides access to the GitHub REST API.
type GitHubClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewGitHubClient creates a new GitHubClient.
// If baseURL is empty, the default GitHub API URL is used.
func NewGitHubClient(baseURL string, httpClient *http.Client) *GitHubClient {
	if baseURL == "" {
		baseURL = defaultGitHubBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &GitHubClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// CreatePullRequest creates a new pull request on GitHub.
func (c *GitHubClient) CreatePullRequest(ctx context.Context, opts CreatePROptions) (*PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls", c.baseURL, opts.Workspace, opts.RepoSlug)

	body := map[string]interface{}{
		"title": opts.Title,
		"body":  opts.Description,
		"head":  opts.Source,
		"base":  opts.Destination,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling PR body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, readGitHubError(resp)
	}

	var prResp githubPRResponse
	if err := json.NewDecoder(resp.Body).Decode(&prResp); err != nil {
		return nil, fmt.Errorf("decoding PR response: %w", err)
	}

	return &PullRequest{
		ID:    prResp.Number,
		Title: prResp.Title,
		URL:   prResp.HTMLURL,
		State: prResp.State,
	}, nil
}

// RequestReviewers requests reviewers for an existing pull request.
func (c *GitHubClient) RequestReviewers(ctx context.Context, owner, repo string, prNumber int, reviewers []string, token string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/requested_reviewers", c.baseURL, owner, repo, prNumber)

	body := map[string]interface{}{
		"reviewers": reviewers,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling reviewers body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return readGitHubError(resp)
	}

	return nil
}

// GetRepository retrieves repository information from GitHub.
func (c *GitHubClient) GetRepository(ctx context.Context, owner, repo, token string) (*Repository, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", c.baseURL, owner, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readGitHubError(resp)
	}

	var ghRepo githubRepoResponse
	if err := json.NewDecoder(resp.Body).Decode(&ghRepo); err != nil {
		return nil, fmt.Errorf("decoding repository response: %w", err)
	}

	return &Repository{
		Slug:     ghRepo.Name,
		Name:     ghRepo.Name,
		FullName: ghRepo.FullName,
	}, nil
}

// ListBranches retrieves branches for a repository from GitHub.
func (c *GitHubClient) ListBranches(ctx context.Context, owner, repo, token string) ([]Branch, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/branches?per_page=100", c.baseURL, owner, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readGitHubError(resp)
	}

	var ghBranches []githubBranchResponse
	if err := json.NewDecoder(resp.Body).Decode(&ghBranches); err != nil {
		return nil, fmt.Errorf("decoding branches response: %w", err)
	}

	branches := make([]Branch, len(ghBranches))
	for i, b := range ghBranches {
		branches[i] = Branch{
			Name: b.Name,
			Target: struct {
				Hash string `json:"hash"`
			}{Hash: b.Commit.SHA},
		}
	}

	return branches, nil
}

// GitHub API response types.

type githubPRResponse struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

type githubRepoResponse struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

type githubBranchResponse struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type githubErrorResponse struct {
	Message string `json:"message"`
}

// Regex patterns for parsing GitHub URLs.
var (
	githubHTTPSPattern = regexp.MustCompile(`^https?://(?:www\.)?github\.com/([^/]+)/([^/.]+?)(?:\.git)?/?$`)
	githubSSHPattern   = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/.]+?)(?:\.git)?$`)
)

// ParseGitHubURL extracts the owner and repository name from a GitHub URL.
// It supports both HTTPS and SSH URL formats.
func ParseGitHubURL(repoURL string) (owner, repoName string, err error) {
	repoURL = strings.TrimSpace(repoURL)

	if matches := githubHTTPSPattern.FindStringSubmatch(repoURL); len(matches) == 3 {
		return matches[1], matches[2], nil
	}

	if matches := githubSSHPattern.FindStringSubmatch(repoURL); len(matches) == 3 {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("unable to parse GitHub URL: %s", repoURL)
}

// readGitHubError reads the response body and returns a formatted error.
func readGitHubError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github API error (status %d)", resp.StatusCode)
	}

	var errResp githubErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
		return fmt.Errorf("github API error (status %d): %s", resp.StatusCode, errResp.Message)
	}

	return fmt.Errorf("github API error (status %d): %s", resp.StatusCode, string(body))
}
