package adapter

import (
	"context"
	"fmt"

	cmtypes "github.com/cometbft/cometbft/types"
	adapterpb "github.com/rollkit/go-execution-abci/adapter/proto"
	"github.com/rollkit/rollkit/store"
	"google.golang.org/protobuf/proto"
)

const stateKey = "abci-s"

// State contains information about current state of the blockchain.
type State struct {
	ConsensusParams                  cmtypes.ConsensusParams
	LastHeightConsensusParamsChanged int64
	Validators                       *cmtypes.ValidatorSet
	NextValidators                   *cmtypes.ValidatorSet
	LastValidators                   *cmtypes.ValidatorSet
	LastHeightValidatorsChanged      int64
}

// ToProto converts the State to a protobuf message
func (s *State) ToProto() (*adapterpb.StateProto, error) {
	consensusParamsProto := s.ConsensusParams.ToProto()
	validatorSetProto, err := s.Validators.ToProto()
	if err != nil {
		return nil, fmt.Errorf("failed to convert validators to proto: %w", err)
	}

	// Convert next validators to proto
	nextValidatorSetProto, err := s.NextValidators.ToProto()
	if err != nil {
		return nil, fmt.Errorf("failed to convert next validators to proto: %w", err)
	}

	// Convert last validators to proto
	lastValidatorSetProto, err := s.LastValidators.ToProto()
	if err != nil {
		return nil, fmt.Errorf("failed to convert last validators to proto: %w", err)
	}

	statePb := &adapterpb.StateProto{
		ConsensusParams:                  &consensusParamsProto,
		LastHeightConsensusParamsChanged: s.LastHeightConsensusParamsChanged,
		Validators:                       validatorSetProto,
		NextValidators:                   nextValidatorSetProto,
		LastValidators:                   lastValidatorSetProto,
		LastHeightValidatorsChanged:      s.LastHeightValidatorsChanged,
	}

	return statePb, nil
}

// FromProto converts a protobuf message to a State
func StateFromProto(sp *adapterpb.StateProto) (*State, error) {
	// Create empty validator sets
	validators, err := cmtypes.ValidatorSetFromProto(sp.Validators)
	if err != nil {
		return nil, fmt.Errorf("failed to convert validators to proto: %w", err)
	}
	nextValidators, err := cmtypes.ValidatorSetFromProto(sp.NextValidators)
	if err != nil {
		return nil, fmt.Errorf("failed to convert next validators to proto: %w", err)
	}
	lastValidators, err := cmtypes.ValidatorSetFromProto(sp.LastValidators)
	if err != nil {
		return nil, fmt.Errorf("failed to convert last validators to proto: %w", err)
	}

	consensusParams := cmtypes.ConsensusParamsFromProto(*sp.ConsensusParams)

	return &State{
		ConsensusParams:                  consensusParams,
		LastHeightConsensusParamsChanged: sp.LastHeightConsensusParamsChanged,
		Validators:                       validators,
		NextValidators:                   nextValidators,
		LastValidators:                   lastValidators,
		LastHeightValidatorsChanged:      sp.LastHeightValidatorsChanged,
	}, nil
}

// loadState loads the state from disk
func loadState(ctx context.Context, s store.Store) (*State, error) {
	data, err := s.GetMetadata(ctx, stateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get state metadata: %w", err)
	}
	if data == nil {
		return &State{}, nil
	}

	var stateProto adapterpb.StateProto
	if err := proto.Unmarshal(data, &stateProto); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return StateFromProto(&stateProto)
}

// saveState saves the state to disk
func saveState(ctx context.Context, s store.Store, state *State) error {
	stateProto, err := state.ToProto()
	if err != nil {
		return fmt.Errorf("failed to convert state to proto: %w", err)
	}

	data, err := proto.Marshal(stateProto)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return s.SetMetadata(ctx, stateKey, data)
}
