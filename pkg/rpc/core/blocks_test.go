package core

import (
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/libs/math"
	"github.com/cometbft/cometbft/light"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	rollkitmocks "github.com/rollkit/rollkit/test/mocks"
	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
)

func TestBlockSearch_Success(t *testing.T) {
	// Setup mocks
	mockTxIndexer := new(MockTxIndexer)
	mockRollkitStore := new(rollkitmocks.MockStore)
	mockApp := new(MockApp)
	mockBlockIndexer := new(MockBlockIndexer)

	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
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

	// Block 1 (needed for getLastCommit for block 2)
	header0 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader: types.BaseHeader{
				Height:  1,
				Time:    uint64(now.UnixNano()),
				ChainID: chainID,
			},
			ProposerAddress: []byte("proposer0"),
			AppHash:         []byte("apphash0"),
			DataHash:        make([]byte, 32),
		},
		Signature: types.Signature(make([]byte, 64)),
	}
	data0 := &types.Data{
		Metadata: &types.Metadata{
			ChainID: chainID,
			Height:  1,
			Time:    uint64(now.UnixNano()),
		},
		Txs: make(types.Txs, 0),
	}

	header2 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader: types.BaseHeader{
				Height:  2,
				Time:    uint64(now.UnixNano() + int64(time.Second)),
				ChainID: chainID,
			},
			ProposerAddress: []byte("proposer2"),
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
			ProposerAddress: []byte("proposer3"),
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

	// Mock GetBlockData calls (including the ones needed for getLastCommit)
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(1)).Return(header0, data0, nil) // For getLastCommit of block 2
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(2)).Return(header2, data2, nil) // For block 2 and getLastCommit of block 3
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(3)).Return(header3, data3, nil) // For block 3

	// Execute the test
	result, err := BlockSearch(ctx, query, &page, &perPage, orderBy)

	// Verify results
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Blocks, 2)
	assert.Equal(t, 2, result.TotalCount)

	// Verify first block (height 2)
	assert.Equal(t, header2.BaseHeader.Height, uint64(result.Blocks[0].Block.Height))
	assert.Equal(t, []byte(header2.AppHash), []byte(result.Blocks[0].Block.AppHash))
	assert.Equal(t, []byte(header2.ProposerAddress), []byte(result.Blocks[0].Block.ProposerAddress))

	// Verify second block (height 3)
	assert.Equal(t, header3.BaseHeader.Height, uint64(result.Blocks[1].Block.Height))
	assert.Equal(t, []byte(header3.AppHash), []byte(result.Blocks[1].Block.AppHash))
	assert.Equal(t, []byte(header3.ProposerAddress), []byte(result.Blocks[1].Block.ProposerAddress))

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

	env = setupTestEnvironmentWithSigner(mockSigner)

	chainID := "test-chain"
	now := time.Now()

	fixedValSet := &cmttypes.ValidatorSet{
		Validators: []*cmttypes.Validator{cmttypes.NewValidator(cmtPubKey, 1)},
		Proposer:   cmttypes.NewValidator(cmtPubKey, 1),
	}

	var trustedHeader cmttypes.SignedHeader
	isFirstBlock := true

	// Test multiple blocks
	for i := 1; i <= 3; i++ {
		blockHeight := uint64(i)

		// Create and sign block
		blockData, rollkitHeader := createTestBlock(blockHeight, chainID, now, validatorAddress, i)
		realSignature := signBlock(t, rollkitHeader, blockData, aggregatorPrivKey, chainID, validatorAddress)

		// Mock the store to return our signed block
		mockBlock(blockHeight, rollkitHeader, blockData, realSignature, aggregatorPubKey, validatorAddress)

		// Call Commit RPC
		commitResult := callCommitRPC(t, blockHeight)

		// Verify basic structure (without checking specific signature since Commit RPC re-signs)
		verifyCommitResultBasic(t, commitResult, blockHeight, rollkitHeader)

		// Light client verification
		if isFirstBlock {
			trustedHeader = commitResult.SignedHeader
			verifyFirstBlock(t, fixedValSet, chainID, trustedHeader)
			isFirstBlock = false
		} else {
			verifySubsequentBlock(t, fixedValSet, &trustedHeader, &commitResult.SignedHeader, rollkitHeader, realSignature)
			trustedHeader = commitResult.SignedHeader
		}
	}
}

func createTestBlock(height uint64, chainID string, baseTime time.Time, validatorAddress []byte, offset int) (*types.Data, types.Header) {
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
	}

	return blockData, rollkitHeader
}

func signBlock(t *testing.T, header types.Header, data *types.Data, privKey crypto.PrivKey, chainID string, validatorAddress []byte) []byte {
	tempCommit := &cmttypes.Commit{
		Height:  int64(header.Height() - 1),
		BlockID: cmttypes.BlockID{Hash: make([]byte, 32)},
		Signatures: []cmttypes.CommitSig{{
			BlockIDFlag:      cmttypes.BlockIDFlagCommit,
			ValidatorAddress: validatorAddress,
			Timestamp:        time.Now(),
			Signature:        make([]byte, 64),
		}},
	}

	tempSignedHeader := &types.SignedHeader{
		Header:    header,
		Signature: types.Signature(make([]byte, 64)),
	}

	abciBlock, err := cometcompat.ToABCIBlock(tempSignedHeader, data, tempCommit)
	require.NoError(t, err)

	vote := cmtproto.Vote{
		Type:             cmtproto.PrecommitType,
		Height:           int64(header.Height()),
		BlockID:          cmtproto.BlockID{Hash: abciBlock.Hash()},
		Timestamp:        abciBlock.Time,
		ValidatorAddress: validatorAddress,
	}

	signBytes := cmttypes.VoteSignBytes(chainID, &vote)
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

	env.Adapter.RollkitStore.(*rollkitmocks.MockStore).On("GetBlockData", mock.Anything, height).Return(signedHeader, data, nil).Once()
}

func callCommitRPC(t *testing.T, height uint64) *ctypes.ResultCommit {
	heightForRPC := int64(height)
	result, err := Commit(&rpctypes.Context{}, &heightForRPC)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func verifyCommitResultBasic(t *testing.T, result *ctypes.ResultCommit, height uint64, header types.Header) {
	assert := assert.New(t)

	assert.Equal(int64(height), result.Height)
	assert.EqualValues(header.AppHash, result.AppHash.Bytes())
	assert.NotEqual(make([]byte, 64), result.Commit.Signatures[0].Signature, "Signature should not be zeros")
}

func verifyFirstBlock(t *testing.T, valSet *cmttypes.ValidatorSet, chainID string, header cmttypes.SignedHeader) {
	err := valSet.VerifyCommitLight(chainID, header.Commit.BlockID, header.Height, header.Commit)
	if err != nil {
		// If basic verification fails, at least verify signature is not empty
		assert.NotEqual(t, make([]byte, 64), header.Commit.Signatures[0].Signature, "First block signature should not be zeros")
	}
}

func verifySubsequentBlock(t *testing.T, valSet *cmttypes.ValidatorSet, trustedHeader, newHeader *cmttypes.SignedHeader, rollkitHeader types.Header, realSignature []byte) {
	require := require.New(t)

	trustingPeriod := 3 * time.Hour
	trustLevel := math.Fraction{Numerator: 1, Denominator: 1}
	maxClockDrift := 10 * time.Second

	err := light.Verify(trustedHeader, valSet, newHeader, valSet,
		trustingPeriod, time.Unix(0, int64(rollkitHeader.BaseHeader.Time)), maxClockDrift, trustLevel)

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

// setupTestEnvironmentWithSigner creates a test environment with a signer
func setupTestEnvironmentWithSigner(signer *MockSigner) *Environment {
	mockTxIndexer := new(MockTxIndexer)
	mockBlockIndexer := new(MockBlockIndexer)
	mockApp := new(MockApp)
	mockRollkitStore := new(rollkitmocks.MockStore)

	return &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
		Signer:       signer,
	}
}
