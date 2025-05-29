package rollkitadapter

import (
	"github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/rpc"
)

func CreateCometBFTCommitHasher() types.CommitHashProvider {
	return func(signature *types.Signature, header *types.Header, proposerAddress []byte) (types.Hash, error) {
		abciCommit := rpc.GetABCICommit(header.Height(), header.Hash(), proposerAddress, header.Time(), *signature)
		abciCommit.Signatures[0].ValidatorAddress = proposerAddress
		abciCommit.Signatures[0].Timestamp = header.Time()
		return types.Hash(abciCommit.Hash()), nil
	}
}
