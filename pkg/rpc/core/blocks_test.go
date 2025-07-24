package core

import (
	"context"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/libs/math"
	"github.com/cometbft/cometbft/light"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	ds "github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	rollkitmocks "github.com/rollkit/rollkit/test/mocks"
	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
	execstore "github.com/rollkit/go-execution-abci/pkg/store"
)

func TestBlockSearch_Success(t *testing.T) {
	// Setup mocks
	mockTxIndexer := new(MockTxIndexer)
	mockRollkitStore := new(rollkitmocks.MockStore)
	mockApp := new(MockApp)
	mockBlockIndexer := new(MockBlockIndexer)

	// Create a real adapter store for the test
	dsStore := ds.NewMapDatastore()
	abciExecStore := execstore.NewExecABCIStore(dsStore)

	// Save BlockID data needed for GetLastCommit calls
	// For GetLastCommit(2), we need BlockID(1)
	blockID1 := &cmttypes.BlockID{
		Hash: make([]byte, 32), // 32-byte hash
		PartSetHeader: cmttypes.PartSetHeader{
			Total: 1,
			Hash:  make([]byte, 32), // 32-byte hash
		},
	}
	storeCtx := context.Background()
	err := abciExecStore.SaveBlockID(storeCtx, 1, blockID1)
	require.NoError(t, err)

	// For GetLastCommit(3), we need BlockID(2)
	blockID2 := &cmttypes.BlockID{
		Hash: make([]byte, 32), // 32-byte hash
		PartSetHeader: cmttypes.PartSetHeader{
			Total: 1,
			Hash:  make([]byte, 32), // 32-byte hash
		},
	}
	err = abciExecStore.SaveBlockID(storeCtx, 2, blockID2)
	require.NoError(t, err)

	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
			Store:        abciExecStore,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
	}

	ctx := &rpctypes.Context{}
	query := "tx.height > 1"
	page := 1
	perPage := 10
	orderBy := "asc"
	mockedBlockHeights := []int64{2, 3}

	mockBlockIndexer.On("Search", mock.Anything, mock.AnythingOfType("*query.Query")).Return(mockedBlockHeights, nil)

	now := time.Now()
	chainID := "test-chain"

	// Create header for height 1 (needed for GetLastCommit of height 2)
	header1 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader: types.BaseHeader{
				Height:  1,
				Time:    uint64(now.UnixNano()),
				ChainID: chainID,
			},
			ProposerAddress: make([]byte, 20),
			AppHash:         []byte("apphash1"),
			DataHash:        make([]byte, 32),
		},
		Signature: types.Signature(make([]byte, 64)),
	}

	header2 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader: types.BaseHeader{
				Height:  2,
				Time:    uint64(now.UnixNano() + int64(time.Second)),
				ChainID: chainID,
			},
			ProposerAddress: make([]byte, 20),
			AppHash:         []byte("apphash2"),
			DataHash:        make([]byte, 32),
		},
		Signature: types.Signature(make([]byte, 64)),
	}
	data2 := &types.Data{
		Metadata: &types.Metadata{
			ChainID: chainID,
			Height:  2,
			Time:    uint64(now.UnixNano() + int64(time.Second)),
		},
		Txs: make(types.Txs, 0),
	}

	header3 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader: types.BaseHeader{
				Height:  3,
				Time:    uint64(now.UnixNano() + 2*int64(time.Second)),
				ChainID: chainID,
			},
			ProposerAddress: make([]byte, 20),
			AppHash:         []byte("apphash3"),
			DataHash:        make([]byte, 32),
		},
		Signature: types.Signature(make([]byte, 64)),
	}
	data3 := &types.Data{
		Metadata: &types.Metadata{
			ChainID: chainID,
			Height:  3,
			Time:    uint64(now.UnixNano() + 2*int64(time.Second)),
		},
		Txs: make(types.Txs, 0),
	}

	// Mock GetBlockData calls
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(2)).Return(header2, data2, nil) // For block 2
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(3)).Return(header3, data3, nil) // For block 3

	// Mock GetHeader calls needed for GetLastCommit
	mockRollkitStore.On("GetHeader", mock.Anything, uint64(1)).Return(header1, nil) // For GetLastCommit(2)
	mockRollkitStore.On("GetHeader", mock.Anything, uint64(2)).Return(header2, nil) // For GetLastCommit(3)

	// Execute the test
	result, err := BlockSearch(ctx, query, &page, &perPage, orderBy)

	// Verify results
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Blocks, 2)
	require.Equal(t, 2, result.TotalCount)

	// Verify first block (height 2)
	require.Equal(t, header2.BaseHeader.Height, uint64(result.Blocks[0].Block.Height))
	require.Equal(t, []byte(header2.AppHash), []byte(result.Blocks[0].Block.AppHash))
	require.Equal(t, []byte(header2.ProposerAddress), []byte(result.Blocks[0].Block.ProposerAddress))

	// Verify second block (height 3)
	require.Equal(t, header3.BaseHeader.Height, uint64(result.Blocks[1].Block.Height))
	require.Equal(t, []byte(header3.AppHash), []byte(result.Blocks[1].Block.AppHash))
	require.Equal(t, []byte(header3.ProposerAddress), []byte(result.Blocks[1].Block.ProposerAddress))

	// Verify all mocks were called as expected
	mockTxIndexer.AssertExpectations(t)
	mockBlockIndexer.AssertExpectations(t)
	mockRollkitStore.AssertExpectations(t)
	mockApp.AssertExpectations(t)
}

func TestCommit_VerifyCometBFTLightClientCompatibility_MultipleBlocks(t *testing.T) {
	require := require.New(t)

	// Create validator and signer
	cmtPrivKey := ed25519.GenPrivKey()
	cmtPubKey := cmtPrivKey.PubKey()
	aggregatorPrivKey, err := crypto.UnmarshalEd25519PrivateKey(cmtPrivKey.Bytes())
	require.NoError(err)
	aggregatorPubKey := aggregatorPrivKey.GetPublic()
	validatorAddress := cmtPubKey.Address().Bytes()[:20]

	// Create a mock signer that uses the same private key
	mockSigner := &MockSigner{}
	mockSigner.On("Sign", mock.Anything).Return(func(message []byte) []byte {
		sig, _ := aggregatorPrivKey.Sign(message)
		return sig
	}, nil)
	mockSigner.On("GetPublic").Return(aggregatorPubKey, nil)
	mockSigner.On("GetAddress").Return(validatorAddress, nil)

	env = setupTestEnvironment(mockSigner)

	chainID := "test-chain"
	now := time.Now()

	// use the validator hasher helper from cometcompat
	validatorHash, err := cometcompat.ValidatorHasherProvider()(validatorAddress, aggregatorPubKey)
	require.NoError(err)

	fixedValSet := &cmttypes.ValidatorSet{
		Validators: []*cmttypes.Validator{cmttypes.NewValidator(cmtPubKey, 1)},
		Proposer:   cmttypes.NewValidator(cmtPubKey, 1),
	}

	var trustedHeader cmttypes.SignedHeader

	// Test multiple blocks
	for i := 1; i <= 10; i++ {
		blockHeight := uint64(i)

		blockData, rollkitHeader := createTestBlock(blockHeight, chainID, now, validatorAddress, validatorHash, i)

		// create an ABCI header to get the correct hash for light client compatibility
		lastCommit, err := env.Adapter.GetLastCommit(context.Background(), blockHeight)
		require.NoError(err, "Failed to get last commit for height %d", blockHeight)

		abciHeader, err := cometcompat.ToABCIHeader(rollkitHeader, lastCommit)
		require.NoError(err, "Failed to create ABCI header")

		abciBlock, err := cometcompat.ToABCIBlock(abciHeader, lastCommit, blockData)
		require.NoError(err, "Failed to create ABCI block")

		blockParts, err := abciBlock.MakePartSet(cmttypes.BlockPartSizeBytes)
		require.NoError(err, "Failed to create part set")

		// use abci header hash to match the light client validation check
		blockID := &cmttypes.BlockID{
			Hash:          abciHeader.Hash(),
			PartSetHeader: blockParts.Header(),
		}

		err = env.Adapter.Store.SaveBlockID(context.Background(), blockHeight, blockID)
		require.NoError(err, "Failed to save BlockID for height %d", blockHeight)

		// create the signature for the rollkit block
		realSignature := signBlock(t, env.Adapter.Store, rollkitHeader, aggregatorPrivKey)

		// mock the store to return our signed block
		mockBlock(blockHeight, rollkitHeader, blockData, realSignature, aggregatorPubKey, validatorAddress)

		// call Commit RPC
		commitResult := callCommitRPC(t, blockHeight)

		// light client verification
		if blockHeight == 1 {
			trustedHeader = commitResult.SignedHeader
			verifyFirstBlock(t, fixedValSet, chainID, trustedHeader)
		} else {
			verifySubsequentBlock(t, fixedValSet, &trustedHeader, &commitResult.SignedHeader)
			trustedHeader = commitResult.SignedHeader
		}
	}
}

func createTestBlock(height uint64, chainID string, baseTime time.Time, validatorAddress []byte, validatorHash []byte, offset int) (*types.Data, types.Header) {
	blockTime := uint64(baseTime.UnixNano() + int64(offset-1)*int64(time.Second))

	blockData := &types.Data{
		Metadata: &types.Metadata{
			ChainID: chainID,
			Height:  height,
			Time:    blockTime,
		},
		Txs: make(types.Txs, 0),
	}

	rollkitHeader := types.Header{
		BaseHeader: types.BaseHeader{
			Height:  height,
			Time:    blockTime,
			ChainID: chainID,
		},
		Version:         types.Version{Block: 1, App: 1},
		DataHash:        blockData.DACommitment(),
		AppHash:         make([]byte, 32),
		ProposerAddress: validatorAddress,
		ValidatorHash:   validatorHash,
	}

	return blockData, rollkitHeader
}

func signBlock(t *testing.T, abciExecStore *execstore.Store, header types.Header, privKey crypto.PrivKey) []byte {
	signBytes, err := cometcompat.SignaturePayloadProvider(abciExecStore)(&header)
	require.NoError(t, err)

	signature, err := privKey.Sign(signBytes)
	require.NoError(t, err)

	return signature
}

func mockBlock(height uint64, header types.Header, data *types.Data, signature []byte, pubKey crypto.PubKey, address []byte) {
	signedHeader := &types.SignedHeader{
		Header:    header,
		Signature: types.Signature(signature),
		Signer: types.Signer{
			PubKey:  pubKey,
			Address: address,
		},
	}

	env.Adapter.RollkitStore.(*rollkitmocks.MockStore).On("Height", mock.Anything).Return(header.Height(), nil).Once()

	env.Adapter.RollkitStore.(*rollkitmocks.MockStore).On("GetBlockData", mock.Anything, height).Return(signedHeader, data, nil).Once()

	env.Adapter.RollkitStore.(*rollkitmocks.MockStore).On("GetHeader", mock.Anything, height).Return(signedHeader, nil).Maybe()
}

func callCommitRPC(t *testing.T, height uint64) *ctypes.ResultCommit {
	heightForRPC := int64(height)
	result, err := Commit(&rpctypes.Context{}, &heightForRPC)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func verifyFirstBlock(t *testing.T, valSet *cmttypes.ValidatorSet, chainID string, header cmttypes.SignedHeader) {
	err := valSet.VerifyCommitLight(chainID, header.Commit.BlockID, header.Height, header.Commit)
	require.NoError(t, err, "First block verification must pass")
}

func verifySubsequentBlock(t *testing.T, valSet *cmttypes.ValidatorSet, trustedHeader, newHeader *cmttypes.SignedHeader) {
	require := require.New(t)

	trustingPeriod := 3 * time.Hour
	trustLevel := math.Fraction{Numerator: 1, Denominator: 1}
	maxClockDrift := 10 * time.Second

	err := light.Verify(
		trustedHeader,
		valSet,
		newHeader,
		valSet,
		trustingPeriod,
		newHeader.Time,
		maxClockDrift,
		trustLevel,
	)
	require.NoError(err, "Light client verification must pass")
}

// MockSigner implements the signer.Signer interface for testing
type MockSigner struct {
	mock.Mock
}

func (m *MockSigner) Sign(message []byte) ([]byte, error) {
	args := m.Called(message)
	if fn, ok := args.Get(0).(func([]byte) []byte); ok {
		return fn(message), args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockSigner) GetPublic() (crypto.PubKey, error) {
	args := m.Called()
	return args.Get(0).(crypto.PubKey), args.Error(1)
}

func (m *MockSigner) GetAddress() ([]byte, error) {
	args := m.Called()
	return args.Get(0).([]byte), args.Error(1)
}

// setupTestEnvironment creates a test environment
func setupTestEnvironment(signer *MockSigner) *Environment {
	mockTxIndexer := new(MockTxIndexer)
	mockBlockIndexer := new(MockBlockIndexer)
	mockApp := new(MockApp)
	mockRollkitStore := new(rollkitmocks.MockStore)

	// Create a real adapter store for the test
	dsStore := ds.NewMapDatastore()
	abciExecStore := execstore.NewExecABCIStore(dsStore)

	return &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
			Store:        abciExecStore,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
		Signer:       signer,
	}
}
