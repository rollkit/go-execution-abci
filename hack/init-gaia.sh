#!/bin/bash

# Function for formatted logging
log() {
  local color=$1
  local emoji=$2
  local message=$3
  shift 3
  printf "\e[${color}m${emoji} [$(date '+%T')] ${message}\e[0m\n" "$@"
}

# Hardcoded mnemonic for validator account (igual que en Wordled)
VALIDATOR_MNEMONIC="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GAIA_HOME=${1:-"${CURRENT_DIR}/testnet"}
GAIAD_BIN=${2:-"${CURRENT_DIR}/downloads/gaiad"}

# Kill existing Gaia processes
log "31" "💀" "Checking for existing Gaia processes..."
GAIA_PID=$(pgrep gaiad)
if [ -n "$GAIA_PID" ]; then
  log "31" "🔪" "Killing existing Gaia process (PID: $GAIA_PID)..."
  kill -9 "$GAIA_PID"
else
  log "32" "👌" "No existing Gaia process found."
fi

# Clean previous configurations
log "32" "🔥" "Cleaning previous Gaia configurations..."
rm -rf "$GAIA_HOME"

# 1. Initialize Gaia chain
log "36" "🆕" "Initializing Gaia chain..."
"$GAIAD_BIN" init my-node --chain-id localnet-1 --home "$GAIA_HOME"

# 2. Create/Recover validator account usando la mnemónica fija
log "35" "👤" "Generating validator account from mnemonic..."
echo "$VALIDATOR_MNEMONIC" | "$GAIAD_BIN" keys add validator \
  --keyring-backend test \
  --home "$GAIA_HOME" \
  --recover > /dev/null 2>&1

# 3. Add account to genesis
log "34" "📝" "Adding account to genesis..."
"$GAIAD_BIN" genesis add-genesis-account validator 10000000000000000stake --keyring-backend test --home "$GAIA_HOME"

# 4. Generate gentx
log "33" "📜" "Creating validator transaction..."
"$GAIAD_BIN" genesis gentx validator 1000000000stake \
  --chain-id localnet-1 \
  --keyring-backend test \
  --home "$GAIA_HOME"

# 5. Collect gentxs
log "32" "📦" "Collecting genesis transactions..."
"$GAIAD_BIN" genesis collect-gentxs --home "$GAIA_HOME"

# 6. Configure minimum gas prices
log "36" "⛽" "Setting minimum gas prices..."
sed -i.bak -E 's#minimum-gas-prices = ""#minimum-gas-prices = "0stake"#g' "$GAIA_HOME/config/app.toml"

# 7. Modify consensus timeouts
log "35" "⏱️" "Adjusting consensus timeouts..."
sed -i.bak -E 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$GAIA_HOME/config/config.toml"
sed -i.bak -E 's/timeout_propose = "3s"/timeout_propose = "1s"/g' "$GAIA_HOME/config/config.toml"

# 8. Start Gaia chain
log "34" "🚀" "Starting Gaia node..."
"$GAIAD_BIN" start --home "$GAIA_HOME" --minimum-gas-prices "0stake" --rpc.laddr tcp://0.0.0.0:26654 --rpc.pprof_laddr localhost:6061 --p2p.laddr tcp://0.0.0.0:26653 --grpc.address 0.0.0.0:9091  | tee "$GAIA_HOME/gaia.log" &
GAIA_PID=$!

# Wait for initialization
log "33" "⏳" "Waiting for Gaia initialization (port 26667)..."
while ! nc -z localhost 26667; do
  sleep 1
done
log "32" "✅" "Gaia chain running successfully!"

# Show recent logs
log "36" "📄" "Last lines of Gaia log:"
tail -n 5 "$GAIA_HOME/gaia.log"

# Keep script alive
log "35" "👀" "Monitoring Gaia chain activity..."
wait $GAIA_PID