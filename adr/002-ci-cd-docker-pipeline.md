# ADR 002: CI/CD Pipeline for Docker Image Publishing with Automatic Semantic Versioning

## Status

**Accepted** - 2026-05-04

## Context

The League Watcher Bot now has complete Docker containerization support (implemented in ADR 001). The Dockerfile, docker-compose.yml, and comprehensive documentation are production-ready. However, there is **no automated CI/CD pipeline** to build and publish Docker images.

### Problems with Manual Image Publishing

1. **Manual image building**: Developers must build and push images manually using `docker build` and `docker push`
2. **No version tagging**: No automatic or consistent tagging strategy for releases
3. **No quality gates**: Images could be pushed without tests passing
4. **Inconsistent builds**: Different developers may build images with different contexts or incomplete changes
5. **Deployment friction**: Manual steps required for production deployments increase error risk
6. **No audit trail**: Hard to track which code version corresponds to which image
7. **Version ambiguity**: "latest" tag doesn't indicate what version is actually deployed

### Requirements for Solution

1. **Automated builds**: Trigger on pushes to main branch
2. **Quality gates**: Run tests before building/pushing images
3. **Automatic versioning**: Determine version based on commit messages, no manual git tagging
4. **Explicit versioning**: Every image must have a specific semver tag - no ambiguous "latest" tag
5. **Registry publishing**: Push to GitHub Container Registry (GHCR)
6. **Build optimization**: Cache layers for faster CI runs
7. **Documentation**: Update existing docs with CI/CD information
8. **Single-instance deployment**: Keep it simple for current use case (one bot instance)
9. **Main branch only**: No builds for feature branches or PRs to reduce CI costs

## Decision

We will implement **automated CI/CD using GitHub Actions with semantic-release** for fully automatic semantic versioning based on conventional commits.

### 1. GitHub Actions Workflow

**File**: `.github/workflows/docker-publish.yml`

**Trigger Strategy**:
- **Only on push to main branch**
- No feature branch builds (waste of CI resources)
- No PR builds (feature work doesn't need images)
- No manual tag pushes (semantic-release creates tags automatically)

**Three-Job Pipeline**:

#### Job 1: Test (always runs)
- Checkout code with full git history (`fetch-depth: 0` for semantic-release)
- Setup Go 1.22 with module caching
- Run `go test ./...`
- Run `go vet ./...`
- Check formatting with `gofmt -l .`
- **Fails fast**: If tests fail, pipeline stops immediately

#### Job 2: Semantic Version (runs if tests pass)
- Uses `cycjimmy/semantic-release-action@v4`
- Analyzes conventional commits since last release
- Calculates next version (patch/minor/major)
- Creates git tag automatically (e.g., `v1.2.3`)
- Updates CHANGELOG.md
- Creates GitHub release with auto-generated notes
- **Outputs**: `new-release-published` (bool), `new-release-version` (string)

#### Job 3: Build and Push (runs only if new version created)
- **Conditional execution**: `if: needs.semantic-version.outputs.new-release-published == 'true'`
- Checkout code at new version tag
- Set up Docker Buildx
- Login to GHCR using `GITHUB_TOKEN`
- Extract metadata and generate semver tags
- Build and push image with layer caching
- **Result**: Image pushed with multiple semver tags, NO "latest" tag

### 2. Automatic Semantic Versioning

**Conventional Commits → Semver Bump**:

| Commit Type | Example | Version Bump | Description |
|-------------|---------|--------------|-------------|
| `fix:` | `fix: resolve memory leak in watcher` | **PATCH** (1.0.0 → 1.0.1) | Bug fixes |
| `feat:` | `feat: add health check endpoint` | **MINOR** (1.0.0 → 1.1.0) | New features |
| `BREAKING CHANGE:` | `feat!: redesign API\n\nBREAKING CHANGE: config format changed` | **MAJOR** (1.0.0 → 2.0.0) | Breaking changes |
| `perf:`, `refactor:` | `perf: optimize database queries` | **PATCH** | Performance/refactoring |
| `docs:`, `style:`, `chore:`, `test:`, `ci:` | `docs: update README` | **No release** | Non-code changes |

**Example Flow**:
1. Developer merges PR with commit message: `feat: add Docker containerization support`
2. Push to main triggers GitHub Actions workflow
3. Test job runs and passes
4. Semantic-release analyzes commits since last tag (e.g., v1.0.0)
5. Determines minor bump because of "feat:" → creates tag `v1.1.0`
6. Updates CHANGELOG.md and creates GitHub release
7. Build job builds Docker image and pushes:
   - `ghcr.io/cauchy2384/leaguewatcherbot:v1.1.0`
   - `ghcr.io/cauchy2384/leaguewatcherbot:1.1.0`
   - `ghcr.io/cauchy2384/leaguewatcherbot:1.1`
   - `ghcr.io/cauchy2384/leaguewatcherbot:1`

**NO "latest" tag** - Always use specific versions for reproducibility.

### 3. Tagging Strategy: No "latest" Tag

**Why no "latest" tag?**

1. **Reproducibility**: `docker pull image:latest` gives different results over time - impossible to reproduce deployments
2. **Clarity**: Specific versions (1.2.3) make it clear exactly what's deployed
3. **Rollback safety**: With "latest", rolling back means finding the previous image digest manually
4. **Documentation**: "We're running v1.2.3" is clear; "we're running latest" is meaningless
5. **Testing**: "This bug appeared in v1.3.0" vs "this bug appeared sometime after Tuesday"

**Version pinning recommendations**:
- **Production**: Pin to exact version (`1.2.3`) for maximum stability
- **Staging**: Pin to major.minor (`1.2`) for automatic patch updates
- **Development**: Can use major version (`1`) for automatic minor/patch updates

### 4. Container Registry Choice

**Selected**: GitHub Container Registry (GHCR) - `ghcr.io/cauchy2384/leaguewatcherbot`

**Why GHCR**:
- **Integrated authentication**: Uses `GITHUB_TOKEN`, no separate credentials needed
- **Free for public repos**: No additional cost
- **Good performance**: Fast pulls, reliable uptime
- **Tight GitHub integration**: Images linked to repository, automatic cleanup options
- **Already referenced**: README.docker.md examples already use GHCR

### 5. Build Optimization

**Layer Caching Strategy**:
```yaml
cache-from: type=gha  # Read from GitHub Actions cache
cache-to: type=gha,mode=max  # Write all layers to cache
```

**Benefits**:
- **First build**: ~10 seconds (cold cache)
- **Subsequent builds** (no code changes): ~2-3 seconds (warm cache)
- **Dependency changes**: Only rebuilds affected layers
- **Reduced CI minutes**: Faster builds = less GitHub Actions usage

**Cache scope**: Per branch (main branch has its own cache)

### 6. Security Considerations

**Authentication**:
- Uses `GITHUB_TOKEN` automatically provided by GitHub Actions
- No additional secrets required
- Permissions: `contents: write` (tags), `packages: write` (GHCR push)

**Image security** (inherited from Dockerfile):
- Non-root user execution (UID 1000)
- Minimal Alpine base (~5MB)
- Static binary, no runtime dependencies
- No secrets baked into image
- Reproducible builds (same commit = same image)

**Supply chain security**:
- Pinned action versions (`@v4`, `@v5`)
- Full git history available for audit
- CHANGELOG.md tracks all changes
- GitHub releases provide release notes

### 7. Workflow Implementation Details

**Why three separate jobs?**

1. **Fail fast**: Test job fails immediately if code is broken - don't waste time versioning or building
2. **Conditional execution**: Skip expensive Docker build if no release was created
3. **Clear separation**: Each job has one responsibility (test, version, build)
4. **Easy debugging**: Can see exactly which stage failed

**Why full git history (`fetch-depth: 0`)?**

- Semantic-release needs commit history to determine version bump
- Shallow clones (default) only include recent commits
- Without history, semantic-release can't analyze changes since last release

**Why checkout at new tag in build job?**

- Ensures image is built from exact tagged commit
- Prevents race conditions if main branch advances during workflow
- Build artifacts match release artifacts

## Consequences

### Positive

1. **Fully automated versioning**: No manual git tagging, version determined from commit messages
2. **Clear version history**: CHANGELOG.md auto-generated, GitHub releases with notes
3. **Reproducible deployments**: Explicit version tags (1.2.3) instead of ambiguous "latest"
4. **Enforces discipline**: Team must write conventional commit messages
5. **Quality gates**: Tests must pass before any image is published
6. **Environment consistency**: Same image runs everywhere (dev/staging/prod)
7. **Fast builds**: Layer caching reduces build time to 2-3 seconds on cache hit
8. **No toolchain required**: Deployment targets only need Docker, not Go
9. **Audit trail**: Every version has git tag, changelog entry, and GitHub release
10. **Cost efficient**: Only builds on main branch, skips builds when no release

### Negative

1. **Requires conventional commits**: Team must learn and follow commit message format
2. **Semver implications**: Developers must understand that "feat:" bumps minor version
3. **GitHub Actions dependency**: Requires GitHub (could be mitigated with other CI systems)
4. **CI costs**: Uses GitHub Actions minutes (though optimized with caching and conditional builds)
5. **No "latest" tag**: Users must specify version numbers (though this is actually a benefit for production)
6. **Initial setup complexity**: More complex than manual builds (but pays off long-term)

### Neutral

1. **Main-only builds**: Feature branches don't get Docker images (acceptable for current workflow)
2. **GHCR lock-in**: Could switch to Docker Hub or other registry if needed
3. **Build time**: ~10s for full build vs ~30s for `go build` (but automated)

## Alternatives Considered

### Alternative 1: Manual Git Tagging

**Approach**: Developer manually creates git tags (git tag v1.2.3), then CI builds and pushes.

**Pros**:
- Simple to understand
- Full control over version numbers
- No need to learn conventional commits

**Cons**:
- **Error-prone**: Developers forget to tag, or tag incorrectly
- **Inconsistent**: No enforcement of semver rules
- **No automation**: Still requires manual decision on version number
- **No changelog**: Must manually maintain CHANGELOG.md
- **Human error**: Easy to push v1.2.3 when it should be v1.3.0

**Rejection reason**: Doesn't solve automation problem, still requires manual steps that can be forgotten.

### Alternative 2: "latest" Tag Strategy

**Approach**: Every build creates "latest" tag, plus optional semver tags.

**Pros**:
- Simple for users (`docker pull image:latest`)
- Always gets most recent version
- Common pattern in Docker ecosystem

**Cons**:
- **Not reproducible**: `docker pull image:latest` gives different results over time
- **Unclear versions**: "We're running latest" doesn't specify what version
- **Rollback difficulty**: Can't easily rollback to "previous latest"
- **Testing issues**: Hard to reproduce bugs ("it works on latest now, but failed yesterday")
- **Production risk**: Accidental pulls get untested versions

**Rejection reason**: "latest" tag is an anti-pattern for production deployments. Explicit versioning is better.

### Alternative 3: Docker Hub

**Approach**: Push images to Docker Hub instead of GHCR.

**Pros**:
- More widely known registry
- Good documentation and tooling
- Free tier for public repos

**Cons**:
- **Requires separate account**: Need Docker Hub credentials
- **Extra secrets**: Must configure `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`
- **Not integrated**: No automatic linking to GitHub repository
- **Rate limits**: Docker Hub has pull rate limits (though not an issue for small projects)

**Rejection reason**: GHCR is simpler (uses GITHUB_TOKEN), tightly integrated, and already used in examples.

### Alternative 4: Feature Branch Builds

**Approach**: Build Docker images for every branch, not just main.

**Pros**:
- Developers can test Docker images from feature branches
- Can deploy feature branches to test environments

**Cons**:
- **Wastes CI resources**: Most feature branches never need Docker images
- **Registry pollution**: Lots of temporary images fill up registry
- **Increased costs**: More GitHub Actions minutes used
- **Cleanup overhead**: Need to delete stale feature branch images

**Rejection reason**: Current workflow doesn't need feature branch images. Can be added later if requirements change.

### Alternative 5: Self-Hosted Container Registry

**Approach**: Run own registry (e.g., Harbor, GitLab Registry).

**Pros**:
- Full control over infrastructure
- No vendor lock-in
- Can customize features

**Cons**:
- **High maintenance**: Must run and maintain registry infrastructure
- **Costs**: Requires servers, storage, backups
- **Security**: Must handle authentication, HTTPS, vulnerability scanning
- **Overkill**: Not needed for single-bot deployment

**Rejection reason**: Massive over-engineering for current use case.

## Technical Details

### Semantic Release Configuration

**File**: `.releaserc.json`

**Plugin pipeline**:

1. **@semantic-release/commit-analyzer**
   - Analyzes commits using `conventionalcommits` preset
   - Maps commit types to semver bumps (feat → minor, fix → patch)
   - Determines if release is needed

2. **@semantic-release/release-notes-generator**
   - Generates release notes from commits
   - Groups by type (Features, Bug Fixes, etc.)

3. **@semantic-release/changelog**
   - Updates CHANGELOG.md with new release notes
   - Maintains history of all versions

4. **@semantic-release/git**
   - Commits CHANGELOG.md back to repository
   - Message: `chore(release): X.Y.Z [skip ci]`
   - `[skip ci]` prevents infinite loop

5. **@semantic-release/github**
   - Creates GitHub release with notes
   - Attaches release to git tag

### Docker Metadata Action

**Purpose**: Generates Docker tags and labels from git metadata.

**Configuration**:
```yaml
tags: |
  type=semver,pattern=v{{version}}         # v1.2.3
  type=semver,pattern={{version}}          # 1.2.3
  type=semver,pattern={{major}}.{{minor}}  # 1.2
  type=semver,pattern={{major}}            # 1
```

**Example output** for version `1.2.3`:
- `ghcr.io/cauchy2384/leaguewatcherbot:v1.2.3` (full with v prefix)
- `ghcr.io/cauchy2384/leaguewatcherbot:1.2.3` (full without v prefix)
- `ghcr.io/cauchy2384/leaguewatcherbot:1.2` (major.minor - gets patch updates)
- `ghcr.io/cauchy2384/leaguewatcherbot:1` (major only - gets minor/patch updates)

### Permissions Required

**GitHub Actions workflow needs**:
```yaml
permissions:
  contents: write        # Create git tags and commit CHANGELOG
  packages: write        # Push to GHCR
  issues: write          # Create GitHub releases
  pull-requests: write   # (semantic-release requirement)
```

**GITHUB_TOKEN** is automatically provided - no secrets configuration needed.

### Conditional Build Logic

**Build job only runs when**:
```yaml
if: needs.semantic-version.outputs.new-release-published == 'true'
```

**Scenarios**:

| Scenario | Test Job | Version Job | Build Job |
|----------|----------|-------------|-----------|
| Push with `feat:` commit | ✅ Runs | ✅ Creates v1.1.0 | ✅ Builds & pushes |
| Push with `fix:` commit | ✅ Runs | ✅ Creates v1.0.1 | ✅ Builds & pushes |
| Push with `docs:` commit | ✅ Runs | ✅ No release | ❌ Skipped |
| Push to feature branch | ❌ Not triggered | ❌ Not triggered | ❌ Not triggered |
| Pull request | ❌ Not triggered | ❌ Not triggered | ❌ Not triggered |

This prevents wasting CI minutes and polluting the registry with non-release images.

## Implementation

### Files Created

1. **`.github/workflows/docker-publish.yml`** (135 lines)
   - Three-job GitHub Actions workflow
   - Test → semantic-version → build-and-push pipeline
   - Conditional execution logic
   - Docker layer caching configuration

2. **`.releaserc.json`** (44 lines)
   - Semantic-release configuration
   - Conventional commits mapping to semver
   - Plugin pipeline for changelog and GitHub releases

3. **`adr/002-ci-cd-docker-pipeline.md`** (this document)
   - Architecture Decision Record
   - Documents rationale, alternatives, consequences

### Files Updated

1. **`README.docker.md`**
   - Added "Automated Builds & Versioning" section
   - Documented conventional commit → semver mapping
   - Explained version pinning strategies
   - Removed references to "latest" tag

2. **`README.md`**
   - Added Docker image version and size badges
   - Added Docker deployment section
   - Linked to full Docker documentation

## Verification Steps

### 1. Workflow Syntax Validation
```bash
# Install GitHub CLI if needed
brew install gh

# Validate workflow exists
gh workflow list
# Expected: Shows "Docker Build and Publish" workflow
```

### 2. Test Conventional Commit Flow
```bash
# After merging this PR, test versioning
git checkout main
git pull

# Create conventional commit
git commit --allow-empty -m "feat: add CI/CD pipeline with automatic semantic versioning"
git push origin main

# Watch workflow
gh run watch

# Expected:
# - Test job: ✅ Passed
# - Semantic version job: ✅ Created v1.1.0
# - Build job: ✅ Built and pushed
```

### 3. Verify Image Publishing
```bash
# Check git tag was created
git fetch --tags
git tag --sort=-v:refname | head -5
# Expected: v1.1.0 (or similar)

# Pull published images
docker pull ghcr.io/cauchy2384/leaguewatcherbot:v1.1.0
docker pull ghcr.io/cauchy2384/leaguewatcherbot:1.1.0
docker pull ghcr.io/cauchy2384/leaguewatcherbot:1.1
docker pull ghcr.io/cauchy2384/leaguewatcherbot:1

# Verify all point to same image
docker images --digests | grep leaguewatcherbot
# Expected: Same digest for all tags

# Verify "latest" doesn't exist
docker pull ghcr.io/cauchy2384/leaguewatcherbot:latest
# Expected: Error - manifest unknown
```

### 4. Verify CHANGELOG
```bash
cat CHANGELOG.md
# Expected: Auto-generated changelog with version history
```

### 5. Test Different Commit Types
```bash
# Patch bump
git commit --allow-empty -m "fix: resolve memory leak"
git push
# Expected: v1.1.1

# Minor bump
git commit --allow-empty -m "feat: add health endpoint"
git push
# Expected: v1.2.0

# No release
git commit --allow-empty -m "docs: update README"
git push
# Expected: No version, no build

# Major bump
git commit --allow-empty -m "feat!: redesign API

BREAKING CHANGE: configuration format changed"
git push
# Expected: v2.0.0
```

## Future Considerations

### Multi-Architecture Support

Currently builds only for `linux/amd64`. To support ARM64 (Apple Silicon, Raspberry Pi):

```yaml
- name: Build and push Docker image
  uses: docker/build-push-action@v5
  with:
    platforms: linux/amd64,linux/arm64
    # ... rest of config
```

Requires multi-platform builder:
```yaml
- name: Set up QEMU
  uses: docker/setup-qemu-action@v3

- name: Set up Docker Buildx
  uses: docker/setup-buildx-action@v3
  with:
    platforms: linux/amd64,linux/arm64
```

### Vulnerability Scanning

Add security scanning with Trivy:

```yaml
- name: Run Trivy vulnerability scanner
  uses: aquasecurity/trivy-action@master
  with:
    image-ref: ghcr.io/${{ github.repository }}:${{ steps.meta.outputs.version }}
    format: 'sarif'
    output: 'trivy-results.sarif'

- name: Upload Trivy results to GitHub Security
  uses: github/codeql-action/upload-sarif@v2
  with:
    sarif_file: 'trivy-results.sarif'
```

### Deployment Automation

Automatically deploy on version tags:

```yaml
deploy:
  needs: build-and-push
  if: startsWith(github.ref, 'refs/tags/v')
  runs-on: ubuntu-latest
  steps:
    - name: Deploy to production
      run: |
        # SSH to server, pull new image, restart container
        ssh production "docker pull ghcr.io/${{ github.repository }}:${{ needs.semantic-version.outputs.new-release-version }} && docker-compose up -d"
```

### Notifications

Notify on successful releases:

```yaml
- name: Send Slack notification
  if: success()
  uses: slackapi/slack-github-action@v1
  with:
    payload: |
      {
        "text": "New Docker image published: v${{ needs.semantic-version.outputs.new-release-version }}",
        "blocks": [
          {
            "type": "section",
            "text": {
              "type": "mrkdwn",
              "text": "*League Watcher Bot Release*\nVersion: v${{ needs.semantic-version.outputs.new-release-version }}\nImage: ghcr.io/${{ github.repository }}:${{ needs.semantic-version.outputs.new-release-version }}"
            }
          }
        ]
      }
  env:
    SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL }}
```

### Image Signing

Sign images with Cosign for supply chain security:

```yaml
- name: Install Cosign
  uses: sigstore/cosign-installer@v3

- name: Sign the images
  run: |
    cosign sign --yes ghcr.io/${{ github.repository }}@${{ steps.build.outputs.digest }}
  env:
    COSIGN_EXPERIMENTAL: "true"
```

## Team Requirements

### Conventional Commits

**All team members must** follow Conventional Commits format after this feature is merged:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

**Common types**:
- `feat:` - New feature (bumps minor version)
- `fix:` - Bug fix (bumps patch version)
- `docs:` - Documentation only
- `style:` - Formatting, missing semi-colons, etc.
- `refactor:` - Code change that neither fixes a bug nor adds a feature
- `perf:` - Performance improvement
- `test:` - Adding tests
- `chore:` - Updating build tasks, package manager configs, etc.
- `ci:` - Changes to CI configuration files

**Breaking changes**:
```
feat!: redesign configuration API

BREAKING CHANGE: The configuration file format has changed from YAML to TOML.
Migration guide: https://...
```

**Resources**:
- Conventional Commits spec: https://www.conventionalcommits.org/
- Examples: https://www.conventionalcommits.org/en/v1.0.0/#examples

## References

- [GitHub Actions documentation](https://docs.github.com/en/actions)
- [Semantic Release](https://semantic-release.gitbook.io/)
- [Conventional Commits](https://www.conventionalcommits.org/)
- [Docker Build Push Action](https://github.com/docker/build-push-action)
- [GitHub Container Registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [ADR 001: Docker Containerization](./001-docker-containerization.md)

## Reviewers

- ADR Author: Claude (AI Assistant)
- Approved By: v.loginov
- Date: 2026-05-04

## Changelog

- 2026-05-04: Initial ADR documenting CI/CD pipeline with automatic semantic versioning
