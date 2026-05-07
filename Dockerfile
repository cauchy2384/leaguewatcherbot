# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy dependency files first (layer caching optimization)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build static binary
# CGO_ENABLED=0: static linking for portability
# -ldflags="-w -s": strip debug symbols (~30% size reduction)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o leaguewatcherbot \
    cmd/leaguewatcher/main.go

# Verify binary exists
RUN test -f /build/leaguewatcherbot

# Runtime stage
FROM alpine:3.19 AS runtime

# Install runtime dependencies
# ca-certificates: HTTPS to Discord/Mobalytics
# tzdata: correct timestamps in logs/JSON files
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user for security
RUN addgroup -g 1000 leaguewatcher && \
    adduser -D -u 1000 -G leaguewatcher leaguewatcher

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/leaguewatcherbot /app/leaguewatcherbot

# Set ownership to non-root user
RUN chown -R leaguewatcher:leaguewatcher /app

# Switch to non-root user
USER leaguewatcher

# Health check - verify process is running
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD pgrep -f leaguewatcherbot || exit 1

# Run the bot
ENTRYPOINT ["/app/leaguewatcherbot"]
