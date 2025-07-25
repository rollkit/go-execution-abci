package keeper

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/evstack/ev-abci/modules/network/types"
)

func TestJoinAttesterSet(t *testing.T) {
	myValAddr := sdk.ValAddress("validator4")

	type testCase struct {
		setup  func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper)
		msg    *types.MsgJoinAttesterSet
		expErr error
		expSet bool
	}

	tests := map[string]testCase{
		"valid": {
			setup: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper) {
				validator := stakingtypes.Validator{
					OperatorAddress: myValAddr.String(),
					Status:          stakingtypes.Bonded,
				}
				err := sk.SetValidator(ctx, validator)
				require.NoError(t, err, "failed to set validator")
			},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expSet: true,
		},
		"invalid_addr": {
			setup:  func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper) {},
			msg:    &types.MsgJoinAttesterSet{Validator: "invalidAddr"},
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"val not exists": {
			setup:  func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper) {},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expErr: sdkerrors.ErrNotFound,
		},
		"val not bonded": {
			setup: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper) {
				validator := stakingtypes.Validator{
					OperatorAddress: myValAddr.String(),
					Status:          stakingtypes.Unbonded, // Validator is not bonded
				}
				err := sk.SetValidator(ctx, validator)
				require.NoError(t, err, "failed to set validator")
			},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"already set": {
			setup: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper) {
				validator := stakingtypes.Validator{
					OperatorAddress: myValAddr.String(),
					Status:          stakingtypes.Bonded,
				}
				require.NoError(t, sk.SetValidator(ctx, validator))
				require.NoError(t, keeper.SetAttesterSetMember(ctx, myValAddr.String()))
			},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expErr: sdkerrors.ErrInvalidRequest,
			expSet: true,
		},
		//{
		//	name: "failed to set attester set member",
		//	setup: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper) {
		//		validatorAddr := sdk.ValAddress([]byte("validator5"))
		//		validator := stakingtypes.Validator{
		//			OperatorAddress: validatorAddr.String(),
		//			Status:          stakingtypes.Bonded,
		//		}
		//		err := sk.SetValidator(ctx, validator)
		//		require.NoError(t, err, "failed to set validator")
		//		keeper.forceError = true
		//	},
		//	msg: &types.MsgJoinAttesterSet{
		//		Validator: "validator5",
		//	},
		//	expErr:  sdkerrors.ErrInternal,
		//	expectResponse: false,
		//},
	}

	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			sk := NewMockStakingKeeper()

			cdc := moduletestutil.MakeTestEncodingConfig().Codec

			keys := storetypes.NewKVStoreKeys(types.StoreKey)

			logger := log.NewTestLogger(t)
			cms := integration.CreateMultiStore(keys, logger)
			authority := authtypes.NewModuleAddress("gov")
			keeper := NewKeeper(cdc, runtime.NewKVStoreService(keys[types.StoreKey]), sk, nil, nil, authority.String())
			server := msgServer{Keeper: keeper}
			ctx := sdk.NewContext(cms, cmtproto.Header{ChainID: "test-chain", Time: time.Now().UTC(), Height: 10}, false, logger).
				WithContext(t.Context())

			spec.setup(t, ctx, &keeper, &sk)

			// when
			rsp, err := server.JoinAttesterSet(ctx, spec.msg)
			// then
			if spec.expErr != nil {
				require.ErrorIs(t, err, spec.expErr)
				require.Nil(t, rsp)
				exists, gotErr := keeper.AttesterSet.Has(ctx, spec.msg.Validator)
				require.NoError(t, gotErr)
				assert.Equal(t, exists, spec.expSet)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rsp)
			exists, gotErr := keeper.AttesterSet.Has(ctx, spec.msg.Validator)
			require.NoError(t, gotErr)
			assert.True(t, exists)
		})
	}
}

var _ types.StakingKeeper = &MockStakingKeeper{}

type MockStakingKeeper struct {
	activeSet map[string]stakingtypes.Validator
}

func NewMockStakingKeeper() MockStakingKeeper {
	return MockStakingKeeper{
		activeSet: make(map[string]stakingtypes.Validator),
	}
}

func (m *MockStakingKeeper) SetValidator(ctx context.Context, validator stakingtypes.Validator) error {
	m.activeSet[validator.GetOperator()] = validator
	return nil
}

func (m MockStakingKeeper) GetAllValidators(ctx context.Context) (validators []stakingtypes.Validator, err error) {
	return slices.SortedFunc(maps.Values(m.activeSet), func(v1 stakingtypes.Validator, v2 stakingtypes.Validator) int {
		return strings.Compare(v1.OperatorAddress, v2.OperatorAddress)
	}), nil
}

func (m MockStakingKeeper) GetValidator(ctx context.Context, addr sdk.ValAddress) (validator stakingtypes.Validator, err error) {
	validator, found := m.activeSet[addr.String()]
	if !found {
		return validator, sdkerrors.ErrNotFound
	}
	return validator, nil
}

func (m MockStakingKeeper) GetLastValidators(ctx context.Context) (validators []stakingtypes.Validator, err error) {
	for _, validator := range m.activeSet {
		if validator.IsBonded() { // Assuming IsBonded() identifies if a validator is in the last validators
			validators = append(validators, validator)
		}
	}
	return
}

func (m MockStakingKeeper) GetLastTotalPower(ctx context.Context) (math.Int, error) {
	return math.NewInt(int64(len(m.activeSet))), nil
}
