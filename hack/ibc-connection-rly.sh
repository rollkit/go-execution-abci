#!/bin/bash
set -e

# Configuration variables
GAIA_CHAIN_ID="localnet-1"
WORDLED_CHAIN_ID="rollkitnet-1"

GAIA_RPC="http://localhost:26654"
WORDLED_RPC="http://localhost:26657"

GAIA_GRPC="http://localhost:9091"
WORDLED_GRPC="http://localhost:9090"

GAIA_DENOM="stake"
WORDLED_DENOM="stake"

RELAYER_WALLET="relayer"  # Name of relayer account

# Hardcoded mnemonic (must match initialization scripts)
RELAYER_MNEMONIC="reject camp lock magic dragon degree loop ignore quantum verify invest primary object afraid crane unveil parrot jelly rubber risk mirror globe torch category"

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
RELAYER_BIN="${RELAYER_BIN:-$CURRENT_DIR/downloads/rly}"


# Verify Go IBC Relayer is installed
if ! command -v $RELAYER_BIN &> /dev/null; then
    log_error "Go IBC Relayer not found. Please install it with: go install github.com/cosmos/relayer/v2@latest"
    exit 1
fi
log_success "Go IBC Relayer installed"

CONFIG_DIR="${CURRENT_DIR}/testnet/go-relayer"
rm -rf "$CONFIG_DIR"
mkdir -p "$CONFIG_DIR"

# Initialize the relayer configuration
log_info "Initializing Go IBC Relayer configuration..."
$RELAYER_BIN config init --home "$CONFIG_DIR"
log_success "Relayer configuration initialized"

# Add chains to the relayer configuration
log_info "Adding $GAIA_CHAIN_ID chain to the relayer..."
cat <<EOF > gaia-config.json
{
  "type": "cosmos",
  "value": {
    "key": "$RELAYER_WALLET",
    "chain-id": "$GAIA_CHAIN_ID",
    "rpc-addr": "$GAIA_RPC",
    "grpc-addr": "$GAIA_GRPC",
    "account-prefix": "cosmos",
    "keyring-backend": "test",
    "gas-adjustment": 2.0,
    "gas-prices": "0.1$GAIA_DENOM",
    "debug": true,
    "timeout": "10s",
    "output-format": "json",
    "sign-mode": "direct",
    "trusting-period": "504h"
  }
}
EOF
$RELAYER_BIN chains add $GAIA_CHAIN_ID -f gaia-config.json --home "$CONFIG_DIR"
log_success "$GAIA_CHAIN_ID chain added"

log_info "Adding $WORDLED_CHAIN_ID chain to the relayer..."
cat <<EOF > wordled-config.json
{
  "type": "cosmos",
  "value": {
    "key": "$RELAYER_WALLET",
    "chain-id": "$WORDLED_CHAIN_ID",
    "rpc-addr": "$WORDLED_RPC",
    "grpc-addr": "$WORDLED_GRPC",
    "account-prefix": "gm",
    "keyring-backend": "test",
    "gas-adjustment": 2.0,
    "gas-prices": "0.1$WORDLED_DENOM",
    "debug": true,
    "timeout": "10s",
    "output-format": "json",
    "sign-mode": "direct",
    "trusting-period": "504h"
  }
}
EOF
$RELAYER_BIN chains add $WORDLED_CHAIN_ID -f wordled-config.json --home "$CONFIG_DIR"
log_success "$WORDLED_CHAIN_ID chain added"

# Import keys to relayer using mnemonic
log_info "Importing key for $GAIA_CHAIN_ID..."
$RELAYER_BIN keys restore $GAIA_CHAIN_ID $RELAYER_WALLET "$RELAYER_MNEMONIC" --home "$CONFIG_DIR"
log_success "Key imported for $GAIA_CHAIN_ID"

log_info "Importing key for $WORDLED_CHAIN_ID..."
$RELAYER_BIN keys restore $WORDLED_CHAIN_ID $RELAYER_WALLET "$RELAYER_MNEMONIC" --home "$CONFIG_DIR"
log_success "Key imported for $WORDLED_CHAIN_ID"
# Clean up config files
rm gaia-config.json wordled-config.json

# Show configured addresses (optional)
log_info "Showing configured addresses:"
$RELAYER_BIN keys list $GAIA_CHAIN_ID --home "$CONFIG_DIR"
$RELAYER_BIN keys list $WORDLED_CHAIN_ID --home "$CONFIG_DIR"

# Create a path between the chains
PATH_NAME="gaia-rollkit"
log_info "Creating path $PATH_NAME between $GAIA_CHAIN_ID and $WORDLED_CHAIN_ID..."
$RELAYER_BIN paths new $GAIA_CHAIN_ID $WORDLED_CHAIN_ID $PATH_NAME --home "$CONFIG_DIR"
log_success "Path created"

# Create IBC channel between chains
log_info "Creating IBC connection and channel between $GAIA_CHAIN_ID and $WORDLED_CHAIN_ID..."
$RELAYER_BIN tx link $PATH_NAME --home "$CONFIG_DIR" --src-port transfer --dst-port transfer --order unordered --log-level=DEBUG --debug
log_success "IBC connection and channel created"

# Start the relayer
log_info "Starting Go IBC Relayer..."
$RELAYER_BIN start $PATH_NAME --home "$CONFIG_DIR"
