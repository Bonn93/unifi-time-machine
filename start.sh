#!/bin/bash

# --- Configuration ---
# This script configures and launches the unifi-time-machine Docker container.
# All application settings are controlled by the environment variables below.

# Set the local directory where snapshots and videos will be stored.
# This directory will be mounted into the Docker container.
LOCAL_DATA_DIR="$PWD/data"

# --- Docker Container Settings ---
TAG="v0.0.4"
DOCKER_IMAGE="mbern/unifi-time-machine:$TAG"
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
TARGET_CAMERA_ID="68cbac880021ec03e401c50e"


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

# --- High-Quality Snapshot Settings ---
# Determines if high-quality snapshots are used.
# "true": Always use high-quality snapshots.
# "false": Never use high-quality snapshots.
# "auto": (Default) Check camera capability at startup and use if supported.
HQSNAP="auto"


# --- Data Retention and Cleanup Settings ---
# The number of days of 24-hour daily timelapses to generate and keep.
DAYS_OF_24_HOUR_SNAPSHOTS="30"
# The number of days to retain individual snapshots.
SNAPSHOT_RETENTION_DAYS="30"
# The number of days to retain gallery images.
GALLERY_RETENTION_DAYS="365"
# The number of hours a shared link is valid for. Set to 0 for unlimited.
SHARE_LINK_EXPIRY_HOURS="24"
# Number of calendar-week timelapses (Mon–Sun) to keep. Default is 4.
WEEKLY_LAPSES_TO_KEEP="4"
# Number of calendar-month timelapses to keep. Default is 3.
MONTHLY_LAPSES_TO_KEEP="3"
# The directory inside the container for storing snapshots.
SNAPSHOTS_DIR="snapshots"
# The directory inside the container for storing gallery images.
GALLERY_DIR="gallery"

# (Optional) The directory inside the container for storing data.
# This is set to match the Docker volume target path.
# DATA_DIR="/app/data"

# --- Formatting Settings ---
# The format for displaying dates (e.g. DD/MM/YYYY, MM/DD/YYYY, YYYY-MM-DD)
DATE_FORMAT="DD/MM/YYYY"
# The format for displaying times (e.g. 12h, 24h)
TIME_FORMAT="12h"

# --- Daylight Filtering Settings ---
# Only images taken within this hour range are included in weekly/monthly/yearly timelapses.
# The 24-hour daily timelapse always includes all hours regardless of this setting.
# Default: 7 (7am) to 19 (7pm). Set to 0 and 24 to disable filtering entirely.
DAYLIGHT_START_HOUR="7"
DAYLIGHT_END_HOUR="19"
# Target hour for the daily frame picker used by monthly timelapses.
# The image closest to this hour is chosen for each day. Default is 12 (noon).
DAYLIGHT_TARGET_HOUR="12"

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
  -e DAYS_OF_24_HOUR_SNAPSHOTS="$DAYS_OF_24_HOUR_SNAPSHOTS" \
  -e SNAPSHOT_RETENTION_DAYS="$SNAPSHOT_RETENTION_DAYS" \
  -e GALLERY_RETENTION_DAYS="$GALLERY_RETENTION_DAYS" \
  -e VIDEO_QUALITY="$VIDEO_QUALITY" \
  -e SNAPSHOTS_DIR="$SNAPSHOTS_DIR" \
  -e GALLERY_DIR="$GALLERY_DIR" \
  -e GIN_MODE="$GIN_MODE" \
  -e HQSNAP="$HQSNAP" \
  -e SHARE_LINK_EXPIRY_HOURS="$SHARE_LINK_EXPIRY_HOURS" \
  -e DATE_FORMAT="$DATE_FORMAT" \
  -e TIME_FORMAT="$TIME_FORMAT" \
  -e DAYLIGHT_START_HOUR="$DAYLIGHT_START_HOUR" \
  -e DAYLIGHT_END_HOUR="$DAYLIGHT_END_HOUR" \
  -e DAYLIGHT_TARGET_HOUR="$DAYLIGHT_TARGET_HOUR" \
  -e WEEKLY_LAPSES_TO_KEEP="$WEEKLY_LAPSES_TO_KEEP" \
  -e MONTHLY_LAPSES_TO_KEEP="$MONTHLY_LAPSES_TO_KEEP" \
  -v "$LOCAL_DATA_DIR":/app/data \
  "$DOCKER_IMAGE"

echo "==> Done. Container '$CONTAINER_NAME' is starting."
echo "    Web UI will be available at http://localhost:$HTTP_PORT"
echo "    To view logs, run: docker logs -f $CONTAINER_NAME"
