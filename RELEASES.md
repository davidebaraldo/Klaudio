# Release Process

## Branches

| Branch | Purpose |
|--------|---------|
| `main` | Stable releases. Protected — requires Pull Request to merge. |
| `dev` | Development. All new work happens here. Dev releases are tagged from this branch. |

## Workflow

```
dev (daily work) ──tag v0.2.0-dev.1──► Dev pre-release
  │
  └── PR to main ──tag v0.2.0──► Stable release
```

1. Work on `dev` branch
2. Tag `v*-dev.*` for dev/test releases (pre-release on GitHub)
3. When ready, open a PR from `dev` to `main`
4. After merge, tag `v*` on `main` for a stable release

## Tag Conventions

| Pattern | Type | GitHub Release | Docker `:latest` |
|---------|------|----------------|-------------------|
| `v1.0.0` | **Stable** | Release (latest) | Updated |
| `v1.0.0-dev.1` | Dev | Pre-release | Not touched |
| `v1.0.0-alpha.1` | Alpha | Pre-release | Not touched |
| `v1.0.0-beta.1` | Beta | Pre-release | Not touched |
| `v1.0.0-rc.1` | Release Candidate | Pre-release | Not touched |

## How to Create a Release

### Dev release (from `dev` branch)

```bash
git checkout dev
git tag v0.2.0-dev.1
git push origin v0.2.0-dev.1
```

### Stable release (from `main` branch)

```bash
# 1. Merge dev into main via PR on GitHub
# 2. After merge:
git checkout main
git pull
git tag -a v0.2.0 -m "v0.2.0 — Short description"
git push origin v0.2.0
```

## What Happens Automatically

When a `v*` tag is pushed, GitHub Actions will:

1. **Build** cross-platform binaries (Linux, macOS, Windows — amd64 + arm64)
2. **Build frontend** and embed it in each binary
3. **Build and push** the `klaudio-agent` Docker image to `ghcr.io`
4. **Create a GitHub Release** with:
   - All binaries as downloadable assets
   - SHA256 checksums
   - Auto-generated changelog from commits since previous tag
   - Marked as **pre-release** if the tag contains `-dev`, `-alpha`, `-beta`, or `-rc`

## CI Pipeline

CI runs on every push and PR to both `main` and `dev`:

- **Backend** — `go vet`, `go test`, build
- **Frontend** — `npm ci`, type check, build
- **Full build** — frontend + backend combined binary

## Version in Binary

The version is injected at build time:

```bash
./klaudio --version
# klaudio v0.2.0-dev.1
```

For local builds, the version defaults to `dev` unless overridden by Makefile (which reads from `git describe`).
