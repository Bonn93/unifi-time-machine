#!/bin/bash

# This script builds the Docker image for the unifi-time-machine application.
# It supports building for both x86 (amd64) and ARM64 architectures.

set -e

# --- Configuration ---
DOCKER_REPO="mbern"
IMAGE_NAME="unifi-time-machine"
DEFAULT_TAG="latest"

# --- Script Logic ---
TAG="${1:-$DEFAULT_TAG}"
PLATFORMS="linux/amd64,linux/arm64"

echo "==> Building and pushing multi-arch Docker image..."
echo "    Image: $DOCKER_REPO/$IMAGE_NAME:$TAG"
echo "    Platforms: $PLATFORMS"

# Ensure buildx is available
if ! docker buildx inspect mybuilder > /dev/null 2>&1; then
  docker buildx create --name mybuilder --use
else
  docker buildx use mybuilder
fi

# Build and push the image
docker buildx build \
  --platform "$PLATFORMS" \
  --tag "$DOCKER_REPO/$IMAGE_NAME:$TAG" \
  --tag "$DOCKER_REPO/$IMAGE_NAME:$(date +%Y%m%d)" \
  --push \
  .

echo "==> Done."

