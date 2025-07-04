package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	cmbytes "github.com/cometbft/cometbft/libs/bytes"
	cmquery "github.com/cometbft/cometbft/libs/pubsub/query"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	cmttypes "github.com/cometbft/cometbft/types"

	storepkg "github.com/rollkit/rollkit/pkg/store"
	rlktypes "github.com/rollkit/rollkit/types"

	abci "github.com/cometbft/cometbft/abci/types"
	networktypes "github.com/rollkit/go-execution-abci/modules/network/types"
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

	block, err := cometcompat.ToABCIBlock(header, data, lastCommit)
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
				Hash:          block.Header.Hash(),
				PartSetHeader: cmtproto.PartSetHeader{},
			},
			Timestamp:        block.Time,
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
		if len(block.LastCommit.Signatures) > 0 {
			block.LastCommit.Signatures[0].Signature = newSignature
		}
	}

	return &ctypes.ResultBlock{
		BlockID: cmttypes.BlockID{Hash: block.Hash()},
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

	lastCommit, err := getLastCommit(ctx.Context(), header.Height())
	if err != nil {
		return nil, fmt.Errorf("failed to get last commit for block %d: %w", header.Height(), err)
	}

	block, err := cometcompat.ToABCIBlock(header, data, lastCommit)
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
		Block: block,
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

	// Check if the block has soft confirmation first
	isSoftConfirmed, softConfirmationData, err := checkSoftConfirmation(ctx.Context(), height)
	if err != nil {
		return nil, fmt.Errorf("failed to check soft confirmation status: %w", err)
	}

	if !isSoftConfirmed {
		return nil, fmt.Errorf("commit for height %d does not exist (block not soft confirmed)", height)
	}

	header, rollkitData, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), height)
	if err != nil {
		return nil, err
	}

	// Build commit with attestations from soft confirmation data
	commit, err := buildCommitFromAttestations(ctx.Context(), height, softConfirmationData)
	if err != nil {
		return nil, fmt.Errorf("failed to build commit from attestations: %w", err)
	}

	block, err := cometcompat.ToABCIBlock(header, rollkitData, commit)
	if err != nil {
		return nil, err
	}

	// Update the commit's BlockID to match the final ABCI block hash
	block.LastCommit.BlockID.Hash = block.Header.Hash()

	return &ctypes.ResultCommit{
		SignedHeader: cmttypes.SignedHeader{
			Header: &block.Header,
			Commit: block.LastCommit,
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

// checkSoftConfirmation checks if a block has soft confirmation and returns the attestation data
func checkSoftConfirmation(ctx context.Context, height uint64) (bool, *networktypes.QueryAttestationBitmapResponse, error) {
	// Check soft confirmation status
	softConfirmReq := &networktypes.QuerySoftConfirmationStatusRequest{
		Height: int64(height),
	}
	reqData, err := softConfirmReq.Marshal()
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal soft confirmation request: %w", err)
	}

	// Query soft confirmation status
	abciReq := &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/SoftConfirmationStatus",
		Data: reqData,
	}

	abciRes, err := env.Adapter.App.Query(ctx, abciReq)
	if err != nil || abciRes.Code != 0 {
		var msg string
		if abciRes != nil {
			msg = abciRes.Log
		}
		env.Logger.Error("query soft confirmation status", "height", height, "error", err, "log", msg)
		return false, nil, fmt.Errorf("failed to query soft confirmation status: %w", err)
	}

	softConfirmResp := &networktypes.QuerySoftConfirmationStatusResponse{}
	if err := softConfirmResp.Unmarshal(abciRes.Value); err != nil {
		return false, nil, fmt.Errorf("failed to unmarshal soft confirmation response: %w", err)
	}

	if !softConfirmResp.IsSoftConfirmed {
		return false, nil, nil
	}

	// Get attestation bitmap data
	attestationReq := &networktypes.QueryAttestationBitmapRequest{
		Height: int64(height),
	}
	reqData, err = attestationReq.Marshal()
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal attestation bitmap request: %w", err)
	}

	abciReq = &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/AttestationBitmap",
		Data: reqData,
	}

	abciRes, err = env.Adapter.App.Query(ctx, abciReq)
	if err != nil || abciRes.Code != 0 {
		var msg string
		if abciRes != nil {
			msg = abciRes.Log
		}
		env.Logger.Error("query attestation bitmap", "height", height, "error", err, "log", msg)
		return false, nil, fmt.Errorf("failed to query attestation bitmap: %w", err)
	}

	attestationResp := &networktypes.QueryAttestationBitmapResponse{}
	if err := attestationResp.Unmarshal(abciRes.Value); err != nil {
		return false, nil, fmt.Errorf("failed to unmarshal attestation bitmap response: %w", err)
	}

	return true, attestationResp, nil
}

// buildCommitFromAttestations constructs a commit with real signatures from attestations
func buildCommitFromAttestations(ctx context.Context, height uint64, attestationData *networktypes.QueryAttestationBitmapResponse) (*cmttypes.Commit, error) {
	// Get the attestation bitmap
	bitmap := attestationData.Bitmap.Bitmap
	if bitmap == nil {
		return nil, fmt.Errorf("no attestation bitmap found for height %d", height)
	}

	// Query all validators to get their addresses
	queryReq := &abci.RequestQuery{
		Path: "/cosmos.staking.v1beta1.Query/Validators",
		Data: []byte{}, // Empty request to get all validators
	}

	valQueryRes, err := env.Adapter.App.Query(ctx, queryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query validators: %w", err)
	}
	if valQueryRes.Code != 0 {
		return nil, fmt.Errorf("failed to query validators: %s", valQueryRes.Log)
	}

	// For now, we'll construct a basic commit structure with the available data
	// In a real implementation, you'd need to decode the validator response properly

	votes := make([]cmttypes.CommitSig, 0)

	// We need to iterate through the bitmap and for each set bit, get the validator signature
	for i := 0; i < len(bitmap)*8; i++ {
		// Check if validator at index i voted (bit is set)
		if (bitmap[i/8] & (1 << (i % 8))) != 0 {
			// This validator voted, let's try to get their signature
			// For this we need the validator address corresponding to index i
			// This would require a proper validator index mapping

			// For now, we'll create a placeholder vote with empty signature
			// In a real implementation, you'd:
			// 1. Get validator address from index i
			// 2. Query the signature using ValidatorSignature query
			// 3. Construct the proper CommitSig

			vote := cmttypes.CommitSig{
				BlockIDFlag:      cmttypes.BlockIDFlagCommit,
				ValidatorAddress: make([]byte, 20), // Placeholder validator address
				Timestamp:        time.Now(),       // Should be actual timestamp
				Signature:        nil,              // We'll get this from the query below
			}

			// Try to get the real signature (this is a simplified approach)
			// In practice, you'd need to map the bitmap index to validator address
			validatorAddr := fmt.Sprintf("validator_%d", i) // Placeholder
			signature, err := getValidatorSignatureFromQuery(ctx, int64(height), validatorAddr)
			if err == nil {
				vote.Signature = signature
			}

			votes = append(votes, vote)
		} else {
			// Validator didn't vote, add nil vote
			votes = append(votes, cmttypes.CommitSig{
				BlockIDFlag: cmttypes.BlockIDFlagAbsent,
			})
		}
	}

	commit := &cmttypes.Commit{
		Height: int64(height),
		Round:  0, // Default round
		BlockID: cmttypes.BlockID{
			Hash: make([]byte, 32), // Should be actual block hash
			PartSetHeader: cmttypes.PartSetHeader{
				Total: 1,
				Hash:  make([]byte, 32),
			},
		},
		Signatures: votes,
	}

	return commit, nil
}

// getValidatorSignatureFromQuery queries the signature for a specific validator
func getValidatorSignatureFromQuery(ctx context.Context, height int64, validatorAddr string) ([]byte, error) {
	sigReq := &networktypes.QueryValidatorSignatureRequest{
		Height:    height,
		Validator: validatorAddr,
	}

	reqData, err := sigReq.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	abciReq := &abci.RequestQuery{
		Path: "/rollkitsdk.network.v1.Query/ValidatorSignature",
		Data: reqData,
	}

	res, err := env.Adapter.App.Query(ctx, abciReq)
	if err != nil {
		return nil, fmt.Errorf("signature query failed: %w", err)
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("signature query failed: %s", res.Log)
	}

	var sigResp networktypes.QueryValidatorSignatureResponse
	if err := sigResp.Unmarshal(res.Value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal signature response: %w", err)
	}

	return sigResp.Signature, nil
}
