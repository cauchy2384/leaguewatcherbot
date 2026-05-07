# Contributing to League Watcher Bot

Thank you for your interest in contributing to League Watcher Bot! We're excited to have you here. This project is a Discord bot that monitors League of Legends player matches and posts updates to Discord channels. Whether you're fixing a bug, adding a feature, or improving documentation, your contributions are welcome.

Before diving in, please take a moment to read through these guidelines to understand how the project works and how you can contribute effectively.

For a comprehensive understanding of the system architecture, see [ARCHITECTURE.md](ARCHITECTURE.md).

---

## Table of Contents

- [How Can I Contribute?](#how-can-i-contribute)
  - [Reporting Bugs](#reporting-bugs)
  - [Suggesting Enhancements](#suggesting-enhancements)
  - [Code Contributions](#code-contributions)
- [Development Environment Setup](#development-environment-setup)
- [Testing Guidelines](#testing-guidelines)
- [Code Style Guidelines](#code-style-guidelines)
- [Commit Message Convention](#commit-message-convention)
- [Pull Request Process](#pull-request-process)
- [Code of Conduct](#code-of-conduct)
- [Recognition](#recognition)
- [Questions & Support](#questions--support)

---

## How Can I Contribute?

### Reporting Bugs

Found a bug? Please help us fix it by creating a detailed bug report.

**Before submitting a bug report:**
- Check the [existing issues](https://github.com/cauchy2384/leaguewatcherbot/issues) to avoid duplicates
- Make sure you're using the latest version of the code
- Collect relevant information about your environment

**Bug Report Template:**

```markdown
**Go Version:** (output of `go version`)
**OS:** (e.g., macOS 14.0, Ubuntu 22.04, Windows 11)

**Steps to Reproduce:**
1. Set up config.yaml with...
2. Run the bot with...
3. Execute command...

**Expected Behavior:**
What you expected to happen

**Actual Behavior:**
What actually happened

**Logs:**
```
Paste relevant logs from zap output here
(Make sure to sanitize any sensitive information like bot tokens)
```

**Configuration:**
```yaml
# Paste your config.yaml here (remove any sensitive data)
```

**Additional Context:**
Any other information that might help
```

### Suggesting Enhancements

Have an idea for a new feature or improvement? We'd love to hear it!

**Enhancement Request Template:**

```markdown
**Problem Description:**
What problem does this enhancement solve? What's the use case?

**Proposed Solution:**
Describe your proposed implementation approach

**Alternatives Considered:**
What other approaches did you consider?

**Additional Context:**
Screenshots, mockups, examples from other projects, etc.
```

### Code Contributions

Ready to write code? Awesome! Please follow the guidelines in this document, especially:
- [Development Environment Setup](#development-environment-setup)
- [Testing Guidelines](#testing-guidelines)
- [Code Style Guidelines](#code-style-guidelines)
- [Commit Message Convention](#commit-message-convention)
- [Pull Request Process](#pull-request-process)

---

## Development Environment Setup

### Prerequisites

- **Go 1.26 or higher** ([Download Go](https://go.dev/dl/))
- **Git** for version control
- **Discord Bot Token** (create one at [Discord Developer Portal](https://discord.com/developers/applications))
- **Discord Account** for testing

### Step-by-Step Setup

1. **Fork the repository** on GitHub

2. **Clone your fork:**
   ```bash
   git clone git@github.com:YOUR_USERNAME/leaguewatcherbot.git
   cd leaguewatcherbot
   ```

3. **Install dependencies:**
   ```bash
   go mod download
   ```

4. **Create configuration file** (`config.yaml` in the repository root):
   ```yaml
   poll_period: 1m
   played_gap: 60m
   channel_id: YOUR_DISCORD_CHANNEL_ID
   players:
     - name: exampleplayer
       tag: euw
       region: euw
       real_name: Test Player
   ```
   
   See [config.yaml](config.yaml) in the repository for a complete example.

5. **Set environment variables:**
   ```bash
   export BOT_DISCORD_TOKEN="your-discord-bot-token"
   export BOT_OWNER_ID="your-discord-username"
   ```
   
   On Windows (PowerShell):
   ```powershell
   $env:BOT_DISCORD_TOKEN="your-discord-bot-token"
   $env:BOT_OWNER_ID="your-discord-username"
   ```

6. **Run the bot:**
   ```bash
   go run cmd/leaguewatcher/main.go
   ```

7. **Verify the setup** by running tests:
   ```bash
   go test ./...
   ```

### Troubleshooting

- **"config.yaml not found"**: Make sure the file is in the same directory as the executable
- **"invalid Discord token"**: Verify your `BOT_DISCORD_TOKEN` is correct
- **"cannot connect to Discord"**: Check your internet connection and Discord service status

---

## Testing Guidelines

We take testing seriously. All new features and bug fixes should include appropriate tests.

### Test Framework

This project uses:
- Go standard `testing` package
- [`github.com/matryer/is`](https://github.com/matryer/is) for concise assertions
- [`github.com/stretchr/testify`](https://github.com/stretchr/testify) for structured assertions

### Test Patterns

**1. Table-Driven Tests** (preferred pattern):

```go
func TestMatchURL(t *testing.T) {
    testCases := []struct {
        name     string
        region   string
        matchID  string
        expected string
    }{
        {
            name:     "EUW match",
            region:   "euw",
            matchID:  "EUW1_123456789",
            expected: "https://mobalytics.gg/lol/match/euw/EUW1_123456789",
        },
        {
            name:     "NA match",
            region:   "na",
            matchID:  "NA1_987654321",
            expected: "https://mobalytics.gg/lol/match/na/NA1_987654321",
        },
    }
    
    for _, tt := range testCases {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()  // Enable parallel execution
            is := is.New(t)
            
            match := Match{Region: tt.region, ID: tt.matchID}
            url := match.URL()
            
            is.Equal(url, tt.expected)
        })
    }
}
```

**2. Parallel Test Execution:**

Use `t.Parallel()` in subtests when tests are independent and don't share state:

```go
for _, tt := range testCases {
    tt := tt  // Capture range variable (Go < 1.22)
    t.Run(tt.name, func(t *testing.T) {
        t.Parallel()  // Tests run concurrently
        // ... test body
    })
}
```

**3. Matryer/is Assertions:**

```go
is := is.New(t)
is.NoErr(err)                    // Assert no error
is.Equal(got, want)              // Assert equality
is.True(len(items) > 0)          // Assert condition
```

**4. Testify Assertions:**

```go
require.NoError(t, err)          // Fail immediately on error
assert.NotEmpty(t, result)       // Continue on failure
assert.Len(t, items, 5)          // Check length
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test ./internal/leaguewatcher/bot/...

# Run a specific test
go test -run TestMatchURL ./internal/leaguewatcher/

# Run tests with coverage
go test -cover ./...
```

### Test Coverage Requirements

- **New features:** Must include tests covering the main functionality
- **Bug fixes:** Should include a test that reproduces the bug
- **Refactoring:** Ensure existing tests still pass

### Integration Tests

Some tests (e.g., `TestClientMatches` in `watcher/mobalytics/client_test.go`) make real API calls to Mobalytics. These tests:
- May be slower than unit tests
- Require internet connectivity
- Use real summoner names from the codebase

---

## Code Style Guidelines

Follow these conventions to keep the codebase clean and consistent.

### Go Formatting

**Always format your code:**
```bash
go fmt ./...
go vet ./...
```

Consider running these commands before every commit.

### Project Structure

```
cmd/              # Application entry points
internal/         # Private application code
  leaguewatcher/  # Core domain logic
    bot/          # Discord bot implementation
    watcher/      # Match detection system
  khaleesi/       # Text mutation utilities
tools/            # Supporting tools and utilities
```

Follow this structure when adding new code:
- **New commands/entry points** → `cmd/`
- **Core application logic** → `internal/leaguewatcher/`
- **Utilities/tools** → `tools/`

### Naming Conventions

- **Packages:** lowercase, single word (e.g., `bot`, `watcher`, `mobalytics`)
- **Types:** PascalCase (e.g., `Match`, `Config`, `Bot`)
- **Functions/Methods:** camelCase (e.g., `getMatches`, `formatMessage`)
- **Constants:** PascalCase or ALL_CAPS for exported constants
- **Interfaces:** PascalCase, often ending in `-er` (e.g., `Watcher`, `Repository`)

### Error Handling

**Use error wrapping with context:**

```go
if err != nil {
    return fmt.Errorf("failed to fetch matches for %s: %w", player.Name, err)
}
```

**For non-recoverable errors in main/init:**

```go
if err != nil {
    logger.Error("Can't connect to Discord", zap.Error(err))
    return
}
```

### Logging

**Use structured logging with `go.uber.org/zap`:**

```go
logger.Info("Processing match", 
    zap.String("player", match.Player.RealName),
    zap.String("champion", match.Champion),
    zap.Bool("win", match.Win),
)

logger.Error("Failed to post message", 
    zap.Error(err),
    zap.String("channel", channelID),
)
```

**Log levels:**
- `Info` - Normal operation events
- `Error` - Error conditions that need attention
- `Debug` - Detailed diagnostic information (use sparingly)

### Concurrency

**Use context for cancellation:**

```go
func (w *Watcher) Run(ctx context.Context) (<-chan Match, <-chan struct{}) {
    // ...
    go func() {
        defer close(done)
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                // Do work
            }
        }
    }()
}
```

**Use `errgroup` for concurrent operations:**

```go
g, ctx := errgroup.WithContext(ctx)
for _, player := range players {
    player := player  // Capture
    g.Go(func() error {
        return fetchMatches(ctx, player)
    })
}
if err := g.Wait(); err != nil {
    // Handle error
}
```

### Best Practices

- **No global variables** (except package-level constants)
- **Avoid `init()` functions** unless absolutely necessary
- **Prefer composition over inheritance**
- **Keep functions small and focused** (single responsibility)
- **Use interfaces for dependencies** (dependency injection)
- **Document exported types and functions** with godoc comments

---

## Commit Message Convention

We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification with a strict 3-phase workflow.

### Commit Format

```
<type>: <description>

[optional body]

[optional footer]
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code restructuring without behavior change
- `style`: Formatting, linting (no code change)
- `test`: Adding or updating tests
- `docs`: Documentation changes
- `chore`: Maintenance tasks (dependencies, build, etc.)

### Examples

```
feat: add champion mastery tracking
fix: correct LP calculation for placement matches
refactor: extract match formatting to separate function
style: apply gofmt and fix linting issues
test: add table-driven tests for config validation
docs: update ARCHITECTURE.md with new components
chore: update dependencies to latest versions
```

### 3-Phase Commit Workflow

**CRITICAL:** When implementing changes, follow this strict sequence. Do NOT bundle these phases together.

#### Phase A: Style Changes
Apply formatting, linting, or naming convention fixes.

```bash
# Make style changes
go fmt ./...
go vet ./...

# Commit
git add .
git commit -m "style: apply gofmt and fix linting issues"

# Verify
go test ./...

# Push
git push origin feature-branch
```

#### Phase B: Refactoring
Refactor code to improve structure or readability without changing behavior.

```bash
# Make refactoring changes
# (extract functions, rename variables, reorganize code)

# Commit
git add .
git commit -m "refactor: extract champion data logic to separate package"

# Verify
go test ./...

# Push
git push origin feature-branch
```

#### Phase C: Business Logic
Implement the requested feature or logic change.

```bash
# Implement feature
# (add new functionality or fix bug)

# Commit
git add .
git commit -m "feat: add champion mastery tracking and display"

# Verify
go test ./...

# Push
git push origin feature-branch
```

### Complete Example

```bash
# Scenario: Adding a new feature to track champion mastery

# Create feature branch
git checkout main
git pull origin main
git checkout -b feature/champion-mastery

# Phase A: Style
# (Fix formatting issues discovered while working)
git add .
git commit -m "style: apply gofmt to champion files"
go test ./...
git push origin feature/champion-mastery

# Phase B: Refactoring
# (Extract reusable champion data logic)
git add .
git commit -m "refactor: extract champion stats to ChampionStats type"
go test ./...
git push origin feature/champion-mastery

# Phase C: Feature Implementation
# (Add the actual mastery tracking feature)
git add .
git commit -m "feat: add champion mastery tracking with Discord embed display"
go test ./...
git push origin feature/champion-mastery

# Create pull request
gh pr create --title "feat: add champion mastery tracking" \
  --body "Adds champion mastery tracking feature. Displays mastery level and points in Discord embeds."
```

**Why this workflow?**
- **Clean history:** Each commit has a single, clear purpose
- **Easy review:** Reviewers can focus on one type of change at a time
- **Bisectable:** `git bisect` can pinpoint exactly when a bug was introduced
- **Revertable:** Individual commits can be reverted without affecting others

---

## Pull Request Process

Follow these steps to submit your contribution.

### 1. Before You Start

- **Create an issue** for discussion (for major changes)
- **Check existing PRs** to avoid duplicate work
- **Ensure your fork is up to date** with `main`

### 2. Create a Feature Branch

```bash
git checkout main
git pull origin main
git checkout -b feature/your-feature-name
```

**Branch naming conventions:**
- `feature/description` - New features
- `fix/description` - Bug fixes
- `refactor/description` - Code refactoring
- `docs/description` - Documentation updates

### 3. Make Your Changes

- Follow the [3-Phase Commit Workflow](#3-phase-commit-workflow)
- Write tests for new functionality
- Update documentation if needed
- Keep commits focused and atomic

### 4. Run Tests Locally

```bash
# Run all tests
go test ./...

# Run formatting
go fmt ./...

# Run linting
go vet ./...
```

All tests must pass before submitting.

### 5. Push to Your Fork

```bash
git push origin feature/your-feature-name
```

### 6. Create Pull Request

- Go to [https://github.com/cauchy2384/leaguewatcherbot/pulls](https://github.com/cauchy2384/leaguewatcherbot/pulls)
- Click "New Pull Request"
- Select your branch
- Fill out the PR template with:
  - **Description:** What does this PR do?
  - **Motivation:** Why is this change needed?
  - **Testing:** How was this tested?
  - **Related Issues:** Link to issues this PR addresses

**PR Title Format:**
```
<type>: <concise description>
```

Examples:
- `feat: add webhook support for match notifications`
- `fix: correct timezone handling in pidor game`
- `docs: add setup instructions for Windows users`

### 7. Code Review

- Respond to feedback constructively
- Make requested changes in new commits (don't force-push during review)
- Mark conversations as resolved when addressed
- Re-request review after updates

### 8. Merging

- **Do NOT merge your own PR** (even if you have permissions)
- **Do NOT merge to `main` locally**
- Maintainers will merge after approval
- Your branch will be deleted after merge

### PR Checklist

Before submitting, verify:

- [ ] Code follows the [Code Style Guidelines](#code-style-guidelines)
- [ ] Commits follow the [3-Phase Workflow](#3-phase-commit-workflow)
- [ ] Commit messages follow [Conventional Commits](#commit-message-convention)
- [ ] All tests pass (`go test ./...`)
- [ ] New features have tests
- [ ] Documentation is updated (if applicable)
- [ ] No merge conflicts with `main`
- [ ] PR description clearly explains the changes

---

## Code of Conduct

### Our Pledge

This is a fun project created for friends who play League of Legends together. We're here to have a good time, learn, and build something cool.

### Our Standards

**Do:**
- Be respectful and constructive in discussions
- Focus on what's best for the project
- Accept feedback gracefully
- Help others learn and grow
- Have fun!

**Don't:**
- Be disrespectful or dismissive
- Make personal attacks
- Submit low-effort or spam contributions
- Demand immediate responses or reviews

### Enforcement

Project maintainers have the right and responsibility to remove, edit, or reject comments, commits, code, issues, and other contributions that don't align with this Code of Conduct.

---

## Recognition

We appreciate every contribution, big or small! Contributors will be:

- Listed in the repository's contributor graph
- Acknowledged in release notes (for significant contributions)
- Given credit in commit history

Thank you for making League Watcher Bot better!

---

## Questions & Support

### Getting Help

- **Questions about contributing?** Open a [GitHub Discussion](https://github.com/cauchy2384/leaguewatcherbot/discussions) (if enabled) or [create an issue](https://github.com/cauchy2384/leaguewatcherbot/issues/new)
- **Found a bug?** See [Reporting Bugs](#reporting-bugs)
- **Feature idea?** See [Suggesting Enhancements](#suggesting-enhancements)

### Resources

- **Architecture Documentation:** [ARCHITECTURE.md](ARCHITECTURE.md)
- **Repository:** [github.com/cauchy2384/leaguewatcherbot](https://github.com/cauchy2384/leaguewatcherbot)
- **License:** [MIT License](LICENSE)
- **Maintainer:** [@cauchy2384](https://github.com/cauchy2384)

### Additional Reading

- [How to Build a CONTRIBUTING.md](https://contributing.md/how-to-build-contributing-md/)
- [GitHub's Contribution Guidelines](https://docs.github.com/en/communities/setting-up-your-project-for-healthy-contributions/setting-guidelines-for-repository-contributors)
- [Conventional Commits](https://www.conventionalcommits.org/)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

---

**Happy Contributing!** 🎮🤖
