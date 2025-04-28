package rpc

import (
	cometrpc "github.com/cometbft/cometbft/rpc/client"
)

// RpcClient defines the interface needed by various RPC services.
// It aggregates multiple client interfaces from CometBFT.
type RpcClient interface {
	cometrpc.ABCIClient
	cometrpc.HistoryClient
	cometrpc.NetworkClient
	cometrpc.SignClient // Note: SignClient methods might not be fully applicable/implemented in Rollkit context
	cometrpc.StatusClient
	cometrpc.EventsClient
	cometrpc.EvidenceClient
	cometrpc.MempoolClient
}
