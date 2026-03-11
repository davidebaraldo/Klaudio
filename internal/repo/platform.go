package repo

import "strings"

// Platform represents a Git hosting platform.
type Platform string

const (
	PlatformBitbucket Platform = "bitbucket"
	PlatformGitHub    Platform = "github"
	PlatformUnknown   Platform = "unknown"
)

// DetectPlatform determines the Git hosting platform from a repository URL.
func DetectPlatform(repoURL string) Platform {
	lower := strings.ToLower(repoURL)
	switch {
	case strings.Contains(lower, "github.com"):
		return PlatformGitHub
	case strings.Contains(lower, "bitbucket.org"):
		return PlatformBitbucket
	default:
		return PlatformUnknown
	}
}

// authUsername returns the appropriate HTTP Basic Auth username for a platform.
// GitHub uses "x-access-token", Bitbucket uses "x-token-auth".
func authUsername(p Platform) string {
	switch p {
	case PlatformGitHub:
		return "x-access-token"
	case PlatformBitbucket:
		return "x-token-auth"
	default:
		// Generic fallback — works for most platforms.
		return "git"
	}
}
