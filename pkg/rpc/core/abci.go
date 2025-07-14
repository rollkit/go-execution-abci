package core

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/bytes"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
)

// ABCIQuery queries the application for some information.
// More: https://docs.cometbft.com/v0.37/rpc/#/ABCI/abci_query
func ABCIQuery(
	ctx *rpctypes.Context,
	path string,
	data bytes.HexBytes,
	height int64,
	prove bool,
) (*ctypes.ResultABCIQuery, error) {
	resp, err := env.Adapter.App.Query(ctx.Context(), &abci.RequestQuery{
		Data:   data,
		Path:   path,
		Height: height,
		Prove:  prove,
	})
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}

// ABCIInfo gets some info about the application.
// More: https://docs.cometbft.com/v0.37/rpc/#/ABCI/abci_info
func ABCIInfo(ctx *rpctypes.Context) (*ctypes.ResultABCIInfo, error) {
	// Get current height from store
	currentHeight, err := env.Adapter.RollkitStore.Height(ctx.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get current height: %w", err)
	}

	// Find the last soft-confirmed height by iterating backwards
	var lastConfirmedHeight uint64
	for h := currentHeight; h > 0; h-- {
		isSoftConfirmed, _, err := checkSoftConfirmation(ctx.Context(), h)
		if err != nil {
			return nil, fmt.Errorf("failed to check soft confirmation for height %d: %w", h, err)
		}
		if isSoftConfirmed {
			lastConfirmedHeight = h
			break
		}
	}

	// If no confirmed height found (edge case), fallback to 0 or handle appropriately
	if lastConfirmedHeight == 0 {
		return nil, fmt.Errorf("no soft-confirmed height found")
	}

	// Now get the app info for this adjusted height
	infoReq := &abci.RequestInfo{}
	info, err := env.Adapter.App.Info(infoReq)
	if err != nil {
		return nil, err
	}

	// Override LastBlockHeight with the last confirmed one
	info.LastBlockHeight = int64(lastConfirmedHeight)

	// Optionally, fetch and set the correct AppHash for this height if needed
	// (Assuming AppHash is stored per height; adjust based on actual storage)
	header, _, err := env.Adapter.RollkitStore.GetBlockData(ctx.Context(), lastConfirmedHeight)
	if err != nil {
		return nil, fmt.Errorf("failed to get block data for confirmed height %d: %w", lastConfirmedHeight, err)
	}
	info.LastBlockAppHash = header.AppHash

	return &ctypes.ResultABCIInfo{
		Response: *info,
	}, nil
}
