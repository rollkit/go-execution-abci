package cometcompat

import (
	"github.com/rollkit/rollkit/types"
)

func CommitHasher(
	signature *types.Signature,
	header *types.Header,
	proposerAddress []byte,
) (types.Hash, error) {
	abciCommit := GetABCICommit(header.Height(), header.Hash(), proposerAddress, header.Time(), *signature)
	abciCommit.Signatures[0].ValidatorAddress = proposerAddress
	abciCommit.Signatures[0].Timestamp = header.Time()
	return types.Hash(abciCommit.Hash()), nil
}
