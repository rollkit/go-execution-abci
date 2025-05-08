package core

import (
	"errors"
	"fmt"
	"sort"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"
	cmtypes "github.com/cometbft/cometbft/types"
	rlktypes "github.com/rollkit/rollkit/types"
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

	results, err := env.TxIndexer.Search(wrappedCtx, q)
	if err != nil {
		return nil, err
	}

	// Sort the results
	switch orderBy {
	case "desc":
		sort.Slice(results, func(i, j int) bool {
			return results[i].Height > results[j].Height
		})

	case "asc", "":
		sort.Slice(results, func(i, j int) bool {
			return results[i].Height < results[j].Height
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
		header, data, err := env.Adapter.RollkitStore.GetBlockData(wrappedCtx, uint64(results[i].Height))
		if err != nil {
			return nil, err
		}
		block, err := ToABCIBlock(header, data)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, &ctypes.ResultBlock{
			Block: block,
			BlockID: cmtypes.BlockID{
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
	var heightValue uint64

	switch {
	// block tag = included
	case heightPtr != nil && *heightPtr == -1:
		return nil, errors.New("DA included height not implemented")
	default:
		heightValue = normalizeHeight(heightPtr)
	}
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), heightValue)
	if err != nil {
		return nil, err
	}
	if header == nil || data == nil { // Added nil check for safety
		return nil, fmt.Errorf("nil header or data for height %d from store", heightValue)
	}

	abciBlock, err := ToABCIBlock(header, data)
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{
			Hash: cmtbytes.HexBytes(header.Hash()), // header.Hash() is []byte, needs conversion
			PartSetHeader: cmttypes.PartSetHeader{
				Total: 0,
				Hash:  nil,
			},
		},
		Block: abciBlock,
	}, nil
}

// BlockByHash gets block by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block_by_hash
func BlockByHash(ctx *rpctypes.Context, hash []byte) (*ctypes.ResultBlock, error) {
	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}
	if header == nil || data == nil {
		return nil, fmt.Errorf("nil header or data for hash %X from store", hash)
	}

	abciBlock, err := ToABCIBlock(header, data)
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{
			Hash: cmtbytes.HexBytes(hash), // hash is []byte, needs conversion
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
	heightValue := normalizeHeight(heightPtr)
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), heightValue)
	if err != nil {
		return nil, err
	}
	if header == nil || data == nil { // Added nil check for safety
		return nil, fmt.Errorf("nil header or data for height %d from store", heightValue)
	}

	// we should have a single validator
	if len(header.ProposerAddress) == 0 {
		return nil, errors.New("empty proposer address found in block header")
	}

	commitSigs := []cmttypes.CommitSig{
		{
			BlockIDFlag:      cmttypes.BlockIDFlagCommit,
			ValidatorAddress: header.ProposerAddress,
			Timestamp:        header.Time(),
			Signature:        header.Signature,
		},
	}

	commit := &cmttypes.Commit{
		Height:     int64(heightValue),
		Round:      0, // Round information is not typically in rlktypes.Header, default to 0
		BlockID:    cmttypes.BlockID{Hash: cmtbytes.HexBytes(header.Hash())},
		Signatures: commitSigs,
	}

	abciBlockForCommit, err := ToABCIBlock(header, data)
	if err != nil {
		return nil, err
	}

	return ctypes.NewResultCommit(&abciBlockForCommit.Header, commit, true), nil
}

// BlockResults is not fully implemented as in FullClient because
// env.Adapter.RollkitStore (pkg/store.Store) does not provide GetBlockResponses method.
func BlockResults(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultBlockResults, error) {
	_ = normalizeHeight(heightPtr) // Use height to avoid unused variable error
	// To fully implement this like FullClient, env.Adapter.RollkitStore would need a method
	// equivalent to GetBlockResponses(ctx, height) -> (*rlktypes.BlockResponses, error)
	// and rlktypes.BlockResponses would need fields like TxResults, Events, etc.
	return &ctypes.ResultBlockResults{
		// Height: int64(normalizeHeight(heightPtr)) // Could set height, but other fields are empty.
	}, nil
}

// Header gets block header at a given height.
// If no height is provided, it will fetch the latest header.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header
func Header(ctx *rpctypes.Context, heightPtr *int64) (*ctypes.ResultHeader, error) {
	height := normalizeHeight(heightPtr)
	blockMeta := getBlockMeta(ctx.Context(), height)
	if blockMeta == nil {
		return nil, fmt.Errorf("block at height %d not found", height)
	}
	return &ctypes.ResultHeader{Header: &blockMeta.Header}, nil
}

// HeaderByHash gets header by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/header_by_hash
func HeaderByHash(ctx *rpctypes.Context, hash cmtbytes.HexBytes) (*ctypes.ResultHeader, error) {
	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}
	if header == nil || data == nil {
		return nil, fmt.Errorf("nil header or data for hash %X from store", hash)
	}

	blockMeta, err := ToABCIBlockMeta(header, data)
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
	const limit int64 = 20 // Default limit used in the original code

	height, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get current height: %w", err)
	}

	const baseHeight int64 = 0                // Kept change: baseHeight to 0
	minHeight, maxHeight, err = filterMinMax( // Use local function
		baseHeight,
		int64(height),
		minHeight,
		maxHeight,
		limit,
	)
	if err != nil {
		return nil, err
	}

	blocks := make([]*cmttypes.BlockMeta, 0, maxHeight-minHeight+1)
	for h := maxHeight; h >= minHeight; h-- {
		bMeta := getBlockMeta(ctx.Context(), uint64(h))
		if bMeta != nil {
			blocks = append(blocks, bMeta)
		}
	}

	finalHeight, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get final height: %w", err)
	}

	return &ctypes.ResultBlockchainInfo{
		LastHeight: int64(finalHeight), //nolint:gosec
		BlockMetas: blocks,
	}, nil
}
