package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AnalysisResult holds the static analysis output for a repository.
type AnalysisResult struct {
	Content      string            `json:"content"`       // Markdown summary
	FileTree     []string          `json:"file_tree"`     // Top-level directory listing
	Languages    map[string]int    `json:"languages"`     // Extension -> file count
	Frameworks   []string          `json:"frameworks"`    // Detected frameworks
	KeyFiles     []string          `json:"key_files"`     // Important files found
	Dependencies map[string]string `json:"dependencies"`  // Name -> version (best effort)
}

// maxTreeDepth is the maximum depth for the file tree listing.
const maxTreeDepth = 3

// maxTreeFiles is the maximum number of entries in the file tree.
const maxTreeFiles = 200

// skipDirs are directories to skip during analysis.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".next":        true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
	".svelte-kit":  true,
	".venv":        true,
	"venv":         true,
	"target":       true, // Rust/Java
	"bin":          true,
	"obj":          true, // .NET
	".terraform":   true,
}

// languageExtensions maps file extensions to language names.
var languageExtensions = map[string]string{
	".go":     "Go",
	".js":     "JavaScript",
	".ts":     "TypeScript",
	".jsx":    "React JSX",
	".tsx":    "React TSX",
	".svelte": "Svelte",
	".vue":    "Vue",
	".py":     "Python",
	".rs":     "Rust",
	".java":   "Java",
	".kt":     "Kotlin",
	".rb":     "Ruby",
	".php":    "PHP",
	".cs":     "C#",
	".c":      "C",
	".cpp":    "C++",
	".h":      "C/C++ Header",
	".swift":  "Swift",
	".dart":   "Dart",
	".sql":    "SQL",
	".html":   "HTML",
	".css":    "CSS",
	".scss":   "SCSS",
	".yaml":   "YAML",
	".yml":    "YAML",
	".json":   "JSON",
	".toml":   "TOML",
	".md":     "Markdown",
	".sh":     "Shell",
	".bash":   "Shell",
	".tf":     "Terraform",
	".proto":  "Protocol Buffers",
}

// frameworkDetectors maps marker files to framework/tool names.
var frameworkDetectors = map[string]string{
	"go.mod":            "Go Modules",
	"package.json":      "Node.js",
	"Cargo.toml":        "Rust (Cargo)",
	"requirements.txt":  "Python (pip)",
	"pyproject.toml":    "Python (pyproject)",
	"setup.py":          "Python (setuptools)",
	"Pipfile":           "Python (Pipenv)",
	"pom.xml":           "Java (Maven)",
	"build.gradle":      "Java (Gradle)",
	"Gemfile":           "Ruby (Bundler)",
	"composer.json":     "PHP (Composer)",
	"Makefile":          "Make",
	"CMakeLists.txt":    "CMake",
	"Dockerfile":        "Docker",
	"docker-compose.yml": "Docker Compose",
	"docker-compose.yaml": "Docker Compose",
	".github/workflows":  "GitHub Actions",
	".gitlab-ci.yml":    "GitLab CI",
	"Jenkinsfile":       "Jenkins",
	"terraform.tf":      "Terraform",
	"main.tf":           "Terraform",
	"svelte.config.js":  "SvelteKit",
	"svelte.config.ts":  "SvelteKit",
	"next.config.js":    "Next.js",
	"next.config.ts":    "Next.js",
	"nuxt.config.js":    "Nuxt.js",
	"nuxt.config.ts":    "Nuxt.js",
	"angular.json":      "Angular",
	"tailwind.config.js": "Tailwind CSS",
	"tailwind.config.ts": "Tailwind CSS",
	"vite.config.js":    "Vite",
	"vite.config.ts":    "Vite",
	"tsconfig.json":     "TypeScript",
	".eslintrc.json":    "ESLint",
	".eslintrc.js":      "ESLint",
	"jest.config.js":    "Jest",
	"vitest.config.ts":  "Vitest",
	"pytest.ini":        "pytest",
	"CLAUDE.md":         "Claude Code",
}

// keyFilePatterns are files/patterns considered important for understanding a repo.
var keyFilePatterns = []string{
	"README.md",
	"README",
	"CLAUDE.md",
	"Makefile",
	"Dockerfile",
	"docker-compose.yml",
	"docker-compose.yaml",
	"config.yaml",
	"config.yml",
	".env.example",
	"CONTRIBUTING.md",
	"CHANGELOG.md",
	"LICENSE",
}

// Analyze performs a static analysis of the repository at repoDir.
func Analyze(repoDir string) (*AnalysisResult, error) {
	result := &AnalysisResult{
		Languages:    make(map[string]int),
		Dependencies: make(map[string]string),
	}

	// Walk the directory tree
	var allPaths []string
	langCounts := make(map[string]int)

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip errors
		}

		relPath, err := filepath.Rel(repoDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		if relPath == "." {
			return nil
		}

		// Skip excluded directories
		if info.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
		}

		// Count depth
		depth := strings.Count(relPath, "/")

		// Collect tree entries up to max depth
		if depth < maxTreeDepth && len(allPaths) < maxTreeFiles {
			entry := relPath
			if info.IsDir() {
				entry += "/"
			}
			allPaths = append(allPaths, entry)
		}

		// Count languages
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(info.Name()))
			if lang, ok := languageExtensions[ext]; ok {
				langCounts[lang]++
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking repository: %w", err)
	}

	result.FileTree = allPaths
	result.Languages = langCounts

	// Detect frameworks
	seen := make(map[string]bool)
	for marker, framework := range frameworkDetectors {
		markerPath := filepath.Join(repoDir, marker)
		if _, err := os.Stat(markerPath); err == nil {
			if !seen[framework] {
				result.Frameworks = append(result.Frameworks, framework)
				seen[framework] = true
			}
		}
	}
	sort.Strings(result.Frameworks)

	// Find key files
	for _, pattern := range keyFilePatterns {
		keyPath := filepath.Join(repoDir, pattern)
		if _, err := os.Stat(keyPath); err == nil {
			result.KeyFiles = append(result.KeyFiles, pattern)
		}
	}

	// Also find entry points
	for _, ep := range []string{"main.go", "cmd/", "src/", "app/", "lib/", "internal/", "pkg/"} {
		epPath := filepath.Join(repoDir, ep)
		if _, err := os.Stat(epPath); err == nil {
			if ep[len(ep)-1] == '/' {
				result.KeyFiles = append(result.KeyFiles, ep[:len(ep)-1]+"/")
			} else {
				result.KeyFiles = append(result.KeyFiles, ep)
			}
		}
	}

	// Parse dependencies
	result.Dependencies = parseDependencies(repoDir)

	// Generate markdown content
	result.Content = generateMarkdown(result)

	return result, nil
}

// parseDependencies reads dependency files and extracts top-level dependencies.
func parseDependencies(repoDir string) map[string]string {
	deps := make(map[string]string)

	// Try go.mod
	goMod := filepath.Join(repoDir, "go.mod")
	if data, err := os.ReadFile(goMod); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "require ") && !strings.Contains(line, "(") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					deps[parts[1]] = parts[2]
				}
			}
			// Inside require block
			if !strings.HasPrefix(line, "require") && !strings.HasPrefix(line, ")") && !strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "module") && !strings.HasPrefix(line, "go ") && len(line) > 0 {
				parts := strings.Fields(line)
				if len(parts) >= 2 && strings.Contains(parts[0], "/") {
					deps[parts[0]] = parts[1]
				}
			}
		}
	}

	// Try package.json
	pkgJSON := filepath.Join(repoDir, "package.json")
	if data, err := os.ReadFile(pkgJSON); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			for k, v := range pkg.Dependencies {
				deps[k] = v
			}
			for k, v := range pkg.DevDependencies {
				deps[k+" (dev)"] = v
			}
		}
	}

	// Try requirements.txt
	reqTxt := filepath.Join(repoDir, "requirements.txt")
	if data, err := os.ReadFile(reqTxt); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.IndexAny(line, "=<>!~"); idx > 0 {
				deps[line[:idx]] = line[idx:]
			} else {
				deps[line] = "*"
			}
		}
	}

	// Try Cargo.toml (basic)
	cargoToml := filepath.Join(repoDir, "Cargo.toml")
	if data, err := os.ReadFile(cargoToml); err == nil {
		inDeps := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "[dependencies]" || line == "[dev-dependencies]" {
				inDeps = true
				continue
			}
			if strings.HasPrefix(line, "[") {
				inDeps = false
				continue
			}
			if inDeps && strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				name := strings.TrimSpace(parts[0])
				version := strings.TrimSpace(parts[1])
				version = strings.Trim(version, "\"")
				deps[name] = version
			}
		}
	}

	return deps
}

// generateMarkdown creates a structured markdown document from the analysis.
func generateMarkdown(result *AnalysisResult) string {
	var b strings.Builder

	b.WriteString("# Repository Analysis\n\n")

	// Languages
	if len(result.Languages) > 0 {
		b.WriteString("## Languages\n")
		// Sort by count descending
		type langCount struct {
			lang  string
			count int
		}
		var sorted []langCount
		for lang, count := range result.Languages {
			sorted = append(sorted, langCount{lang, count})
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].count > sorted[j].count
		})
		for _, lc := range sorted {
			b.WriteString(fmt.Sprintf("- %s: %d files\n", lc.lang, lc.count))
		}
		b.WriteString("\n")
	}

	// Frameworks
	if len(result.Frameworks) > 0 {
		b.WriteString("## Frameworks & Tools\n")
		for _, fw := range result.Frameworks {
			b.WriteString(fmt.Sprintf("- %s\n", fw))
		}
		b.WriteString("\n")
	}

	// Key files
	if len(result.KeyFiles) > 0 {
		b.WriteString("## Key Files & Directories\n")
		for _, f := range result.KeyFiles {
			b.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		b.WriteString("\n")
	}

	// Dependencies (limited to top 20)
	if len(result.Dependencies) > 0 {
		b.WriteString("## Dependencies\n")
		var depNames []string
		for name := range result.Dependencies {
			depNames = append(depNames, name)
		}
		sort.Strings(depNames)
		limit := 20
		if len(depNames) < limit {
			limit = len(depNames)
		}
		for _, name := range depNames[:limit] {
			b.WriteString(fmt.Sprintf("- `%s` %s\n", name, result.Dependencies[name]))
		}
		if len(depNames) > 20 {
			b.WriteString(fmt.Sprintf("- ... and %d more\n", len(depNames)-20))
		}
		b.WriteString("\n")
	}

	// File tree
	if len(result.FileTree) > 0 {
		b.WriteString("## Directory Structure\n```\n")
		for _, entry := range result.FileTree {
			b.WriteString(entry + "\n")
		}
		b.WriteString("```\n")
	}

	return b.String()
}
