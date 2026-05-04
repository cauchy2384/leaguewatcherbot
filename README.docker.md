# League Watcher Bot - Docker Deployment Guide

This guide explains how to build, run, and manage the League Watcher Bot using Docker and Docker Compose.

## Prerequisites

- **Docker** (v20.10+): [Install Docker](https://docs.docker.com/get-docker/)
- **Docker Compose** (v2.0+): Usually included with Docker Desktop
- **Discord Bot Token**: Create a bot at [Discord Developer Portal](https://discord.com/developers/applications)
- **Discord Bot Permissions**: The bot needs the following permissions in your Discord server:
  - Read Messages
  - Send Messages
  - Manage Members (for some commands)

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/cauchy2384/leaguewatcherbot.git
cd leaguewatcherbot
```

### 2. Create Environment File

Copy the example environment file and fill in your Discord bot credentials:

```bash
cp .env.example .env
```

Edit `.env` and set the following required variables:

```bash
BOT_DISCORD_TOKEN=your_actual_discord_bot_token
BOT_OWNER_ID=your_discord_username
```

**IMPORTANT**: 
- `BOT_OWNER_ID` is your Discord **username** (e.g., "cauchy2384"), NOT the numeric user ID
- Never commit the `.env` file to git (it's already in `.gitignore`)

### 3. Configure Players

Edit `config.yaml` to add the League of Legends players you want to track:

```yaml
poll_period: 1m              # How often to check for new matches
played_gap: 60m              # Only show matches older than this
channel_id: 932549761406476309  # Discord channel ID to post to
players:
  - name: summoner_name      # League of Legends summoner name
    tag: euw                 # Region tag
    region: euw              # Region code
    real_name: Display Name  # Human-readable name for Discord
```

### 4. Build and Run

```bash
# Build the Docker image
docker-compose build

# Start the bot in the background
docker-compose up -d

# View logs
docker-compose logs -f leaguewatcher
```

You should see output like:
```
Starting leaguewatcher
Executable path /app
Config loaded ...
```

### 5. Verify Bot is Running

```bash
# Check container status
docker-compose ps

# Should show: Status "Up (healthy)"
```

The bot is now running and monitoring the configured players!

## Automated Builds & Versioning

Docker images are automatically built and published to GitHub Container Registry (GHCR) via GitHub Actions with **automatic semantic versioning**.

### How Versioning Works

Every push to `main` automatically:
1. Analyzes commit messages using [Conventional Commits](https://www.conventionalcommits.org/)
2. Determines version bump based on commit type:
   - `fix:` commits → **PATCH** version (1.0.0 → 1.0.1)
   - `feat:` commits → **MINOR** version (1.0.0 → 1.1.0)
   - `BREAKING CHANGE:` in commit footer → **MAJOR** version (1.0.0 → 2.0.0)
3. Creates git tag automatically (e.g., `v1.2.3`)
4. Builds and publishes Docker image with multiple semver tags
5. Updates CHANGELOG.md and creates GitHub release

**Conventional Commit Examples**:
```bash
# Patch version bump (bug fix)
git commit -m "fix: resolve memory leak in match processing"
# Result: v1.0.1

# Minor version bump (new feature)
git commit -m "feat: add Docker containerization support"
# Result: v1.1.0

# Major version bump (breaking change)
git commit -m "feat!: redesign configuration API

BREAKING CHANGE: config.yaml format has changed"
# Result: v2.0.0

# No release (documentation)
git commit -m "docs: update README with examples"
# Result: No version created, no Docker build
```

**Note**: Only commits to the `main` branch trigger versioning and builds. Feature branches and pull requests do NOT create Docker images.

### Available Images

Pre-built images are available at: `ghcr.io/cauchy2384/leaguewatcherbot`

**Version Tags** (all point to the same image for a given release):
```bash
# Full version with v prefix
ghcr.io/cauchy2384/leaguewatcherbot:v1.2.3

# Full version without v prefix
ghcr.io/cauchy2384/leaguewatcherbot:1.2.3

# Major.minor (receives patch updates automatically)
ghcr.io/cauchy2384/leaguewatcherbot:1.2

# Major version only (receives minor and patch updates automatically)
ghcr.io/cauchy2384/leaguewatcherbot:1
```

**IMPORTANT**: There is **NO "latest" tag**. Always use explicit version numbers for reproducible deployments.

Browse all versions at: https://github.com/cauchy2384/leaguewatcherbot/pkgs/container/leaguewatcherbot

### Using Pre-Built Images

Instead of building locally, you can use pre-built images in your `docker-compose.yml`:

```yaml
services:
  leaguewatcher:
    # Use pre-built image instead of building locally
    image: ghcr.io/cauchy2384/leaguewatcherbot:1.2
    
    # Remove the 'build:' section when using pre-built images
    
    environment:
      - BOT_DISCORD_TOKEN=${BOT_DISCORD_TOKEN}
      - BOT_OWNER_ID=${BOT_OWNER_ID}
    volumes:
      - leaguewatcher-data:/app
      - ./config.yaml:/app/config.yaml:ro
    restart: unless-stopped
```

### Version Pinning Strategy

Choose your pinning strategy based on environment:

| Environment | Recommended Tag | Behavior | Example |
|-------------|----------------|----------|---------|
| **Production** | Exact version (`1.2.3`) | No automatic updates, maximum stability | `ghcr.io/cauchy2384/leaguewatcherbot:1.2.3` |
| **Staging** | Major.minor (`1.2`) | Automatic patch updates only | `ghcr.io/cauchy2384/leaguewatcherbot:1.2` |
| **Development** | Major version (`1`) | Automatic minor and patch updates | `ghcr.io/cauchy2384/leaguewatcherbot:1` |

**Never** rely on "latest" tag (it doesn't exist in this project by design).

### Manual Image Pull

To pull a specific version:

```bash
# Pull exact version
docker pull ghcr.io/cauchy2384/leaguewatcherbot:1.2.3

# Or use major.minor for automatic patch updates
docker pull ghcr.io/cauchy2384/leaguewatcherbot:1.2

# Verify the image
docker images | grep leaguewatcherbot
```

### Build Information

Each image includes metadata labels:
```bash
# Inspect image metadata
docker inspect ghcr.io/cauchy2384/leaguewatcherbot:1.2.3

# Shows:
# - org.opencontainers.image.version: 1.2.3
# - org.opencontainers.image.created: timestamp
# - org.opencontainers.image.source: GitHub repository URL
```

## Configuration

### Environment Variables

All environment variables are defined in the `.env` file:

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `BOT_DISCORD_TOKEN` | Yes | Discord bot token for authentication | `MTIzNDU2Nzg5MDEyMzQ1Njc4OQ...` |
| `BOT_OWNER_ID` | Yes | Discord username (NOT numeric ID) | `cauchy2384` |
| `LOG_LEVEL` | No | Logging level (info/debug/error) | `info` |

### Config File (`config.yaml`)

The `config.yaml` file defines bot behavior:

```yaml
# Polling interval - how often to check for new matches
poll_period: 1m

# Minimum match age - only show matches older than this
# (prevents showing ongoing/just-finished games)
played_gap: 60m

# Discord channel ID where match notifications are posted
channel_id: 932549761406476309

# List of players to track
players:
  - name: summoner_name     # In-game summoner name
    tag: euw                # Region tag (lowercase)
    region: euw             # Region for API (uppercase in API calls)
    real_name: Display Name # Friendly name shown in Discord
```

**To update configuration**:
1. Edit `config.yaml` on your host machine
2. Restart the container: `docker-compose restart`
3. The new config is loaded automatically (bind-mounted)

## Common Operations

### Start the Bot

```bash
# Start in background (detached mode)
docker-compose up -d

# Start in foreground (see logs directly)
docker-compose up
```

### Stop the Bot

```bash
# Stop gracefully
docker-compose down

# Stop and remove volumes (CAUTION: deletes game state)
docker-compose down -v
```

### View Logs

```bash
# Follow logs in real-time
docker-compose logs -f leaguewatcher

# View last 100 lines
docker-compose logs --tail=100 leaguewatcher

# View logs since specific time
docker-compose logs --since 10m leaguewatcher
```

### Restart the Bot

```bash
# Restart (e.g., after config change)
docker-compose restart

# Rebuild and restart (after code changes)
docker-compose build
docker-compose up -d
```

### Shell Access

```bash
# Open shell in running container
docker-compose exec leaguewatcher sh

# Inspect files
docker-compose exec leaguewatcher ls -la /app
docker-compose exec leaguewatcher cat /app/config.yaml
```

### Update to Latest Version

```bash
# Pull latest code
git pull origin main

# Rebuild image
docker-compose build --no-cache

# Restart with new image
docker-compose up -d
```

## Data Persistence

### How It Works

The bot creates two data files that persist across container restarts:

1. **`pidors.json`** - Game state for the "pidor of the day" game
2. **`log.json`** - Audit trail of all commands executed

These files are stored in a Docker named volume (`leaguewatcher-data`) that survives container recreation.

### Backup Data

```bash
# Backup pidors.json and log.json
docker run --rm \
  -v leaguewatcher_leaguewatcher-data:/app \
  -v $(pwd)/backup:/backup \
  alpine tar czf /backup/leaguewatcher-$(date +%Y%m%d).tar.gz \
    -C /app pidors.json log.json

# Verify backup
tar -tzf backup/leaguewatcher-*.tar.gz
```

### Restore Data

```bash
# Restore from backup
docker run --rm \
  -v leaguewatcher_leaguewatcher-data:/app \
  -v $(pwd)/backup:/backup \
  alpine tar xzf /backup/leaguewatcher-20260504.tar.gz -C /app

# Restart bot to use restored data
docker-compose restart
```

### Reset Game State

To clear the pidor game state and start fresh:

```bash
# Stop bot and delete volume
docker-compose down -v

# Start fresh (volume will be recreated)
docker-compose up -d
```

## Troubleshooting

### Bot Not Starting

**Check logs**:
```bash
docker-compose logs leaguewatcher
```

**Common issues**:

1. **"Can't open config file"**
   - Verify `config.yaml` exists in repository root
   - Check file permissions: `ls -la config.yaml`
   - Verify mount: `docker-compose exec leaguewatcher ls -la /app/config.yaml`

2. **No error but bot not responding in Discord**
   - Verify environment variables are set:
     ```bash
     docker-compose exec leaguewatcher sh -c 'echo $BOT_DISCORD_TOKEN | head -c 20'
     docker-compose exec leaguewatcher sh -c 'echo $BOT_OWNER_ID'
     ```
   - Check `.env` file exists and has correct values
   - Verify Discord bot token is valid

3. **"Config is invalid"**
   - Check `config.yaml` syntax (valid YAML)
   - Ensure all required fields are present
   - Verify `poll_period` and `played_gap` are valid durations (e.g., "1m", "60m")

### Container Crashes/Restarts

**View crash logs**:
```bash
docker-compose logs --tail=50 leaguewatcher
```

**Check container health**:
```bash
docker inspect leaguewatcher-bot | grep -A 10 Health
```

**Check resource usage**:
```bash
docker stats leaguewatcher-bot --no-stream
```

If memory exceeds 256MB, increase the limit in `docker-compose.yml`:
```yaml
deploy:
  resources:
    limits:
      memory: 512M
```

### Permission Denied on Data Files

**Fix ownership**:
```bash
# The container runs as UID 1000
docker-compose down
docker run --rm -v leaguewatcher_leaguewatcher-data:/app alpine chown -R 1000:1000 /app
docker-compose up -d
```

### Data Lost After Restart

**Verify volume exists**:
```bash
docker volume ls | grep leaguewatcher
```

**Check volume mount**:
```bash
docker inspect leaguewatcher-bot | grep -A 10 Mounts
```

If volume doesn't exist, it may have been deleted. Restore from backup (see Data Persistence section).

### High Memory Usage

**Current limits**: 256MB max, 128MB reserved

**To increase**:
Edit `docker-compose.yml`:
```yaml
deploy:
  resources:
    limits:
      memory: 512M  # Increase as needed
```

Then restart:
```bash
docker-compose up -d
```

### Bot Not Connecting to Discord

1. **Verify token**:
   ```bash
   # Check first 20 chars (don't print full token)
   docker-compose exec leaguewatcher sh -c 'echo $BOT_DISCORD_TOKEN | head -c 20'
   ```

2. **Check network connectivity**:
   ```bash
   docker-compose exec leaguewatcher ping -c 3 discord.com
   ```

3. **Verify bot has permissions in Discord server**:
   - Read Messages
   - Send Messages
   - Manage Members

4. **Check Discord API status**: https://discordstatus.com/

## Production Deployment

### Build and Tag for Registry

```bash
# Build production image
docker build -t leaguewatcher:1.0.0 .

# Tag for registry
docker tag leaguewatcher:1.0.0 registry.example.com/leaguewatcher:1.0.0
docker tag leaguewatcher:1.0.0 registry.example.com/leaguewatcher:latest

# Push to registry
docker push registry.example.com/leaguewatcher:1.0.0
docker push registry.example.com/leaguewatcher:latest
```

### Deploy to Production

```bash
# Create .env file with production credentials
cat > .env <<EOF
BOT_DISCORD_TOKEN=prod_token_here
BOT_OWNER_ID=prod_owner_username
LOG_LEVEL=info
EOF

# Create config.yaml with production settings
# (edit with production channel ID and players)

# Deploy
docker run -d \
  --name leaguewatcher \
  --restart unless-stopped \
  --env-file .env \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v leaguewatcher-data:/app \
  --memory=256m \
  --cpus=0.5 \
  registry.example.com/leaguewatcher:1.0.0

# Verify
docker logs -f leaguewatcher
```

### Health Checks

The container includes a built-in health check that runs every 30 seconds:

```bash
# Check health status
docker inspect leaguewatcher-bot | grep -A 5 '"Health"'

# Should show: "Status": "healthy"
```

### Resource Limits

Default limits (defined in `docker-compose.yml`):
- **CPU**: 0.5 cores max, 0.25 cores reserved
- **Memory**: 256MB max, 128MB reserved

These are appropriate for small to medium Discord servers. Adjust based on:
- Number of tracked players
- Polling frequency
- Server size/activity

### Security Considerations

1. **Non-root user**: Container runs as UID 1000 (not root)
2. **Read-only config**: Config file mounted read-only (`:ro`)
3. **No exposed ports**: Bot connects outbound only
4. **Secrets management**: Use `.env` file, never hardcode tokens
5. **Minimal base image**: Alpine Linux reduces attack surface
6. **Static binary**: No runtime dependencies to exploit

### Monitoring

**Check if bot is running**:
```bash
docker ps | grep leaguewatcher
# Should show: Up X hours (healthy)
```

**Monitor resource usage**:
```bash
docker stats leaguewatcher-bot
```

**Set up alerts** (example with Docker events):
```bash
# Monitor for container stops/restarts
docker events --filter 'container=leaguewatcher-bot' --filter 'event=stop' --filter 'event=die'
```

## Advanced Topics

### Multi-Architecture Support

Build for multiple architectures (AMD64, ARM64):

```bash
# Enable buildx
docker buildx create --use

# Build for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t leaguewatcher:latest \
  --push \
  .
```

### Custom Logging

To use production-grade logging:

1. Modify `main.go` to read `LOG_MODE` env var
2. Set in `.env`: `LOG_MODE=production`
3. Rebuild: `docker-compose build`

### CI/CD Integration

Example GitHub Actions workflow:

```yaml
name: Build and Push Docker Image

on:
  push:
    branches: [main]
    tags: ['v*']

jobs:
  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - uses: docker/setup-buildx-action@v2
      
      - uses: docker/login-action@v2
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      
      - uses: docker/build-push-action@v4
        with:
          context: .
          push: true
          tags: ghcr.io/${{ github.repository }}:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

## FAQ

**Q: Can I run multiple bot instances?**  
A: Yes, but each instance needs its own data volume and Discord channel. Create separate `docker-compose.yml` files with different service names and volumes.

**Q: How do I upgrade Go version?**  
A: Edit `Dockerfile` line 4: `FROM golang:1.XX-alpine AS builder`, then rebuild.

**Q: Can I use docker-compose v1?**  
A: Yes, but v2 is recommended. Remove `version: '3.8'` line for v1 compatibility.

**Q: Why is the image so small (~40MB)?**  
A: Multi-stage build discards build tools, and the binary is statically linked with debug symbols stripped.

**Q: Do I need to expose any ports?**  
A: No, the bot connects outbound to Discord and Mobalytics APIs. No inbound ports needed.

**Q: Can I use this with Kubernetes?**  
A: Yes, the Docker image works with Kubernetes. You'll need to create Deployment, ConfigMap, and Secret manifests. See `adr/001-docker-containerization.md` for considerations.

## Support

- **Issues**: https://github.com/cauchy2384/leaguewatcherbot/issues
- **Documentation**: See main [README.md](README.md) and [ARCHITECTURE.md](ARCHITECTURE.md)
- **ADR**: See [adr/001-docker-containerization.md](adr/001-docker-containerization.md) for design decisions

## License

MIT License - see [LICENSE](LICENSE)
