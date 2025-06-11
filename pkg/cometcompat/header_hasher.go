package cometcompat

import (
	rollkittypes "github.com/rollkit/rollkit/types"
)

func ProvideHeaderHasher() HeaderHasher {
	return func(header *rollkittypes.Header) (rollkittypes.Hash, error) {
		abciHeader, err := ToABCIHeader(header)
		if err != nil {
			return nil, err
		}

		return rollkittypes.Hash(abciHeader.Hash()), nil
	}
}
