# Optional Cosmos SDK modules

This package contains optional modules for the Cosmos SDK when using Rollkit.
They are meant to enhance the UX when using Rollkit by simplifying staking and the management of rollkit sequencer, attesters and migration from CometBFT to Rollkit.

## Staking

The staking module is a wrapper around the Cosmos SDK staking module.
It changes the way the staking module works by adding No-Ops on methods that do not make sense when using Rollkit.

Think of slashing, jailing or validator updates.

## Rollkit Manager

The `rollkitmngr` module is a meant to define attesters and sequencers in a Rollkit chain.
This is the module that handles the validator updates on the SDK side.
Additionally, it has additional queries to get the sequencer information, and the attesters information.

Additionally, when added to a CometBFT chain, the `rollkitmngr` module will handle the switch from a CometBFT validator set to a Rollkit sequencer at a given height.
