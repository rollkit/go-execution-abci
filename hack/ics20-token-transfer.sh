#!/bin/bash
set -e

# Configuration variables (matching ibc-connection-hermes.sh)
GAIA_CHAIN_ID="localnet-1"
ROLLKIT_CHAIN_ID="gm"

GAIA_RPC="http://localhost:26654"
ROLLKIT_RPC="http://localhost:26657"

GAIA_DENOM="stake"
ROLLKIT_DENOM="stake"

# Validator account (same as in other scripts)
VALIDATOR="validator"

# IBC channel information
CHANNEL_ID=""
TRANSFER_PORT="transfer"

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
GAIAD_BIN="${GAIAD_BIN:-$CURRENT_DIR/downloads/gaiad}"
GMD_BIN="gmd"

# Verify binaries are installed
if [ ! -f "$GAIAD_BIN" ]; then
    log_error "Gaiad not found at $GAIAD_BIN"
    exit 1
fi
log_success "Gaiad installed"

if ! command -v $GMD_BIN &> /dev/null; then
    log_error "gmd binary not found in PATH"
    exit 1
fi
log_success "gmd installed"

# Get validator addresses
log_info "Getting validator addresses..."
VALIDATOR_ADDRESS_GAIA=$($GAIAD_BIN keys show $VALIDATOR -a --keyring-backend test --home "$CURRENT_DIR/testnet")
VALIDATOR_ADDRESS_ROLLKIT=$($GMD_BIN keys show $VALIDATOR -a --keyring-backend test)

log_info "Gaia validator address: $VALIDATOR_ADDRESS_GAIA"
log_info "Rollkit validator address: $VALIDATOR_ADDRESS_ROLLKIT"

# Get channel ID
log_info "Getting IBC channel ID..."
CHANNEL_INFO=$($GAIAD_BIN q ibc channel channels --node $GAIA_RPC --home "$CURRENT_DIR/testnet" -o json)
CHANNEL_ID=$(echo $CHANNEL_INFO | jq -r '.channels[0].channel_id')

if [ -z "$CHANNEL_ID" ] || [ "$CHANNEL_ID" == "null" ]; then
    log_error "Failed to get channel ID. Make sure IBC connection is established."
    exit 1
fi

log_info "Using IBC channel: $CHANNEL_ID"

# Check initial balances
log_info "Checking initial balances..."
log_info "Gaia validator balance:"
$GAIAD_BIN q bank balances $VALIDATOR_ADDRESS_GAIA --node $GAIA_RPC --home "$CURRENT_DIR/testnet"

log_info "Rollkit validator balance:"
$GMD_BIN q bank balances $VALIDATOR_ADDRESS_ROLLKIT --node $ROLLKIT_RPC

# Transfer Rollkit tokens to Gaia
ROLLKIT_TRANSFER_AMOUNT="10000000"
log_info "Transferring ${ROLLKIT_TRANSFER_AMOUNT}${ROLLKIT_DENOM} from Rollkit to Gaia..."
TX_HASH=$($GMD_BIN tx ibc-transfer transfer $TRANSFER_PORT $CHANNEL_ID $VALIDATOR_ADDRESS_GAIA ${ROLLKIT_TRANSFER_AMOUNT}${ROLLKIT_DENOM} \
    --from $VALIDATOR \
    --chain-id $ROLLKIT_CHAIN_ID \
    --node $ROLLKIT_RPC \
    --keyring-backend test \
    --gas auto \
    --gas-adjustment 1.4 \
    --gas-prices 1stake \
    --yes -o json | jq -r '.txhash')

log_info "sleeping 10s for the relayer to work"
sleep 10

# Check balances after Rollkit to Gaia transfer
log_info "Checking balances after Rollkit to Gaia transfer..."
log_info "Gaia validator balance (should show IBC tokens):"
$GAIAD_BIN q bank balances $VALIDATOR_ADDRESS_GAIA --node $GAIA_RPC --home "$CURRENT_DIR/testnet"

log_info "Rollkit validator balance:"
$GMD_BIN q bank balances $VALIDATOR_ADDRESS_ROLLKIT --node $ROLLKIT_RPC

# Transfer tokens from Gaia to Rollkit
TRANSFER_AMOUNT="10000000"
log_info "Transferring $TRANSFER_AMOUNT$GAIA_DENOM from Gaia to Rollkit..."
TX_HASH=$($GAIAD_BIN tx ibc-transfer transfer $TRANSFER_PORT $CHANNEL_ID $VALIDATOR_ADDRESS_ROLLKIT ${TRANSFER_AMOUNT}${GAIA_DENOM} \
    --from $VALIDATOR \
    --chain-id $GAIA_CHAIN_ID \
    --node $GAIA_RPC \
    --keyring-backend test \
    --home "$CURRENT_DIR/testnet" \
    --gas auto \
    --gas-adjustment 1.4 \
    --gas-prices 1stake \
    --yes -o json | jq -r '.txhash')

log_info "sleeping 10s for the relayer to work"
sleep 10

# Check balances after transfer to Rollkit
log_info "Checking balances after transfer to Rollkit..."
log_info "Gaia validator balance:"
$GAIAD_BIN q bank balances $VALIDATOR_ADDRESS_GAIA --node $GAIA_RPC --home "$CURRENT_DIR/testnet"

log_info "Rollkit validator balance (should show IBC tokens):"
$GMD_BIN q bank balances $VALIDATOR_ADDRESS_ROLLKIT --node $ROLLKIT_RPC

log_success "ICS20 token transfer test completed!"