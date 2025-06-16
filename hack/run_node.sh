#!/bin/bash

# Define paths
CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_PATH="gmd"
#ROLLINKY_HOME="${1:-"${CURRENT_DIR}/testnet/gm"}"

# Build and start local DA
echo "Building and starting local DA..."
# --- Kill previous local-da process if running ---
echo "Checking for existing local-da process on port 7980..."
# Find PID using lsof on the DA port (adjust if your DA uses a different port)
DA_PORT=7980
EXISTING_PID=$(lsof -ti tcp:${DA_PORT})

if [ -n "$EXISTING_PID" ]; then
    echo "Found existing processes on port $DA_PORT with PIDs: $EXISTING_PID. Killing..."
    # Kill the process(es)
    kill -9 $EXISTING_PID
    # Allow a moment for the OS to release the port
    sleep 2
else
    echo "No existing process found on port $DA_PORT."
fi
# --- End Kill previous local-da process ---

if [ -d "../../rollkit" ]; then
  cd ../../rollkit
  echo "Building local DA..."
  make build-da
  echo "Starting local DA in background..."
  ./build/local-da &
  DA_PID=$!
  echo "Local DA started with PID $DA_PID"
  cd - > /dev/null # Go back silently
  sleep 2 # Give DA a moment to start
else
  echo "Warning: ../../rollkit directory not found. Skipping local DA setup."
fi

echo "Starting the node from ${BINARY_PATH}"

# Execute the built binary
$BINARY_PATH start --rollkit.node.aggregator=true --rollkit.node.lazy_block_interval=1s #--home=${ROLLINKY_HOME}