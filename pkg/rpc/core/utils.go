package core

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/cometbft/cometbft/libs/bytes"
	cmttypes "github.com/cometbft/cometbft/types"
	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"

	rlktypes "github.com/rollkit/rollkit/types"

	"github.com/rollkit/go-execution-abci/pkg/cometcompat"
)

const NodeIDByteLength = 20

func normalizeHeight(height *int64) uint64 {
	var heightValue uint64
	if height == nil {
		var err error
		// TODO: Decide how to handle context here. Using background for now.
		heightValue, err = env.Adapter.RollkitStore.Height(context.Background())
		if err != nil {
			// TODO: Consider logging or returning error
			env.Logger.Error("Failed to get current height in normalizeHeight", "err", err)
			return 0
		}
	} else if *height < 0 {
		// Handle negative heights if they have special meaning (e.g., -1 for latest)
		// Currently, just treat them as 0 or latest, adjust as needed.
		// For now, let's assume negative height means latest valid height.
		var err error
		heightValue, err = env.Adapter.RollkitStore.Height(context.Background())
		if err != nil {
			env.Logger.Error("Failed to get current height for negative height in normalizeHeight", "err", err)
			return 0
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue
}

func getLastCommit(ctx context.Context, blockHeight uint64) (*cmttypes.Commit, error) {
	if blockHeight > 1 {
		header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx, blockHeight-1)
		if err != nil {
			return nil, fmt.Errorf("failed to get previous block data: %w", err)
		}

		commitForPrevBlock := &cmttypes.Commit{
			Height:  int64(header.Height()),
			Round:   0,
			BlockID: cmttypes.BlockID{Hash: bytes.HexBytes(header.Hash()), PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: bytes.HexBytes(data.Hash())}},
			Signatures: []cmttypes.CommitSig{
				{
					BlockIDFlag:      cmttypes.BlockIDFlagCommit,
					ValidatorAddress: cmttypes.Address(header.ProposerAddress),
					Timestamp:        header.Time(),
					Signature:        header.Signature,
				},
			},
		}

		return commitForPrevBlock, nil
	}

	return &cmttypes.Commit{
		Height:     int64(blockHeight),
		Round:      0,
		BlockID:    cmttypes.BlockID{},
		Signatures: []cmttypes.CommitSig{},
	}, nil
}

func getBlockMeta(ctx context.Context, n uint64) *cmttypes.BlockMeta {
	header, data, err := env.Adapter.RollkitStore.GetBlockData(ctx, n)
	if err != nil {
		env.Logger.Error("Failed to get block data in getBlockMeta", "height", n, "err", err)
		return nil
	}

	if header == nil || data == nil {
		env.Logger.Error("Nil header or data returned from GetBlockData", "height", n)
		return nil
	}

	// Create empty commit for ToABCIBlockMeta call
	emptyCommit := &cmttypes.Commit{
		Height:     int64(header.Height()),
		Round:      0,
		BlockID:    cmttypes.BlockID{},
		Signatures: []cmttypes.CommitSig{},
	}

	// Assuming ToABCIBlockMeta is now in pkg/rpc/provider/provider_utils.go
	bmeta, err := cometcompat.ToABCIBlockMeta(header, data, emptyCommit) // Removed rpc. prefix
	if err != nil {
		env.Logger.Error("Failed to convert block to ABCI block meta", "height", n, "err", err)
		return nil
	}

	return bmeta
}

func filterMinMax(base, height, mini, maxi, limit int64) (int64, int64, error) {
	// filter negatives
	if mini < 0 || maxi < 0 {
		return mini, maxi, errors.New("height must be greater than zero")
	}

	// adjust for default values
	if mini == 0 {
		mini = 1
	}
	if maxi == 0 {
		maxi = height
	}

	// limit max to the height
	maxi = min(height, maxi)

	// limit min to the base
	mini = max(base, mini)

	// limit min to within `limit` of max
	// so the total number of blocks returned will be `limit`
	mini = max(mini, maxi-limit+1)

	if mini > maxi {
		return mini, maxi, fmt.Errorf("%w: min height %d can't be greater than max height %d",
			errors.New("invalid request"), mini, maxi)
	}
	return mini, maxi, nil
}

// TruncateNodeID from rollkit we receive a 32 bytes node id, but we only need the first 20 bytes
// to be compatible with the ABCI node info
func TruncateNodeID(idStr string) (string, error) {
	idBytes, err := hex.DecodeString(idStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode node ID: %w", err)
	}
	if len(idBytes) < NodeIDByteLength {
		return "", fmt.Errorf("node ID too short, expected at least %d bytes, got %d", NodeIDByteLength, len(idBytes))
	}
	return hex.EncodeToString(idBytes[:NodeIDByteLength]), nil
}

func getHeightFromEntry(field string, value []byte) (uint64, error) {
	switch field {
	case "data":
		data := new(rlktypes.Data)
		if err := data.UnmarshalBinary(value); err != nil {
			return 0, err
		}
		return data.Height(), nil
	case "header":
		header := new(rlktypes.SignedHeader)
		if err := header.UnmarshalBinary(value); err != nil {
			return 0, err
		}
		return header.Height(), nil
	}
	return 0, fmt.Errorf("unknown field: %s", field)
}

type blockFilter struct { // needs this for the Filter interface
	max   int64
	min   int64
	field string // need this field for differentiation between getting headers and getting data
}

func (f *blockFilter) Filter(e dsq.Entry) bool {
	height, err := getHeightFromEntry(f.field, e.Value)
	if err != nil {
		return false
	}
	return height >= uint64(f.min) && height <= uint64(f.max)
}

// BlockIterator returns a slice of BlockResponse objects containing paired headers and data
// for blocks within the specified height range. It efficiently retrieves blocks by performing
// only two datastore queries (one for headers, one for data) rather than querying each block
// individually.
// Returns a slice of BlockResponse objects sorted by height in descending order.
func BlockIterator(ctx context.Context, max int64, min int64) []BlockResponse {
	var blocks []BlockResponse
	ds, ok := env.Adapter.RollkitStore.(ds.Batching)
	if !ok {
		return blocks
	}
	filterData := &blockFilter{max: max, min: min, field: "data"}
	filterHeader := &blockFilter{max: max, min: min, field: "header"}

	// we need to do two queries, one for the block header and one for the block data
	qHeader := dsq.Query{
		Prefix: "h",
	}
	qHeader.Filters = append(qHeader.Filters, filterHeader)

	qData := dsq.Query{
		Prefix: "d",
	}
	qData.Filters = append(qData.Filters, filterData)

	rHeader, err := ds.Query(ctx, qHeader)
	if err != nil {
		return blocks
	}
	rData, err := ds.Query(ctx, qData)
	if err != nil {
		return blocks
	}
	defer rHeader.Close() //nolint:errcheck
	defer rData.Close()   //nolint:errcheck

	// we need to match the data to the header using the height, for that we use a map
	headerMap := make(map[uint64]*rlktypes.SignedHeader)
	for res := range rHeader.Next() {
		if res.Error != nil {
			continue
		}
		header := new(rlktypes.SignedHeader)
		if err := header.UnmarshalBinary(res.Value); err != nil {
			continue
		}
		headerMap[header.Height()] = header
	}

	dataMap := make(map[uint64]*rlktypes.Data)
	for res := range rData.Next() {
		if res.Error != nil {
			continue
		}
		data := new(rlktypes.Data)
		if err := data.UnmarshalBinary(res.Value); err != nil {
			continue
		}
		dataMap[data.Height()] = data
	}

	// maps the headers to the data
	for height, header := range headerMap {
		if data, ok := dataMap[height]; ok {
			blocks = append(blocks, BlockResponse{header: header, data: data})
		}
	}

	// sort blocks by height descending
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].header.Height() > blocks[j].header.Height()
	})

	return blocks
}

// BlockResponse represents a paired block header and data for efficient iteration.
// It's returned by BlockIterator to provide access to both components of a block.
type BlockResponse struct {
	header *rlktypes.SignedHeader
	data   *rlktypes.Data
}

// returns the block's signed header.
func (br BlockResponse) Header() *rlktypes.SignedHeader {
	return br.header
}

// returns the block's data.
func (br BlockResponse) Data() *rlktypes.Data {
	return br.data
}
