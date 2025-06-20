<<<<<<< Updated upstream
```shell
./init-gaia.sh
./run_node.sh
```
=======
## Setup and Run

```shell
./init-gaia.sh
./run_node.sh
```

## IBC Connection

After setting up both chains, you can establish an IBC connection between them:

```shell
./ibc-connection-hermes.sh
```

## ICS20 Token Transfer

Once the IBC connection is established, you can test token transfers between chains:

```shell
./ics20-token-transfer.sh
```

This script will:
1. Transfer tokens from the Gaia chain to the Rollkit chain
2. Verify the transfer was successful
3. Transfer tokens back from the Rollkit chain to the Gaia chain
4. Verify the return transfer was successful

The script automatically:
- Gets validator addresses from both chains
- Retrieves the IBC channel ID
- Checks balances before and after transfers
- Handles IBC denominations
>>>>>>> Stashed changes
