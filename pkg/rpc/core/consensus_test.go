package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	testifyassert "github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	testifyrequire "github.com/stretchr/testify/require"

	ds "github.com/ipfs/go-datastore"
	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

func newTestValidator() *cmttypes.Validator {
	pk := ed25519.GenPrivKey().PubKey()
	return &cmttypes.Validator{
		Address:          pk.Address(),
		PubKey:           pk,
		VotingPower:      10,
		ProposerPriority: 0,
	}
}

func newValidState() cmtstate.State {
	val := newTestValidator()
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{val})
	valSet.Proposer = val // Ensure proposer is set

	sampleHash1 := make([]byte, 32)
	for i := range sampleHash1 {
		sampleHash1[i] = byte(i)
	}
	sampleHash2 := make([]byte, 32)
	for i := range sampleHash2 {
		sampleHash2[i] = byte(i + 100) // Make it different from sampleHash1
	}

	return cmtstate.State{
		ChainID:         "test-chain-id",
		InitialHeight:   1,
		LastBlockHeight: 1,
		LastBlockID:     cmttypes.BlockID{Hash: sampleHash1, PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: sampleHash2}},
		LastBlockTime:   time.Now().UTC(),
		Validators:      valSet,
		NextValidators:  valSet,
		LastValidators:  valSet,
		AppHash:         []byte("app_hash"),
	}
}

func TestValidators(t *testing.T) {
	assert := testifyassert.New(t)
	require := testifyrequire.New(t)
	ctx := newTestRPCContext() // From mocks_test.go

	samplePubKey := ed25519.GenPrivKey().PubKey()
	sampleAddress := samplePubKey.Address()

	genesisValidator := cmttypes.GenesisValidator{
		Address: sampleAddress,
		PubKey:  samplePubKey,
		Power:   1,
		Name:    "genesis-validator",
	}

	sampleConsensusParams := &cmttypes.ConsensusParams{
		Block: cmttypes.BlockParams{MaxBytes: 10, MaxGas: 100},
	}

	t.Run("Success_OneValidator_LatestHeight", func(t *testing.T) {
		mockStore := new(MockRollkitStore)
		adapterInstance := &adapter.Adapter{
			RollkitStore: mockStore,
			AppGenesis: &genutiltypes.AppGenesis{
				Consensus: &genutiltypes.ConsensusGenesis{
					Validators: []cmttypes.GenesisValidator{genesisValidator},
					Params:     sampleConsensusParams,
				},
			},
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		expectedHeight := uint64(100)
		mockStore.On("Height", testifymock.Anything).Return(expectedHeight, nil).Once()

		result, err := Validators(ctx, nil, nil, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(int64(expectedHeight), result.BlockHeight)
		assert.Len(result.Validators, 1)
		assert.Equal(1, result.Count)
		assert.Equal(1, result.Total)
		assert.Equal(sampleAddress, result.Validators[0].Address)
		assert.Equal(samplePubKey, result.Validators[0].PubKey)
		assert.Equal(int64(1), result.Validators[0].VotingPower)
		assert.Equal(int64(1), result.Validators[0].ProposerPriority)
		mockStore.AssertExpectations(t)
	})

	t.Run("Success_OneValidator_SpecificHeight", func(t *testing.T) {
		mockStore := new(MockRollkitStore)
		adapterInstance := &adapter.Adapter{
			RollkitStore: mockStore,
			AppGenesis: &genutiltypes.AppGenesis{
				Consensus: &genutiltypes.ConsensusGenesis{
					Validators: []cmttypes.GenesisValidator{genesisValidator},
					Params:     sampleConsensusParams,
				},
			},
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}
		specificHeight := int64(50)

		result, err := Validators(ctx, &specificHeight, nil, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(specificHeight, result.BlockHeight)
		assert.Len(result.Validators, 1)
		assert.Equal(1, result.Count)
		assert.Equal(1, result.Total)
		assert.Equal(sampleAddress, result.Validators[0].Address)
		assert.Equal(samplePubKey, result.Validators[0].PubKey)
		assert.Equal(int64(1), result.Validators[0].VotingPower)
		assert.Equal(int64(1), result.Validators[0].ProposerPriority)
	})

	t.Run("Error_NoValidators", func(t *testing.T) {
		mockStore := new(MockRollkitStore)
		adapterInstance := &adapter.Adapter{
			RollkitStore: mockStore,
			AppGenesis: &genutiltypes.AppGenesis{
				Consensus: &genutiltypes.ConsensusGenesis{
					Validators: []cmttypes.GenesisValidator{},
					Params:     sampleConsensusParams,
				},
			},
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		mockStore.On("Height", testifymock.Anything).Return(uint64(0), nil).Maybe()

		result, err := Validators(ctx, nil, nil, nil)
		require.Error(err)
		assert.Nil(result)
		assert.Contains(err.Error(), "there should be exactly one validator in genesis")
		mockStore.AssertExpectations(t)
	})

	t.Run("Error_TooManyValidators", func(t *testing.T) {
		mockStore := new(MockRollkitStore)
		adapterInstance := &adapter.Adapter{
			RollkitStore: mockStore,
			AppGenesis: &genutiltypes.AppGenesis{
				Consensus: &genutiltypes.ConsensusGenesis{
					Validators: []cmttypes.GenesisValidator{genesisValidator, genesisValidator},
					Params:     sampleConsensusParams,
				},
			},
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		mockStore.On("Height", testifymock.Anything).Return(uint64(0), nil).Maybe()

		result, err := Validators(ctx, nil, nil, nil)
		require.Error(err)
		assert.Nil(result)
		assert.Contains(err.Error(), "there should be exactly one validator in genesis")
		mockStore.AssertExpectations(t)
	})

	t.Run("Success_HeightNormalizationReturnsZeroOnError", func(t *testing.T) {
		mockStore := new(MockRollkitStore)
		adapterInstance := &adapter.Adapter{
			RollkitStore: mockStore,
			AppGenesis: &genutiltypes.AppGenesis{
				Consensus: &genutiltypes.ConsensusGenesis{
					Validators: []cmttypes.GenesisValidator{genesisValidator},
					Params:     sampleConsensusParams,
				},
			},
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		mockStore.On("Height", testifymock.Anything).Return(uint64(0), errors.New("failed to get height")).Once()

		result, err := Validators(ctx, nil, nil, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(int64(0), result.BlockHeight)
		mockStore.AssertExpectations(t)
	})
}

func TestDumpConsensusState(t *testing.T) {
	assert := testifyassert.New(t)
	require := testifyrequire.New(t)
	ctx := newTestRPCContext()

	result, err := DumpConsensusState(ctx)

	require.Error(err)
	assert.Nil(result)
	assert.Equal(ErrConsensusStateNotAvailable, err)
}

func TestConsensusState(t *testing.T) {
	assert := testifyassert.New(t)
	require := testifyrequire.New(t)
	ctx := newTestRPCContext()

	result, err := ConsensusState(ctx)

	require.Error(err)
	assert.Nil(result)
	assert.Equal(ErrConsensusStateNotAvailable, err)
}

func TestConsensusParams(t *testing.T) {
	assert := testifyassert.New(t)
	require := testifyrequire.New(t)
	ctx := newTestRPCContext()

	sampleProtoParams := cmtproto.ConsensusParams{
		Block: &cmtproto.BlockParams{MaxBytes: 1024, MaxGas: 200000},
		Evidence: &cmtproto.EvidenceParams{
			MaxAgeNumBlocks: 1000, MaxAgeDuration: time.Hour, MaxBytes: 512,
		},
		Validator: &cmtproto.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		Version:   &cmtproto.VersionParams{App: 1},
	}

	mockStateWithConsensusParams := newValidState()
	mockStateWithConsensusParams.ConsensusParams = cmttypes.ConsensusParamsFromProto(sampleProtoParams)
	mockStateWithConsensusParams.LastHeightConsensusParamsChanged = mockStateWithConsensusParams.InitialHeight

	t.Run("Success_LatestHeight", func(t *testing.T) {
		mockStore := new(MockRollkitStore)
		dsStore := ds.NewMapDatastore()
		abciStore := adapter.NewStore(dsStore)

		adapterInstance := &adapter.Adapter{
			RollkitStore: mockStore,
			Store:        abciStore,
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		expectedHeight := uint64(120)
		mockStore.On("Height", testifymock.Anything).Return(expectedHeight, nil).Once()

		err := abciStore.SaveState(context.Background(), &mockStateWithConsensusParams)
		require.NoError(err)

		result, err := ConsensusParams(ctx, nil)
		require.NoError(err)
		require.NotNil(result)

		assert.Equal(int64(expectedHeight), result.BlockHeight)
		assert.Equal(sampleProtoParams.Block.MaxBytes, result.ConsensusParams.Block.MaxBytes)
		assert.Equal(sampleProtoParams.Block.MaxGas, result.ConsensusParams.Block.MaxGas)

		mockStore.AssertExpectations(t)
	})

	t.Run("Success_SpecificHeight", func(t *testing.T) {
		dsStore := ds.NewMapDatastore()
		abciStore := adapter.NewStore(dsStore)

		adapterInstance := &adapter.Adapter{
			Store: abciStore,
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		specificHeight := int64(60)
		err := abciStore.SaveState(context.Background(), &mockStateWithConsensusParams)
		require.NoError(err)

		result, err := ConsensusParams(ctx, &specificHeight)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(specificHeight, result.BlockHeight)
		assert.Equal(sampleProtoParams.Block.MaxBytes, result.ConsensusParams.Block.MaxBytes)
	})

	t.Run("Error_LoadStateFails", func(t *testing.T) {
		mockRollkitStore := new(MockRollkitStore)
		dsStore := ds.NewMapDatastore()
		abciStore := adapter.NewStore(dsStore)

		adapterInstance := &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			Store:        abciStore,
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		mockRollkitStore.On("Height", testifymock.Anything).Return(uint64(100), nil).Maybe()

		result, err := ConsensusParams(ctx, nil)

		require.Error(err)
		assert.Nil(result)
		assert.ErrorContains(err, "failed to get state metadata")
		require.True(errors.Is(err, ds.ErrNotFound), "error should wrap ds.ErrNotFound")

		mockRollkitStore.AssertExpectations(t)
	})

	t.Run("Success_HeightNormalizationReturnsZeroOnError", func(t *testing.T) {
		mockRollkitStore := new(MockRollkitStore)
		dsStore := ds.NewMapDatastore()
		abciStore := adapter.NewStore(dsStore)
		adapterInstance := &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			Store:        abciStore,
		}
		env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}

		mockRollkitStore.On("Height", testifymock.Anything).Return(uint64(0), errors.New("failed to get height")).Once()

		err := abciStore.SaveState(context.Background(), &mockStateWithConsensusParams)
		require.NoError(err)

		result, err := ConsensusParams(ctx, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(int64(0), result.BlockHeight)
		assert.Equal(sampleProtoParams.Block.MaxBytes, result.ConsensusParams.Block.MaxBytes)
		mockRollkitStore.AssertExpectations(t)
	})
}
