package rollkitadapter

import (
	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/common"
)

func CreateCometBFTHeaderHasher() types.HeaderHasher {
	return func(header *types.Header) (types.Hash, error) {
		abciHeader, err := common.ToABCIHeader(header)
		if err != nil {
			return nil, err
		}

		return types.Hash(abciHeader.Hash()), nil
	}
}
