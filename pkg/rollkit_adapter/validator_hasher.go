package rollkitadapter

import (
	"bytes"
	stdsha256 "crypto/sha256"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/log"
	tmcryptoed25519 "github.com/cometbft/cometbft/crypto/ed25519"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/libp2p/go-libp2p/core/crypto"
	rollkittypes "github.com/rollkit/rollkit/types"
)

// CreateCometBFTValidatorHasher returns a function that calculates the ValidatorHash
// compatible with CometBFT. This function is intended to be injected into Rollkit's Manager.
func CreateCometBFTValidatorHasher(logger log.Logger) rollkittypes.ValidatorHasher {
	return func(proposerAddress []byte, pubKey crypto.PubKey) (rollkittypes.Hash, error) {
		var calculatedHash rollkittypes.Hash

		var cometBftPubKey tmcryptoed25519.PubKey
		if pubKey.Type() == crypto.Ed25519 {
			rawKey, err := pubKey.Raw()
			if err != nil {
				logger.Error("ValidatorHasher: failed to get raw bytes from libp2p public key", "error", err)
				return calculatedHash, fmt.Errorf("ValidatorHasher: failed to get raw bytes from libp2p public key: %w", err)
			}
			if len(rawKey) != tmcryptoed25519.PubKeySize {
				errMsg := fmt.Sprintf("ValidatorHasher: libp2p public key size (%d) does not match CometBFT Ed25519 PubKeySize (%d)", len(rawKey), tmcryptoed25519.PubKeySize)
				logger.Error(errMsg)
				return calculatedHash, fmt.Errorf("%s", errMsg)
			}
			cometBftPubKey = rawKey
		} else {
			errMsg := fmt.Sprintf("ValidatorHasher: unsupported public key type '%s', expected Ed25519 for CometBFT compatibility", pubKey.Type())
			logger.Error(errMsg)
			return calculatedHash, fmt.Errorf("%s", errMsg)
		}

		votingPower := int64(1)
		sequencerValidator := tmtypes.NewValidator(cometBftPubKey, votingPower)

		derivedAddress := sequencerValidator.Address.Bytes()
		if !bytes.Equal(derivedAddress, proposerAddress) {
			errMsg := fmt.Sprintf("ValidatorHasher: CRITICAL MISMATCH - derived validator address (%s) does not match expected proposer address (%s). PubKey used for derivation: %s",
				hex.EncodeToString(derivedAddress),
				hex.EncodeToString(proposerAddress),
				hex.EncodeToString(cometBftPubKey.Bytes()))
			logger.Error(errMsg)
			return calculatedHash, fmt.Errorf("%s", errMsg)
		}

		sequencerValidatorSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{sequencerValidator})

		hashSumBytes := sequencerValidatorSet.Hash()

		calculatedHash = make(rollkittypes.Hash, stdsha256.Size)
		copy(calculatedHash, hashSumBytes)

		logger.Debug("ValidatorHasher: Hashing process completed successfully", "calculatedHash", hex.EncodeToString(calculatedHash))
		return calculatedHash, nil
	}
}
