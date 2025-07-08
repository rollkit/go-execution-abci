package keeper

import (
	"crypto/sha256"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	ds "github.com/ipfs/go-datastore"
	kt "github.com/ipfs/go-datastore/keytransform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rollnode "github.com/rollkit/rollkit/node"
	rstore "github.com/rollkit/rollkit/pkg/store"
	rollkittypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

func TestJoinAttesterSet(t *testing.T) {
	myValAddr := sdk.ValAddress("validator")

	type testCase struct {
		setup  func(t *testing.T, env testEnv)
		msg    *types.MsgJoinAttesterSet
		expErr error
		expSet bool
	}

	tests := map[string]testCase{
		"valid": {
			setup: func(t *testing.T, env testEnv) {
				validator := stakingtypes.Validator{
					OperatorAddress: myValAddr.String(),
					Status:          stakingtypes.Bonded,
				}
				err := env.SK.SetValidator(env.Ctx, validator)
				require.NoError(t, err, "failed to set validator")
			},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expSet: true,
		},
		"invalid_addr": {
			setup:  func(t *testing.T, env testEnv) {},
			msg:    &types.MsgJoinAttesterSet{Validator: "invalidAddr"},
			expErr: sdkerrors.ErrInvalidAddress,
		},
		"val not exists": {
			setup:  func(t *testing.T, env testEnv) {},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expErr: sdkerrors.ErrNotFound,
		},
		"val not bonded": {
			setup: func(t *testing.T, env testEnv) {
				validator := stakingtypes.Validator{
					OperatorAddress: myValAddr.String(),
					Status:          stakingtypes.Unbonded, // Validator is not bonded
				}
				err := env.SK.SetValidator(env.Ctx, validator)
				require.NoError(t, err, "failed to set validator")
			},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"already set": {
			setup: func(t *testing.T, env testEnv) {
				validator := stakingtypes.Validator{
					OperatorAddress: myValAddr.String(),
					Status:          stakingtypes.Bonded,
				}
				require.NoError(t, env.SK.SetValidator(env.Ctx, validator))
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, myValAddr.String()))
			},
			msg:    &types.MsgJoinAttesterSet{Validator: myValAddr.String()},
			expErr: sdkerrors.ErrInvalidRequest,
			expSet: true,
		},
	}
	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			// Setup test environment
			env := setupTestEnv(t, 10)

			// Apply test-specific setup
			spec.setup(t, env)

			// when
			rsp, err := env.Server.JoinAttesterSet(env.Ctx, spec.msg)

			// then
			if spec.expErr != nil {
				require.ErrorIs(t, err, spec.expErr)
				require.Nil(t, rsp)
				exists, gotErr := env.Keeper.AttesterSet.Has(env.Ctx, spec.msg.Validator)
				require.NoError(t, gotErr)
				assert.Equal(t, exists, spec.expSet)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rsp)
			exists, gotErr := env.Keeper.AttesterSet.Has(env.Ctx, spec.msg.Validator)
			require.NoError(t, gotErr)
			assert.True(t, exists)
		})
	}
}

func TestAttest(t *testing.T) {
	const epochLength = 10
	var (
		myHash     = sha256.Sum256([]byte("app_hash"))
		myAppHash  = myHash[:]
		voteSigner = ed25519.GenPrivKey()
		valAddrStr = sdk.ValAddress(voteSigner.PubKey().Address()).String()
	)

	// Setup test environment with block store
	env := setupTestEnv(t, 2*epochLength)

	// Set up validator
	validator, err := stakingtypes.NewValidator(valAddrStr, voteSigner.PubKey(), stakingtypes.Description{})
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	require.NoError(t, env.SK.SetValidator(env.Ctx, validator))

	// Save block data
	signedHeader := HeaderFixture(voteSigner, myAppHash)
	data := &rollkittypes.Data{Txs: rollkittypes.Txs{}}
	var signature rollkittypes.Signature
	require.NoError(t, env.BlockStore.SaveBlockData(env.Ctx, signedHeader, data, &signature))

	var (
		validVote   = VoteFixture(myAppHash, voteSigner)
		validVoteBz = must(proto.Marshal(validVote))
	)
	parentCtx := env.Ctx

	specs := map[string]struct {
		setup  func(t *testing.T, env testEnv) sdk.Context
		msg    func(t *testing.T) *types.MsgAttest
		expErr error
	}{
		"valid attestation": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, valAddrStr))
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx
			},
			msg: func(t *testing.T) *types.MsgAttest {
				return &types.MsgAttest{Validator: valAddrStr, Height: epochLength, Vote: validVoteBz}
			},
		},
		"invalid vote content": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, valAddrStr))
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx
			},
			msg: func(t *testing.T) *types.MsgAttest {
				return &types.MsgAttest{Validator: valAddrStr, Height: epochLength, Vote: []byte("not a valid proto vote")}
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"validator not in attester set": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx
			},
			msg: func(t *testing.T) *types.MsgAttest {
				return &types.MsgAttest{Validator: valAddrStr, Height: epochLength, Vote: validVoteBz}
			},
			expErr: sdkerrors.ErrUnauthorized,
		},
		"invalid signature": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, valAddrStr))
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx
			},
			msg: func(t *testing.T) *types.MsgAttest {
				invalidVote := VoteFixture(myAppHash, voteSigner, func(vote *cmtproto.Vote) {
					vote.Signature = []byte("invalid signature")
				})
				return &types.MsgAttest{Validator: valAddrStr, Height: epochLength, Vote: must(proto.Marshal(invalidVote))}
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"not a checkpoint height": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, valAddrStr))
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx
			},
			msg: func(t *testing.T) *types.MsgAttest {
				return &types.MsgAttest{Validator: valAddrStr, Height: epochLength + 1, Vote: validVoteBz}
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"vote window expired": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, valAddrStr))
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx.WithBlockHeight(2*epochLength + 1)
			},
			msg: func(t *testing.T) *types.MsgAttest {
				return &types.MsgAttest{Validator: valAddrStr, Height: epochLength, Vote: validVoteBz}
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"voting for a future epoch": {
			setup: func(t *testing.T, env testEnv) sdk.Context {
				require.NoError(t, env.Keeper.SetAttesterSetMember(env.Ctx, valAddrStr))
				require.NoError(t, env.Keeper.SetValidatorIndex(env.Ctx, valAddrStr, 0, 100))
				return env.Ctx.WithBlockHeight(2 * epochLength)
			},
			msg: func(t *testing.T) *types.MsgAttest {
				return &types.MsgAttest{Validator: valAddrStr, Height: 3 * epochLength, Vote: validVoteBz}
			},
			expErr: sdkerrors.ErrInvalidRequest,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			// Create a new environment for each test case with a cached context
			testEnv := env
			testEnv.Ctx, _ = parentCtx.CacheContext()
			ctx := spec.setup(t, testEnv)

			// when
			srcMsg := spec.msg(t)
			gotRsp, gotErr := testEnv.Server.Attest(ctx, srcMsg)

			// then
			if spec.expErr != nil {
				require.Error(t, gotErr)
				require.ErrorIs(t, gotErr, spec.expErr)
				// and ensure the signature is not stored
				_, err := testEnv.Keeper.GetVote(ctx, srcMsg.Height, valAddrStr)
				assert.ErrorIs(t, err, collections.ErrNotFound)
				return
			}

			require.NoError(t, gotErr)
			require.NotNil(t, gotRsp)

			// and attestation marked
			bitmap, gotErr := testEnv.Keeper.GetAttestationBitmap(ctx, srcMsg.Height)
			require.NoError(t, gotErr)
			require.NotEmpty(t, bitmap)
			require.Equal(t, byte(1), bitmap[0])

			// and the signature was stored properly
			gotSig, err := testEnv.Keeper.GetVote(ctx, srcMsg.Height, valAddrStr)
			require.NoError(t, err)
			var vote cmtproto.Vote
			require.NoError(t, proto.Unmarshal(srcMsg.Vote, &vote))
			assert.Equal(t, vote.Signature, gotSig)
		})
	}
}

func TestVerifyVote(t *testing.T) {
	var (
		myHash                = sha256.Sum256([]byte("app_hash"))
		myAppHash             = myHash[:]
		validatorPrivKey      = ed25519.GenPrivKey()
		valAddrStr            = sdk.ValAddress(validatorPrivKey.PubKey().Address()).String()
		otherValidatorPrivKey = ed25519.GenPrivKey()
	)

	// Setup test environment with block store
	env := setupTestEnv(t, 10)

	// Set up validator
	validator, err := stakingtypes.NewValidator(valAddrStr, validatorPrivKey.PubKey(), stakingtypes.Description{})
	require.NoError(t, err)
	validator.Status = stakingtypes.Bonded
	require.NoError(t, env.SK.SetValidator(env.Ctx, validator))

	// Save block data
	header := HeaderFixture(validatorPrivKey, myAppHash)
	var signature rollkittypes.Signature
	require.NoError(t, env.BlockStore.SaveBlockData(env.Ctx, header, &rollkittypes.Data{}, &signature))

	parentCtx := env.Ctx

	testCases := map[string]struct {
		voteFn func(t *testing.T) *cmtproto.Vote
		sender string
		expErr error
	}{
		"valid vote": {
			voteFn: func(t *testing.T) *cmtproto.Vote {
				return VoteFixture(myAppHash, validatorPrivKey)
			},
			sender: valAddrStr,
		},
		"block data not found": {
			voteFn: func(t *testing.T) *cmtproto.Vote {
				return VoteFixture(myAppHash, validatorPrivKey, func(vote *cmtproto.Vote) {
					vote.Height++
					vote.Signature = must(validatorPrivKey.Sign(cmttypes.VoteSignBytes("testing", vote)))
				})
			},
			sender: valAddrStr,
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"validator not found": {
			voteFn: func(t *testing.T) *cmtproto.Vote {
				return VoteFixture(myAppHash, ed25519.GenPrivKey())
			},
			expErr: sdkerrors.ErrUnauthorized,
			sender: sdk.ValAddress(otherValidatorPrivKey.PubKey().Address()).String(),
		},
		"invalid vote signature": {
			voteFn: func(t *testing.T) *cmtproto.Vote {
				return VoteFixture(myAppHash, validatorPrivKey, func(vote *cmtproto.Vote) {
					vote.Signature = []byte("invalid signature")
				})
			},
			sender: valAddrStr,
			expErr: sdkerrors.ErrInvalidRequest,
		},
		"invalid sender": {
			voteFn: func(t *testing.T) *cmtproto.Vote {
				return VoteFixture(myAppHash, validatorPrivKey)
			},
			sender: sdk.ValAddress(otherValidatorPrivKey.PubKey().Address()).String(),
			expErr: sdkerrors.ErrUnauthorized,
		},
	}
	for name, spec := range testCases {
		t.Run(name, func(t *testing.T) {
			ctx, _ := parentCtx.CacheContext()

			// when
			vote, err := env.Server.verifyVote(ctx, &types.MsgAttest{
				Height:    10,
				Validator: spec.sender,
				Vote:      must(proto.Marshal(spec.voteFn(t))),
			})

			// then
			if spec.expErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, spec.expErr)
				require.Nil(t, vote)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, vote)
		})
	}
}

// testEnv contains all the common components needed for testing
type testEnv struct {
	Ctx        sdk.Context
	Keeper     Keeper
	Server     msgServer
	SK         MockStakingKeeper
	BlockStore rstore.Store
}

func setupTestEnv(t *testing.T, height int64) testEnv {
	t.Helper()
	// Set up codec and store
	cdc := moduletestutil.MakeTestEncodingConfig().Codec
	keys := storetypes.NewKVStoreKeys(types.StoreKey)
	cms := integration.CreateMultiStore(keys, log.NewTestLogger(t))

	sk := NewMockStakingKeeper()
	rollkitPrefixStore := kt.Wrap(ds.NewMapDatastore(), &kt.PrefixTransform{
		Prefix: ds.NewKey(rollnode.RollkitPrefix),
	})
	bs := rstore.New(rollkitPrefixStore)

	authority := authtypes.NewModuleAddress("gov")
	keeper := NewKeeper(cdc, runtime.NewKVStoreService(keys[types.StoreKey]), &sk, nil, nil, authority.String())

	ctx := sdk.NewContext(cms, cmtproto.Header{
		ChainID: "testing",
		Time:    time.Now().UTC(),
		Height:  height,
	}, false, log.NewTestLogger(t)).WithContext(t.Context())

	server := msgServer{Keeper: keeper}

	params := types.DefaultParams()
	params.EpochLength = 10 // test default
	require.NoError(t, keeper.SetParams(ctx, params))

	return testEnv{
		Ctx:        ctx,
		Keeper:     keeper,
		Server:     server,
		SK:         sk,
		BlockStore: bs,
	}
}

func must[T any](r T, err error) T {
	if err != nil {
		panic(err)
	}
	return r
}
