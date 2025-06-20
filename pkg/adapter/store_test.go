package adapter

import (
	"testing"
	"time"

	"github.com/cometbft/cometbft/crypto/secp256k1"
	"github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/types"
	ds "github.com/ipfs/go-datastore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateIO(t *testing.T) {
	db := ds.NewMapDatastore()
	abciStore := NewExecABCIStore(db)
	myState := stateFixture()
	require.NoError(t, abciStore.SaveState(t.Context(), myState))
	gotState, gotErr := abciStore.LoadState(t.Context())
	require.NoError(t, gotErr)
	assert.Equal(t, myState, gotState)
	exists, gotErr := db.Has(t.Context(), ds.NewKey("/abci/s"))
	require.NoError(t, gotErr)
	assert.True(t, exists)
}

func stateFixture() *state.State {
	val := anyValidator()
	return &state.State{
		ChainID:         "test-chain",
		InitialHeight:   1,
		LastBlockHeight: 10,
		LastBlockID:     types.BlockID{Hash: make([]byte, 32)},
		LastBlockTime:   time.Now().UTC(),
		LastResultsHash: make([]byte, 32),
		AppHash:         make([]byte, 32),
		LastValidators:  types.NewValidatorSet([]*types.Validator{val}),
		Validators:      types.NewValidatorSet([]*types.Validator{val}),
		NextValidators:  types.NewValidatorSet([]*types.Validator{val}),
		ConsensusParams: types.ConsensusParams{
			Block: types.BlockParams{
				MaxBytes: 104857600, // 100MiB
				MaxGas:   0,
			},
			Evidence:  types.EvidenceParams{},
			Validator: types.ValidatorParams{},
			Version:   types.VersionParams{},
			ABCI:      types.ABCIParams{},
		},
	}
}

func anyValidator() *types.Validator {
	pk := secp256k1.GenPrivKey()
	return &types.Validator{
		Address:          pk.PubKey().Address(),
		PubKey:           pk.PubKey(),
		VotingPower:      30,
		ProposerPriority: 3,
	}
}
