#!/bin/bash
set -x

# Function for cleanup on script interruption
cleanup() {
    log "31" "🛑" "Cleaning up processes..."
    if [ -n "$DA_PID" ]; then
        kill -9 "$DA_PID" 2>/dev/null || true
    fi
    if [ -n "$ROLLKIT_PID" ]; then
        kill -9 "$ROLLKIT_PID" 2>/dev/null || true
    fi
    exit 0
}

trap cleanup INT TERM

# Function for formatted logging
log() {
  local color=$1
  local emoji=$2
  local message=$3
  shift 3
  printf "\e[${color}m${emoji} [$(date '+%T')] ${message}\e[0m\n" "$@"
}


# Define paths
CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROLLKIT_BIN="gmd"
ROLLKIT_HOME="${1:-"${CURRENT_DIR}/testnet/gm"}"
LOCAL_DA_PATH="${2:-"../../rollkit/build/local-da"}"
CHAIN_ID="${3:-"rollkitnet-1"}"

# Clean previous configurations
log "32" "🔥" "Cleaning previous rollkit configurations..."
rm -rf "$ROLLKIT_HOME"

"$ROLLKIT_BIN" init my-rollkit-node --chain-id "$CHAIN_ID" --home "$ROLLKIT_HOME"

# Hardcoded mnemonic for validator account (igual que en Wordled)
VALIDATOR_MNEMONIC="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
#ATTESTER_MNEMONIC="tennis sponsor brick almost coyote soup rib wisdom warm bean onion tray devote pretty crime grid rough boil wear december travel inch work note"
RELAYER_MNEMONIC="reject camp lock magic dragon degree loop ignore quantum verify invest primary object afraid crane unveil parrot jelly rubber risk mirror globe torch category"
USER_MNEMONIC="sport head real antique sad expect ignore feature claim manual heavy mouse coil rebuild police flag robust picture milk symptom suffer chuckle worry virus"


echo "$VALIDATOR_MNEMONIC" | "$ROLLKIT_BIN" keys add validator \
  --keyring-backend test \
  --home "$ROLLKIT_HOME" \
  --recover > /dev/null 2>&1

#echo "$ATTESTER_MNEMONIC" | "$ROLLKIT_BIN" keys add attester \
#  --keyring-backend test \
#  --home "$ROLLKIT_HOME" \
#  --recover > /dev/null 2>&1

echo "$RELAYER_MNEMONIC" | "$ROLLKIT_BIN" keys add relayer \
  --keyring-backend test \
  --home "$ROLLKIT_HOME" \
  --recover > /dev/null 2>&1

echo "$USER_MNEMONIC" | "$ROLLKIT_BIN" keys add carl \
  --keyring-backend test \
  --home "$ROLLKIT_HOME" \
  --recover > /dev/null 2>&1

"$ROLLKIT_BIN" genesis add-genesis-account validator "10000000000000000stake" --keyring-backend test --home "$ROLLKIT_HOME"
#"$ROLLKIT_BIN" genesis add-genesis-account attester "10000000000000000stake" --keyring-backend test --home "$ROLLKIT_HOME"
"$ROLLKIT_BIN" genesis add-genesis-account relayer "10000000000000000stake" --keyring-backend test --home "$ROLLKIT_HOME"
"$ROLLKIT_BIN" genesis add-genesis-account carl "10000000000000000stake" --keyring-backend test --home "$ROLLKIT_HOME"


log "33" "📜" "Creating validator transaction..."
"$ROLLKIT_BIN" genesis gentx validator 1000000000stake \
  --chain-id "$CHAIN_ID" \
  --keyring-backend test \
  --home "$ROLLKIT_HOME"

# 5. Collect gentxs
log "32" "📦" "Collecting genesis transactions..."
"$ROLLKIT_BIN" genesis collect-gentxs --home "$ROLLKIT_HOME"

# Set validator in consensus block
log "32" "🔄" "Setting validator in consensus block..."
# Extract validator address and pubkey from validator key file, then modify genesis file to set the validator
jq -r '.address as $addr | .pub_key | { 
    address: $addr, 
    pub_key: { type: "tendermint/PubKeyEd25519", value: .value }, 
    power: "1000",
    name: "Rollkit Sequencer"
}' "$ROLLKIT_HOME/config/priv_validator_key.json" | \
jq --slurpfile genesis "$ROLLKIT_HOME/config/genesis.json" \
  '. as $validator | ($genesis[0] | .consensus.validators += [$validator])' > \
  "$ROLLKIT_HOME/config/tmp_genesis.json" && \
mv "$ROLLKIT_HOME/config/tmp_genesis.json" "$ROLLKIT_HOME/config/genesis.json"

# 6. Configure minimum gas prices
log "33" "⛽" "Setting minimum gas prices..."
sed -i.bak -E 's#minimum-gas-prices = ""#minimum-gas-prices = "0stake"#g' "$ROLLKIT_HOME/config/app.toml"

# 7. Modify consensus timeouts
log "34" "⏱️" "Adjusting consensus timeouts..."
sed -i.bak -E 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$ROLLKIT_HOME/config/config.toml"
sed -i.bak -E 's/timeout_propose = "3s"/timeout_propose = "1s"/g' "$ROLLKIT_HOME/config/config.toml"

# enable api
sed -i.bak 's#enable = false#enable = true#g' "$ROLLKIT_HOME/config/app.toml"



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


if [ ! -f "$LOCAL_DA_PATH" ]; then
  echo "Error: local-da binary not found at $LOCAL_DA_PATH"
  exit 1
else
  echo "Starting local DA in background..."
  $LOCAL_DA_PATH &
  DA_PID=$!
  echo "Local DA started with PID $DA_PID"
  sleep 2 # Give DA a moment to start
fi

log "35" "🚀" "Starting ROLLKIT node..."
"$ROLLKIT_BIN" start  --home "$ROLLKIT_HOME" --rollkit.node.aggregator --minimum-gas-prices "0stake"  --rollkit.node.lazy_block_interval=150ms --rollkit.node.block_time=100ms  --rollkit.da.block_time=500ms   --pruning=nothing --rollkit.network.soft-confirmation --log_level=debug &
ROLLKIT_PID=$!
log "36" "✅" "ROLLKIT chain running successfully!"
wait $ROLLKIT_PID
