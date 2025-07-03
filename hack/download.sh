#!/bin/bash


HERMES_VERSION=${1:-"v1.13.1"}
GAIAD_VERSION=${2:-"v24.0.0"}
COSMOS_RELAYER_VERSION=${3:-"v2.6.0"}

ARCH=$(uname -m)
OS=$(uname -s)

case "$ARCH" in
  "arm64")
    ARCH_LABEL="aarch64"
    ;;
  "x86_64")
    ARCH_LABEL="x86_64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

case "$OS" in
  "Darwin")
    OS_LABEL="apple-darwin"
    ;;
  "Linux")
    OS_LABEL="unknown-linux-gnu"
    ;;
  *)
    echo "Unsupported operating system: $OS"
    exit 1
    ;;
esac

HERMES_URL="https://github.com/informalsystems/hermes/releases/download/$HERMES_VERSION/hermes-$HERMES_VERSION-${ARCH_LABEL}-${OS_LABEL}.tar.gz"
GAIAD_URL="https://github.com/cosmos/gaia/releases/download/$GAIAD_VERSION/gaiad-$GAIAD_VERSION-darwin-$ARCH"

# For Cosmos Relayer, we need to map the architecture differently
COSMOS_RELAYER_ARCH="amd64"
if [ "$ARCH" == "arm64" ]; then
  COSMOS_RELAYER_ARCH="arm64"
fi

COSMOS_RELAYER_OS="linux"
if [ "$OS" == "Darwin" ]; then
  COSMOS_RELAYER_OS="darwin"
fi

COSMOS_RELAYER_URL="https://github.com/cosmos/relayer/releases/download/$COSMOS_RELAYER_VERSION/Cosmos.Relayer_${COSMOS_RELAYER_VERSION#v}_${COSMOS_RELAYER_OS}_${COSMOS_RELAYER_ARCH}.tar.gz"

if [ "$OS" == "Linux" ]; then
  GAIAD_URL="https://github.com/cosmos/gaia/releases/download/$GAIAD_VERSION/gaiad-$GAIAD_VERSION-linux-amd64"
fi

# Define output directories
DOWNLOAD_DIR="./downloads"
mkdir -p "$DOWNLOAD_DIR"

# Function to download a file
download_file() {
  local url=$1
  local output_path=$2

  echo "Downloading: $url"
  curl -L -o "$output_path" "$url"
  if [ $? -ne 0 ]; then
    echo "Failed to download $url"
    exit 1
  fi
}

# Download hermes
HERMES_ARCHIVE="$DOWNLOAD_DIR/hermes.tar.gz"
download_file "$HERMES_URL" "$HERMES_ARCHIVE"

# Extract hermes if tar.gz
if [[ "$HERMES_ARCHIVE" == *.tar.gz ]]; then
  echo "Extracting: $HERMES_ARCHIVE"
  tar -xzvf "$HERMES_ARCHIVE" -C "$DOWNLOAD_DIR"
elif [[ "$HERMES_ARCHIVE" == *.zip ]]; then
  echo "Extracting: $HERMES_ARCHIVE"
  unzip "$HERMES_ARCHIVE" -d "$DOWNLOAD_DIR"
fi

GAIAD_BINARY="$DOWNLOAD_DIR/gaiad"
download_file "$GAIAD_URL" "$GAIAD_BINARY"

# Make gaiad binary executable
chmod +x "$GAIAD_BINARY"

# Download Cosmos Relayer
COSMOS_RELAYER_ARCHIVE="$DOWNLOAD_DIR/cosmos-relayer.tar.gz"
download_file "$COSMOS_RELAYER_URL" "$COSMOS_RELAYER_ARCHIVE"

# Extract Cosmos Relayer
echo "Extracting: $COSMOS_RELAYER_ARCHIVE"
tar -xzvf "$COSMOS_RELAYER_ARCHIVE" --strip-components=1 -C "$DOWNLOAD_DIR"

echo "Hermes, Gaiad, and Cosmos Relayer downloaded successfully to $DOWNLOAD_DIR"
