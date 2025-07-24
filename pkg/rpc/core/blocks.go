package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
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

		lastCommit, err := env.Adapter.GetLastCommit(wrappedCtx, uint64(results[i]))
		if err != nil {
			return nil, fmt.Errorf("failed to get last commit for block %d: %w", results[i], err)
		}

		abciHeader, err := cometcompat.ToABCIHeader(header.Header, lastCommit)
		if err != nil {
			return nil, fmt.Errorf("failed to convert header to ABCI format: %w", err)
		}

		abciBlock, err := cometcompat.ToABCIBlock(abciHeader, lastCommit, data)
		if err != nil {
			return nil, err
		}

		blockParts, err := abciBlock.MakePartSet(cmttypes.BlockPartSizeBytes)
		if err != nil {
			return nil, fmt.Errorf("make part set: %w", err)
		}

		blocks = append(blocks, &ctypes.ResultBlock{
			Block: abciBlock,
			BlockID: cmttypes.BlockID{
				Hash:          abciHeader.Hash(),
				PartSetHeader: blockParts.Header(),
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

	blockMeta, block := getBlockMeta(ctx.Context(), heightValue)
	if blockMeta == nil {
		return &ctypes.ResultBlock{
			BlockID: cmttypes.BlockID{},
			Block:   block,
		}, nil
	}

	return &ctypes.ResultBlock{
		BlockID: blockMeta.BlockID,
		Block:   block,
	}, nil
}

// BlockByHash gets block by hash.
// More: https://docs.cometbft.com/v0.37/rpc/#/Info/block_by_hash
func BlockByHash(ctx *rpctypes.Context, hash []byte) (*ctypes.ResultBlock, error) {
	header, data, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}

	lastCommit, err := env.Adapter.GetLastCommit(ctx.Context(), header.Height())
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for block %d: %w", header.Height(), err)
	}

	abciHeader, err := cometcompat.ToABCIHeader(header.Header, lastCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to convert header to ABCI format: %w", err)
	}

	abciBlock, err := cometcompat.ToABCIBlock(abciHeader, lastCommit, data)
	if err != nil {
		return nil, err
	}

	blockParts, err := abciBlock.MakePartSet(cmttypes.BlockPartSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("make part set: %w", err)
	}

	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{
			Hash:          abciHeader.Hash(),
			PartSetHeader: blockParts.Header(),
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

	blockMeta, _ := getBlockMeta(ctx.Context(), height)
	if blockMeta == nil {
		return nil, nil
	}
	abciHeader := blockMeta.Header

	// get current commit
	commit, err := env.Adapter.GetLastCommit(ctx.Context(), height+1)
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for height %d: %w", height, err)
	}

	return ctypes.NewResultCommit(&abciHeader, commit, true), nil
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

	blockMeta, _ := getBlockMeta(ctx.Context(), height)
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

	header, _, err := env.Adapter.RollkitStore.GetBlockByHash(ctx.Context(), rlktypes.Hash(hash))
	if err != nil {
		return nil, err
	}

	lastCommit, err := env.Adapter.GetLastCommit(ctx.Context(), header.Height())
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for block %d: %w", header.Height(), err)
	}

	abciHeader, err := cometcompat.ToABCIHeader(header.Header, lastCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to convert header to ABCI format: %w", err)
	}

	return &ctypes.ResultHeader{Header: &abciHeader}, nil
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

	// Currently blocks are not pruned and are synced linearly so the base height is 0.
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

	blockMetas := []*cmttypes.BlockMeta{}
	for height := maxHeight; height >= minHeight; height-- {
		blockMeta, _ := getBlockMeta(ctx.Context(), uint64(height))
		blockMetas = append(blockMetas, blockMeta)
	}

	return &ctypes.ResultBlockchainInfo{
		LastHeight: int64(height), //nolint:gosec
		BlockMetas: blockMetas,
	}, nil
}
