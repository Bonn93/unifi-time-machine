# --- Stage 1: Test Runner ---
FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/go:latest-dev@sha256:6935d7096121c0a0ce4c26192c40bc7ed4ae9076d2567e6d46d6c32817934aff AS tester

WORKDIR /app

# Copy all source code for testing
COPY . .

# Run tests
RUN go test -v ./...

# --- Stage 2: Builder ---
# Built natively for each target platform: the binary links SQLite via cgo,
# which cannot cross-compile without a cross toolchain.
FROM cgr.dev/chainguard/go:latest-dev@sha256:6935d7096121c0a0ce4c26192c40bc7ed4ae9076d2567e6d46d6c32817934aff AS builder

# Copy source from the tester stage
COPY --from=tester /app /app
WORKDIR /app

# Build the final application binary
# Static linking is recommended for smaller, self-contained binaries on Linux
RUN CGO_ENABLED=1 go build -ldflags '-s -w -extldflags "-static"' -tags osusergo,netgo -o /unifi-time-machine ./cmd/server

# --- Stage 3: FFmpeg Builder ---
# Compile FFmpeg with the SVT-AV1, VP9 and x264 encoders the app uses
FROM cgr.dev/chainguard/wolfi-base:latest@sha256:360896efb0753a5701c48f860e17130c205eae5a264797aa3e7ea126ee2923a7 AS ffmpeg

RUN apk add --no-cache \
        build-base \
        nasm \
        pkgconf \
        curl \
        xz \
        zlib-dev \
        x264-dev \
        svt-av1-dev \
        libvpx-dev

COPY scripts/build-ffmpeg.sh /usr/local/bin/build-ffmpeg.sh
RUN sh /usr/local/bin/build-ffmpeg.sh /opt/ffmpeg

# --- Stage 4: Runtime Image ---
FROM cgr.dev/chainguard/wolfi-base:latest@sha256:360896efb0753a5701c48f860e17130c205eae5a264797aa3e7ea126ee2923a7 AS runtime

# Install runtime packages and the shared codec libraries FFmpeg links against
# ca-certificates is necessary for HTTPS connections (even with InsecureSkipVerify: true)
# tzdata ensures correct timezone handling if needed
COPY --from=ffmpeg /opt/ffmpeg/runtime-deps /tmp/ffmpeg-runtime-deps
RUN apk add --no-cache \
        dumb-init \
        ca-certificates \
        tzdata \
        $(cat /tmp/ffmpeg-runtime-deps) && \
    rm /tmp/ffmpeg-runtime-deps

# FFmpeg is crucial for video generation
COPY --from=ffmpeg /opt/ffmpeg/bin/ffmpeg /opt/ffmpeg/bin/ffprobe /usr/local/bin/

# Fail the build if an encoder the app relies on is missing
RUN ffmpeg -hide_banner -encoders | grep -q libsvtav1 && \
    ffmpeg -hide_banner -encoders | grep -q libvpx-vp9 && \
    ffmpeg -hide_banner -encoders | grep -q libx264

# Make appuser with UID and GID 1000
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser -s /bin/sh appuser

# Create application data directory and set permissions
RUN mkdir -p /app/data && chown -R appuser:appuser /app
VOLUME /app/data

# Switch to non-root user
USER appuser

WORKDIR /app

# Copy the compiled Go binary from the builder stage
COPY --from=builder /unifi-time-machine /usr/local/bin/unifi-time-machine

# --- ASSET COPY ---
COPY web ./web

# Expose the port
EXPOSE 8080

# Run the compiled application
ENTRYPOINT ["/usr/bin/dumb-init", "--", "/usr/local/bin/unifi-time-machine"]

# --- Stage 5: Vulnerability Scan ---
# Scan the runtime filesystem with Grype; the build fails on findings at or
# above GRYPE_FAIL_ON (negligible, low, medium, high, critical).
FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/grype:latest@sha256:24fa9dc23acfd5a8e2f40d6460df296c6d5861466e9d8ded2d99236c6b7ea38c AS scan

ARG GRYPE_FAIL_ON=critical
ENV GRYPE_FAIL_ON_SEVERITY=${GRYPE_FAIL_ON} \
    GRYPE_DB_CACHE_DIR=/tmp/grype-db

COPY --from=runtime / /scan
RUN ["grype", "dir:/scan", "-o", "table", "-o", "json=/tmp/grype-report.json"]

# --- Stage 6: Final Image ---
# Ship the scan report; copying it also makes the scan stage part of every build
FROM runtime

COPY --from=scan /tmp/grype-report.json /usr/share/unifi-time-machine/grype-report.json
