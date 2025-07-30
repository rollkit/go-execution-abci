package keeper_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/evstack/ev-abci/modules/rollkitmngr"
	"github.com/evstack/ev-abci/modules/rollkitmngr/keeper"
	"github.com/evstack/ev-abci/modules/rollkitmngr/types"
)

// mockStakingKeeper is a minimal mock for stakingKeeper used in Attesters tests.
type mockStakingKeeper struct {
	vals []stakingtypes.Validator
	err  error
}

func (m *mockStakingKeeper) GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error) {
	for _, val := range m.vals {
		if valBz, err := val.GetConsAddr(); err == nil && bytes.Equal(valBz, consAddr) {
			return val, nil
		}
	}
	return stakingtypes.Validator{}, stakingtypes.ErrNoValidatorFound
}

func (m *mockStakingKeeper) GetLastValidators(context.Context) ([]stakingtypes.Validator, error) {
	return m.vals, m.err
}

func (m *mockStakingKeeper) IterateBondedValidatorsByPower(ctx context.Context, cb func(index int64, validator stakingtypes.ValidatorI) (stop bool)) error {
	for i, val := range m.vals {
		if cb(int64(i), val) {
			break
		}
	}
	return nil
}

type fixture struct {
	ctx        sdk.Context
	kvStoreKey *storetypes.KVStoreKey

	stakingKeeper *mockStakingKeeper
	keeper        keeper.Keeper
	queryServer   types.QueryServer
	msgServer     types.MsgServer
}

func initFixture(tb testing.TB) *fixture {
	tb.Helper()

	stakingKeeper := &mockStakingKeeper{}
	key := storetypes.NewKVStoreKey(types.ModuleName)
	storeService := runtime.NewKVStoreService(key)
	encCfg := moduletestutil.MakeTestEncodingConfig(rollkitmngr.AppModuleBasic{})
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("transient"))

	k := keeper.NewKeeper(
		encCfg.Codec,
		storeService,
		addressCodec,
		stakingKeeper,
		nil,
		sdk.AccAddress(address.Module(types.ModuleName)).String(),
	)

	return &fixture{
		ctx:           ctx,
		kvStoreKey:    key,
		stakingKeeper: stakingKeeper,
		keeper:        k,
		queryServer:   keeper.NewQueryServer(k),
		msgServer:     keeper.NewMsgServerImpl(k),
	}
}

func TestAttesters_Success(t *testing.T) {
	s := initFixture(t)

	// attesters returns the current attesters
	s.stakingKeeper.vals = []stakingtypes.Validator{{Description: stakingtypes.NewDescription("foo", "", "", "", "")}}
	s.stakingKeeper.err = nil

	resp, err := s.queryServer.Attesters(s.ctx, &types.QueryAttestersRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Attesters, 1)
	require.Equal(t, "foo", resp.Attesters[0].Name)
}

func TestAttesters_Error(t *testing.T) {
	s := initFixture(t)

	s.stakingKeeper.err = errors.New("fail")
	resp, err := s.queryServer.Attesters(s.ctx, &types.QueryAttestersRequest{})
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestIsMigrating(t *testing.T) {
	s := initFixture(t)

	// set up migration
	require.NoError(t, s.keeper.Migration.Set(s.ctx, types.RollkitMigration{
		BlockHeight: 1,
		Sequencer:   types.Sequencer{Name: "foo"},
	}))

	s.ctx = s.ctx.WithBlockHeight(1)
	resp, err := s.queryServer.IsMigrating(s.ctx, &types.QueryIsMigratingRequest{})
	require.NoError(t, err)
	require.True(t, resp.IsMigrating)
	require.Equal(t, uint64(1), resp.StartBlockHeight)
	require.Equal(t, uint64(2), resp.EndBlockHeight)
}

func TestIsMigrating_IBCEnabled(t *testing.T) {
	stakingKeeper := &mockStakingKeeper{}
	key := storetypes.NewKVStoreKey(types.ModuleName)
	storeService := runtime.NewKVStoreService(key)
	encCfg := moduletestutil.MakeTestEncodingConfig(rollkitmngr.AppModuleBasic{})
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	ibcKey := storetypes.NewKVStoreKey("ibc")
	ctx := testutil.DefaultContextWithKeys(map[string]*storetypes.KVStoreKey{
		types.ModuleName: key,
		"ibc":            ibcKey,
	}, nil, nil)

	k := keeper.NewKeeper(
		encCfg.Codec,
		storeService,
		addressCodec,
		stakingKeeper,
		func() *storetypes.KVStoreKey { return key },
		sdk.AccAddress(address.Module(types.ModuleName)).String(),
	)

	// set up migration
	require.NoError(t, k.Migration.Set(ctx, types.RollkitMigration{
		BlockHeight: 1,
		Sequencer:   types.Sequencer{Name: "foo"},
	}))

	ctx = ctx.WithBlockHeight(1)
	resp, err := keeper.NewQueryServer(k).IsMigrating(ctx, &types.QueryIsMigratingRequest{})
	require.NoError(t, err)
	require.True(t, resp.IsMigrating)
	require.Equal(t, uint64(1), resp.StartBlockHeight)
	require.Equal(t, 1+keeper.IBCSmoothingFactor, resp.EndBlockHeight)
}

func TestSequencer_Migrating(t *testing.T) {
	s := initFixture(t)

	// set up migration
	require.NoError(t, s.keeper.Migration.Set(s.ctx, types.RollkitMigration{
		BlockHeight: 1,
		Sequencer:   types.Sequencer{Name: "foo"},
	}))

	resp, err := s.queryServer.Sequencer(s.ctx, &types.QuerySequencerRequest{})
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestSequencer_Success(t *testing.T) {
	s := initFixture(t)

	// sequencer returns the current sequencer
	seq := types.Sequencer{Name: "foo"}
	require.NoError(t, s.keeper.Sequencer.Set(s.ctx, seq))

	resp, err := s.queryServer.Sequencer(s.ctx, &types.QuerySequencerRequest{})
	require.NoError(t, err)
	require.Equal(t, seq, resp.Sequencer)
}

func TestSequencer_Error(t *testing.T) {
	s := initFixture(t)

	resp, err := s.queryServer.Sequencer(s.ctx, &types.QuerySequencerRequest{})
	require.Error(t, err)
	require.Nil(t, resp)
}
