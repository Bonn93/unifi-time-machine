# --- Stage 1: Builder (Using a Debian-based Go image) ---
# golang:1.22-bullseye is a good choice for a stable build environment
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
RUN echo "I am running on $BUILDPLATFORM, building for $TARGETPLATFORM"

WORKDIR /app

# Copy dependency files and download modules
COPY go.mod .
COPY go.sum .
COPY cmd cmd
COPY pkg pkg
RUN go mod tidy
RUN go mod download

# Build the final application binary
# Static linking is recommended for smaller, self-contained binaries on Linux
ARG GOOS
ARG GOARCH
RUN CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH go build -ldflags '-s -w -extldflags "-static"' -tags osusergo,netgo -o /go-timelapse ./cmd/server


# --- Stage 2: Final Runtime Image ---
# Use a lightweight Debian image that still supports necessary libraries
FROM debian:bookworm-slim

# Install FFmpeg and other necessary runtime packages
# FFmpeg is crucial for video generation
# ca-certificates is necessary for HTTPS connections (even with InsecureSkipVerify: true)
# tzdata ensures correct timezone handling if needed
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ffmpeg \
        ca-certificates \
        tzdata && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the compiled Go binary from the builder stage
COPY --from=builder /go-timelapse /usr/local/bin/go-timelapse

# --- ASSET COPY FIXES ---

# 1. Copy the HTML file
# The Go application expects to find index.html in its working directory (`/app`).
COPY web/templates/index.html .
COPY web/templates/admin.html .
COPY web/templates/login.html .
COPY web/templates/error.html .

# We no longer need the /app/static/ directories since CSS and JS are inlined.

# --- END ASSET COPY FIXES ---

# Create the data volume directory as configured in main.go
# This is where snapshots, logs, and videos will be stored
RUN mkdir -p /app/data
VOLUME /app/data

# Expose the port
EXPOSE 8080

# Run the compiled application
ENTRYPOINT ["/usr/local/bin/go-timelapse"]