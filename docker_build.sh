#!/bin/bash

# Function to print usage
print_usage() {
  echo "Usage: $0 [-i image] [-p architecture] [-b use_buildpack]"
  echo "  -i image           Docker image (e.g., krasaee/alethic-ism-stream-api:latest)"
  echo "  -p platform        Target platform architecture (linux/amd64, linux/arm64, ...)"
  echo "  -b                 Use buildpack instead of direct Docker build (optional)"
}

# Default values
ARCH="linux/amd64"
USE_BUILDPACK=false

# Parse command line arguments
while getopts 'i:p:b' flag; do
  case "${flag}" in
    i) IMAGE="${OPTARG}" ;;
    p) ARCH="${OPTARG}" ;;
    b) USE_BUILDPACK=true ;;
    *) print_usage
       exit 1 ;;
  esac
done

# Check if IMAGE is provided
if [ -z "$IMAGE" ]; then
  echo "Error: Image name is required"
  print_usage
  exit 1
fi

# derive tag for latest version
LATEST=$(echo $IMAGE | sed -e 's/\:.*$/:latest/g')

# Display arguments
echo "Platform: $ARCH"
echo "Image: $IMAGE"
echo "Using Buildpack: $USE_BUILDPACK"

if [ "$USE_BUILDPACK" = true ]; then
  echo "Building with buildpack..."
#  pack build "$IMAGE" \
#    --builder paketobuildpacks/builder-jammy-buildpackless-static \
#    --buildpack paketo-buildpacks/go \
#    --env "CGO_ENABLED=0" \
#    --env "BP_GO_BUILD_FLAGS=-buildmode=default" \
#    --env BP_PLATFORM_API="$ARCH"

pack build "$IMAGE" \
  --builder paketobuildpacks/builder-jammy-buildpackless-static \
  --buildpack paketo-buildpacks/go \
  --env "CGO_ENABLED=0" \
  --env "BP_GO_BUILD_FLAGS=-buildmode=default" \
  --env "GOPRIVATE=github.com/quantumwake/*" \
  --env "GIT_AUTH_TOKEN=$GITHUB_TOKEN" \
  --env BP_PLATFORM_API="$ARCH"

#    --builder paketobuildpacks/builder:base \
#    --path . \
#    --env BP_DOCKERFILE=Dockerfile \
else
  echo "Building with Docker..."
  docker build --progress=plain \
    --platform "$ARCH" -t "$IMAGE" -t $LATEST \
    --no-cache .
fi