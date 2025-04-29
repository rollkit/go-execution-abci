package provider

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
)

// ABCIInfo implements client.CometRPC.
func (p *RpcProvider) ABCIInfo(context.Context) (*coretypes.ResultABCIInfo, error) {
	info, err := p.adapter.App.Info(&abci.RequestInfo{})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIInfo{
		Response: *info,
	}, nil
}

// ABCIQuery implements client.CometRPC.
func (p *RpcProvider) ABCIQuery(ctx context.Context, path string, data cmtbytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	resp, err := p.adapter.App.Query(ctx, &abci.RequestQuery{
		Data: data,
		Path: path,
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}

// ABCIQueryWithOptions implements client.CometRPC.
func (p *RpcProvider) ABCIQueryWithOptions(ctx context.Context, path string, data cmtbytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	resp, err := p.adapter.App.Query(ctx, &abci.RequestQuery{
		Data:   data,
		Path:   path,
		Height: opts.Height,
		Prove:  opts.Prove,
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{
		Response: *resp,
	}, nil
}
