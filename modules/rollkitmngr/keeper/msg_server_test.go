package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/rollkit/go-execution-abci/modules/rollkitmngr/types"
)

// helper to create a dummy consensus pubkey Any
func dummyConsensusPubkeyAny() *codectypes.Any {
	pub, _ := codectypes.NewAnyWithValue(ed25519.GenPrivKey().PubKey())
	return pub
}

func TestMigrateToRollkit_AuthorityError(t *testing.T) {
	s := initFixture(t)
	msg := &types.MsgMigrateToRollkit{Authority: "bad"}
	_, err := s.msgServer.MigrateToRollkit(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority")
}

func TestMigrateToRollkit_BlockHeightError(t *testing.T) {
	s := initFixture(t)
	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	msg := &types.MsgMigrateToRollkit{Authority: auth, BlockHeight: 1}
	s.ctx = s.ctx.WithBlockHeight(2)
	_, err := s.msgServer.MigrateToRollkit(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "block height")
}

func TestMigrateToRollkit_Success(t *testing.T) {
	s := initFixture(t)
	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	msg := &types.MsgMigrateToRollkit{Authority: auth, BlockHeight: 10}
	resp, err := s.msgServer.MigrateToRollkit(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestEditAttesters_AuthorityError(t *testing.T) {
	s := initFixture(t)
	msg := &types.MsgEditAttesters{Authority: "bad"}
	_, err := s.msgServer.EditAttesters(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid authority")
}

func TestEditAttesters_EmptyFields(t *testing.T) {
	s := initFixture(t)
	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	msg := &types.MsgEditAttesters{
		Authority: auth,
		Attesters: []types.Attester{{Name: "", ConsensusPubkey: nil}},
	}
	_, err := s.msgServer.EditAttesters(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestEditAttesters_DuplicateKey(t *testing.T) {
	s := initFixture(t)
	pubkey := dummyConsensusPubkeyAny()
	s.stakingKeeper.vals = []stakingtypes.Validator{
		{ConsensusPubkey: pubkey},
	}

	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	msg := &types.MsgEditAttesters{
		Authority: auth,
		Attesters: []types.Attester{
			{Name: "a", ConsensusPubkey: pubkey},
			{Name: "b", ConsensusPubkey: pubkey},
		},
	}
	_, err := s.msgServer.EditAttesters(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate attester consensus public key")
}

func TestEditAttesters_InvalidPubKeyType(t *testing.T) {
	s := initFixture(t)
	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	invalidAny := &codectypes.Any{TypeUrl: "/invalid", Value: []byte("bad")}
	msg := &types.MsgEditAttesters{
		Authority: auth,
		Attesters: []types.Attester{{Name: "a", ConsensusPubkey: invalidAny}},
	}
	_, err := s.msgServer.EditAttesters(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid consensus public key type")
}

func TestEditAttesters_ValidatorNotFound(t *testing.T) {
	s := initFixture(t)
	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	pubkey := dummyConsensusPubkeyAny()
	msg := &types.MsgEditAttesters{
		Authority: auth,
		Attesters: []types.Attester{{Name: "a", ConsensusPubkey: pubkey}},
	}
	_, err := s.msgServer.EditAttesters(s.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get validator by consensus address")
}

func TestEditAttesters_Success(t *testing.T) {
	s := initFixture(t)
	auth := sdk.AccAddress(address.Module(types.ModuleName)).String()
	pubkey := dummyConsensusPubkeyAny()
	s.stakingKeeper.vals = []stakingtypes.Validator{
		{
			Description:     stakingtypes.NewDescription("a", "", "", "", ""),
			ConsensusPubkey: pubkey,
		},
	}

	msg := &types.MsgEditAttesters{
		Authority: auth,
		Attesters: []types.Attester{{Name: "a", ConsensusPubkey: pubkey}},
	}
	resp, err := s.msgServer.EditAttesters(s.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
