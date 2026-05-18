#!/bin/bash

# UniFi Time Machine — quick-start script
#
# Set the four required values below (or export UFP_API_KEY and APP_KEY
# as environment variables before running this script), then execute:
#   bash start.sh
#
# All other settings (video format, quality, retention, scheduling, etc.)
# are configured at runtime via Admin → Settings — no restart required.

# ── Required ──────────────────────────────────────────────────────────────────

LOCAL_DATA_DIR="$PWD/data"
DOCKER_IMAGE="mbern/unifi-time-machine:latest"
CONTAINER_NAME="unifi-time-machine"
HTTP_PORT="8000"

UFP_HOST="192.168.1.1"
TARGET_CAMERA_ID="your_camera_id_here"
ADMIN_PASSWORD="ChangeMe123"

# Export these before running, or set them here:
# export UFP_API_KEY="your_api_key_here"
# export APP_KEY=$(head -c 32 /dev/urandom | base64)

# ── Optional: seed initial DB settings (first launch only) ────────────────────
# Uncomment to override defaults on first run. Ignored on every subsequent start.
# Use Admin → Settings in the UI to change them after that.
#
# VIDEO_FORMAT="hls"         # webm (AV1) | mp4 (H.264) | hls (adaptive, recommended)
# VIDEO_QUALITY="high"       # low | medium | high | ultra
# TIMELAPSE_INTERVAL="3600"  # seconds between snapshots
# HQSNAP="auto"              # auto | true | false

# ── Pre-flight checks ─────────────────────────────────────────────────────────

if [ -z "$UFP_API_KEY" ]; then
  echo "ERROR: UFP_API_KEY is not set. Export it or set it at the top of this script."
  exit 1
fi

if [ -z "$APP_KEY" ]; then
  echo "ERROR: APP_KEY is not set. Export it or generate one with: head -c 32 /dev/urandom | base64"
  exit 1
fi

# ── Launch ────────────────────────────────────────────────────────────────────

mkdir -p "$LOCAL_DATA_DIR"

echo "==> Stopping any existing container named '$CONTAINER_NAME'..."
docker stop "$CONTAINER_NAME" >/dev/null 2>&1 || true
docker rm   "$CONTAINER_NAME" >/dev/null 2>&1 || true

echo "==> Starting '$CONTAINER_NAME'..."
docker run -d --name "$CONTAINER_NAME" \
  --restart=always \
  -p "$HTTP_PORT":8080 \
  -e TZ="Australia/Sydney" \
  -e GIN_MODE="release" \
  -e UFP_HOST="$UFP_HOST" \
  -e UFP_API_KEY="$UFP_API_KEY" \
  -e TARGET_CAMERA_ID="$TARGET_CAMERA_ID" \
  -e APP_KEY="$APP_KEY" \
  -e ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  ${VIDEO_FORMAT:+-e VIDEO_FORMAT="$VIDEO_FORMAT"} \
  ${VIDEO_QUALITY:+-e VIDEO_QUALITY="$VIDEO_QUALITY"} \
  ${TIMELAPSE_INTERVAL:+-e TIMELAPSE_INTERVAL="$TIMELAPSE_INTERVAL"} \
  ${HQSNAP:+-e HQSNAP="$HQSNAP"} \
  -v "$LOCAL_DATA_DIR":/app/data \
  "$DOCKER_IMAGE"

echo "==> Done. Web UI: http://localhost:$HTTP_PORT"
echo "    Logs: docker logs -f $CONTAINER_NAME"
