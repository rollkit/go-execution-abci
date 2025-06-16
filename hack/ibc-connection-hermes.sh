#!/bin/bash
set -e

# Configuration variables
GAIA_CHAIN_ID="localnet-1"
WORDLED_CHAIN_ID="gm"

GAIA_RPC="http://localhost:26654"
WORDLED_RPC="http://localhost:26657"

GAIA_GRPC="http://localhost:9091"
WORDLED_GRPC="http://localhost:9090"

GAIA_DENOM="stake"
WORDLED_DENOM="stake"

RELAYER_WALLET="validator"  # Name of validator account

# Hardcoded mnemonic (must match initialization scripts)
VALIDATOR_MNEMONIC="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

# Logging functions
log_info() {
    echo "[INFO] $1"
}

log_success() {
    echo "[SUCCESS] $1"
}

log_error() {
    echo "[ERROR] $1" >&2
}

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HERMES_BIN="${HERMES_BIN:-$CURRENT_DIR/downloads/hermes}"

# Verify Hermes is installed
if [ ! -f "$HERMES_BIN" ]; then
    log_error "Hermes not found at $HERMES_BIN"
    exit 1
fi
log_success "Hermes installed"

CONFIG_DIR="${CURRENT_DIR}/testnet/hermes"
rm -rf "$CONFIG_DIR"
mkdir -p "$CONFIG_DIR"

# Generate configuration file (config.toml)
CONFIG_FILE="$CONFIG_DIR/config.toml"
log_info "Generating Hermes configuration at $CONFIG_FILE..."

cat <<EOF > "$CONFIG_FILE"
[global]
log_level = "trace"

[mode]
  [mode.clients]
  enabled = true
  refresh = true
  misbehaviour = true
  [mode.connections]
  enabled = true
  [mode.channels]
  enabled = true
  [mode.packets]
  enabled = true

[[chains]]
id = "$GAIA_CHAIN_ID"
rpc_addr = "$GAIA_RPC"
grpc_addr = "$GAIA_GRPC"
event_source = { mode = "pull", interval = "1s", max_retries = 4 }
store_prefix = "ibc"
account_prefix = "cosmos"
key_name = "$RELAYER_WALLET"
gas_price = { price = 3.5, denom = "stake" }
gas_multiplier = 1.4
rpc_timeout = "10s"
trusting_period = "503h"
clock_drift = "10s"
key_store_folder = "$CONFIG_DIR/$GAIA_CHAIN_ID-keys"

[[chains]]
id = "$WORDLED_CHAIN_ID"
rpc_addr = "$WORDLED_RPC"
grpc_addr = "$WORDLED_GRPC"
store_prefix = "ibc"
event_source = { mode = "pull", interval = "1s", max_retries = 4 }
account_prefix = "gm"
key_name = "$RELAYER_WALLET"
gas_price = { price = 0.025, denom = "stake" }
gas_multiplier = 1.4
rpc_timeout = "10s"
trusting_period = "503h"
clock_drift = "10s"
key_store_folder = "$CONFIG_DIR/$WORDLED_CHAIN_ID-keys"
EOF

log_success "Configuration file generated"

# Import keys to relayer (Hermes) using mnemonic
TMP_MNEMONIC=$(mktemp)
echo "$VALIDATOR_MNEMONIC" > "$TMP_MNEMONIC"

log_info "Importing key for $GAIA_CHAIN_ID..."
"$HERMES_BIN" --config "$CONFIG_FILE" keys add --chain "$GAIA_CHAIN_ID" --mnemonic-file "$TMP_MNEMONIC"
log_success "Key imported for $GAIA_CHAIN_ID"

log_info "Importing key for $WORDLED_CHAIN_ID..."
"$HERMES_BIN" --config "$CONFIG_FILE" keys add --chain "$WORDLED_CHAIN_ID" --mnemonic-file "$TMP_MNEMONIC"
log_success "Key imported for $WORDLED_CHAIN_ID"

rm "$TMP_MNEMONIC"

# Show configured addresses (optional)
log_info "Showing configured addresses:"
"$HERMES_BIN" --config "$CONFIG_FILE" keys list --chain "$GAIA_CHAIN_ID"
"$HERMES_BIN" --config "$CONFIG_FILE" keys list --chain "$WORDLED_CHAIN_ID"

# Create IBC channel between chains
log_info "Creating IBC channel between $GAIA_CHAIN_ID and $WORDLED_CHAIN_ID..."
                       "$HERMES_BIN" --config "$CONFIG_FILE" create channel --a-chain "$GAIA_CHAIN_ID" --a-port transfer \
                       --b-chain "$WORDLED_CHAIN_ID" --b-port transfer \
                       --new-client-connection --yes
log_success "IBC channel created"

# Start Hermes
log_info "Starting Hermes..."
"$HERMES_BIN" --config "$CONFIG_FILE" start