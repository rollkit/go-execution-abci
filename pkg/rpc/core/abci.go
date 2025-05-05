package core

import (
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
		Data: data,
		Path: path,
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
	info, err := env.Adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	return &ctypes.ResultABCIInfo{
		Response: *info,
	}, nil
}
