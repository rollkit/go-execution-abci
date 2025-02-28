package main

import (
	cmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
)

// State contains information about current state of the blockchain. Some of these fields
// are going to be stored by Rollkit.
type State struct {
	// Consensus parameters used for validating blocks.
	// Changes returned by EndBlock and updated after Commit.
	ConsensusParams                  cmproto.ConsensusParams
	LastHeightConsensusParamsChanged int64

	Validators                  *types.ValidatorSet
	NextValidators              *types.ValidatorSet
	LastValidators              *types.ValidatorSet
	LastHeightValidatorsChanged int64
}
