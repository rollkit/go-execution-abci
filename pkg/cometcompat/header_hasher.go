package cometcompat

import (
	rollkittypes "github.com/rollkit/rollkit/types"
)

// HeaderHasher defines the function signature for a component that can calculate
// the HeaderHash for a block header.
func HeaderHasher(header *rollkittypes.Header) (rollkittypes.Hash, error) {
	abciHeader, err := ToABCIHeader(header)
	if err != nil {
		return nil, err
	}

	return rollkittypes.Hash(abciHeader.Hash()), nil
}
