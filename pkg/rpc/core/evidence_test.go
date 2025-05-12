package core

import (
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastEvidence(t *testing.T) {
	env = &Environment{Logger: cmtlog.NewNopLogger()}

	// Create sample evidence (DuplicateVoteEvidence is a concrete type)
	// We need to provide the necessary fields for DuplicateVoteEvidence.
	// For simplicity, we'll use some placeholder values.
	// Note: Creating valid evidence, especially one that passes ValidateBasic,
	// can be complex. For this test, we're primarily concerned with the no-op
	// behavior of BroadcastEvidence and its return value.

	// Create a dummy vote
	dummyVote := func(valIndex int32, round int32) *cmttypes.Vote {
		privKey := ed25519.GenPrivKey()
		pubKey := privKey.PubKey()
		addr := pubKey.Address()
		return &cmttypes.Vote{
			ValidatorAddress: addr,
			ValidatorIndex:   valIndex,
			Height:           1,
			Round:            round,
			Timestamp:        time.Now(),
			Type:             cmtproto.PrevoteType,
			BlockID:          cmttypes.BlockID{Hash: []byte("block_hash_test_evidence"), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: []byte("part_hash_test_evidence_")}},
		}
	}

	ev := &cmttypes.DuplicateVoteEvidence{
		VoteA:            dummyVote(0, 0),
		VoteB:            dummyVote(0, 1), // Different round to make it a "duplicate" in a simple sense
		TotalVotingPower: 100,
		ValidatorPower:   10,
		Timestamp:        time.Now(),
	}

	result, err := BroadcastEvidence(nil, ev) // ctx is not used by the function

	require.NoError(t, err, "BroadcastEvidence should not return an error")
	require.NotNil(t, result, "Result should not be nil")
	assert.Equal(t, ev.Hash(), result.Hash, "Returned hash should match evidence hash")
}
