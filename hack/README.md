# Hack

Local test scripts

## Requirements

* `gaiad` + `hermes` app are available. Use `download.sh` script to fetch
* Rollkit example app `gmd` is built with network integration and available in the path
* `local-da` is built and located in `../../rollkit/build/local-da`

## Run local setup

```shell
./init-gaia.sh  # setup and start gaia app
./run_node.sh # setup and start gmd example app + local da
go run ./attester --chain-id=rollkitnet-1 --home=$(pwd)/testnet/gm --from=validator
./ibc-connection-hermes.sh # relayer
./ics20-token-transfer.sh # token transfer
```
