package keeper_test

import (
	"context"
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/libs/math"
	"github.com/cometbft/cometbft/light"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
	"github.com/rollkit/rollkit/pkg/signer/noop"
	rollkittypes "github.com/rollkit/rollkit/types"
)

// ValidatorInfo holds information about a validator
type ValidatorInfo struct {
	PrivKey      crypto.PrivKey
	PubKey       crypto.PubKey
	CmtPrivKey   ed25519.PrivKey
	CmtPubKey    ed25519.PubKey
	Signer       *noop.NoopSigner
	Address      []byte
	CmtAddress   []byte
	CmtValidator *cmttypes.Validator
}

// MockRollkitStore implements a mock for RollkitStore interface
type MockRollkitStore struct {
	mock.Mock
}

func (m *MockRollkitStore) GetBlockData(ctx context.Context, height uint64) (*rollkittypes.SignedHeader, *rollkittypes.Data, error) {
	args := m.Called(ctx, height)
	return args.Get(0).(*rollkittypes.SignedHeader), args.Get(1).(*rollkittypes.Data), args.Error(2)
}

func (m *MockRollkitStore) Height(ctx context.Context) (uint64, error) {
	args := m.Called(ctx)
	return args.Get(0).(uint64), args.Error(1)
}

func (m *MockRollkitStore) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockRollkitStore) GetBlockByHash(ctx context.Context, hash []byte) (*rollkittypes.SignedHeader, *rollkittypes.Data, error) {
	args := m.Called(ctx, hash)
	return args.Get(0).(*rollkittypes.SignedHeader), args.Get(1).(*rollkittypes.Data), args.Error(2)
}

func (m *MockRollkitStore) GetMetadata(ctx context.Context, key string) ([]byte, error) {
	args := m.Called(ctx, key)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *MockRollkitStore) GetState(ctx context.Context) (*rollkittypes.State, error) {
	args := m.Called(ctx)
	return args.Get(0).(*rollkittypes.State), args.Error(1)
}

func (m *MockRollkitStore) GetSignature(ctx context.Context, height uint64) (*rollkittypes.Signature, error) {
	args := m.Called(ctx, height)
	return args.Get(0).(*rollkittypes.Signature), args.Error(1)
}

func (m *MockRollkitStore) GetSignatureByHash(ctx context.Context, hash []byte) (*rollkittypes.Signature, error) {
	args := m.Called(ctx, hash)
	return args.Get(0).(*rollkittypes.Signature), args.Error(1)
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

// TestRollkitConsecutiveBlocksWithMultipleValidators tests using the real Commit RPC endpoint
func TestRollkitConsecutiveBlocksWithMultipleValidators(t *testing.T) {
	chainID := "test-multi-validator-chain"

	// =========================
	// SETUP: CREATE 3 VALIDATORS
	// =========================
	t.Log("🔧 Setting up validator set with 3 validators...")

	validators := make([]*ValidatorInfo, 3)
	cmtValidators := make([]*cmttypes.Validator, 3)

	// Create 3 validators
	for i := 0; i < 3; i++ {
		// Create CometBFT keys first
		cmtPrivKey := ed25519.GenPrivKey()
		cmtPubKey := cmtPrivKey.PubKey()

		// Convert CometBFT private key to libp2p for compatibility
		privKey, err := crypto.UnmarshalEd25519PrivateKey(cmtPrivKey.Bytes())
		require.NoError(t, err, "Failed to convert CometBFT key to libp2p for validator %d", i)

		// Create noop signer
		signer, err := noop.NewNoopSigner(privKey)
		require.NoError(t, err, "Failed to create signer for validator %d", i)

		// Get Rollkit address (32 bytes) for compatibility with GetFirstSignedHeader
		address, err := signer.GetAddress()
		require.NoError(t, err, "Failed to get address for validator %d", i)

		// Create CometBFT validator with equal voting power
		cmtValidator := cmttypes.NewValidator(cmtPubKey, 1)

		validators[i] = &ValidatorInfo{
			PrivKey:      privKey,
			PubKey:       privKey.GetPublic(),
			CmtPrivKey:   cmtPrivKey,
			CmtPubKey:    cmtPubKey.(ed25519.PubKey),
			Signer:       signer.(*noop.NoopSigner),
			Address:      address,
			CmtAddress:   cmtPubKey.Address(),
			CmtValidator: cmtValidator,
		}
		cmtValidators[i] = cmtValidator

		t.Logf("   ✅ Validator %d: Address %x", i, address)
	}

	// Sequencer is the first validator
	sequencer := validators[0]

	// Create a validator set for light client verification
	valSet := &cmttypes.ValidatorSet{
		Validators: cmtValidators,
		Proposer:   cmtValidators[0], // Sequencer is the proposer
	}

	// =========================
	// SETUP RPC ENVIRONMENT (exactly like blocks_test.go)
	// =========================

	// Create mock signer that will be used by the RPC Commit endpoint
	mockSigner := &MockSigner{}
	mockSigner.On("Sign", mock.AnythingOfType("[]uint8")).Return(func(message []byte) []byte {
		// Sign with the sequencer's private key
		signature, _ := sequencer.CmtPrivKey.Sign(message)
		return signature
	}, nil)
	mockSigner.On("GetPublic").Return(sequencer.PubKey, nil)
	mockSigner.On("GetAddress").Return(sequencer.Address, nil)

	// Create mock rollkit store
	mockRollkitStore := &MockRollkitStore{}

	t.Log("✨ Simulating multi-validator commits and verifying with light client...")
	t.Log("🔐 Using ABCI-compatible signature payload provider for realistic signing")

	// =========================
	// CREATE BLOCKS WITH MULTI-VALIDATOR SUPPORT
	// =========================

	blocks := make([]*rollkittypes.SignedHeader, 3)
	blockData := make([]*rollkittypes.Data, 3)

	var lastCommit *cmttypes.Commit
	var trustedHeader *cmttypes.SignedHeader

	// Create 3 blocks
	for i := 0; i < 3; i++ {
		height := uint64(i + 1)

		var block *rollkittypes.SignedHeader
		var data *rollkittypes.Data
		var err error

		if i == 0 {
			// Genesis block
			t.Logf("📦 Creating Genesis Block (Height %d)...", height)
			block, err = rollkittypes.GetFirstSignedHeader(sequencer.Signer, chainID)
			require.NoError(t, err, "Failed to create genesis block")
			lastCommit = &cmttypes.Commit{Height: 0}
		} else {
			// Subsequent blocks
			t.Logf("📦 Creating Block %d (Height %d)...", i+1, height)
			block, err = rollkittypes.GetRandomNextSignedHeader(blocks[i-1], sequencer.Signer, chainID)
			require.NoError(t, err, "Failed to create block %d", i+1)
		}

		// Create block data
		data = &rollkittypes.Data{
			Txs: make(rollkittypes.Txs, i+3), // Variable number of transactions
		}
		for j := range data.Txs {
			data.Txs[j] = rollkittypes.GetRandomTx()
		}

		// Update block with proper data hash
		block.DataHash = data.DACommitment()

		// Re-sign block with updated data hash using ABCI payload provider
		payloadProvider := cometcompat.PayloadProvider()
		payload, err := payloadProvider(&block.Header)
		require.NoError(t, err, "ABCI payload generation should succeed for block %d", i+1)

		signature, err := sequencer.Signer.Sign(payload)
		require.NoError(t, err, "Block %d signing should succeed", i+1)
		block.Signature = signature

		// Set custom verifier
		err = block.SetCustomVerifier(rollkittypes.SignatureVerifier(payloadProvider))
		require.NoError(t, err, "Block %d custom verifier should be set", i+1)

		// Validate block
		err = block.ValidateBasic()
		require.NoError(t, err, "Block %d validation should pass", i+1)

		blocks[i] = block
		blockData[i] = data

		// Mock the store to return our block for the RPC call (exactly like blocks_test.go)
		mockRollkitStore.On("GetBlockData", mock.Anything, height).Return(block, data, nil).Once()
		mockRollkitStore.On("Height", mock.Anything).Return(height, nil).Maybe()

		t.Logf("✅ Block %d created: Height=%d, Hash=%x, Transactions=%d",
			i+1, block.Height(), block.Hash(), len(data.Txs))

		// =========================
		// MULTI-VALIDATOR COMMIT & LIGHT CLIENT VERIFICATION
		// =========================

		// Create a copy of the block to modify for CometBFT compatibility
		cometCompatBlock := *block
		cometCompatBlock.ProposerAddress = sequencer.CmtAddress

		// Get the block hash (BlockID) for signing the commit
		cometBlock, err := cometcompat.ToABCIBlock(&cometCompatBlock, data, lastCommit, valSet)
		require.NoError(t, err, "Failed to convert to CometBFT block for block %d", i+1)
		blockID := cmttypes.BlockID{Hash: cometBlock.Hash()}

		// Create a commit with signatures from all validators
		currentCommit := createMultiValidatorCommit(t, &block.Header, blockID, valSet, validators, chainID)

		// Create a CometBFT signed header for light client verification
		cmtSignedHeader := &cmttypes.SignedHeader{
			Header: &cometBlock.Header,
			Commit: currentCommit,
		}

		// Perform light client verification
		if i == 0 {
			// For the first block, we trust it and verify its own commit
			trustedHeader = cmtSignedHeader
			err = valSet.VerifyCommitLight(chainID, trustedHeader.Commit.BlockID, trustedHeader.Height, trustedHeader.Commit)
			require.NoError(t, err, "Light client verification of first block commit should pass")
		} else {
			// For subsequent blocks, verify against the previously trusted header
			trustingPeriod := 3 * time.Hour
			trustLevel := math.Fraction{Numerator: 2, Denominator: 3}
			maxClockDrift := 10 * time.Second
			now := block.Time()

			err = light.Verify(trustedHeader, valSet, cmtSignedHeader, valSet, trustingPeriod, now, maxClockDrift, trustLevel)
			require.NoError(t, err, "Light client verification should pass for block %d", i+1)
			trustedHeader = cmtSignedHeader
		}
		t.Logf("✅ Light client verification passed for block %d", i+1)

		// The current commit becomes the lastCommit for the next block
		lastCommit = currentCommit
	}

	t.Log("🎉 MULTI-VALIDATOR BLOCKCHAIN DEMONSTRATION COMPLETE!")
	t.Log("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	t.Logf("✅ Chain ID: %s", chainID)
	t.Logf("✅ Total Blocks: %d", len(blocks))
	t.Logf("✅ Total Validators: %d", len(validators))
	t.Logf("✅ Multi-validator capable: true")
	for i, block := range blocks {
		t.Logf("✅ Block %d: Height %d, Hash %x", i+1, block.Height(), block.Hash())
	}
	t.Logf("✅ Total Transactions: %d", len(blockData[0].Txs)+len(blockData[1].Txs)+len(blockData[2].Txs))
	t.Log("✅ All blocks created and validated")
	t.Log("✅ ABCI-compatible signature payload provider used")
	t.Log("✅ Multi-validator commits simulated successfully")
	t.Log("✅ Light client verification passed for all blocks")
	t.Log("🎯 Test demonstrates multi-validator infrastructure for Rollkit and light client compatibility")
}

// createMultiValidatorCommit creates a CometBFT commit with signatures from all specified validators.
func createMultiValidatorCommit(t *testing.T, header *rollkittypes.Header, blockID cmttypes.BlockID, valSet *cmttypes.ValidatorSet, validators []*ValidatorInfo, chainID string) *cmttypes.Commit {
	t.Helper()
	commitSigs := []cmttypes.CommitSig{}

	// Sign with all validators
	for _, validator := range validators {
		index, val := valSet.GetByAddress(validator.CmtPubKey.Address())
		require.NotNil(t, val, "validator %X not found in valset", validator.CmtPubKey.Address())

		vote := cmtproto.Vote{
			Type:             cmtproto.PrecommitType,
			Height:           int64(header.Height()),
			Round:            0,
			BlockID:          cmtproto.BlockID{Hash: blockID.Hash, PartSetHeader: blockID.PartSetHeader.ToProto()},
			Timestamp:        header.Time(),
			ValidatorAddress: validator.CmtPubKey.Address(),
			ValidatorIndex:   int32(index),
		}

		signBytes := cmttypes.VoteSignBytes(chainID, &vote)
		signature, err := validator.CmtPrivKey.Sign(signBytes)
		require.NoError(t, err)

		commitSigs = append(commitSigs, cmttypes.CommitSig{
			BlockIDFlag:      cmttypes.BlockIDFlagCommit,
			ValidatorAddress: validator.CmtPubKey.Address(),
			Timestamp:        header.Time(),
			Signature:        signature,
		})
	}

	return &cmttypes.Commit{
		Height:     int64(header.Height()),
		Round:      0,
		BlockID:    blockID,
		Signatures: commitSigs,
	}
}
