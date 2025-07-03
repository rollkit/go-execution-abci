package server

import (
	"testing"

	"github.com/cometbft/cometbft/state"
	cometbfttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestCreateRollkitMigrationGenesis_SingleValidator(t *testing.T) {
	// Test case: single validator should work with existing logic
	validator := &cometbfttypes.Validator{
		Address:     []byte("test_validator_addr"),
		PubKey:      cometbfttypes.NewMockPV().PrivKey.PubKey(),
		VotingPower: 100, // Set voting power to avoid the validation error
	}
	
	cometBFTState := state.State{
		ChainID:       "test-chain",
		InitialHeight: 1,
		LastValidators: cometbfttypes.NewValidatorSet([]*cometbfttypes.Validator{validator}),
	}

	// Use a temporary directory for testing
	tmpDir := t.TempDir()
	
	// This should succeed with the single validator fallback logic
	err := createRollkitMigrationGenesis(tmpDir, cometBFTState)
	require.NoError(t, err)
}

func TestCreateRollkitMigrationGenesis_MultipleValidators_NoRollkitState(t *testing.T) {
	// Test case: multiple validators without rollkitmngr state should fail gracefully
	validator1 := &cometbfttypes.Validator{
		Address:     []byte("test_validator_addr1"),
		PubKey:      cometbfttypes.NewMockPV().PrivKey.PubKey(),
		VotingPower: 100,
	}
	validator2 := &cometbfttypes.Validator{
		Address:     []byte("test_validator_addr2"),
		PubKey:      cometbfttypes.NewMockPV().PrivKey.PubKey(),
		VotingPower: 100,
	}
	
	cometBFTState := state.State{
		ChainID:       "test-chain",
		InitialHeight: 1,
		LastValidators: cometbfttypes.NewValidatorSet([]*cometbfttypes.Validator{validator1, validator2}),
	}

	// Use a temporary directory for testing (no rollkitmngr state present)
	tmpDir := t.TempDir()
	
	// This should fail with our new logic when rollkitmngr state is not available
	err := createRollkitMigrationGenesis(tmpDir, cometBFTState)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected exactly one validator")
}

func TestCreateRollkitMigrationGenesis_NoValidators(t *testing.T) {
	// Test case: no validators should return an error
	cometBFTState := state.State{
		ChainID:       "test-chain",
		InitialHeight: 1,
		LastValidators: cometbfttypes.NewValidatorSet([]*cometbfttypes.Validator{}),
	}

	// Use a temporary directory for testing
	tmpDir := t.TempDir()
	
	// This should fail
	err := createRollkitMigrationGenesis(tmpDir, cometBFTState)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no validators found")
}

func TestGetSequencerFromRollkitMngrState_NoDatabase(t *testing.T) {
	// Test case: should fail gracefully when no application database exists
	tmpDir := t.TempDir()
	
	cometBFTState := state.State{
		ChainID: "test-chain",
	}
	
	_, err := getSequencerFromRollkitMngrState(tmpDir, cometBFTState)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no application database found")
}

func TestSequencerInfo_Validation(t *testing.T) {
	// Test the sequencer validation logic by creating a mock scenario
	// where we have multiple validators and verify the error messages
	
	validator1 := &cometbfttypes.Validator{
		Address:     []byte("test_validator_addr1"),
		PubKey:      cometbfttypes.NewMockPV().PrivKey.PubKey(),
		VotingPower: 100,
	}
	validator2 := &cometbfttypes.Validator{
		Address:     []byte("test_validator_addr2"),
		PubKey:      cometbfttypes.NewMockPV().PrivKey.PubKey(),
		VotingPower: 100,
	}
	
	cometBFTState := state.State{
		ChainID:       "test-chain",
		InitialHeight: 1,
		LastValidators: cometbfttypes.NewValidatorSet([]*cometbfttypes.Validator{validator1, validator2}),
	}

	tmpDir := t.TempDir()
	
	// This should fail since there's no rollkitmngr state, and provide a helpful error message
	err := createRollkitMigrationGenesis(tmpDir, cometBFTState)
	require.Error(t, err)
	
	// The error should mention both the validator count issue and the rollkitmngr state issue
	require.Contains(t, err.Error(), "expected exactly one validator")
	require.Contains(t, err.Error(), "found 2")
	require.Contains(t, err.Error(), "unable to determine sequencer from rollkitmngr state")
}

func TestMigrateToRollkitCmd_CommandSetup(t *testing.T) {
	// Test that the command is properly configured
	cmd := MigrateToRollkitCmd()
	
	require.NotNil(t, cmd)
	require.Equal(t, "rollkit-migrate", cmd.Use)
	require.Equal(t, "Migrate the data from the CometBFT chain to Rollkit", cmd.Short)
	require.NotNil(t, cmd.RunE)
	
	// Check that the da-height flag is available
	flag := cmd.Flags().Lookup("da-height")
	require.NotNil(t, flag)
	require.Equal(t, "1", flag.DefValue)
}