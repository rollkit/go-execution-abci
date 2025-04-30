package provider

import (
	"context"
	"errors"

	"cosmossdk.io/log"
	"github.com/cometbft/cometbft/state/indexer"
	"github.com/cometbft/cometbft/state/txindex"
	cmtypes "github.com/cometbft/cometbft/types" // Keep for getBlockMeta

	"github.com/rollkit/go-execution-abci/pkg/adapter"
)

// ErrConsensusStateNotAvailable is returned because Rollkit doesn't use Tendermint consensus.
// Exported error.
var ErrConsensusStateNotAvailable = errors.New("consensus state not available in Rollkit") // Changed to exported

// RpcProvider implements the interfaces required by the JSON-RPC service,
// primarily by delegating calls to the underlying adapter and indexers.
type RpcProvider struct {
	adapter      *adapter.Adapter
	txIndexer    txindex.TxIndexer
	blockIndexer indexer.BlockIndexer
	logger       log.Logger
}

// NewRpcProvider creates a new instance of rpcProvider.
func NewRpcProvider(
	adapter *adapter.Adapter,
	txIndexer txindex.TxIndexer,
	blockIndexer indexer.BlockIndexer,
	logger log.Logger,
) *RpcProvider { // Corrected return type to local *RpcProvider
	return &RpcProvider{
		adapter:      adapter,
		txIndexer:    txIndexer,
		blockIndexer: blockIndexer,
		logger:       logger,
	}
}

// Logger returns the logger used by the RpcProvider.
func (p *RpcProvider) Logger() log.Logger {
	return p.logger
}

func (p *RpcProvider) normalizeHeight(height *int64) uint64 {
	var heightValue uint64
	if height == nil {
		var err error
		// TODO: Decide how to handle context here. Using background for now.
		heightValue, err = p.adapter.RollkitStore.Height(context.Background())
		if err != nil {
			// TODO: Consider logging or returning error
			p.logger.Error("Failed to get current height in normalizeHeight", "err", err)
			return 0
		}
	} else if *height < 0 {
		// Handle negative heights if they have special meaning (e.g., -1 for latest)
		// Currently, just treat them as 0 or latest, adjust as needed.
		// For now, let's assume negative height means latest valid height.
		var err error
		heightValue, err = p.adapter.RollkitStore.Height(context.Background())
		if err != nil {
			p.logger.Error("Failed to get current height for negative height in normalizeHeight", "err", err)
			return 0
		}
	} else {
		heightValue = uint64(*height)
	}

	return heightValue
}

func (p *RpcProvider) getBlockMeta(ctx context.Context, n uint64) *cmtypes.BlockMeta {
	header, data, err := p.adapter.RollkitStore.GetBlockData(ctx, n)
	if err != nil {
		p.logger.Error("Failed to get block data in getBlockMeta", "height", n, "err", err)
		return nil
	}
	if header == nil || data == nil {
		p.logger.Error("Nil header or data returned from GetBlockData", "height", n)
		return nil
	}
	// Assuming ToABCIBlockMeta is now in pkg/rpc/provider/provider_utils.go
	bmeta, err := ToABCIBlockMeta(header, data) // Removed rpc. prefix
	if err != nil {
		p.logger.Error("Failed to convert block to ABCI block meta", "height", n, "err", err)
		return nil
	}

	return bmeta
}
