# --- Stage 1: Test Runner ---
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS tester

WORKDIR /app

# Copy all source code for testing
COPY . .

# Run tests
RUN go test -v ./...

# --- Stage 2: Builder (Using a Debian-based Go image) ---
# golang:1.22-bullseye is a good choice for a stable build environment
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

# Copy source from the tester stage
COPY --from=tester /app /app
WORKDIR /app

ARG TARGETPLATFORM
ARG BUILDPLATFORM
RUN echo "I am running on $BUILDPLATFORM, building for $TARGETPLATFORM"

# Build the final application binary
# Static linking is recommended for smaller, self-contained binaries on Linux
ARG GOOS
ARG GOARCH
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH go build -ldflags '-s -w -extldflags "-static"' -tags osusergo,netgo -o /unifi-time-machine ./cmd/server


# --- Stage 3: Final Runtime Image ---
# Use a lightweight Debian image that still supports necessary libraries
FROM debian:bookworm-slim

# Install FFmpeg and other necessary runtime packages
# FFmpeg is crucial for video generation
# ca-certificates is necessary for HTTPS connections (even with InsecureSkipVerify: true)
# tzdata ensures correct timezone handling if needed
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        dumb-init \
        ffmpeg \
        ca-certificates \
        tzdata && \
    rm -rf /var/lib/apt/lists/*

# Make appuser with UID and GID 1000
RUN groupadd -g 1000 appuser
RUN useradd -m -u 1000 -g 1000 -s /bin/bash appuser

# Create application data directory and set as a volume
RUN mkdir -p /app/data
VOLUME /app/data

# set permissions
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

WORKDIR /app

# Copy the compiled Go binary from the builder stage
COPY --from=builder /unifi-time-machine /usr/local/bin/unifi-time-machine

# --- ASSET COPY ---
COPY web/templates/index.html .
COPY web/templates/admin.html .
COPY web/templates/login.html .
COPY web/templates/error.html .
COPY web/templates/log.html .


RUN mkdir -p /app/data
VOLUME /app/data

# Expose the port
EXPOSE 8080

# Run the compiled application
ENTRYPOINT ["/usr/bin/dumb-init", "--", "/usr/local/bin/unifi-time-machine"]