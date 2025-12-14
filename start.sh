#!/bin/bash

# --- Configuration ---
# This script configures and launches the unifi-time-machine Docker container.
# All application settings are controlled by the environment variables below.

# Set the local directory where snapshots and videos will be stored.
# This directory will be mounted into the Docker container.
LOCAL_DATA_DIR="$PWD/data"

# --- Docker Container Settings ---
DOCKER_IMAGE="mbern/unifi-time-machine:latest"
CONTAINER_NAME="unifi-time-machine"
HTTP_PORT="8000" # The external port to access the web UI.

# --- UniFi Protect Integration Settings ---
# The IP address or hostname of your UniFi Protect controller.
UFP_HOST="192.168.1.1"

# The API key from your UniFi Protect user account.
# This must be set as an environment variable.
# export UFP_API_KEY="your_key_here"

# The ID of the camera you want to capture snapshots from.
# You can find this in the URL when viewing the camera in the Protect web UI.
TARGET_CAMERA_ID="68ccb308002eec03e40320d9"


# --- Authentication Settings ---
# A mandatory, base64-encoded secret key for application security.
# This must be set as an environment variable.
# export APP_KEY=$(head -c 32 /dev/urandom | base64)

# The password for the initial 'admin' user, created on first launch.
# This is required to be set on the first run. It can be changed or removed later.
ADMIN_PASSWORD="VerySecure123"


# --- Timelapse and Video Generation Settings ---
# The interval, in seconds, between each snapshot.
# Default is 3600 (1 hour). A value of 2 is very frequent.
TIMELAPSE_INTERVAL="60"

# The interval, in seconds, at which to generate a new timelapse video.
# Default is 300 (5 minutes).
VIDEO_CRON_INTERVAL="600"

# The quality of the generated video.
# Options: low, medium, high, ultra
VIDEO_QUALITY="ultra"


# --- Data Retention and Cleanup Settings ---
# The number of old archived timelapse videos to keep.
VIDEO_ARCHIVES_TO_KEEP="3"
# The directory inside the container for storing snapshots.
SNAPSHOTS_DIR="snapshots"
# The directory inside the container for storing gallery images.
GALLERY_DIR="gallery"
# The path to the ffmpeg log file.
FFMPEG_LOG_PATH="ffmpeg_log.txt"

# (Optional) The directory inside the container for storing data.
# This is set to match the Docker volume target path.
# DATA_DIR="/app/data"

# --- Gin Web Framework Settings ---
# Set the Gin mode to 'release' for production use.
GIN_MODE="release"

# --- Script Execution ---

# Check for required environment variables
if [ -z "$UFP_API_KEY" ]; then
  echo "ERROR: UFP_API_KEY environment variable is not set."
  exit 1
fi

if [ -z "$APP_KEY" ]; then
  echo "ERROR: APP_KEY environment variable is not set."
  exit 1
fi

echo "==> Ensuring data directory exists at: $LOCAL_DATA_DIR"
mkdir -p "$LOCAL_DATA_DIR"

echo "==> Stopping and removing any existing container named '$CONTAINER_NAME'..."
docker stop "$CONTAINER_NAME" >/dev/null 2>&1 || true
docker rm "$CONTAINER_NAME" >/dev/null 2>&1

echo "==> Starting new container '$CONTAINER_NAME'..."
docker run -d --name "$CONTAINER_NAME" \
  --restart=always \
  -p "$HTTP_PORT":8080 \
  -e TZ="Australia/Sydney" \
  -e APP_KEY="$APP_KEY" \
  -e ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  -e UFP_HOST="$UFP_HOST" \
  -e UFP_API_KEY="$UFP_API_KEY" \
  -e TARGET_CAMERA_ID="$TARGET_CAMERA_ID" \
  -e TIMELAPSE_INTERVAL="$TIMELAPSE_INTERVAL" \
  -e VIDEO_CRON_INTERVAL="$VIDEO_CRON_INTERVAL" \
  -e VIDEO_ARCHIVES_TO_KEEP="$VIDEO_ARCHIVES_TO_KEEP" \
  -e VIDEO_QUALITY="$VIDEO_QUALITY" \
  -e SNAPSHOTS_DIR="$SNAPSHOTS_DIR" \
  -e GALLERY_DIR="$GALLERY_DIR" \
  -e FFMPEG_LOG_PATH="$FFMPEG_LOG_PATH" \
  -e GIN_MODE="$GIN_MODE" \
  -v "$LOCAL_DATA_DIR":/app/data \
  "$DOCKER_IMAGE"

echo "==> Done. Container '$CONTAINER_NAME' is starting."
echo "    Web UI will be available at http://localhost:$HTTP_PORT"
echo "    To view logs, run: docker logs -f $CONTAINER_NAME"
