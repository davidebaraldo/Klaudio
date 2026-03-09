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

const defaultBitbucketBaseURL = "https://api.bitbucket.org/2.0"

// BitbucketClient provides access to the Bitbucket REST API v2.
type BitbucketClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewBitbucketClient creates a new BitbucketClient.
// If baseURL is empty, the default Bitbucket API URL is used.
func NewBitbucketClient(baseURL string, httpClient *http.Client) *BitbucketClient {
	if baseURL == "" {
		baseURL = defaultBitbucketBaseURL
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &BitbucketClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// CreatePROptions holds the parameters for creating a pull request.
type CreatePROptions struct {
	Workspace   string
	RepoSlug    string
	Title       string
	Description string
	Source      string   // source branch name
	Destination string   // destination branch name
	Reviewers   []string // usernames or UUIDs
	AccessToken string
}

// PullRequest represents a Bitbucket pull request.
type PullRequest struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	State string `json:"state"`
}

// Repository represents a Bitbucket repository.
type Repository struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

// Branch represents a Bitbucket branch.
type Branch struct {
	Name   string `json:"name"`
	Target struct {
		Hash string `json:"hash"`
	} `json:"target"`
}

// bitbucketPRResponse is the raw response from the Bitbucket create PR endpoint.
type bitbucketPRResponse struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

// bitbucketBranchesResponse is the raw paginated response for branches.
type bitbucketBranchesResponse struct {
	Values []Branch `json:"values"`
}

// bitbucketErrorResponse captures Bitbucket API error details.
type bitbucketErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// CreatePullRequest creates a new pull request on Bitbucket.
func (c *BitbucketClient) CreatePullRequest(ctx context.Context, opts CreatePROptions) (*PullRequest, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", c.baseURL, opts.Workspace, opts.RepoSlug)

	// Build the request body per Bitbucket API spec.
	body := map[string]interface{}{
		"title":       opts.Title,
		"description": opts.Description,
		"source": map[string]interface{}{
			"branch": map[string]string{
				"name": opts.Source,
			},
		},
		"destination": map[string]interface{}{
			"branch": map[string]string{
				"name": opts.Destination,
			},
		},
		"close_source_branch": false,
	}

	if len(opts.Reviewers) > 0 {
		reviewers := make([]map[string]string, 0, len(opts.Reviewers))
		for _, r := range opts.Reviewers {
			reviewers = append(reviewers, map[string]string{"username": r})
		}
		body["reviewers"] = reviewers
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
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, readBitbucketError(resp)
	}

	var prResp bitbucketPRResponse
	if err := json.NewDecoder(resp.Body).Decode(&prResp); err != nil {
		return nil, fmt.Errorf("decoding PR response: %w", err)
	}

	return &PullRequest{
		ID:    prResp.ID,
		Title: prResp.Title,
		URL:   prResp.Links.HTML.Href,
		State: prResp.State,
	}, nil
}

// GetRepository retrieves repository information from Bitbucket.
func (c *BitbucketClient) GetRepository(ctx context.Context, workspace, repoSlug, token string) (*Repository, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s", c.baseURL, workspace, repoSlug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readBitbucketError(resp)
	}

	var repo Repository
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return nil, fmt.Errorf("decoding repository response: %w", err)
	}

	return &repo, nil
}

// ListBranches retrieves branches for a repository from Bitbucket.
func (c *BitbucketClient) ListBranches(ctx context.Context, workspace, repoSlug, token string) ([]Branch, error) {
	url := fmt.Sprintf("%s/repositories/%s/%s/refs/branches", c.baseURL, workspace, repoSlug)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readBitbucketError(resp)
	}

	var branchResp bitbucketBranchesResponse
	if err := json.NewDecoder(resp.Body).Decode(&branchResp); err != nil {
		return nil, fmt.Errorf("decoding branches response: %w", err)
	}

	return branchResp.Values, nil
}

// Regex patterns for parsing Bitbucket URLs.
var (
	// HTTPS: https://bitbucket.org/{workspace}/{repo}.git or without .git
	httpsPattern = regexp.MustCompile(`^https?://(?:www\.)?bitbucket\.org/([^/]+)/([^/.]+?)(?:\.git)?/?$`)
	// SSH: git@bitbucket.org:{workspace}/{repo}.git
	sshPattern = regexp.MustCompile(`^git@bitbucket\.org:([^/]+)/([^/.]+?)(?:\.git)?$`)
)

// ParseBitbucketURL extracts the workspace and repository slug from a Bitbucket
// repository URL. It supports both HTTPS and SSH URL formats.
func ParseBitbucketURL(repoURL string) (workspace, repoSlug string, err error) {
	repoURL = strings.TrimSpace(repoURL)

	if matches := httpsPattern.FindStringSubmatch(repoURL); len(matches) == 3 {
		return matches[1], matches[2], nil
	}

	if matches := sshPattern.FindStringSubmatch(repoURL); len(matches) == 3 {
		return matches[1], matches[2], nil
	}

	return "", "", fmt.Errorf("unable to parse Bitbucket URL: %s", repoURL)
}

// readBitbucketError reads the response body and returns a formatted error.
func readBitbucketError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("bitbucket API error (status %d)", resp.StatusCode)
	}

	var errResp bitbucketErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Error.Message != "" {
		return fmt.Errorf("bitbucket API error (status %d): %s", resp.StatusCode, errResp.Error.Message)
	}

	return fmt.Errorf("bitbucket API error (status %d): %s", resp.StatusCode, string(body))
}
