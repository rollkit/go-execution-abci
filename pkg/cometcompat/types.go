package cometcompat

import (
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/rollkit/rollkit/types"
)

// ValidatorHasher defines the function signature for a component that can calculate
// the ValidatorHash for a block header.
// It takes the proposer's address and public key (as known by Rollkit's signer)
// and is expected to return a types.Hash compatible with the target consensus system (e.g., CometBFT).
// If the hasher is not configured or an error occurs, it should be handled appropriately
// by the caller (e.g., using a zero hash or returning an error).
type ValidatorHasher func(proposerAddress []byte, pubKey crypto.PubKey) (types.Hash, error)

// HeaderHasher defines the function signature for a component that can calculate
// the HeaderHash for a block header.
// It takes the header and is expected to return a types.Hash compatible with the target consensus system (e.g., CometBFT).
// If the hasher is not configured or an error occurs, it should be handled appropriately
// by the caller (e.g., using a zero hash or returning an error).
type HeaderHasher func(header *types.Header) (types.Hash, error)
