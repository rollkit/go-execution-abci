package core

import "errors"

// ErrConsensusStateNotAvailable is returned because Rollkit doesn't use CometBFT consensus.
var ErrConsensusStateNotAvailable = errors.New("consensus state not available in ev-node")
