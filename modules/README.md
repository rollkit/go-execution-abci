# Optional Cosmos SDK modules

This package contains optional modules for the Cosmos SDK when using Rollkit.
They are meant to enhance the UX when using Rollkit by simplifying staking and the management of the sequencer(s).

## Staking

The staking module is a wrapper around the Cosmos SDK staking module.
It changes the way the staking module works by adding No-Ops on methods that do not make sense when using Rollkit.

Think of slashing, jailing or validator updates.

## Sequencer

The sequencer module is a meant to easily switch between sequencers.
This is the module that handles the validator updates on the SDK side.
Additionally, it has additional queries to get the sequencer information, and the future sequencer changes.

Additionally, when added to a CometBFT chain, the sequencer module will handle the switch from a CometBFT validator set to a Rollkit sequencer at a given height.
