package keeper

import (
	"context"
	"crypto/sha256"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/libp2p/go-libp2p/core/crypto"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	"github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	kt "github.com/ipfs/go-datastore/keytransform"
	rollnode "github.com/rollkit/rollkit/node"
	rstore "github.com/rollkit/rollkit/pkg/store"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	rollkittypes "github.com/rollkit/rollkit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

func TestJoinAttesterSet(t *testing.T) {
	myValAddr := sdk.ValAddress("validator")

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
			keeper := NewKeeper(cdc, runtime.NewKVStoreService(keys[types.StoreKey]), sk, nil, nil, nil, authority.String())
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

func TestAttest(t *testing.T) {
	var (
		myHash      = sha256.Sum256([]byte("app_hash"))
		myAppHash   = myHash[:]
		voteSigner  = ed25519.GenPrivKey()
		valAddrStr  = sdk.ValAddress(voteSigner.PubKey().Address()).String()
		validVote   = VoteFixture(myAppHash, voteSigner)
		validVoteBz = must(proto.Marshal(validVote))
	)

	specs := map[string]struct {
		setup  func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper, bs rstore.Store)
		msg    *types.MsgAttest
		expErr error
	}{
		"valid attestation": {
			setup: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper, bs rstore.Store) {
				validator, err := stakingtypes.NewValidator(valAddrStr, voteSigner.PubKey(), stakingtypes.Description{})
				validator.Status = stakingtypes.Bonded
				require.NoError(t, err)
				require.NoError(t, sk.SetValidator(ctx, validator))
				require.NoError(t, keeper.SetAttesterSetMember(ctx, valAddrStr))
				require.NoError(t, keeper.SetValidatorIndex(ctx, valAddrStr, 0, 100))
				sk.SetValidatorPubKey(valAddrStr, voteSigner.PubKey())

				// Set up params for checkpoint height
				params := types.DefaultParams()
				params.EpochLength = 10
				require.NoError(t, keeper.SetParams(ctx, params))

				signedHeader := HeaderFixture(voteSigner, myAppHash)
				data := &rollkittypes.Data{Txs: rollkittypes.Txs{}}
				var signature rollkittypes.Signature
				require.NoError(t, bs.SaveBlockData(ctx, signedHeader, data, &signature))
			},
			msg: &types.MsgAttest{
				Validator: valAddrStr,
				Height:    10,
				Vote:      validVoteBz,
			},
		},
		//"validator not in attester set": {
		//	setupMock: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper, bs rstore.Store) {
		//		// Set up validator but don't add to attester set
		//		valAddr := sdk.ValAddress("validator1")
		//		validator := stakingtypes.Validator{
		//			OperatorAddress: valAddr.String(),
		//			Status:          stakingtypes.Bonded,
		//		}
		//		require.NoError(t, sk.SetValidator(ctx, validator))
		//
		//		// Set up params for checkpoint height
		//		params := types.DefaultParams()
		//		params.EpochLength = 10 // Make height 10 a checkpoint
		//		require.NoError(t, keeper.SetParams(ctx, params))
		//	},
		//	msg: &types.MsgAttest{
		//		Validator: sdk.ValAddress("validator1").String(),
		//		Height:    10,
		//		Vote:      []byte("mock_vote"),
		//	},
		//	expErr: sdkerrors.ErrUnauthorized,
		//},
		//"not a checkpoint height": {
		//	setupMock: func(t *testing.T, ctx sdk.Context, keeper *Keeper, sk *MockStakingKeeper, bs rstore.Store) {
		//		// Set up validator and add to attester set
		//		valAddr := sdk.ValAddress("validator1")
		//		validator := stakingtypes.Validator{
		//			OperatorAddress: valAddr.String(),
		//			Status:          stakingtypes.Bonded,
		//		}
		//		require.NoError(t, sk.SetValidator(ctx, validator))
		//		require.NoError(t, keeper.SetAttesterSetMember(ctx, valAddr.String()))
		//
		//		// Set up params for checkpoint height
		//		params := types.DefaultParams()
		//		params.EpochLength = 10 // Make height 10 a checkpoint
		//		require.NoError(t, keeper.SetParams(ctx, params))
		//	},
		//	msg: &types.MsgAttest{
		//		Validator: sdk.ValAddress("validator1").String(),
		//		Height:    11, // Not a checkpoint height
		//		Vote:      []byte("mock_vote"),
		//	},
		//	expErr: sdkerrors.ErrInvalidRequest,
		//},
	}

	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			rollkitPrefixStore := kt.Wrap(ds.NewMapDatastore(), &kt.PrefixTransform{
				Prefix: ds.NewKey(rollnode.RollkitPrefix),
			})
			bs := rstore.New(rollkitPrefixStore)
			// Set up codec and store
			cdc := moduletestutil.MakeTestEncodingConfig().Codec
			keys := storetypes.NewKVStoreKeys(types.StoreKey)

			// Set up context and keeper
			logger := log.NewTestLogger(t)
			cms := integration.CreateMultiStore(keys, logger)
			authority := authtypes.NewModuleAddress("gov")
			sk := NewMockStakingKeeper()
			keeper := NewKeeper(cdc, runtime.NewKVStoreService(keys[types.StoreKey]), &sk, nil, nil, bs, authority.String())
			server := NewMsgServerImpl(keeper)

			ctx := sdk.NewContext(cms, cmtproto.Header{ChainID: "test-chain", Time: time.Now().UTC(), Height: 20}, false, logger).
				WithContext(t.Context())

			// Run setup
			spec.setup(t, ctx, &keeper, &sk, bs)

			// when
			gotRsp, gotErr := server.Attest(ctx, spec.msg)

			// then
			if spec.expErr != nil {
				require.Error(t, gotErr)
				require.ErrorIs(t, gotErr, spec.expErr)
				// and ensure the signature is not stored
				_, err := keeper.GetSignature(ctx, spec.msg.Height, valAddrStr)
				assert.ErrorIs(t, err, collections.ErrNotFound)
				return
			}

			require.NoError(t, gotErr)
			require.NotNil(t, gotRsp)

			// and attestation marked
			bitmap, gotErr := keeper.GetAttestationBitmap(ctx, spec.msg.Height)
			require.NoError(t, gotErr)
			require.NotEmpty(t, bitmap)
			require.Equal(t, byte(1), bitmap[0])

			// and the signature was stored properly
			gotSig, err := keeper.GetSignature(ctx, spec.msg.Height, valAddrStr)
			require.NoError(t, err)
			var vote cmtproto.Vote
			require.NoError(t, proto.Unmarshal(spec.msg.Vote, &vote))
			assert.Equal(t, vote.Signature, gotSig)
		})
	}
}

func HeaderFixture(signer *ed25519.PrivKey, myAppHash []byte, mutators ...func(*rollkittypes.SignedHeader)) *rollkittypes.SignedHeader {
	header := rollkittypes.Header{
		BaseHeader: rollkittypes.BaseHeader{
			Height:  10,
			Time:    uint64(time.Now().UnixNano()),
			ChainID: "testing",
		},
		Version:         rollkittypes.Version{Block: 1, App: 1},
		ProposerAddress: signer.PubKey().Address(),
		AppHash:         myAppHash,
		DataHash:        []byte("data_hash"),
		ConsensusHash:   []byte("consensus_hash"),
		ValidatorHash:   []byte("validator_hash"),
	}
	signedHeader := &rollkittypes.SignedHeader{
		Header:    header,
		Signature: myAppHash,
		Signer:    rollkittypes.Signer{PubKey: must(crypto.UnmarshalEd25519PublicKey(signer.PubKey().Bytes()))},
	}
	for _, m := range mutators {
		m(signedHeader)
	}
	return signedHeader
}

func VoteFixture(myAppHash []byte, voteSigner *ed25519.PrivKey, mutators ...func(vote *cmtproto.Vote)) *cmtproto.Vote {
	const chainID = "testing"

	vote := &cmtproto.Vote{
		Type:             cmtproto.PrecommitType,
		Height:           10,
		Round:            0,
		BlockID:          cmtproto.BlockID{Hash: myAppHash, PartSetHeader: cmtproto.PartSetHeader{Total: 1, Hash: myAppHash}},
		Timestamp:        time.Now(),
		ValidatorAddress: voteSigner.PubKey().Address(),
		ValidatorIndex:   0,
	}
	vote.Signature = must(voteSigner.Sign(cmttypes.VoteSignBytes(chainID, vote)))

	for _, m := range mutators {
		m(vote)
	}
	return vote
}

var _ types.StakingKeeper = &MockStakingKeeper{}

type MockStakingKeeper struct {
	activeSet map[string]stakingtypes.Validator
	pubKeys   map[string]cryptotypes.PubKey
}

func NewMockStakingKeeper() MockStakingKeeper {
	return MockStakingKeeper{
		activeSet: make(map[string]stakingtypes.Validator),
		pubKeys:   make(map[string]cryptotypes.PubKey),
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
	// First try to find the validator by address
	validator, found := m.activeSet[addr.String()]
	if found {
		return validator, nil
	}

	// If not found by address, try to find by public key address
	addrStr := addr.String()
	for valAddrStr, pubKey := range m.pubKeys {
		if pubKey.Address().String() == addrStr {
			validator, found = m.activeSet[valAddrStr]
			if found {
				return validator, nil
			}
		}
	}

	return validator, sdkerrors.ErrNotFound
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

func (m *MockStakingKeeper) SetValidatorPubKey(valAddrStr string, key cryptotypes.PubKey) {
	m.pubKeys[valAddrStr] = key
}

func must[T any](r T, err error) T {
	if err != nil {
		panic(err)
	}
	return r
}
