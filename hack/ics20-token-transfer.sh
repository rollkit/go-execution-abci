#!/bin/bash
set -e

# Configuration variables (matching ibc-connection-hermes.sh)
GAIA_CHAIN_ID="localnet-1"
ROLLKIT_CHAIN_ID="rollkitnet-1"

GAIA_RPC="http://localhost:26654"
ROLLKIT_RPC="http://localhost:26657"

GAIA_DENOM="stake"
ROLLKIT_DENOM="stake"

GAIA_KEY_NAME="bob"
ROLLKIT_KEY_NAME="carl"

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
ROLLKIT_HOME="${1:-"${CURRENT_DIR}/testnet/gm"}"

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
log_info "Getting user addresses..."
USER_ADDRESS_GAIA=$($GAIAD_BIN keys show $GAIA_KEY_NAME -a --keyring-backend test --home "$CURRENT_DIR/testnet/gaia")
USER_ADDRESS_ROLLKIT=$($GMD_BIN keys show $ROLLKIT_KEY_NAME -a --keyring-backend test --home "$ROLLKIT_HOME")

log_info "Gaia user address: $USER_ADDRESS_GAIA"
log_info "Rollkit user address: $USER_ADDRESS_ROLLKIT"

# Check initial balances
log_info "Checking initial balances..."
log_info "Gaia $GAIA_KEY_NAME balance:"
$GAIAD_BIN q bank balances $USER_ADDRESS_GAIA --node $GAIA_RPC --home "$CURRENT_DIR/testnet/gaia"

log_info "Rollkit $ROLLKIT_KEY_NAME balance:"
$GMD_BIN q bank balances $USER_ADDRESS_ROLLKIT --node $ROLLKIT_RPC --home "$ROLLKIT_HOME"

# Get channel ID
log_info "Getting IBC channel ID..."
CHANNEL_INFO=$($GAIAD_BIN q ibc channel channels --node $GAIA_RPC --home "$CURRENT_DIR/testnet/gaia" -o json)
CHANNEL_ID=$(echo $CHANNEL_INFO | jq -r '.channels[0].channel_id')

if [ -z "$CHANNEL_ID" ] || [ "$CHANNEL_ID" == "null" ]; then
    log_error "Failed to get channel ID. Make sure IBC connection is established."
    exit 1
fi

log_info "Using IBC channel: $CHANNEL_ID"

# Transfer Rollkit tokens to Gaia
ROLLKIT_TRANSFER_AMOUNT="101"

log_info "Transferring ${ROLLKIT_TRANSFER_AMOUNT}${ROLLKIT_DENOM} from Rollkit to Gaia..."
TX_HASH=$($GMD_BIN tx ibc-transfer transfer $TRANSFER_PORT $CHANNEL_ID $USER_ADDRESS_GAIA ${ROLLKIT_TRANSFER_AMOUNT}${ROLLKIT_DENOM} \
    --from ${ROLLKIT_KEY_NAME} \
    --chain-id $ROLLKIT_CHAIN_ID \
    --node $ROLLKIT_RPC \
    --keyring-backend test \
    --home "$ROLLKIT_HOME" \
    --gas auto \
    --gas-adjustment 1.4 \
    --gas-prices 1stake \
    --yes -o json | jq -r '.txhash')

log_info "Querying gmd tx...  $TX_HASH"
for i in {1..10}; do
  if ! tx_result=$($GMD_BIN q tx --type=hash "$TX_HASH" -o json --home "$ROLLKIT_HOME" 2>/dev/null); then
    sleep 1
    continue
  fi
done
if [ "$(echo "$tx_result" | jq -r '.code')" != "0" ]; then
  log_error "Transaction failed : $tx_result"
  exit 1
fi


# Check balances after Rollkit to Gaia transfer
log_info "Checking balances after Rollkit to Gaia transfer..."
log_info "Gaia user balance (should show IBC tokens):"
$GAIAD_BIN q bank balances $USER_ADDRESS_GAIA --node $GAIA_RPC --home "$CURRENT_DIR/testnet/gaia"

log_info "Rollkit user balance:"
$GMD_BIN q bank balances $USER_ADDRESS_ROLLKIT --node $ROLLKIT_RPC --home "$ROLLKIT_HOME"

# Transfer tokens from Gaia to Rollkit
TRANSFER_AMOUNT="102"
log_info "Transferring $TRANSFER_AMOUNT$GAIA_DENOM from Gaia to Rollkit..."
TX_HASH=$($GAIAD_BIN tx ibc-transfer transfer $TRANSFER_PORT $CHANNEL_ID $USER_ADDRESS_ROLLKIT ${TRANSFER_AMOUNT}${GAIA_DENOM} \
    --from ${GAIA_KEY_NAME} \
    --chain-id $GAIA_CHAIN_ID \
    --node $GAIA_RPC \
    --keyring-backend test \
    --home "$CURRENT_DIR/testnet/gaia" \
    --gas auto \
    --gas-adjustment 1.4 \
    --gas-prices 1stake \
    --yes -o json | jq -r '.txhash')

log_info "Querying gaia tx...  $TX_HASH"
for i in {1..20}; do
  if ! tx_result=$($GAIAD_BIN q tx --type=hash "$TX_HASH" -o json --node $GAIA_RPC 2>/dev/null); then
    echo "$tx_result"
    sleep 1
    continue
  fi
done
if [ "$(echo "$tx_result" | jq -r '.code')" != "0" ]; then
  log_error "Transaction failed : $tx_result"
  exit 1
fi

# Check balances after transfer to Rollkit
log_info "Checking balances after transfer to Rollkit..."
log_info "Gaia user balance:"
$GAIAD_BIN q bank balances $USER_ADDRESS_GAIA --node $GAIA_RPC --home "$CURRENT_DIR/testnet/gaia"

log_info "Rollkit user balance (should show IBC tokens):"
ibc_tokens_visible=false

for i in {1..${20}}; do
  $GMD_BIN q bank balances $USER_ADDRESS_ROLLKIT --node $ROLLKIT_RPC --home "$ROLLKIT_HOME"
  if $GMD_BIN q bank balances $USER_ADDRESS_ROLLKIT --node $ROLLKIT_RPC --home "$ROLLKIT_HOME" | grep -q "ibc"; then
    log_success "IBC tokens are now visible in Rollkit balance"
    ibc_tokens_visible=true
    break
  fi
  sleep 1
done

if [ "$ibc_tokens_visible" = false ]; then
  log_error "IBC tokens did not become visible within time frame"
  exit 1
fi

log_success "ICS20 token transfer test completed!"
