package cometcompat

import (
	"bytes"
	"crypto/rand"
	"testing"

	tmcryptoed25519 "github.com/cometbft/cometbft/crypto/ed25519"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/crypto/pb"
	rollkittypes "github.com/rollkit/rollkit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPubKey is a mock implementation of crypto.PubKey for testing.
type mockPubKey struct {
	crypto.PubKey
	rawBytes []byte
	keyType  pb.KeyType
}

func (m *mockPubKey) Raw() ([]byte, error) {
	return m.rawBytes, nil
}

func (m *mockPubKey) Type() pb.KeyType {
	return m.keyType
}

func TestValidatorsHasher(t *testing.T) {
	t.Run("empty validator set", func(t *testing.T) {
		proposerAddress := []byte("proposer")
		hash, err := ValidatorsHasher([]crypto.PubKey{}, proposerAddress)
		require.NoError(t, err)

		expectedHash := tmtypes.NewValidatorSet([]*tmtypes.Validator{}).Hash()
		assert.Equal(t, rollkittypes.Hash(expectedHash), hash)
	})

	t.Run("single validator, proposer found", func(t *testing.T) {
		_, pubKey, err := crypto.GenerateEd25519Key(rand.Reader)
		require.NoError(t, err)

		rawPubKey, err := pubKey.Raw()
		require.NoError(t, err)
		cometPubKey := tmcryptoed25519.PubKey(rawPubKey)
		tmVal := tmtypes.NewValidator(cometPubKey, 1)
		proposerAddress := tmVal.Address.Bytes()

		hash, err := ValidatorsHasher([]crypto.PubKey{pubKey}, proposerAddress)
		require.NoError(t, err)

		expectedValidatorSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{tmVal})
		expectedHash := expectedValidatorSet.Hash()

		assert.Equal(t, rollkittypes.Hash(expectedHash), hash)
	})

	t.Run("multiple validators, proposer found, order independent", func(t *testing.T) {
		var pubKeys1 []crypto.PubKey
		var tmValidators []*tmtypes.Validator

		for i := 0; i < 3; i++ {
			_, pubKey, err := crypto.GenerateEd25519Key(rand.Reader)
			require.NoError(t, err)
			pubKeys1 = append(pubKeys1, pubKey)

			rawPubKey, err := pubKey.Raw()
			require.NoError(t, err)
			cometPubKey := tmcryptoed25519.PubKey(rawPubKey)
			tmValidators = append(tmValidators, tmtypes.NewValidator(cometPubKey, 1))
		}

		// Proposer is the second validator
		proposerAddress := tmValidators[1].Address.Bytes()

		// Reverse order for pubKeys2
		pubKeys2 := []crypto.PubKey{pubKeys1[2], pubKeys1[1], pubKeys1[0]}

		hash1, err1 := ValidatorsHasher(pubKeys1, proposerAddress)
		require.NoError(t, err1)

		hash2, err2 := ValidatorsHasher(pubKeys2, proposerAddress)
		require.NoError(t, err2)

		assert.True(t, bytes.Equal(hash1, hash2))

		// tmtypes.NewValidatorSet sorts the validators by address, so the hash is deterministic
		expectedValidatorSet := tmtypes.NewValidatorSet(tmValidators)
		expectedHash := expectedValidatorSet.Hash()
		assert.Equal(t, rollkittypes.Hash(expectedHash), hash1)
	})

	t.Run("proposer not in validator set", func(t *testing.T) {
		_, pk, err := crypto.GenerateEd25519Key(rand.Reader)
		require.NoError(t, err)

		// Create a different address for the proposer
		_, proposerKey, err := crypto.GenerateEd25519Key(rand.Reader)
		require.NoError(t, err)
		rawProposerKey, err := proposerKey.Raw()
		require.NoError(t, err)
		cometProposerKey := tmcryptoed25519.PubKey(rawProposerKey)
		proposerAddress := tmtypes.NewValidator(cometProposerKey, 1).Address.Bytes()

		_, err = ValidatorsHasher([]crypto.PubKey{pk}, proposerAddress)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "proposer with address")
	})

	t.Run("invalid public key size", func(t *testing.T) {
		badKey := &mockPubKey{
			rawBytes: []byte{1, 2, 3}, // Invalid size
			keyType:  pb.KeyType_Ed25519,
		}
		proposerAddress := []byte("doesn't matter")

		_, err := ValidatorsHasher([]crypto.PubKey{badKey}, proposerAddress)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match CometBFT Ed25519 PubKeySize")
	})
}
