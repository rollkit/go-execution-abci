package core

import "errors"

// ErrConsensusStateNotAvailable is returned because Rollkit doesn't use Tendermint consensus.
// Exported error.
var ErrConsensusStateNotAvailable = errors.New("consensus state not available in Rollkit") // Changed to exported
