package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"

	storepkg "github.com/rollkit/rollkit/pkg/store"
	rlktypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
)

// BlockSearch searches for a paginated set of blocks matching BeginBlock and
// EndBlock event search criteria.
func BlockSearch(
	ctx *rpctypes.Context,
	query string,
	pagePtr, perPagePtr *int,
	orderBy string,
) (*ctypes.ResultBlockSearch, error) {
	wrappedCtx := ctx.Context()

	q, err := cmquery.New(query)
	if err != nil {
		return nil, err
	}

	results, err := env.BlockIndexer.Search(wrappedCtx, q)
	if err != nil {
		return nil, err
	}

	// Sort the results
	switch orderBy {
	case "desc":
		sort.Slice(results, func(i, j int) bool {
			return results[i] > results[j]
		})

	case "asc", "":
		sort.Slice(results, func(i, j int) bool {
			return results[i] < results[j]
		})
	default:
		return nil, errors.New("expected order_by to be either `asc` or `desc` or empty")
	}

	// Paginate
	totalCount := len(results)
	perPageVal := validatePerPage(perPagePtr)

	pageVal, err := validatePage(pagePtr, perPageVal, totalCount)
	if err != nil {
		return nil, err
	}

	skipCount := validateSkipCount(pageVal, perPageVal)
	pageSize := min(perPageVal, totalCount-skipCount)

	blocks := make([]*ctypes.ResultBlock, 0, pageSize)
	for i := skipCount; i < skipCount+pageSize; i++ {
		header, data, err := env.Adapter.RollkitStore.GetBlockData(wrappedCtx, uint64(results[i]))
		if err != nil {
			return nil, err
		}

		lastCommit, err := getLastCommit(wrappedCtx, uint64(results[i]))
		if err != nil {
			return nil, fmt.Errorf("failed to get last commit for block %d: %w", results[i], err)
		}

		block, err := cometcompat.ToABCIBlock(header, data, lastCommit)
		if err != nil {
			return nil, err
		}

		blocks = append(blocks, &ctypes.ResultBlock{
			Block: block,
			BlockID: cmttypes.BlockID{
				Hash: block.Hash(),
			},
		})
	}

	return &ctypes.ResultBlockSearch{Blocks: blocks, TotalCount: totalCount}, nil
}

// Block gets block at a given height.
// If no height is provided, it will fetch the latest block.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block
func Block(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlock, error) {
	var (
		heightValue uint64
		err         error
	)

	switch {
	case heightPtr != nil && *heightPtr == -1:
		rawVal, err := env.Adapter.RollkitStore.GetMetadata(
			ctx.Context(),
			storepkg.DAIncludedHeightKey,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get DA included height: %w", err)
		}

		if len(rawVal) != 8 {
			return nil, fmt.Errorf("invalid finalized height data length: %d", len(rawVal))
		}

		heightValue = binary.LittleEndian.Uint64(rawVal)
	default:
		heightValue, err = normalizeHeight(ctx.Context(), heightPtr)
		if err != nil {
			return nil, err
		}
	}

	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), heightValue)
	if err != nil {
		return nil, err
	}

	lastCommit, err := getLastCommit(ctx.Context(), heightValue)
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for block %d: %w", heightValue, err)
	}

	// First apply ToABCIBlock to get the final header with all transformations
	abciBlock, err := cometcompat.ToABCIBlock(header, data, lastCommit)
	if err != nil {
		return nil, err
	}

	// Then re-sign the final ABCI header if we have a signer
	if env.Signer != nil {
		// Create a vote for the final ABCI header
		vote := cmtproto.Vote{
			Type:   cmtproto.PrecommitType,
			Height: int64(header.Height()), //nolint:gosec
			Round:  0,
			BlockID: cmtproto.BlockID{
				Hash:          abciBlock.Header.Hash(),
				PartSetHeader: cmtproto.PartSetHeader{},
			},
			Timestamp:        abciBlock.Time,
			ValidatorAddress: header.ProposerAddress,
			ValidatorIndex:   0,
		}
		chainID := header.ChainID()
		finalSignBytes := cmttypes.VoteSignBytes(chainID, &vote)

		newSignature, err := env.Signer.Sign(finalSignBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to sign final ABCI header: %w", err)
		}

		// Update the signature in the block
		if len(abciBlock.LastCommit.Signatures) > 0 {
			abciBlock.LastCommit.Signatures[0].Signature = newSignature
		}
	}

	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{Hash: abciBlock.Hash()},
		Block:   abciBlock,
	}, nil
}

// BlockByHash gets block by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block_by_hash
func BlockByHash(ctx *rpctypes.Context, hash []byte) (*ctypes.ResultBlock, error) {
	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}

	lastCommit, err := getLastCommit(ctx.Context(), header.Height())
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for block %d: %w", header.Height(), err)
	}

	abciBlock, err := cometcompat.ToABCIBlock(header, data, lastCommit)
	if err != nil {
		return nil, err
	}

	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{
			Hash: cmbytes.HexBytes(hash),
			PartSetHeader: cmttypes.PartSetHeader{
				Total: 0,
				Hash:  nil,
			},
		},
		Block: abciBlock,
	}, nil
}

// Commit gets block commit at a given height.
// If no height is provided, it will fetch the commit for the latest block.
// More: https://docs.cometbft.com/main/rpc/#/Info/commit
func Commit(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultCommit, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, err
	}

	header, rollkitData, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), height)
	if err != nil {
		return nil, err
	}

	// Create a proper commit that will be used for ToABCIBlock
	abciCommit := &cmttypes.Commit{
		Height: int64(header.Height()), //nolint:gosec
		Round:  0,
		BlockID: cmttypes.BlockID{
			Hash:          cmbytes.HexBytes(header.Hash()), // This will be updated after ToABCIBlock
			PartSetHeader: cmttypes.PartSetHeader{},
		},
		Signatures: []cmttypes.CommitSig{{
			BlockIDFlag:      cmttypes.BlockIDFlagCommit,
			Signature:        header.Signature, // This will be updated if we have a signer
			ValidatorAddress: header.ProposerAddress,
			Timestamp:        header.Time(),
		}},
	}

	// First apply ToABCIBlock to get the final header with all transformations
	abciBlock, err := cometcompat.ToABCIBlock(header, rollkitData, abciCommit)
	if err != nil {
		return nil, err
	}

	// Then re-sign the final ABCI header if we have a signer
	if env.Signer != nil {
		// Create a vote for the final ABCI header
		vote := cmtproto.Vote{
			Type:   cmtproto.PrecommitType,
			Height: int64(header.Height()), //nolint:gosec
			Round:  0,
			BlockID: cmtproto.BlockID{
				Hash:          abciBlock.Header.Hash(),
				PartSetHeader: cmtproto.PartSetHeader{},
			},
			Timestamp:        abciBlock.Time,
			ValidatorAddress: header.ProposerAddress,
			ValidatorIndex:   0,
		}
		chainID := header.ChainID()
		finalSignBytes := cmttypes.VoteSignBytes(chainID, &vote)

		newSignature, err := env.Signer.Sign(finalSignBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to sign final ABCI header: %w", err)
		}

		// Update the commit with the new signature
		abciBlock.LastCommit.Signatures[0].Signature = newSignature
	}

	// Update the commit's BlockID to match the final ABCI block hash
	abciBlock.LastCommit.BlockID.Hash = abciBlock.Header.Hash()

	return &ctypes.ResultCommit{
		SignedHeader: cmttypes.SignedHeader{
			Header: &abciBlock.Header,
			Commit: abciBlock.LastCommit,
		},
		CanonicalCommit: true,
	}, nil
}

// BlockResults gets block results at a given height.
// If no height is provided, it will fetch the results for the latest block.
func BlockResults(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlockResults, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, err
	}

	resp, err := env.Adapter.Store.GetBlockResponse(ctx.Context(), height)
	if err != nil {
		return nil, err
	}

	return &ctypes.ResultBlockResults{
		Height:                int64(height),
		TxsResults:            resp.TxResults,
		FinalizeBlockEvents:   resp.Events,
		ValidatorUpdates:      resp.ValidatorUpdates,
		ConsensusParamUpdates: resp.ConsensusParamUpdates,
		AppHash:               resp.AppHash,
	}, nil
}

// Header gets block header at a given height.
// If no height is provided, it will fetch the latest header.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header
func Header(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultHeader, error) {
	height, err := normalizeHeight(ctx.Context(), heightPtr)
	if err != nil {
		return nil, err
	}

	blockMeta := getBlockMeta(ctx.Context(), height)
	if blockMeta == nil {
		return nil, fmt.Errorf("block at height %d not found", height)
	}
	return &ctypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash gets header by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header_by_hash
func HeaderByHash(ctx *rpctypes.Context, hash cmbytes.HexBytes) (*ctypes.ResultHeader, error) {
	// N.B. The hash parameter is HexBytes so that the reflective parameter
	// decoding logic in the HTTP service will correctly translate from JSON.
	// See https://github.com/cometbft/cometbft/issues/6802 for context.

	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}

	lastCommit, err := getLastCommit(ctx.Context(), header.Height())
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for block %d: %w", header.Height(), err)
	}

	blockMeta, err := cometcompat.ToABCIBlockMeta(header, data, lastCommit)
	if err != nil {
		return nil, err
	}

	if blockMeta == nil {
		return &ctypes.ResultHeader{}, nil
	}

	return &ctypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// BlockchainInfo gets block headers for minHeight <= height <= maxHeight.
// Block headers are returned in descending order (highest first).
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/blockchain
func BlockchainInfo(ctx *rpctypes.Context, minHeight, maxHeight int64) (*ctypes.ResultBlockchainInfo, error) {
	const limit int64 = 20

	height, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, err
	}

	// Currently blocks are not pruned and are synced linearly so the base height is 0
	minHeight, maxHeight, err = filterMinMax(
		0,
		int64(height), //nolint:gosec
		minHeight,
		maxHeight,
		limit)
	if err != nil {
		return nil, err
	}
	env.Logger.Debug("BlockchainInfo", "maxHeight", maxHeight, "minHeight", minHeight)

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for _, block := range BlockIterator(ctx.Context(), maxHeight, minHeight) {
		if block.header != nil && block.data != nil {
			lastCommit, err := getLastCommit(ctx.Context(), block.header.Height())
			if err != nil {
				return nil, fmt.Errorf("failed to get last commit for block %d: %w", block.header.Height(), err)
			}

			cmblockmeta, err := cometcompat.ToABCIBlockMeta(block.header, block.data, lastCommit)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, cmblockmeta)
		}
	}

	return &ctypes.ResultBlockchainInfo{
		LastHeight: int64(height), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}
