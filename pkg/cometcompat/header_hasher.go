package cometcompat

import (
	rollkittypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/common"
)

func HeaderHasher(header *rollkittypes.Header) (rollkittypes.Hash, error) {
	abciHeader, err := common.ToABCIHeader(header)
	if err != nil {
		return nil, err
	}

	return rollkittypes.Hash(abciHeader.Hash()), nil
}
