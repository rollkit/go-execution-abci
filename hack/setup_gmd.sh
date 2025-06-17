#!/bin/bash

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Hardcoded mnemonic for validator account (igual que en Wordled)
VALIDATOR_MNEMONIC="abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
ATTESTER_MNEMONIC="tennis sponsor brick almost coyote soup rib wisdom warm bean onion tray devote pretty crime grid rough boil wear december travel inch work note"
RELAYER_MNEMONIC="reject camp lock magic dragon degree loop ignore quantum verify invest primary object afraid crane unveil parrot jelly rubber risk mirror globe torch category"
USER_MNEMONIC="sport head real antique sad expect ignore feature claim manual heavy mouse coil rebuild police flag robust picture milk symptom suffer chuckle worry virus"


echo "$VALIDATOR_MNEMONIC" | gmd keys add validator \
  --keyring-backend test \
  --recover > /dev/null 2>&1

echo "$ATTESTER_MNEMONIC" | gmd keys add attester \
  --keyring-backend test \
  --recover > /dev/null 2>&1

echo "$RELAYER_MNEMONIC" | gmd keys add relayer \
  --keyring-backend test \
  --recover > /dev/null 2>&1

echo "$USER_MNEMONIC" | gmd keys add carl \
  --keyring-backend test \
  --recover > /dev/null 2>&1

gmd genesis add-genesis-account validator "10000000000000000stake" --keyring-backend test
gmd genesis add-genesis-account attester "10000000000000000stake" --keyring-backend test
gmd genesis add-genesis-account relayer "10000000000000000stake" --keyring-backend test
gmd genesis add-genesis-account carl "10000000000000000stake" --keyring-backend test
exit 0

# TODO: replace rollkit init with the following?


gmd init --chain-id "$CHAIN_ID" "$MONIKER"
# staking/governance token is hardcoded in config, change this
sed -i "s/\"stake\"/\"$STAKE\"/" "$HOME"/.gmd/config/genesis.json
# this is essential for sub-1s block times (or header times go crazy)
sed -i 's/"time_iota_ms": "1000"/"time_iota_ms": "10"/' "$HOME"/.gmd/config/genesis.json

if ! gmd keys show validator > /dev/null 2>&1 ; then
  (echo "$PASSWORD"; echo "$PASSWORD") | gmd keys add validator
fi
# hardcode the validator account for this instance
echo "$PASSWORD" | gmd genesis add-genesis-account validator "1000000000$STAKE,1000000000$FEE"

# (optionally) add a few more genesis accounts
for addr in "$@"; do
  echo $addr
  gmd genesis add-genesis-account "$addr" "1000000000$STAKE,1000000000$FEE"
done

# submit a genesis validator tx
## Workaround for https://github.com/cosmos/cosmos-sdk/issues/8251
(echo "$PASSWORD"; echo "$PASSWORD"; echo "$PASSWORD") | gmd genesis gentx validator "250000000$STAKE" --chain-id="$CHAIN_ID" --amount="250000000$STAKE"
## should be:
# (echo "$PASSWORD"; echo "$PASSWORD"; echo "$PASSWORD") | gmd gentx validator "250000000$STAKE" --chain-id="$CHAIN_ID"
gmd genesis collect-gentxs







