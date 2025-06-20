package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmtstate "github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	ds "github.com/ipfs/go-datastore"
	testifyassert "github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

var (
	testSamplePubKey     = ed25519.GenPrivKey().PubKey()
	testSampleAddress    = testSamplePubKey.Address()
	testGenesisValidator = cmttypes.GenesisValidator{
		Address: testSampleAddress,
		PubKey:  testSamplePubKey,
		Power:   1,
		Name:    "genesis-validator",
	}
	testSampleConsensusParams = &cmttypes.ConsensusParams{
		Block: cmttypes.BlockParams{MaxBytes: 10, MaxGas: 100},
	}

	testProtoConsensusParams = cmtproto.ConsensusParams{
		Block:     &cmtproto.BlockParams{MaxBytes: 1024, MaxGas: 200000},
		Evidence:  &cmtproto.EvidenceParams{MaxAgeNumBlocks: 1000, MaxAgeDuration: time.Hour, MaxBytes: 512},
		Validator: &cmtproto.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		Version:   &cmtproto.VersionParams{App: 1},
	}
	testMockStateWithConsensusParams cmtstate.State
)

func init() {
	testMockStateWithConsensusParams = newValidState()
	testMockStateWithConsensusParams.ConsensusParams = cmttypes.ConsensusParamsFromProto(testProtoConsensusParams)
	testMockStateWithConsensusParams.LastHeightConsensusParamsChanged = testMockStateWithConsensusParams.InitialHeight
}

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
	valSet.Proposer = val

	sampleHash1 := make([]byte, 32)
	for i := range sampleHash1 {
		sampleHash1[i] = byte(i)
	}
	sampleHash2 := make([]byte, 32)
	for i := range sampleHash2 {
		sampleHash2[i] = byte(i + 100)
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

// setupTestValidatorsEnv helper for TestValidators
func setupTestValidatorsEnv(t *testing.T, gvs []cmttypes.GenesisValidator, consensusParams *cmttypes.ConsensusParams) *MockRollkitStore {
	t.Helper()

	mockStore := new(MockRollkitStore)
	adapterInstance := &adapter.Adapter{
		RollkitStore: mockStore,
		AppGenesis: &genutiltypes.AppGenesis{
			Consensus: &genutiltypes.ConsensusGenesis{
				Validators: gvs,
				Params:     consensusParams,
			},
		},
	}
	env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}
	return mockStore
}

// setupTestConsensusParamsEnv helper for TestConsensusParams
// Returns the mockRollkitStore (if created), the adapter instance, and the abciStore.
func setupTestConsensusParamsEnv(t *testing.T, useMockRollkitStore bool, stateToSave *cmtstate.State) (*MockRollkitStore, *adapter.Store) {
	var mockRollkitStore *MockRollkitStore
	if useMockRollkitStore {
		mockRollkitStore = new(MockRollkitStore)
	}

	dsStore := ds.NewMapDatastore()
	abciStore := adapter.NewExecABCIStore(dsStore)

	if stateToSave != nil {
		err := abciStore.SaveState(context.Background(), stateToSave)
		require.NoError(t, err)
	}

	adapterInstance := &adapter.Adapter{
		Store: abciStore,
	}
	if useMockRollkitStore {
		adapterInstance.RollkitStore = mockRollkitStore
	}

	env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}
	return mockRollkitStore, abciStore
}

func TestValidators(t *testing.T) {
	assert := testifyassert.New(t)
	require := require.New(t)
	ctx := &rpctypes.Context{}

	t.Run("Success_OneValidator_LatestHeight", func(t *testing.T) {
		mockStore := setupTestValidatorsEnv(t, []cmttypes.GenesisValidator{testGenesisValidator}, testSampleConsensusParams)

		expectedHeight := uint64(100)
		mockStore.On("Height", testifymock.Anything).Return(expectedHeight, nil).Once()

		result, err := Validators(ctx, nil, nil, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(int64(expectedHeight), result.BlockHeight)
		assert.Len(result.Validators, 1)
		assert.Equal(1, result.Count)
		assert.Equal(1, result.Total)
		assert.Equal(testSampleAddress, result.Validators[0].Address)
		assert.Equal(testSamplePubKey, result.Validators[0].PubKey)
		assert.Equal(int64(1), result.Validators[0].VotingPower)
		assert.Equal(int64(1), result.Validators[0].ProposerPriority)
		mockStore.AssertExpectations(t)
	})

	t.Run("Success_OneValidator_SpecificHeight", func(t *testing.T) {
		_ = setupTestValidatorsEnv(t, []cmttypes.GenesisValidator{testGenesisValidator}, testSampleConsensusParams) // mockStore not used directly here for Height mock
		specificHeight := int64(50)

		result, err := Validators(ctx, &specificHeight, nil, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(specificHeight, result.BlockHeight)
		assert.Len(result.Validators, 1)
		assert.Equal(1, result.Count)
		assert.Equal(1, result.Total)
		assert.Equal(testSampleAddress, result.Validators[0].Address)
		assert.Equal(testSamplePubKey, result.Validators[0].PubKey)
		assert.Equal(int64(1), result.Validators[0].VotingPower)
		assert.Equal(int64(1), result.Validators[0].ProposerPriority)
	})

	t.Run("Error_NoValidators", func(t *testing.T) {
		mockStore := setupTestValidatorsEnv(t, []cmttypes.GenesisValidator{}, testSampleConsensusParams)
		mockStore.On("Height", testifymock.Anything).Return(uint64(0), nil).Maybe()

		result, err := Validators(ctx, nil, nil, nil)
		require.Error(err)
		assert.Nil(result)
		assert.Contains(err.Error(), "there should be exactly one validator in genesis")
		mockStore.AssertExpectations(t)
	})

	t.Run("Error_TooManyValidators", func(t *testing.T) {
		mockStore := setupTestValidatorsEnv(t, []cmttypes.GenesisValidator{testGenesisValidator, testGenesisValidator}, testSampleConsensusParams)
		mockStore.On("Height", testifymock.Anything).Return(uint64(0), nil).Maybe()

		result, err := Validators(ctx, nil, nil, nil)
		require.Error(err)
		assert.Nil(result)
		assert.Contains(err.Error(), "there should be exactly one validator in genesis")
		mockStore.AssertExpectations(t)
	})

	t.Run("Success_NilHeight", func(t *testing.T) {
		mockStore := setupTestValidatorsEnv(t, []cmttypes.GenesisValidator{testGenesisValidator}, testSampleConsensusParams)
		mockStore.On("Height", testifymock.Anything).Return(uint64(0), nil).Once()

		result, err := Validators(ctx, nil, nil, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(int64(0), result.BlockHeight) // Asserting BlockHeight is 0 as per test name
		assert.Len(result.Validators, 1)           // Still expect validator details
		mockStore.AssertExpectations(t)
	})

	t.Run("Error_NilHeightAndStoreError", func(t *testing.T) {
		mockStore := setupTestValidatorsEnv(t, []cmttypes.GenesisValidator{testGenesisValidator}, testSampleConsensusParams)
		mockStore.On("Height", testifymock.Anything).Return(uint64(0), errors.New("failed to get height")).Once()

		result, err := Validators(ctx, nil, nil, nil)
		require.Error(err)
		assert.Nil(result)
		assert.Contains(err.Error(), "failed to get height")
		mockStore.AssertExpectations(t)
	})
}

func TestDumpConsensusState(t *testing.T) {
	assert := testifyassert.New(t)
	require := require.New(t)
	ctx := &rpctypes.Context{}

	result, err := DumpConsensusState(ctx)

	require.Error(err)
	assert.Nil(result)
	assert.Equal(ErrConsensusStateNotAvailable, err)
}

func TestConsensusState(t *testing.T) {
	assert := testifyassert.New(t)
	require := require.New(t)
	ctx := &rpctypes.Context{}

	result, err := ConsensusState(ctx)

	require.Error(err)
	assert.Nil(result)
	assert.Equal(ErrConsensusStateNotAvailable, err)
}

func TestConsensusParams(t *testing.T) {
	assert := testifyassert.New(t)
	require := require.New(t)
	ctx := &rpctypes.Context{}

	// sampleProtoParams and mockStateWithConsensusParams moved to package level vars (testProtoConsensusParams, testMockStateWithConsensusParams)

	t.Run("Success_LatestHeight", func(t *testing.T) {
		mockRollkitStore, _ := setupTestConsensusParamsEnv(t, true, &testMockStateWithConsensusParams)

		expectedHeight := uint64(120)
		mockRollkitStore.On("Height", testifymock.Anything).Return(expectedHeight, nil).Once()

		result, err := ConsensusParams(ctx, nil)
		require.NoError(err)
		require.NotNil(result)

		assert.Equal(int64(expectedHeight), result.BlockHeight)
		assert.Equal(testProtoConsensusParams.Block.MaxBytes, result.ConsensusParams.Block.MaxBytes)
		assert.Equal(testProtoConsensusParams.Block.MaxGas, result.ConsensusParams.Block.MaxGas)

		mockRollkitStore.AssertExpectations(t)
	})

	t.Run("Success_SpecificHeight", func(t *testing.T) {
		_, _ = setupTestConsensusParamsEnv(t, false, &testMockStateWithConsensusParams) // mockRollkitStore not needed
		specificHeight := int64(60)
		// err := abciStore.SaveState(context.Background(), &testMockStateWithConsensusParams) // Moved to helper
		// require.NoError(err) // Moved to helper

		result, err := ConsensusParams(ctx, &specificHeight)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(specificHeight, result.BlockHeight)
		assert.Equal(testProtoConsensusParams.Block.MaxBytes, result.ConsensusParams.Block.MaxBytes)
	})

	t.Run("Error_LoadStateFails", func(t *testing.T) {
		mockRollkitStore, _ := setupTestConsensusParamsEnv(t, true, nil) // Don't save state to force load failure
		mockRollkitStore.On("Height", testifymock.Anything).Return(uint64(100), nil).Maybe()

		result, err := ConsensusParams(ctx, nil)

		require.Error(err)
		assert.Nil(result)
		// The original test checked: assert.ErrorContains(err, "failed to get state metadata")
		// and require.True(errors.Is(err, ds.ErrNotFound), "error should wrap ds.ErrNotFound")
		// The error message check is kept from original if LoadState returns a clear error.
		// If LoadState wraps ds.ErrNotFound, then errors.Is(err, ds.ErrNotFound) would be more precise.
		assert.ErrorContains(err, "failed to get state metadata")
		require.True(errors.Is(err, ds.ErrNotFound), "error should wrap ds.ErrNotFound")

		mockRollkitStore.AssertExpectations(t)
	})

	t.Run("Error_NilHeightAndStoreError", func(t *testing.T) {
		mockRollkitStore, _ := setupTestConsensusParamsEnv(t, true, &testMockStateWithConsensusParams)
		mockRollkitStore.On("Height", testifymock.Anything).Return(uint64(0), errors.New("failed to get height")).Once()

		_, err := ConsensusParams(ctx, nil)
		require.Error(err)
		mockRollkitStore.AssertExpectations(t)
	})

	t.Run("Success_NilHeight", func(t *testing.T) {
		mockRollkitStore, _ := setupTestConsensusParamsEnv(t, true, &testMockStateWithConsensusParams)
		mockRollkitStore.On("Height", testifymock.Anything).Return(uint64(0), nil).Once()

		result, err := ConsensusParams(ctx, nil)
		require.NoError(err)
		require.NotNil(result)
		assert.Equal(int64(0), result.BlockHeight)
		assert.Equal(testProtoConsensusParams.Block.MaxBytes, result.ConsensusParams.Block.MaxBytes)
		mockRollkitStore.AssertExpectations(t)
	})
}
