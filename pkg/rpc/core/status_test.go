package core

import (
	"errors"
	"testing"
	"time"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	rollkitmocks "github.com/rollkit/rollkit/test/mocks"
	rlktypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

func TestStatus(t *testing.T) {
	ctx := &rpctypes.Context{}

	latestHeight := uint64(10)
	initialHeight := int64(1)
	blockHash := []byte("blockhash")
	appHash := []byte("apphash")
	blockTime := time.Now()
	genesisPubKey := cmttypes.NewMockPV().PrivKey.PubKey()
	genesisAddress := genesisPubKey.Address()
	genesisValidator := cmttypes.GenesisValidator{
		Address: genesisAddress,
		PubKey:  genesisPubKey,
		Power:   1,
		Name:    "validator",
	}
	genesisValidators := []cmttypes.GenesisValidator{genesisValidator}

	sampleSignedHeader := &rlktypes.SignedHeader{
		Header: rlktypes.Header{
			BaseHeader: rlktypes.BaseHeader{
				Height: uint64(initialHeight),
				Time:   uint64(blockTime.UnixNano()),
			},
			ProposerAddress: genesisAddress,
			AppHash:         appHash,
			DataHash:        blockHash,
		},
	}

	// Create mocks for the main test setup
	mockStore := new(rollkitmocks.MockStore)
	mockP2P := new(MockP2PClient)

	adapterInstance := &adapter.Adapter{
		RollkitStore: mockStore,
		AppGenesis: &genutiltypes.AppGenesis{
			InitialHeight: initialHeight,
			Consensus: &genutiltypes.ConsensusGenesis{
				Validators: genesisValidators,
			},
		},
		P2PClient: mockP2P,
	}
	// Set the global env (required by Status function)
	originalEnv := env
	env = &Environment{Adapter: adapterInstance, Logger: cmtlog.NewNopLogger()}
	t.Cleanup(func() { env = originalEnv })

	t.Run("Success", func(t *testing.T) {
		// Expectations for success case
		mockStore.On("Height", mock.Anything).Return(latestHeight, nil).Once()
		mockStore.On("GetHeader", mock.Anything, latestHeight).Return(sampleSignedHeader, nil).Once()
		mockStore.On("GetHeader", mock.Anything, uint64(initialHeight)).Return(sampleSignedHeader, nil).Once()
		mockStore.On("GetState", mock.Anything).Return(rlktypes.State{Version: rlktypes.Version{Block: 1, App: 1}}, nil).Once()
		mockP2P.On("Info").Return("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "addr", "network", nil).Once()

		result, err := Status(ctx)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, int64(latestHeight), result.SyncInfo.LatestBlockHeight)
		assert.Equal(t, cmbytes.HexBytes(blockHash), result.SyncInfo.LatestBlockHash)
		assert.Equal(t, cmbytes.HexBytes(appHash), result.SyncInfo.LatestAppHash)
		assert.Equal(t, int64(initialHeight), result.SyncInfo.EarliestBlockHeight)
		assert.Equal(t, genesisAddress, result.ValidatorInfo.Address)
		assert.Equal(t, genesisPubKey, result.ValidatorInfo.PubKey)
		assert.Equal(t, int64(1), result.ValidatorInfo.VotingPower)

		// Assert expectations for this subtest
		mockStore.AssertExpectations(t)
		mockP2P.AssertExpectations(t)
	})

	t.Run("Error_Height", func(t *testing.T) {
		// Need a specific mock store for this error
		errorMockStore := new(rollkitmocks.MockStore)
		errorMockStore.On("Height", mock.Anything).Return(uint64(0), errors.New("height error")).Once()

		// Temporarily replace the store in the adapter
		originalStore := adapterInstance.RollkitStore
		adapterInstance.RollkitStore = errorMockStore
		t.Cleanup(func() { adapterInstance.RollkitStore = originalStore })

		result, err := Status(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to get latest height")
		assert.Nil(t, result)
		errorMockStore.AssertExpectations(t)
	})

	t.Run("Error_GetHeader_Latest", func(t *testing.T) {
		errorMockStore := new(rollkitmocks.MockStore)
		errorMockStore.On("Height", mock.Anything).Return(latestHeight, nil).Once()
		errorMockStore.On("GetHeader", mock.Anything, latestHeight).Return(nil, errors.New("block error")).Once()

		originalStore := adapterInstance.RollkitStore
		adapterInstance.RollkitStore = errorMockStore
		t.Cleanup(func() { adapterInstance.RollkitStore = originalStore })

		result, err := Status(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to find latest block")
		assert.Nil(t, result)
		errorMockStore.AssertExpectations(t)
	})

	t.Run("Error_GetHeader_Initial", func(t *testing.T) {
		errorMockStore := new(rollkitmocks.MockStore)
		errorMockStore.On("Height", mock.Anything).Return(latestHeight, nil).Once()
		errorMockStore.On("GetHeader", mock.Anything, latestHeight).Return(sampleSignedHeader, nil).Once()
		errorMockStore.On("GetHeader", mock.Anything, uint64(initialHeight)).Return(nil, errors.New("initial block error")).Once()

		originalStore := adapterInstance.RollkitStore
		adapterInstance.RollkitStore = errorMockStore
		t.Cleanup(func() { adapterInstance.RollkitStore = originalStore })

		result, err := Status(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to find earliest block")
		assert.Nil(t, result)
		errorMockStore.AssertExpectations(t)
	})

	t.Run("Error_NoValidators", func(t *testing.T) {
		// This case modifies the adapter state directly
		originalValidators := adapterInstance.AppGenesis.Consensus.Validators
		adapterInstance.AppGenesis.Consensus.Validators = []cmttypes.GenesisValidator{}
		t.Cleanup(func() { adapterInstance.AppGenesis.Consensus.Validators = originalValidators })

		// Need a mock store configured for the calls leading up to the validator check
		validCallsMockStore := new(rollkitmocks.MockStore)
		validCallsMockStore.On("Height", mock.Anything).Return(latestHeight, nil).Once()
		validCallsMockStore.On("GetHeader", mock.Anything, latestHeight).Return(sampleSignedHeader, nil).Once()
		validCallsMockStore.On("GetHeader", mock.Anything, uint64(initialHeight)).Return(sampleSignedHeader, nil).Once()

		originalStore := adapterInstance.RollkitStore
		adapterInstance.RollkitStore = validCallsMockStore
		t.Cleanup(func() { adapterInstance.RollkitStore = originalStore })

		result, err := Status(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "there should be exactly one validator in genesis")
		assert.Nil(t, result)
		validCallsMockStore.AssertExpectations(t)
	})

	t.Run("Error_GetState", func(t *testing.T) {
		errorMockStore := new(rollkitmocks.MockStore)
		errorMockStore.On("Height", mock.Anything).Return(latestHeight, nil).Once()
		errorMockStore.On("GetHeader", mock.Anything, latestHeight).Return(sampleSignedHeader, nil).Once()
		errorMockStore.On("GetHeader", mock.Anything, uint64(initialHeight)).Return(sampleSignedHeader, nil).Once()
		errorMockStore.On("GetState", mock.Anything).Return(rlktypes.State{}, errors.New("state error")).Once()

		originalStore := adapterInstance.RollkitStore
		adapterInstance.RollkitStore = errorMockStore
		t.Cleanup(func() { adapterInstance.RollkitStore = originalStore })

		result, err := Status(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to load the last saved state")
		assert.Nil(t, result)
		errorMockStore.AssertExpectations(t)
	})

	t.Run("Error_P2PInfo", func(t *testing.T) {
		// Need mock store that succeeds until the P2P call
		successStore := new(rollkitmocks.MockStore)
		successStore.On("Height", mock.Anything).Return(latestHeight, nil).Once()
		successStore.On("GetHeader", mock.Anything, latestHeight).Return(sampleSignedHeader, nil).Once()
		successStore.On("GetHeader", mock.Anything, uint64(initialHeight)).Return(sampleSignedHeader, nil).Once()
		successStore.On("GetState", mock.Anything).Return(rlktypes.State{Version: rlktypes.Version{Block: 1, App: 1}}, nil).Once()

		// Specific P2P mock for error
		errorP2P := new(MockP2PClient)
		errorP2P.On("Info").Return("", "", "", errors.New("p2p error")).Once()

		// Temporarily replace store and p2p client
		originalStore := adapterInstance.RollkitStore
		originalP2P := adapterInstance.P2PClient
		adapterInstance.RollkitStore = successStore
		adapterInstance.P2PClient = errorP2P
		t.Cleanup(func() {
			adapterInstance.RollkitStore = originalStore
			adapterInstance.P2PClient = originalP2P
		})

		result, err := Status(ctx)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to load node p2p2 info")
		assert.Nil(t, result)
		successStore.AssertExpectations(t)
		errorP2P.AssertExpectations(t)
	})
}
