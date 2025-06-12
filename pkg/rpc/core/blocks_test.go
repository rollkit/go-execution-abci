package core

import (
	"testing"
	"time"

	cryptotypes "github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/libs/math"
	"github.com/cometbft/cometbft/light"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
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
	t.Skip()

	require := require.New(t)
	assert := assert.New(t)

	mockTxIndexer := new(MockTxIndexer)
	mockBlockIndexer := new(MockBlockIndexer)
	mockApp := new(MockApp)
	mockRollkitStore := new(MockRollkitStore)

	env = &Environment{
		Adapter: &adapter.Adapter{
			RollkitStore: mockRollkitStore,
			App:          mockApp,
		},
		TxIndexer:    mockTxIndexer,
		BlockIndexer: mockBlockIndexer,
		Logger:       cmtlog.NewNopLogger(),
	}

	privKey, pubKey, err := crypto.GenerateEd25519Key(nil)
	require.NoError(err)

	pubKeyBytes, err := pubKey.Raw()
	require.NoError(err)
	var cmtEdPubKey cryptotypes.PubKey = ed25519.PubKey(pubKeyBytes)

	fixedValSet := &cmttypes.ValidatorSet{
		Validators: []*cmttypes.Validator{
			cmttypes.NewValidator(cmtEdPubKey, 1),
		},
		Proposer: cmttypes.NewValidator(cmtEdPubKey, 1),
	}

	var trustedHeader cmttypes.SignedHeader
	setTrustedHeader := false
	var lastRollkitHeaderHash []byte
	var lastRollkitCommitHash []byte
	chainID := "test-chain-multiple-blocks"
	now := time.Now()

	for i := 1; i <= 3; i++ {
		blockHeight := uint64(i)
		heightForRPC := int64(blockHeight)

		// Create Rollkit Block Data
		blockData := &types.Data{
			Metadata: &types.Metadata{
				ChainID:      chainID,
				Height:       blockHeight,
				Time:         uint64(now.UnixNano() + int64(i-1)*int64(time.Second)),
				LastDataHash: nil,
			},
			Txs: make(types.Txs, 0),
		}
		dataHash := blockData.DACommitment()

		// Create Rollkit Header
		rollkitHeader := types.Header{
			BaseHeader: types.BaseHeader{
				Height:  blockHeight,
				Time:    uint64(now.UnixNano() + int64(i-1)*int64(time.Second)),
				ChainID: chainID,
			},
			Version:         types.Version{Block: 1, App: 1},
			LastHeaderHash:  lastRollkitHeaderHash,
			LastCommitHash:  lastRollkitCommitHash,
			DataHash:        dataHash,
			ConsensusHash:   BytesToSliceHash([]byte{byte(i)}),
			AppHash:         BytesToSliceHash([]byte{byte(i + 10)}),
			LastResultsHash: BytesToSliceHash([]byte{byte(i + 20)}),
			ValidatorHash:   BytesToSliceHash(fixedValSet.Hash()),
			ProposerAddress: fixedValSet.Proposer.Address,
		}

		abciHeaderForSigning, err := cometcompat.ToABCIHeader(&rollkitHeader)
		require.NoError(err)
		abciHeaderHashForSigning := abciHeaderForSigning.Hash()
		abciHeaderTimeForSigning := abciHeaderForSigning.Time

		voteProto := cmtproto.Vote{
			Type:             cmtproto.PrecommitType,
			Height:           heightForRPC,
			Round:            0,
			BlockID:          cmtproto.BlockID{Hash: abciHeaderHashForSigning, PartSetHeader: cmtproto.PartSetHeader{Total: 0, Hash: nil}},
			Timestamp:        abciHeaderTimeForSigning,
			ValidatorAddress: fixedValSet.Proposer.Address,
			ValidatorIndex:   0,
		}
		payloadBytes := cmttypes.VoteSignBytes(chainID, &voteProto)
		realSignature, err := privKey.Sign(payloadBytes)
		require.NoError(err)

		// Create Rollkit Signed Header with the new signature
		signer, err := types.NewSigner(pubKey)
		require.NoError(err)
		rollkitSignedHeader := &types.SignedHeader{
			Header:    rollkitHeader,
			Signature: types.Signature(realSignature),
			Signer:    signer,
		}

		// Mock RollkitStore
		mockRollkitStore.On("GetBlockData", mock.Anything, blockHeight).Return(rollkitSignedHeader, blockData, nil).Once()

		// Call the Commit RPC method
		rpcCtx := &rpctypes.Context{}
		commitResult, err := Commit(rpcCtx, &heightForRPC)
		require.NoError(err)
		require.NotNil(commitResult)
		require.NotNil(commitResult.Header)
		require.NotNil(commitResult.Commit)
		assert.Equal(heightForRPC, commitResult.Height)
		assert.EqualValues(rollkitHeader.AppHash, commitResult.AppHash.Bytes()) // AppHash is []byte vs HexBytes

		// Verify with light client
		if !setTrustedHeader {
			trustedHeader = commitResult.SignedHeader
			setTrustedHeader = true
		} else {
			trustingPeriod := 3 * time.Hour
			trustLevel := math.Fraction{Numerator: 1, Denominator: 1}
			maxClockDrift := 10 * time.Second

			err = light.Verify(&trustedHeader, fixedValSet, &commitResult.SignedHeader, fixedValSet, trustingPeriod, time.Unix(0, int64(rollkitHeader.BaseHeader.Time)), maxClockDrift, trustLevel)
			require.NoError(err, "failed to pass light.Verify() for block %d", blockHeight)
			trustedHeader = commitResult.SignedHeader
		}

		// Update last hashes for the next iteration
		currentRollkitHeaderHash := rollkitHeader.Hash()
		lastRollkitHeaderHash = BytesToSliceHash(currentRollkitHeaderHash)

		if commitResult.Commit != nil {
			lastRollkitCommitHash = BytesToSliceHash(commitResult.Commit.Hash())
		}
	}

	mockRollkitStore.AssertExpectations(t)
	mockApp.AssertExpectations(t)
	mockTxIndexer.AssertExpectations(t)
	mockBlockIndexer.AssertExpectations(t)
}

// Renamed and modified helper function to return []byte of 32 length
func BytesToSliceHash(b []byte) []byte {
	h := make([]byte, 32)
	copy(h, b) // copy will take min(len(h), len(b))
	return h
}
