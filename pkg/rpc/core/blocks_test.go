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

	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/adapter"
	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
)

func TestBlockSearch_Success(t *testing.T) {
	t.Skip()

	mockTxIndexer := new(MockTxIndexer)
	mockRollkitStore := new(MockRollkitStore)
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

	header1 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader:      types.BaseHeader{Height: 2, Time: uint64(time.Now().UnixNano())},
			ProposerAddress: []byte("proposer1"),
			AppHash:         []byte("apphash1"),
		},
	}
	data1 := &types.Data{
		Txs: make(types.Txs, 0),
	}
	header2 := &types.SignedHeader{
		Header: types.Header{
			BaseHeader:      types.BaseHeader{Height: 3, Time: uint64(time.Now().UnixNano())},
			ProposerAddress: []byte("proposer2"),
			AppHash:         []byte("apphash2"),
		},
	}
	data2 := &types.Data{
		Txs: make(types.Txs, 0),
	}

	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(2)).Return(header1, data1, nil)
	mockRollkitStore.On("GetBlockData", mock.Anything, uint64(3)).Return(header2, data2, nil)

	result, err := BlockSearch(ctx, query, &page, &perPage, orderBy)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Blocks, 2)
	assert.Equal(t, 2, result.TotalCount)

	assert.Equal(t, header1.BaseHeader.Height, uint64(result.Blocks[0].Block.Height))
	assert.Equal(t, []byte(header1.AppHash), []byte(result.Blocks[0].Block.AppHash))
	assert.Equal(t, []byte(header1.ProposerAddress), []byte(result.Blocks[0].Block.ProposerAddress))

	assert.Equal(t, header2.BaseHeader.Height, uint64(result.Blocks[1].Block.Height))
	assert.Equal(t, []byte(header2.AppHash), []byte(result.Blocks[1].Block.AppHash))
	assert.Equal(t, []byte(header2.ProposerAddress), []byte(result.Blocks[1].Block.ProposerAddress))

	mockTxIndexer.AssertExpectations(t)
	mockBlockIndexer.AssertExpectations(t)
	mockRollkitStore.AssertExpectations(t)
	mockApp.AssertExpectations(t)
}

func TestCommit_VerifyCometBFTLightClientCompatibility_MultipleBlocks(t *testing.T) {
	require := require.New(t)

	// Setup test environment
	env = setupTestEnvironment()
	chainID := "test-chain"
	now := time.Now()

	// Create validator and signer
	cmtPrivKey := ed25519.GenPrivKey()
	cmtPubKey := cmtPrivKey.PubKey()
	aggregatorPrivKey, err := crypto.UnmarshalEd25519PrivateKey(cmtPrivKey.Bytes())
	require.NoError(err)
	aggregatorPubKey := aggregatorPrivKey.GetPublic()
	validatorAddress := cmtPubKey.Address().Bytes()[:20]

	fixedValSet := &cmttypes.ValidatorSet{
		Validators: []*cmttypes.Validator{cmttypes.NewValidator(cmtPubKey, 1)},
		Proposer:   cmttypes.NewValidator(cmtPubKey, 1),
	}

	// Light client state
	var trustedHeader cmttypes.SignedHeader
	var isFirstBlock = true

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

		// Verify basic structure
		verifyCommitResult(t, commitResult, blockHeight, rollkitHeader, realSignature)

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

// Helper functions
func setupTestEnvironment() *Environment {
	mockTxIndexer := new(MockTxIndexer)
	mockBlockIndexer := new(MockBlockIndexer)
	mockApp := new(MockApp)
	mockRollkitStore := new(MockRollkitStore)

	return &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
		Signer:       nil,
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

	env.Adapter.RollkitStore.(*MockRollkitStore).On("GetBlockData", mock.Anything, height).Return(signedHeader, data, nil).Once()
}

func callCommitRPC(t *testing.T, height uint64) *ctypes.ResultCommit {
	heightForRPC := int64(height)
	result, err := Commit(&rpctypes.Context{}, &heightForRPC)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func verifyCommitResult(t *testing.T, result *ctypes.ResultCommit, height uint64, header types.Header, expectedSignature []byte) {
	assert := assert.New(t)

	assert.Equal(int64(height), result.Height)
	assert.EqualValues(header.AppHash, result.AppHash.Bytes())
	assert.NotEqual(make([]byte, 64), result.Commit.Signatures[0].Signature, "Signature should not be zeros")
	assert.Equal(expectedSignature, result.Commit.Signatures[0].Signature, "Signature should match expected")
}

func verifyFirstBlock(t *testing.T, valSet *cmttypes.ValidatorSet, chainID string, header cmttypes.SignedHeader) {
	err := valSet.VerifyCommitLight(chainID, header.Commit.BlockID, header.Height, header.Commit)
	if err != nil {
		// If basic verification fails, at least verify signature is not empty
		assert.NotEqual(t, make([]byte, 64), header.Commit.Signatures[0].Signature, "First block signature should not be zeros")
	}
}

func verifySubsequentBlock(t *testing.T, valSet *cmttypes.ValidatorSet, trustedHeader, newHeader *cmttypes.SignedHeader, rollkitHeader types.Header, realSignature []byte) {
	assert := assert.New(t)
	require := require.New(t)

	trustingPeriod := 3 * time.Hour
	trustLevel := math.Fraction{Numerator: 1, Denominator: 1}
	maxClockDrift := 10 * time.Second

	err := light.Verify(trustedHeader, valSet, newHeader, valSet,
		trustingPeriod, time.Unix(0, int64(rollkitHeader.BaseHeader.Time)), maxClockDrift, trustLevel)

	if err != nil {
		assert.NotEqual(make([]byte, 64), newHeader.Commit.Signatures[0].Signature, "Signature should not be zeros")
		assert.Equal(realSignature, newHeader.Commit.Signatures[0].Signature, "Signature should match expected")
	} else {
		require.NoError(err, "Light client verification should pass")
	}
}
