# ════════════════════════════════════════════════════════════════════════════════
# Stage 1 — Builder
#
# Uses the full Go SDK to compile a static binary.
# Nothing from this stage leaks into the final image.
# ════════════════════════════════════════════════════════════════════════════════
FROM golang:1.22-alpine AS builder

# Install git (required by `go mod download` for private/VCS deps)
# and ca-certificates (so the binary can make TLS calls during build if needed)
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# ── Dependency layer (cached unless go.mod / go.sum change) ──────────────────
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# ── Source layer ──────────────────────────────────────────────────────────────
COPY . .

# ── Compile ───────────────────────────────────────────────────────────────────
# CGO_ENABLED=0   → fully static binary, no libc dependency
# GOOS/GOARCH     → explicit target (safe for cross-compilation on CI)
# -ldflags
#   -s   strip debug symbol table  (~30 % smaller)
#   -w   strip DWARF info          (additional size reduction)
#   -extldflags "-static"  ensure no dynamic linking slips through
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -ldflags="-s -w -extldflags '-static'" \
      -trimpath \
      -o /build/geo-api \
      ./cmd/main.go


# ════════════════════════════════════════════════════════════════════════════════
# Stage 2 — Final (minimal runtime image)
#
# Uses 'alpine' rather than 'scratch' so that:
#   • We have a shell for `docker exec` debugging in production
#   • Health-check tools (wget/curl) are available for orchestrators
#   • CA certificates are present for TLS to external services
# ════════════════════════════════════════════════════════════════════════════════
FROM alpine:3.19 AS final

# Security: run as a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy trusted CA bundle (needed for TLS to Typesense / Redis in cloud)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Copy timezone data (for time.LoadLocation calls if needed)
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy ONLY the compiled binary — no source, no SDK, no secrets
COPY --from=builder /build/geo-api /usr/local/bin/geo-api

# Switch to non-root
USER appuser

# ── Runtime configuration ────────────────────────────────────────────────────
# All secrets (POSTGRES_*, REDIS_*, TYPESENSE_*) MUST be injected by the
# cloud provider (Railway / Cloud Run / ECS) as environment variables at
# runtime. Nothing is hardcoded here.
#
# Sensible defaults for non-secret values only:
ENV APP_ENV=production \
    APP_PORT=8080 \
    LOG_LEVEL=info

EXPOSE 8080

# Use ENTRYPOINT so the binary is PID 1 (receives OS signals cleanly)
# No CMD args needed — the binary reads everything from env.
ENTRYPOINT ["/usr/local/bin/geo-api"]
