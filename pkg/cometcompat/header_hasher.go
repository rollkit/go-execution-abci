package cometcompat

import (
	"github.com/libp2p/go-libp2p/core/crypto"

	rollkittypes "github.com/rollkit/rollkit/types"
)

func HeaderHasher(proposerKey crypto.PubKey, header *rollkittypes.Header) (rollkittypes.Hash, error) {
	abciHeader, err := ToABCIHeader(proposerKey, header)
	if err != nil {
		return nil, err
	}

	return rollkittypes.Hash(abciHeader.Hash()), nil
}
