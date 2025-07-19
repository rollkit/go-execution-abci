package server

import (
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtstate "github.com/cometbft/cometbft/proto/tendermint/state"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cometbft/cometbft/state"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	rollkittypes "github.com/rollkit/rollkit/types"
)

func TestCometBlockToRollkit(t *testing.T) {
	// create mock private key and address
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	address := pubKey.Address()

	// create mock block
	blockHeight := int64(100)
	blockTime := time.Now()
	chainID := "test-chain"

	// create mock transactions
	txs := []cmttypes.Tx{
		[]byte("tx1"),
		[]byte("tx2"),
		[]byte("transaction3"),
	}

	// create mock block
	block := &cmttypes.Block{
		Header: cmttypes.Header{
			Version: version.Consensus{
				Block: 11,
				App:   1,
			},
			ChainID:            chainID,
			Height:             blockHeight,
			Time:               blockTime,
			LastBlockID:        cmttypes.BlockID{Hash: []byte("lastblockhash")},
			LastCommitHash:     []byte("lastcommithash"),
			DataHash:           []byte("datahash"),
			ValidatorsHash:     []byte("validatorshash"),
			NextValidatorsHash: []byte("nextvalidatorshash"),
			ConsensusHash:      []byte("consensushash"),
			AppHash:            []byte("apphash"),
			LastResultsHash:    []byte("lastresultshash"),
			EvidenceHash:       []byte("evidencehash"),
			ProposerAddress:    address,
		},
		Data: cmttypes.Data{
			Txs: txs,
		},
		Evidence: cmttypes.EvidenceData{},
		LastCommit: &cmttypes.Commit{
			Height: blockHeight - 1,
			Round:  0,
			BlockID: cmttypes.BlockID{
				Hash: []byte("previousblockhash"),
			},
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: address,
					Timestamp:        blockTime,
					Signature:        []byte("signature"),
				},
			},
		},
	}

	// call the function under test
	header, data, signature := cometBlockToRollkit(block)

	// verify header conversion
	require.NotNil(t, header)
	require.Equal(t, uint64(blockHeight), header.Height())
	require.Equal(t, uint64(blockTime.UnixNano()), uint64(header.Time().UnixNano()))
	require.Equal(t, chainID, header.ChainID())
	require.Equal(t, uint64(11), header.Version.Block)
	require.Equal(t, uint64(1), header.Version.App)
	require.Equal(t, block.Header.Hash().Bytes(), []byte(header.LastHeaderHash))
	require.Equal(t, []byte("lastcommithash"), []byte(header.LastCommitHash))
	require.Equal(t, []byte("datahash"), []byte(header.DataHash))
	require.Equal(t, []byte("consensushash"), []byte(header.ConsensusHash))
	require.Equal(t, []byte("apphash"), []byte(header.AppHash))
	require.Equal(t, []byte("lastresultshash"), []byte(header.LastResultsHash))
	require.Equal(t, []byte("validatorshash"), []byte(header.ValidatorHash))
	require.Equal(t, address.Bytes(), header.ProposerAddress)

	// verify signature extraction
	require.Equal(t, []byte("signature"), []byte(signature))
	require.Equal(t, []byte("signature"), []byte(header.Signature))

	// verify data conversion
	require.NotNil(t, data)
	require.Equal(t, chainID, data.Metadata.ChainID)
	require.Equal(t, uint64(blockHeight), data.Metadata.Height)
	require.Equal(t, uint64(blockTime.UnixNano()), data.Metadata.Time)
	require.Equal(t, []byte("datahash"), []byte(data.LastDataHash))

	// verify transactions
	require.Len(t, data.Txs, len(txs))
	for i, tx := range txs {
		require.Equal(t, rollkittypes.Tx(tx), data.Txs[i])
	}
}

func TestCometBlockToRollkitNoSignature(t *testing.T) {
	// create mock private key and address
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	address := pubKey.Address()

	// create different address for proposer
	proposerPrivKey := ed25519.GenPrivKey()
	proposerPubKey := proposerPrivKey.PubKey()
	proposerAddress := proposerPubKey.Address()

	// create mock block with no matching signature
	block := &cmttypes.Block{
		Header: cmttypes.Header{
			Version: version.Consensus{
				Block: 11,
				App:   1,
			},
			ChainID:         "test-chain",
			Height:          100,
			Time:            time.Now(),
			ProposerAddress: proposerAddress,
		},
		Data: cmttypes.Data{
			Txs: []cmttypes.Tx{},
		},
		Evidence: cmttypes.EvidenceData{},
		LastCommit: &cmttypes.Commit{
			Height: 99,
			Round:  0,
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: address, // different from proposer
					Timestamp:        time.Now(),
					Signature:        []byte("signature"),
				},
			},
		},
	}

	// call the function under test
	header, data, signature := cometBlockToRollkit(block)

	// verify signature is empty when proposer signature not found
	require.Empty(t, signature)
	require.Empty(t, header.Signature)

	// verify other fields are still converted correctly
	require.Equal(t, uint64(100), header.Height())
	require.Equal(t, "test-chain", header.ChainID())
	require.NotNil(t, data)
}

func TestCometbftStateToRollkitState(t *testing.T) {
	// create mock cometbft state
	chainID := "test-chain"
	blockHeight := int64(100)
	blockTime := time.Now()
	daHeight := uint64(50)

	cometBFTState := state.State{
		Version: cmtstate.Version{
			Consensus: version.Consensus{
				Block: 11,
				App:   1,
			},
		},
		ChainID:         chainID,
		InitialHeight:   1,
		LastBlockHeight: blockHeight,
		LastBlockTime:   blockTime,
		LastResultsHash: []byte("lastresultshash"),
		AppHash:         []byte("apphash"),
	}

	// call the function under test
	rollkitState, err := cometbftStateToRollkitState(cometBFTState, daHeight)

	// verify no error
	require.NoError(t, err)

	// verify state conversion
	require.Equal(t, uint64(11), rollkitState.Version.Block)
	require.Equal(t, uint64(1), rollkitState.Version.App)
	require.Equal(t, chainID, rollkitState.ChainID)
	require.Equal(t, uint64(blockHeight), rollkitState.InitialHeight) // should be migration height
	require.Equal(t, uint64(blockHeight), rollkitState.LastBlockHeight)
	require.Equal(t, blockTime, rollkitState.LastBlockTime)
	require.Equal(t, daHeight, rollkitState.DAHeight)
	require.Equal(t, []byte("lastresultshash"), []byte(rollkitState.LastResultsHash))
	require.Equal(t, []byte("apphash"), []byte(rollkitState.AppHash))
}

func TestRollkitMigrationGenesis(t *testing.T) {
	// create mock private key and validator
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	address := pubKey.Address()

	chainID := "test-chain"
	initialHeight := int64(1)
	blockTime := time.Now()

	// create mock validator set with single validator
	validator := &cmttypes.Validator{
		Address:     address,
		PubKey:      pubKey,
		VotingPower: 100,
	}

	validators := []*cmttypes.Validator{validator}
	validatorSet := cmttypes.NewValidatorSet(validators)

	// create migration genesis
	genesis := rollkitMigrationGenesis{
		ChainID:         chainID,
		InitialHeight:   uint64(initialHeight),
		GenesisTime:     blockTime.UnixNano(),
		SequencerAddr:   address.Bytes(),
		SequencerPubKey: pubKey,
	}

	// convert to rollkit genesis
	rollkitGenesis := genesis.ToRollkitGenesis()

	// verify conversion
	require.NotNil(t, rollkitGenesis)
	require.Equal(t, chainID, rollkitGenesis.ChainID)
	require.Equal(t, uint64(initialHeight), rollkitGenesis.InitialHeight)
	require.Equal(t, time.Unix(0, blockTime.UnixNano()), rollkitGenesis.GenesisDAStartTime)
	require.Equal(t, address.Bytes(), rollkitGenesis.ProposerAddress)

	// verify validator set contains the expected validator
	require.Equal(t, address, validatorSet.Validators[0].Address)
	require.Equal(t, pubKey, validatorSet.Validators[0].PubKey)
}

func TestCometBlockToRollkitWithMultipleSignatures(t *testing.T) {
	// create multiple validators
	privKey1 := ed25519.GenPrivKey()
	pubKey1 := privKey1.PubKey()
	address1 := pubKey1.Address()

	privKey2 := ed25519.GenPrivKey()
	pubKey2 := privKey2.PubKey()
	address2 := pubKey2.Address()

	privKey3 := ed25519.GenPrivKey()
	pubKey3 := privKey3.PubKey()
	address3 := pubKey3.Address()

	// create mock block with multiple signatures
	block := &cmttypes.Block{
		Header: cmttypes.Header{
			Version: version.Consensus{
				Block: 11,
				App:   1,
			},
			ChainID:         "test-chain",
			Height:          100,
			Time:            time.Now(),
			ProposerAddress: address2, // middle validator is proposer
		},
		Data: cmttypes.Data{
			Txs: []cmttypes.Tx{[]byte("tx1")},
		},
		Evidence: cmttypes.EvidenceData{},
		LastCommit: &cmttypes.Commit{
			Height: 99,
			Round:  0,
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: address1,
					Timestamp:        time.Now(),
					Signature:        []byte("signature1"),
				},
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: address2, // proposer signature
					Timestamp:        time.Now(),
					Signature:        []byte("proposer_signature"),
				},
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: address3,
					Timestamp:        time.Now(),
					Signature:        []byte("signature3"),
				},
			},
		},
	}

	// call the function under test
	header, data, signature := cometBlockToRollkit(block)

	// verify correct proposer signature is extracted
	require.Equal(t, []byte("proposer_signature"), []byte(signature))
	require.Equal(t, []byte("proposer_signature"), []byte(header.Signature))

	// verify other fields
	require.Equal(t, address2.Bytes(), header.ProposerAddress)
	require.NotNil(t, data)
	require.Len(t, data.Txs, 1)
}

func TestCometBlockToRollkitEmptyTransactions(t *testing.T) {
	// create mock private key and address
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	address := pubKey.Address()

	// create mock block with no transactions
	block := &cmttypes.Block{
		Header: cmttypes.Header{
			Version: version.Consensus{
				Block: 11,
				App:   1,
			},
			ChainID:         "test-chain",
			Height:          100,
			Time:            time.Now(),
			ProposerAddress: address,
		},
		Data: cmttypes.Data{
			Txs: []cmttypes.Tx{}, // empty transactions
		},
		Evidence: cmttypes.EvidenceData{},
		LastCommit: &cmttypes.Commit{
			Height: 99,
			Round:  0,
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: address,
					Timestamp:        time.Now(),
					Signature:        []byte("signature"),
				},
			},
		},
	}

	// call the function under test
	header, data, signature := cometBlockToRollkit(block)

	// verify empty transactions are handled correctly
	require.NotNil(t, header)
	require.NotNil(t, data)
	require.Empty(t, data.Txs) // should be empty slice, not nil
	require.Equal(t, []byte("signature"), []byte(signature))

	// verify metadata is still set correctly
	require.Equal(t, "test-chain", data.Metadata.ChainID)
	require.Equal(t, uint64(100), data.Metadata.Height)
}

// helper function to create a mock cometbft state
func createMockCometBFTState(chainID string, height int64, validators []*cmttypes.Validator) state.State {
	validatorSet := cmttypes.NewValidatorSet(validators)

	return state.State{
		Version: cmtstate.Version{
			Consensus: version.Consensus{
				Block: 11,
				App:   1,
			},
		},
		ChainID:         chainID,
		InitialHeight:   1,
		LastBlockHeight: height,
		LastBlockTime:   time.Now(),
		LastValidators:  validatorSet,
		Validators:      validatorSet,
		NextValidators:  validatorSet,
		LastResultsHash: []byte("lastresultshash"),
		AppHash:         []byte("apphash"),
	}
}

// helper function to create a mock validator
func createMockValidator() (*cmttypes.Validator, crypto.PrivKey) {
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	address := pubKey.Address()

	validator := &cmttypes.Validator{
		Address:     address,
		PubKey:      pubKey,
		VotingPower: 100,
	}

	return validator, privKey
}

func TestMigrationWithSingleValidator(t *testing.T) {
	// create single validator
	validator, privKey := createMockValidator()
	validators := []*cmttypes.Validator{validator}

	// create mock state
	cometBFTState := createMockCometBFTState("test-chain", 100, validators)

	// test state conversion
	rollkitState, err := cometbftStateToRollkitState(cometBFTState, 50)
	require.NoError(t, err)

	// verify state
	require.Equal(t, "test-chain", rollkitState.ChainID)
	require.Equal(t, uint64(100), rollkitState.LastBlockHeight)
	require.Equal(t, uint64(50), rollkitState.DAHeight)

	// create migration genesis
	genesis := rollkitMigrationGenesis{
		ChainID:         cometBFTState.ChainID,
		InitialHeight:   uint64(cometBFTState.InitialHeight),
		GenesisTime:     cometBFTState.LastBlockTime.UnixNano(),
		SequencerAddr:   validator.Address.Bytes(),
		SequencerPubKey: validator.PubKey,
	}

	// verify genesis
	require.Equal(t, "test-chain", genesis.ChainID)
	require.Equal(t, validator.Address.Bytes(), genesis.SequencerAddr)
	require.Equal(t, validator.PubKey, genesis.SequencerPubKey)

	// verify rollkit genesis conversion
	rollkitGenesis := genesis.ToRollkitGenesis()
	require.Equal(t, "test-chain", rollkitGenesis.ChainID)
	require.Equal(t, validator.Address.Bytes(), rollkitGenesis.ProposerAddress)

	// use privKey to avoid "declared but not used" error
	_ = privKey
}

func TestMigrationIntegration(t *testing.T) {
	// create mock validator and state
	validator, _ := createMockValidator()
	validators := []*cmttypes.Validator{validator}
	cometBFTState := createMockCometBFTState("integration-chain", 200, validators)

	// create mock block
	blockTime := time.Now()
	txs := []cmttypes.Tx{
		[]byte("integration_tx1"),
		[]byte("integration_tx2"),
	}

	block := &cmttypes.Block{
		Header: cmttypes.Header{
			Version: version.Consensus{
				Block: 11,
				App:   1,
			},
			ChainID:         "integration-chain",
			Height:          200,
			Time:            blockTime,
			ProposerAddress: validator.Address,
			DataHash:        []byte("integration_datahash"),
		},
		Data: cmttypes.Data{
			Txs: txs,
		},
		LastCommit: &cmttypes.Commit{
			Height: 199,
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: validator.Address,
					Signature:        []byte("integration_signature"),
				},
			},
		},
	}

	// test block conversion
	header, data, signature := cometBlockToRollkit(block)

	// verify block conversion
	require.Equal(t, uint64(200), header.Height())
	require.Equal(t, "integration-chain", header.ChainID())
	require.Equal(t, []byte("integration_signature"), []byte(signature))
	require.Len(t, data.Txs, 2)
	require.Equal(t, rollkittypes.Tx("integration_tx1"), data.Txs[0])
	require.Equal(t, rollkittypes.Tx("integration_tx2"), data.Txs[1])

	// test state conversion
	rollkitState, err := cometbftStateToRollkitState(cometBFTState, 100)
	require.NoError(t, err)

	// verify state conversion
	require.Equal(t, "integration-chain", rollkitState.ChainID)
	require.Equal(t, uint64(200), rollkitState.LastBlockHeight)
	require.Equal(t, uint64(100), rollkitState.DAHeight)

	// verify consistency between block and state
	require.Equal(t, header.ChainID(), rollkitState.ChainID)
	require.Equal(t, header.Height(), rollkitState.LastBlockHeight)
}
