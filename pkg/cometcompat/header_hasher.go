package cometcompat

import (
	rollkittypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/common"
)

// init automatically sets the CometBFT header hasher as the default when this package is imported
func init() {
	rollkittypes.SetHeaderHasher(CreateCometBFTHeaderHasher())
}

func CreateCometBFTHeaderHasher() rollkittypes.HeaderHasher {
	return func(header *rollkittypes.Header) (rollkittypes.Hash, error) {
		abciHeader, err := common.ToABCIHeader(header)
		if err != nil {
			return nil, err
		}

		return rollkittypes.Hash(abciHeader.Hash()), nil
	}
}
