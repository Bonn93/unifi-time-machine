#!/bin/bash

# --- Configuration ---
# This script configures and launches the unifi-time-machine Docker container.
# All application settings are controlled by the environment variables below.

# Set the local directory where snapshots and videos will be stored.
# This directory will be mounted into the Docker container.
LOCAL_DATA_DIR="$PWD/data"

# --- Docker Container Settings ---
DOCKER_IMAGE="unifi-time-machine:latest"
CONTAINER_NAME="unifi-time-machine"
HTTP_PORT="8000" # The external port to access the web UI.

# --- UniFi Protect Integration Settings ---
# The IP address or hostname of your UniFi Protect controller.
UFP_HOST="192.168.1.1"

# The API key from your UniFi Protect user account.
# Go to your UniFi OS Console -> Users -> Your Account -> API Keys -> Generate New.
UFP_API_KEY=""

# The ID of the camera you want to capture snapshots from.
# You can find this in the URL when viewing the camera in the Protect web UI.
TARGET_CAMERA_ID="68ccb308002eec03e40320d9"


# --- Timelapse and Video Generation Settings ---
# The interval, in seconds, between each snapshot.
# Default is 3600 (1 hour). A value of 2 is very frequent.
TIMELAPSE_INTERVAL="2"

# The interval, in seconds, at which to generate a new timelapse video.
# Default is 300 (5 minutes).
VIDEO_CRON_INTERVAL="300"


# --- Data Retention and Cleanup Settings ---
# The number of old archived timelapse videos to keep.
# The 'timelapse_latest.webm' is not included in this count.
VIDEO_ARCHIVES_TO_KEEP="3"

# (Optional) The directory inside the container for storing data.
# This is set to match the Docker volume target path.
# DATA_DIR="/app/data"

# --- Script Execution ---

echo "==> Ensuring data directory exists at: $LOCAL_DATA_DIR"
mkdir -p "$LOCAL_DATA_DIR"

echo "==> Stopping and removing any existing container named '$CONTAINER_NAME' நானி..."
docker stop "$CONTAINER_NAME"
docker rm "$CONTAINER_NAME"

echo "==> Starting new container '$CONTAINER_NAME' நானி..."
docker run -d --name "$CONTAINER_NAME" \
  -p "$HTTP_PORT":8080 \
  -e UFP_HOST="$UFP_HOST" \
  -e UFP_API_KEY="$UFP_API_KEY" \
  -e TARGET_CAMERA_ID="$TARGET_CAMERA_ID" \
  -e TIMELAPSE_INTERVAL="$TIMELAPSE_INTERVAL" \
  -e VIDEO_CRON_INTERVAL="$VIDEO_CRON_INTERVAL" \
  -e VIDEO_ARCHIVES_TO_KEEP="$VIDEO_ARCHIVES_TO_KEEP" \
  -v "$LOCAL_DATA_DIR":/app/data \
  "$DOCKER_IMAGE"

echo "==> Done. Container '$CONTAINER_NAME' is starting."
echo "    Web UI will be available at http://localhost:$HTTP_PORT"
echo "    To view logs, run: docker logs -f $CONTAINER_NAME"
