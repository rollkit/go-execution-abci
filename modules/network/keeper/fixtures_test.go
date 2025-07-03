package keeper

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/libp2p/go-libp2p/core/crypto"

	rollkittypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/modules/network/types"
)

func HeaderFixture(signer *ed25519.PrivKey, appHash []byte, mutators ...func(*rollkittypes.SignedHeader)) *rollkittypes.SignedHeader {
	header := rollkittypes.Header{
		BaseHeader: rollkittypes.BaseHeader{
			Height:  10,
			Time:    uint64(time.Now().UnixNano()),
			ChainID: "testing",
		},
		Version:         rollkittypes.Version{Block: 1, App: 1},
		ProposerAddress: signer.PubKey().Address(),
		AppHash:         appHash,
		DataHash:        []byte("data_hash"),
		ConsensusHash:   []byte("consensus_hash"),
		ValidatorHash:   []byte("validator_hash"),
	}
	signedHeader := &rollkittypes.SignedHeader{
		Header:    header,
		Signature: appHash,
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
		Timestamp:        time.Now().UTC(),
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
	// First try to find the validator by address
	validator, found := m.activeSet[addr.String()]
	if found {
		return validator, nil
	}

	//// If not found by address, try to find by public key address
	//addrStr := addr.String()
	//for valAddrStr, pubKey := range m.pubKeys {
	//	if pubKey.Address().String() == addrStr {
	//		validator, found = m.activeSet[valAddrStr]
	//		if found {
	//			return validator, nil
	//		}
	//	}
	//}

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
